package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/schedule"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

type daemonRLMHost struct {
	mu       sync.Mutex
	root     *daemon.Session
	agent    *agent.Agent
	services *tools.Services
	history  []llm.Message
	handle   *rlm.ContextHandle
	children map[string]*rlmChild
	pricing  [3]float64
}

type rlmChild struct {
	mu           sync.Mutex
	agent        *agent.Agent
	cancel       context.CancelFunc
	done         chan struct{}
	agentID      string
	capabilities []string
	status       agent.TaskStatus
	output       string
	err          error
}

func (child *rlmChild) settle(output string, status agent.TaskStatus, err error) {
	child.mu.Lock()
	child.output, child.status, child.err = output, status, err
	child.mu.Unlock()
}

func (child *rlmChild) snapshot() (string, agent.TaskStatus, error) {
	child.mu.Lock()
	defer child.mu.Unlock()
	return child.output, child.status, child.err
}

func newDaemonRLMHost(value *agent.Agent, history []llm.Message) *daemonRLMHost {
	return &daemonRLMHost{agent: value, services: value.Services, history: append([]llm.Message(nil), history...), children: make(map[string]*rlmChild)}
}

func (host *daemonRLMHost) SetPricing(input, output, cacheRead float64) {
	host.pricing = [3]float64{input, output, cacheRead}
}

func (host *daemonRLMHost) Bind(root *daemon.Session) error {
	if root == nil {
		return errors.New("RLM host requires a daemon root")
	}
	host.mu.Lock()
	host.root = root
	host.mu.Unlock()
	if err := root.ConfigureModelPricing(host.pricing[0], host.pricing[1], host.pricing[2]); err != nil {
		return err
	}
	host.agent.SetModelCallBudget(root)
	host.agent.TransformInput = host.focusInput
	return nil
}

func (host *daemonRLMHost) Close() {
	host.mu.Lock()
	children := make([]*rlmChild, 0, len(host.children))
	for _, child := range host.children {
		children = append(children, child)
	}
	host.mu.Unlock()
	for _, child := range children {
		child.cancel()
	}
}

type daemonRLMRuntime struct {
	host   *daemonRLMHost
	kernel *rlm.Kernel
}

func (runtime daemonRLMRuntime) Close() {
	runtime.host.Close()
	runtime.kernel.Close()
}

func (host *daemonRLMHost) bound() (*daemon.Session, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.root == nil {
		return nil, errors.New("RLM host is not bound")
	}
	return host.root, nil
}

func (host *daemonRLMHost) focusInput(ctx context.Context, input string) (string, error) {
	root, err := host.bound()
	if err != nil {
		return "", err
	}
	host.mu.Lock()
	handle := host.handle
	host.mu.Unlock()
	if handle == nil {
		data, err := rlm.MarshalHistory(host.history)
		if err != nil {
			return "", err
		}
		value, err := root.StoreContent(ctx, root.AgentID(), session.RuntimePayload{Data: data, MediaType: "application/json", Source: "full conversation history"})
		if err != nil {
			return "", err
		}
		handle = &rlm.ContextHandle{ReferenceID: value.ReferenceID, Size: value.Size, Source: value.Source}
		host.mu.Lock()
		host.handle = handle
		host.history = nil
		host.mu.Unlock()
		host.agent.SetSystemPrompt(rlm.BuildPrompt(root.WorkingDirectory(), handle))
	}
	if len(input) <= session.InlineValueLimit {
		return input, nil
	}
	value, err := root.StoreContent(ctx, root.AgentID(), session.RuntimePayload{Data: []byte(input), MediaType: "text/plain", Source: "root user input"})
	if err != nil {
		return "", err
	}
	const head, tail = 4 << 10, 2 << 10
	prefix := utf8Prefix(input, head)
	suffix, suffixStart := utf8Suffix(input, tail)
	return fmt.Sprintf("%s\n\n[Input continues in context handle %s; size=%d; shown spans=0:%d,%d:%d. Use context.search/read for bounded access.]\n\n%s",
		prefix, value.ReferenceID, value.Size, len(prefix), suffixStart, len(input), suffix), nil
}

func (host *daemonRLMHost) Call(ctx context.Context, module, operation string, arguments map[string]any) (any, error) {
	root, err := host.bound()
	if err != nil {
		return nil, err
	}
	switch module {
	case "context":
		return host.context(ctx, root, operation, arguments)
	case "files":
		return host.files(ctx, operation, arguments)
	case "shell":
		return host.shell(ctx, root, operation, arguments)
	case "models":
		return host.models(ctx, root, operation, arguments)
	case "agents":
		return host.agents(ctx, root, operation, arguments)
	case "messages":
		return host.messages(ctx, root, operation, arguments)
	case "state":
		return host.state(ctx, root, operation, arguments)
	case "artifacts":
		return host.artifacts(ctx, root, operation, arguments)
	case "schedules":
		return host.schedules(ctx, root, operation, arguments)
	case "permissions":
		switch operation {
		case "request":
			return map[string]any{"status": "invoke_operation", "message": "permission is admitted only with the exact operation digest; invoke the operation and present its pending ID to a paired human"}, nil
		case "status":
			id, _ := stringArg(arguments, "id")
			return root.InspectPermission(ctx, id)
		default:
			return nil, fmt.Errorf("unknown permissions operation %q", operation)
		}
	case "answer":
		if operation != "submit" {
			return nil, fmt.Errorf("unknown answer operation %q", operation)
		}
		return map[string]any{"accepted": true, "answer": arguments["text"], "citations": arguments["citations"]}, nil
	default:
		return nil, fmt.Errorf("unknown RLM module %q", module)
	}
}

func (host *daemonRLMHost) context(ctx context.Context, root *daemon.Session, operation string, arguments map[string]any) (any, error) {
	reference, _ := stringArg(arguments, "handle")
	if reference == "" {
		host.mu.Lock()
		if host.handle != nil {
			reference = host.handle.ReferenceID
		}
		host.mu.Unlock()
	}
	if reference == "" {
		return nil, errors.New("context handle is required")
	}
	switch operation {
	case "inspect":
		_, metadata, err := root.ReadContent(ctx, root.AgentID(), reference, 0, 0)
		return metadataMap(metadata), err
	case "read":
		offset := int64Arg(arguments, "offset", 0)
		length := min(intArg(arguments, "length", session.InlineValueLimit), session.InlineValueLimit)
		body, metadata, err := root.ReadContent(ctx, root.AgentID(), reference, offset, length)
		if err != nil {
			return nil, err
		}
		return map[string]any{"text": string(body), "source": metadata.Source, "handle": reference, "span": map[string]any{"start": offset, "end": offset + int64(len(body))}, "size": metadata.Size}, nil
	case "search":
		query, _ := stringArg(arguments, "query")
		if query == "" {
			return nil, errors.New("query is required")
		}
		return searchContent(ctx, root, reference, query)
	default:
		return nil, fmt.Errorf("unknown context operation %q", operation)
	}
}

func searchContent(ctx context.Context, root *daemon.Session, reference, query string) (any, error) {
	const maxScan = 8 << 20
	var offset int64
	var matches []map[string]any
	var metadata session.ContentMetadata
	for offset < maxScan && len(matches) < 20 {
		body, current, err := root.ReadContent(ctx, root.AgentID(), reference, offset, session.MaxContentRead)
		if err != nil {
			return nil, err
		}
		metadata = current
		text := string(body)
		for cursor := 0; len(matches) < 20; {
			index := strings.Index(text[cursor:], query)
			if index < 0 {
				break
			}
			start := cursor + index
			end := start + len(query)
			snippetStart, snippetEnd := max(0, start-80), min(len(text), end+80)
			matches = append(matches, map[string]any{"handle": reference, "source": metadata.Source, "span": map[string]any{"start": offset + int64(start), "end": offset + int64(end)}, "text": text[snippetStart:snippetEnd]})
			cursor = end
		}
		offset += int64(len(body))
		if len(body) == 0 || offset >= metadata.Size {
			break
		}
	}
	return map[string]any{"matches": matches, "scanned": offset, "size": metadata.Size, "truncated": offset < metadata.Size}, nil
}

func (host *daemonRLMHost) files(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	classic := operation
	switch operation {
	case "read", "write":
	case "patch":
		classic = "edit"
		if value, ok := arguments["old"]; ok {
			arguments["old_string"] = value
		}
		if value, ok := arguments["new"]; ok {
			arguments["new_string"] = value
		}
	case "list":
		path, _ := stringArg(arguments, "path")
		if path == "" {
			path = "."
		}
		arguments = map[string]any{"path": path, "_rlm_mode": "list"}
		classic = "read"
	case "search":
		query, _ := stringArg(arguments, "query")
		path, _ := stringArg(arguments, "path")
		if query == "" {
			return nil, errors.New("query is required")
		}
		if path == "" {
			path = "."
		}
		arguments = map[string]any{"path": path, "query": query, "_rlm_mode": "search"}
		classic = "read"
	default:
		return nil, fmt.Errorf("unknown files operation %q", operation)
	}
	return host.invoke(ctx, classic, arguments)
}

func (host *daemonRLMHost) shell(ctx context.Context, root *daemon.Session, operation string, arguments map[string]any) (any, error) {
	switch operation {
	case "run":
		return host.invoke(ctx, "bash", arguments)
	case "read":
		return host.context(ctx, root, "read", arguments)
	default:
		return nil, fmt.Errorf("unknown shell operation %q", operation)
	}
}

func (host *daemonRLMHost) invoke(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	data, err := json.Marshal(arguments)
	if err != nil {
		return nil, err
	}
	output, err := host.services.Invoke(ctx, operation, data)
	if err != nil {
		return map[string]any{"output": output}, err
	}
	root, bindErr := host.bound()
	if bindErr != nil {
		return nil, bindErr
	}
	return host.boundedText(ctx, root, operation+" output", output)
}

func (host *daemonRLMHost) models(ctx context.Context, root *daemon.Session, operation string, arguments map[string]any) (any, error) {
	call := func(prompt string, maxTokens int) map[string]any {
		if maxTokens <= 0 {
			maxTokens = host.agent.MaxTokens
		}
		request := llm.Request{Model: host.agent.Model, Messages: []llm.Message{{Role: "user", Content: prompt}}, MaxTokens: maxTokens}
		estimate := int64(agent.EstimateTokens(request.Messages) + max(maxTokens, 1))
		settle, err := root.ReserveModelCall(ctx, estimate)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		output, usage, err := host.agent.Client.Complete(ctx, request)
		if settleErr := settle(usage); err == nil {
			err = settleErr
		}
		host.agent.AddUsage(usage)
		bounded, boundErr := host.boundedText(ctx, root, "stateless model output", output)
		if bounded == nil {
			bounded = make(map[string]any)
		}
		result := bounded
		result["usage"] = usage
		if err == nil {
			err = boundErr
		}
		if err != nil {
			result["error"] = err.Error()
		}
		return result
	}
	switch operation {
	case "call":
		prompt, _ := stringArg(arguments, "prompt")
		if prompt == "" {
			return nil, errors.New("prompt is required")
		}
		return call(prompt, intArg(arguments, "max_tokens", 0)), nil
	case "batch":
		items, ok := arguments["prompts"].([]any)
		if !ok || len(items) == 0 {
			return nil, errors.New("prompts must be a non-empty list")
		}
		results := make([]map[string]any, len(items))
		for index, item := range items {
			prompt, ok := item.(string)
			if !ok {
				results[index] = map[string]any{"error": "prompt is not a string"}
				continue
			}
			results[index] = call(prompt, intArg(arguments, "max_tokens", 0))
		}
		return results, nil
	default:
		return nil, fmt.Errorf("unknown models operation %q", operation)
	}
}

func (host *daemonRLMHost) agents(ctx context.Context, root *daemon.Session, operation string, arguments map[string]any) (any, error) {
	switch operation {
	case "spawn":
		prompt, _ := stringArg(arguments, "prompt")
		if prompt == "" {
			return nil, errors.New("prompt is required")
		}
		id, _ := stringArg(arguments, "id")
		if id == "" {
			id = "rlm-" + randomSuffix()
		}
		client := *host.agent.Client
		child := agent.NewWithServices(&client, host.agent.Model, host.agent.MaxTokens, systemPrompt(root.WorkingDirectory(), time.Now()), host.services)
		child.ModelName, child.Provider = host.agent.ModelName, host.agent.Provider
		child.ContextLimit, child.Effort = host.agent.ContextLimit, host.agent.Effort
		capabilities, err := childCapabilities(arguments["capabilities"])
		if err != nil {
			return nil, err
		}
		budgets, err := childBudgets(arguments["budgets"])
		if err != nil {
			return nil, err
		}
		if err := root.AdmitRLMSubagent(ctx, id, child, capabilities, budgets); err != nil {
			return nil, err
		}
		if err := root.StartSubagent(ctx, id); err != nil {
			root.StopSubagent(id)
			root.ReleaseSubagent(id)
			child.Close()
			return nil, err
		}
		childCtx, cancel := context.WithCancel(context.Background())
		started := time.Now()
		record := &rlmChild{
			agent: child, cancel: cancel, done: make(chan struct{}), agentID: root.AgentID() + ":" + id,
			capabilities: append([]string(nil), capabilities...), status: agent.TaskRunning,
		}
		host.mu.Lock()
		host.children[id] = record
		host.mu.Unlock()
		if !root.LaunchRuntimeWorker("rlm child "+id, func() {
			defer close(record.done)
			output, turnErr := child.Turn(childCtx, prompt, agent.Events{})
			status := agent.TaskDone
			if errors.Is(childCtx.Err(), context.Canceled) {
				status = agent.TaskCancelled
			} else if turnErr != nil {
				status = agent.TaskError
			}
			finishErr := root.FinishRLMSubagent(context.Background(), id, prompt, output, status, started, child.MessagesSnapshot(), child.ModelName, child.Provider)
			if finishErr != nil {
				status = agent.TaskError
				root.StopSubagent(id)
			}
			record.settle(output, status, errors.Join(turnErr, finishErr))
			child.Close()
			root.ReleaseSubagent(id)
		}) {
			cancel()
			root.StopSubagent(id)
			child.Close()
			root.ReleaseSubagent(id)
			close(record.done)
			return nil, errors.New("root is stopping")
		}
		effectiveBudgets, _ := root.InspectBudgets(ctx, root.AgentID(), root.AgentID()+":"+id)
		return map[string]any{"id": id, "agent_id": root.AgentID() + ":" + id, "status": "running", "effective_capabilities": capabilities, "effective_budgets": effectiveBudgets}, nil
	case "list":
		return root.ListAgentRelatives(ctx, root.AgentID())
	case "inspect":
		id, _ := stringArg(arguments, "id")
		host.mu.Lock()
		record := host.children[id]
		host.mu.Unlock()
		if record == nil {
			return nil, errors.New("child not found")
		}
		output, taskStatus, childErr := record.snapshot()
		status := childStatus(taskStatus)
		blocking := ""
		if taskStatus == agent.TaskRunning {
			switch {
			case record.agent.WaitingOnSubagents():
				blocking = "subagents"
			case record.agent.HasRunningWaits():
				blocking = "wait"
			case record.agent.TurnRunning():
				blocking = "model_or_tools"
			}
		}
		budgets, _ := root.InspectBudgets(ctx, root.AgentID(), root.AgentID()+":"+id)
		return map[string]any{
			"id": id, "agent_id": record.agentID, "status": status, "blocking_reason": blocking,
			"effective_capabilities": record.capabilities, "workspace_scope": root.WorkingDirectory(),
			"output": output, "error": errorString(childErr), "budgets": budgets,
		}, nil
	case "await":
		id, _ := stringArg(arguments, "id")
		host.mu.Lock()
		record := host.children[id]
		host.mu.Unlock()
		if record == nil {
			return nil, errors.New("child not found")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-record.done:
			output, status, childErr := record.snapshot()
			return map[string]any{"id": id, "status": childStatus(status), "output": output, "error": errorString(childErr)}, nil
		}
	case "steer":
		id, _ := stringArg(arguments, "id")
		text, _ := stringArg(arguments, "text")
		return map[string]any{"accepted": true}, root.SteerSubagent(ctx, id, text)
	case "stop":
		id, _ := stringArg(arguments, "id")
		host.mu.Lock()
		record := host.children[id]
		host.mu.Unlock()
		if record != nil {
			record.cancel()
		}
		root.StopSubagent(id)
		return map[string]any{"stopped": id}, nil
	default:
		return nil, fmt.Errorf("unknown agents operation %q", operation)
	}
}

func (host *daemonRLMHost) messages(ctx context.Context, root *daemon.Session, operation string, arguments map[string]any) (any, error) {
	switch operation {
	case "receive":
		return root.ReceiveAgentMessages(ctx, root.AgentID(), intArg(arguments, "limit", 32))
	case "send":
	default:
		return nil, fmt.Errorf("unknown messages operation %q", operation)
	}
	recipient, _ := stringArg(arguments, "recipient")
	body, _ := stringArg(arguments, "body")
	evidence, _ := stringArg(arguments, "evidence_handle")
	if len(body) > session.InlineValueLimit {
		return nil, errors.New("message body exceeds the inline limit; store it as an artifact and send evidence_handle")
	}
	delivery, _ := stringArg(arguments, "delivery")
	if delivery == "" {
		delivery = string(session.DeliveryQueued)
	}
	sequence, err := root.SendAgentMessage(ctx, root.AgentID(), recipient, session.AgentMessage{
		Delivery: session.MessageDelivery(delivery), Body: body, EvidenceReferenceID: evidence,
	})
	return sequence, err
}

func (host *daemonRLMHost) state(ctx context.Context, root *daemon.Session, operation string, arguments map[string]any) (any, error) {
	key, _ := stringArg(arguments, "key")
	payload, err := statePayload(arguments["value"])
	if err != nil {
		return nil, err
	}
	switch operation {
	case "private_get":
		return root.GetPrivateState(ctx, root.AgentID(), key)
	case "private_list":
		return root.ListPrivateState(ctx, root.AgentID())
	case "private_set":
		return root.SetPrivateState(ctx, root.AgentID(), key, payload)
	case "private_append":
		return root.AppendPrivateState(ctx, root.AgentID(), key, payload)
	case "private_cas":
		return root.CompareAndSwapPrivateState(ctx, root.AgentID(), key, int64Arg(arguments, "version", 0), payload)
	case "blackboard_get":
		return root.GetBlackboard(ctx, root.AgentID(), key)
	case "blackboard_set":
		return root.SetBlackboard(ctx, root.AgentID(), key, payload)
	case "blackboard_append":
		return root.AppendBlackboard(ctx, root.AgentID(), key, payload)
	case "blackboard_cas":
		return root.CompareAndSwapBlackboard(ctx, root.AgentID(), key, int64Arg(arguments, "version", 0), payload)
	case "blackboard_history":
		return root.BlackboardHistory(ctx, root.AgentID(), key)
	case "subscribe":
		return root.CreateBlackboardSubscription(ctx, root.AgentID(), key)
	case "subscriptions":
		return root.ListBlackboardSubscriptions(ctx, root.AgentID())
	case "cancel_subscription":
		id, _ := stringArg(arguments, "id")
		return map[string]any{"cancelled": id}, root.CancelBlackboardSubscription(ctx, root.AgentID(), id)
	default:
		return nil, fmt.Errorf("unknown state operation %q", operation)
	}
}

func (host *daemonRLMHost) artifacts(ctx context.Context, root *daemon.Session, operation string, arguments map[string]any) (any, error) {
	switch operation {
	case "put":
		text, _ := stringArg(arguments, "text")
		source, _ := stringArg(arguments, "source")
		if source == "" {
			source = "RLM artifact"
		}
		value, err := root.StoreContent(ctx, root.AgentID(), session.RuntimePayload{Data: []byte(text), MediaType: "text/plain", Source: source})
		return runtimeValueMap(value), err
	case "inspect":
		return host.context(ctx, root, "inspect", arguments)
	case "read":
		return host.context(ctx, root, "read", arguments)
	default:
		return nil, fmt.Errorf("unknown artifacts operation %q", operation)
	}
}

func (host *daemonRLMHost) schedules(ctx context.Context, root *daemon.Session, operation string, arguments map[string]any) (any, error) {
	switch operation {
	case "create":
		expression, _ := stringArg(arguments, "schedule")
		prompt, _ := stringArg(arguments, "prompt")
		if _, err := schedule.Parse(expression); err != nil {
			return nil, err
		}
		id, err := root.AddSchedule(ctx, expression, prompt, time.Now())
		return map[string]any{"id": id}, err
	case "list":
		return root.ListSchedules(ctx)
	case "cancel":
		id := intArg(arguments, "id", 0)
		return map[string]any{"cancelled": id}, root.CancelSchedule(ctx, id)
	default:
		return nil, fmt.Errorf("unknown schedules operation %q", operation)
	}
}

func statePayload(value any) (session.RuntimePayload, error) {
	data, err := json.Marshal(value)
	return session.RuntimePayload{Data: data, MediaType: "application/json", Source: "RLM state"}, err
}

func (host *daemonRLMHost) boundedText(ctx context.Context, root *daemon.Session, source, value string) (map[string]any, error) {
	if len(value) <= session.InlineValueLimit {
		return map[string]any{"output": value}, nil
	}
	stored, err := root.StoreContent(ctx, root.AgentID(), session.RuntimePayload{Data: []byte(value), MediaType: "text/plain", Source: source})
	if err != nil {
		return nil, err
	}
	const preview = 2 << 10
	prefix := utf8Prefix(value, preview)
	suffix, _ := utf8Suffix(value, preview)
	return map[string]any{
		"handle": stored.ReferenceID, "size": stored.Size, "source": stored.Source,
		"preview": prefix + "\n... [handle-backed remainder] ...\n" + suffix,
	}, nil
}

func childCapabilities(value any) ([]string, error) {
	if value == nil {
		return []string{"read"}, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("capabilities must be a list")
	}
	result := make([]string, len(items))
	for index, item := range items {
		name, ok := item.(string)
		if !ok {
			return nil, errors.New("capability names must be strings")
		}
		result[index] = name
	}
	sort.Strings(result)
	return result, nil
}

func childBudgets(value any) ([]session.BudgetLimit, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("budgets must be a dictionary")
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]session.BudgetLimit, 0, len(keys))
	for _, key := range keys {
		limit, ok := items[key].(float64)
		if !ok || limit < 0 || limit != float64(int64(limit)) {
			return nil, fmt.Errorf("budget %q must be a non-negative integer", key)
		}
		result = append(result, session.BudgetLimit{Kind: session.BudgetKind(key), Limit: int64(limit)})
	}
	return result, nil
}

func runtimeValueMap(value session.RuntimeValue) map[string]any {
	return map[string]any{"inline": string(value.Inline), "handle": value.ReferenceID, "digest": value.Digest, "size": value.Size, "media_type": value.MediaType, "source": value.Source}
}

func metadataMap(value session.ContentMetadata) map[string]any {
	return map[string]any{"handle": value.ReferenceID, "digest": value.Digest, "size": value.Size, "media_type": value.MediaType, "source": value.Source}
}

func stringArg(arguments map[string]any, key string) (string, bool) {
	value, ok := arguments[key].(string)
	return value, ok
}

func intArg(arguments map[string]any, key string, fallback int) int {
	value, ok := arguments[key].(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) || value < math.MinInt || value > math.MaxInt {
		return fallback
	}
	return int(value)
}

func int64Arg(arguments map[string]any, key string, fallback int64) int64 {
	value, ok := arguments[key].(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) || value < math.MinInt64 || value > math.MaxInt64 {
		return fallback
	}
	return int64(value)
}

func utf8Prefix(value string, bytes int) string {
	end := min(len(value), bytes)
	for end > 0 && end < len(value) && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

func utf8Suffix(value string, bytes int) (string, int) {
	start := max(0, len(value)-bytes)
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:], start
}

func randomSuffix() string {
	var value [6]byte
	if _, err := rand.Read(value[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(value[:])
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func childStatus(status agent.TaskStatus) string {
	switch status {
	case agent.TaskDone:
		return "succeeded"
	case agent.TaskError:
		return "failed"
	case agent.TaskCancelled:
		return "cancelled"
	default:
		return "running"
	}
}
