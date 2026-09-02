package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func TestDaemonRLMHostUsesDispatcherAndDurableHandles(t *testing.T) {
	childRelease := make(chan struct{})
	releaseChild := sync.OnceFunc(func() { close(childRelease) })
	defer releaseChild()
	batchReady := make(chan struct{})
	var batchCalls, inFlight, maxInFlight atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var call llm.Request
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if call.Stream {
			<-childRelease
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"child done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`+"\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		if strings.HasPrefix(call.Messages[0].Content, "batch-") {
			active := inFlight.Add(1)
			defer inFlight.Add(-1)
			for observed := maxInFlight.Load(); active > observed && !maxInFlight.CompareAndSwap(observed, active); observed = maxInFlight.Load() {
			}
			if batchCalls.Add(1) == 2 {
				close(batchReady)
			}
			<-batchReady
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"stateless done"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	}))
	defer provider.Close()
	workspace := t.TempDir()
	store, err := session.OpenWithDefaultMode(filepath.Join(t.TempDir(), "sessions.db"), session.ModeRLM)
	if err != nil {
		t.Fatal(err)
	}
	rootID, err := store.Create(workspace, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	agent := agent.New(llm.New(provider.URL, "key"), "model", 100, "system")
	temperature, topP := 0.2, 0.8
	agent.ModelName, agent.Provider = "configured-model", "configured-provider"
	agent.ContextLimit, agent.Effort = 16_384, "high"
	agent.Temperature, agent.TopP = &temperature, &topP
	agent.CompactThreshold = 0.7
	agent.WorkingDir = workspace
	agent.Services.SetScreenshotSink(func([][]byte) {})
	host := newDaemonRLMHost(agent, []llm.Message{{Role: "system", Content: "system"}, {Role: "user", Content: "prior"}})
	host.SetPricing(0.000001, 0.000002, 0.0000005)
	owner, err := daemon.New(store, func(context.Context, session.Meta, []llm.Message) (daemon.Components, error) {
		return daemon.Components{Runner: daemon.NewAgentRunner(agent), Bind: host.Bind}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}

	ctx, err := tools.WithTurnIdentity(context.Background(), "rlm-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Call(ctx, "files", "write", map[string]any{"path": "note.txt", "content": "dispatch evidence"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "note.txt"))
	if err != nil || string(data) != "dispatch evidence" {
		t.Fatalf("workspace write = %q, %v", data, err)
	}
	read, err := host.Call(ctx, "files", "read", map[string]any{"path": "note.txt"})
	if err != nil || !strings.Contains(read.(map[string]any)["output"].(string), "dispatch evidence") {
		t.Fatalf("workspace read = %#v, %v", read, err)
	}
	listed, err := host.Call(ctx, "files", "list", map[string]any{"path": "."})
	if err != nil || !strings.Contains(listed.(map[string]any)["output"].(string), "note.txt") {
		t.Fatalf("workspace list = %#v, %v", listed, err)
	}
	searched, err := host.Call(ctx, "files", "search", map[string]any{"path": ".", "query": "dispatch evidence"})
	if err != nil || !strings.Contains(searched.(map[string]any)["output"].(string), "note.txt:1") {
		t.Fatalf("workspace search = %#v, %v", searched, err)
	}
	shell, err := host.Call(ctx, "shell", "run", map[string]any{"command": "printf shell-ok"})
	if err != nil || !strings.Contains(shell.(map[string]any)["output"].(string), "shell-ok") {
		t.Fatalf("shell run = %#v, %v", shell, err)
	}
	if _, err := host.Call(ctx, "files", "patch", map[string]any{"path": "note.txt", "old": "dispatch", "new": "dispatcher"}); err != nil {
		t.Fatal(err)
	}

	artifact, err := host.Call(ctx, "artifacts", "put", map[string]any{"text": strings.Repeat("artifact", 2_000), "source": "test corpus"})
	if err != nil {
		t.Fatal(err)
	}
	handle := artifact.(map[string]any)["handle"].(string)
	if inspected, err := host.Call(ctx, "context", "inspect", map[string]any{"handle": handle}); err != nil || inspected.(map[string]any)["source"] != "test corpus" {
		t.Fatalf("context inspect = %#v, %v", inspected, err)
	}
	if searched, err := host.Call(ctx, "context", "search", map[string]any{"handle": handle, "query": "artifact"}); err != nil || len(searched.(map[string]any)["matches"].([]map[string]any)) == 0 {
		t.Fatalf("context search = %#v, %v", searched, err)
	}
	excerpt, err := host.Call(ctx, "artifacts", "read", map[string]any{"handle": handle, "offset": float64(10), "length": float64(40)})
	if err != nil || excerpt.(map[string]any)["span"].(map[string]any)["start"] != int64(10) {
		t.Fatalf("artifact excerpt = %#v, %v", excerpt, err)
	}
	if _, err := host.Call(ctx, "artifacts", "inspect", map[string]any{"handle": handle}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Call(ctx, "shell", "read", map[string]any{"handle": handle, "length": float64(16)}); err != nil {
		t.Fatal(err)
	}

	if _, err := host.Call(ctx, "state", "blackboard_set", map[string]any{"key": "finding", "value": map[string]any{"handle": handle}}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Call(ctx, "state", "blackboard_get", map[string]any{"key": "finding"}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Call(ctx, "state", "blackboard_history", map[string]any{"key": "finding"}); err != nil {
		t.Fatal(err)
	}
	private, err := host.Call(ctx, "state", "private_set", map[string]any{"key": "notes", "value": []any{"one"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Call(ctx, "state", "private_append", map[string]any{"key": "notes", "value": []any{"two"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Call(ctx, "state", "private_cas", map[string]any{"key": "notes", "version": float64(private.(session.StateValue).Version + 1), "value": []any{"three"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Call(ctx, "state", "private_get", map[string]any{"key": "notes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Call(ctx, "state", "private_list", nil); err != nil {
		t.Fatal(err)
	}
	board, err := host.Call(ctx, "state", "blackboard_set", map[string]any{"key": "list", "value": []any{"one"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Call(ctx, "state", "blackboard_append", map[string]any{"key": "list", "value": []any{"two"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Call(ctx, "state", "blackboard_cas", map[string]any{"key": "list", "version": float64(board.(session.StateValue).Version + 1), "value": []any{"three"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Call(ctx, "state", "subscribe", map[string]any{"key": "finding"}); err != nil {
		t.Fatal(err)
	}
	subscriptions, err := host.Call(ctx, "state", "subscriptions", nil)
	if err != nil || len(subscriptions.([]session.BlackboardSubscription)) != 1 {
		t.Fatalf("subscriptions = %#v, %v", subscriptions, err)
	}
	subscriptionID := subscriptions.([]session.BlackboardSubscription)[0].ID
	if _, err := host.Call(ctx, "state", "cancel_subscription", map[string]any{"id": subscriptionID}); err != nil {
		t.Fatal(err)
	}
	created, err := host.Call(ctx, "schedules", "create", map[string]any{"schedule": "@every 1h", "prompt": "recheck"})
	if err != nil || created.(map[string]any)["id"].(int) < 1 {
		t.Fatalf("schedule = %#v, %v", created, err)
	}
	scheduleID := created.(map[string]any)["id"].(int)
	if schedules, err := host.Call(ctx, "schedules", "list", nil); err != nil || len(schedules.([]session.Schedule)) != 1 {
		t.Fatalf("schedules = %#v, %v", schedules, err)
	}
	if _, err := host.Call(ctx, "schedules", "cancel", map[string]any{"id": float64(scheduleID)}); err != nil {
		t.Fatal(err)
	}

	large := strings.Repeat("head", 3_000) + "middle-secret" + strings.Repeat("tail", 3_000)
	focused, err := host.focusInput(ctx, large)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(focused, "middle-secret") || !strings.Contains(focused, "context handle") || len(focused) >= len(large) {
		t.Fatalf("large input was not focused: %d of %d bytes", len(focused), len(large))
	}
	if prompt := agent.MessagesSnapshot()[0].Content; !strings.Contains(prompt, "rlm_exec") || !strings.Contains(prompt, host.handle.ReferenceID) {
		t.Fatalf("default RLM prompt lost its runtime contract or history handle: %q", prompt)
	}
	if root.Mode() != session.ModeRLM || root.AgentID() == "" {
		t.Fatalf("root identity: mode=%s agent=%q", root.Mode(), root.AgentID())
	}

	modelCall, err := host.Call(ctx, "models", "call", map[string]any{"prompt": "one", "max_tokens": float64(16)})
	if err != nil || modelCall.(map[string]any)["output"] != "stateless done" {
		t.Fatalf("stateless call = %#v, %v", modelCall, err)
	}
	batch, err := host.Call(ctx, "models", "batch", map[string]any{"prompts": []any{"batch-one", float64(2), "batch-three"}, "max_tokens": float64(16)})
	if err != nil || len(batch.([]map[string]any)) != 3 || batch.([]map[string]any)[1]["error"] == nil {
		t.Fatalf("stateless batch = %#v, %v", batch, err)
	}
	if maxInFlight.Load() != 2 {
		t.Fatalf("stateless batch max in-flight calls = %d, want 2", maxInFlight.Load())
	}
	if relatives, err := root.ListAgentRelatives(ctx, root.AgentID()); err != nil || len(relatives.Children) != 0 {
		t.Fatalf("stateless calls created agent rows: %+v, %v", relatives, err)
	}

	if _, err := host.Call(ctx, "agents", "spawn", map[string]any{"prompt": "work", "capabilities": []any{"root"}}); err == nil {
		t.Fatal("child capability escalation was accepted")
	}
	spawned, err := host.Call(ctx, "agents", "spawn", map[string]any{"id": "reader", "prompt": "work", "capabilities": []any{"read"}, "budgets": map[string]any{"tokens": float64(10_000)}})
	if err != nil || spawned.(map[string]any)["status"] != "running" {
		t.Fatalf("spawn = %#v, %v", spawned, err)
	}
	host.mu.Lock()
	spawnedChild := host.children["reader"].agent
	host.mu.Unlock()
	if spawnedChild.ModelName != agent.ModelName || spawnedChild.Provider != agent.Provider || spawnedChild.ContextLimit != agent.ContextLimit || spawnedChild.Effort != agent.Effort ||
		spawnedChild.Temperature != agent.Temperature || spawnedChild.TopP != agent.TopP || spawnedChild.CompactThreshold != agent.CompactThreshold || spawnedChild.WorkingDir != workspace {
		t.Fatalf("spawned child did not inherit model runtime configuration: %+v", spawnedChild)
	}
	if spawnedChild.Client.CacheKey != rootID+"/reader" || !spawnedChild.Services.ScreenshotsEnabled() {
		t.Fatalf("spawned child cache/screenshot routing = %q/%v", spawnedChild.Client.CacheKey, spawnedChild.Services.ScreenshotsEnabled())
	}
	if child, err := host.Call(ctx, "agents", "inspect", map[string]any{"id": "reader"}); err != nil || child.(map[string]any)["status"] != "running" || !slices.Equal(child.(map[string]any)["effective_capabilities"].([]string), []string{"read"}) {
		t.Fatalf("running child inspection = %#v, %v", child, err)
	}
	if _, err := host.Call(ctx, "agents", "spawn", map[string]any{"id": "cancelled", "prompt": "wait", "capabilities": []any{"read"}}); err != nil {
		t.Fatal(err)
	}
	if stopped, err := host.Call(ctx, "agents", "stop", map[string]any{"id": "cancelled"}); err != nil || stopped.(map[string]any)["stopped"] != "cancelled" {
		t.Fatalf("stop = %#v, %v", stopped, err)
	}
	if stopped, err := host.Call(ctx, "agents", "await", map[string]any{"id": "cancelled"}); err != nil || stopped.(map[string]any)["status"] != "cancelled" {
		t.Fatalf("cancelled child = %#v, %v", stopped, err)
	}
	childAgentID := spawned.(map[string]any)["agent_id"].(string)
	if steered, err := host.Call(ctx, "agents", "steer", map[string]any{"id": "reader", "text": "priority"}); err != nil || steered.(map[string]any)["accepted"] != true {
		t.Fatalf("steer = %#v, %v", steered, err)
	}
	message, err := host.Call(ctx, "messages", "send", map[string]any{"recipient": childAgentID, "body": "follow-up"})
	if err != nil || message.(session.InboxSequence).InboxSeq < 1 {
		t.Fatalf("message send = %#v, %v", message, err)
	}
	releaseChild()
	awaited, err := host.Call(ctx, "agents", "await", map[string]any{"id": "reader"})
	if err != nil || awaited.(map[string]any)["output"] != "child done" {
		t.Fatalf("await = %#v, %v", awaited, err)
	}
	transcript, err := store.SubagentTranscript(rootID, "reader")
	if err != nil || len(transcript) < 4 || !slices.ContainsFunc(transcript, func(message llm.Message) bool { return strings.Contains(message.Content, "follow-up") }) {
		t.Fatalf("durable child transcript = %+v, %v", transcript, err)
	}
	if relatives, err := host.Call(ctx, "agents", "list", nil); err != nil || len(relatives.(session.AgentRelatives).Children) != 2 {
		t.Fatalf("agent list = %#v, %v", relatives, err)
	}
	if child, err := host.Call(ctx, "agents", "inspect", map[string]any{"id": "reader"}); err != nil || child.(map[string]any)["status"] != "succeeded" {
		t.Fatalf("agent inspect = %#v, %v", child, err)
	}
	if messages, err := host.Call(ctx, "messages", "receive", nil); err != nil || len(messages.([]session.AgentMessageEnvelope)) != 0 {
		t.Fatalf("empty messages = %#v, %v", messages, err)
	}
	permission, err := host.Call(ctx, "permissions", "request", map[string]any{"operation": "write"})
	if err != nil || permission.(map[string]any)["status"] != "invoke_operation" {
		t.Fatalf("permission request = %#v, %v", permission, err)
	}
	if _, err := host.Call(ctx, "permissions", "status", map[string]any{"id": "missing"}); err == nil {
		t.Fatal("missing permission status succeeded")
	}
	if answer, err := host.Call(ctx, "answer", "submit", map[string]any{"text": "done", "citations": []any{handle}}); err != nil || answer.(map[string]any)["accepted"] != true {
		t.Fatalf("answer = %#v, %v", answer, err)
	}
	budgets, err := root.InspectBudgets(ctx, root.AgentID(), root.AgentID())
	if err != nil {
		t.Fatal(err)
	}
	used := make(map[session.BudgetKind]int64, len(budgets))
	for _, budget := range budgets {
		used[budget.Kind] = budget.Used
		if budget.Reserved != 0 {
			t.Fatalf("root budget reservation leaked: %+v", budget)
		}
	}
	for _, kind := range []session.BudgetKind{session.BudgetTokens, session.BudgetCost, session.BudgetElapsed, session.BudgetDurableBytes, session.BudgetRecordCount, session.BudgetSchedulesSubscriptions} {
		if used[kind] == 0 {
			t.Errorf("RLM did not account %s budget: %+v", kind, used)
		}
	}
}

func TestDaemonRLMHostRejectsInvalidRequestsAndBoundsResults(t *testing.T) {
	workspace := t.TempDir()
	store, err := session.OpenWithDefaultMode(filepath.Join(t.TempDir(), "sessions.db"), session.ModeRLM)
	if err != nil {
		t.Fatal(err)
	}
	rootID, err := store.Create(workspace, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	value := agent.New(llm.New("http://127.0.0.1:1", "key"), "model", 100, "system")
	host := newDaemonRLMHost(value, nil)
	if err := host.Bind(nil); err == nil {
		t.Fatal("nil daemon root was accepted")
	}
	unbound := newDaemonRLMHost(value, nil)
	if _, err := unbound.Call(context.Background(), "answer", "submit", nil); err == nil {
		t.Fatal("unbound host call succeeded")
	}
	owner, err := daemon.New(store, func(context.Context, session.Meta, []llm.Message) (daemon.Components, error) {
		return daemon.Components{Runner: daemon.NewAgentRunner(value), Bind: host.Bind}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := tools.WithTurnIdentity(context.Background(), "rlm-errors")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := host.Call(ctx, "context", "read", nil); err == nil {
		t.Fatal("context read without a handle succeeded")
	}
	artifact, err := host.Call(ctx, "artifacts", "put", map[string]any{"text": "needle"})
	if err != nil {
		t.Fatal(err)
	}
	handle := artifact.(map[string]any)["handle"].(string)
	if _, err := host.Call(ctx, "context", "search", map[string]any{"handle": handle}); err == nil {
		t.Fatal("empty context query succeeded")
	}

	invalid := []struct {
		module    string
		operation string
		arguments map[string]any
	}{
		{module: "missing", operation: "read"},
		{module: "context", operation: "missing", arguments: map[string]any{"handle": handle}},
		{module: "files", operation: "search"},
		{module: "files", operation: "missing"},
		{module: "shell", operation: "missing"},
		{module: "models", operation: "call"},
		{module: "models", operation: "batch"},
		{module: "models", operation: "missing"},
		{module: "agents", operation: "spawn"},
		{module: "agents", operation: "spawn", arguments: map[string]any{"prompt": "work", "capabilities": "read"}},
		{module: "agents", operation: "spawn", arguments: map[string]any{"prompt": "work", "capabilities": []any{float64(1)}}},
		{module: "agents", operation: "spawn", arguments: map[string]any{"prompt": "work", "budgets": []any{}}},
		{module: "agents", operation: "spawn", arguments: map[string]any{"prompt": "work", "budgets": map[string]any{"tokens": 1.5}}},
		{module: "agents", operation: "inspect", arguments: map[string]any{"id": "missing"}},
		{module: "agents", operation: "await", arguments: map[string]any{"id": "missing"}},
		{module: "agents", operation: "missing"},
		{module: "messages", operation: "send", arguments: map[string]any{"recipient": "missing", "body": strings.Repeat("x", session.InlineValueLimit+1)}},
		{module: "messages", operation: "missing"},
		{module: "state", operation: "private_set", arguments: map[string]any{"key": "bad", "value": make(chan int)}},
		{module: "state", operation: "missing"},
		{module: "artifacts", operation: "missing"},
		{module: "schedules", operation: "create", arguments: map[string]any{"schedule": "not a schedule"}},
		{module: "schedules", operation: "missing"},
		{module: "permissions", operation: "missing"},
		{module: "answer", operation: "missing"},
	}
	for _, test := range invalid {
		t.Run(test.module+"_"+test.operation, func(t *testing.T) {
			if _, err := host.Call(ctx, test.module, test.operation, test.arguments); err == nil {
				t.Fatal("invalid RLM request succeeded")
			}
		})
	}

	if _, err := host.invoke(ctx, "read", map[string]any{"value": make(chan int)}); err == nil {
		t.Fatal("unencodable dispatcher arguments succeeded")
	}
	bounded, err := host.boundedText(ctx, root, "large result", strings.Repeat("界", session.InlineValueLimit))
	if err != nil || bounded["handle"] == "" || !strings.Contains(bounded["preview"].(string), "handle-backed remainder") {
		t.Fatalf("bounded result = %#v, %v", bounded, err)
	}
	if _, err := host.Call(ctx, "files", "list", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Call(ctx, "files", "search", map[string]any{"query": "absent"}); err != nil {
		t.Fatal(err)
	}

	waiting := &rlmChild{done: make(chan struct{}), status: agent.TaskRunning, agent: value, cancel: func() {}}
	host.children["waiting"] = waiting
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := host.Call(cancelled, "agents", "await", map[string]any{"id": "waiting"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled await error = %v", err)
	}
	stopped := false
	host.children["closable"] = &rlmChild{cancel: func() { stopped = true }}
	host.Close()
	if !stopped {
		t.Fatal("host close did not cancel children")
	}

	kernel, err := rlm.NewKernel(rlm.KernelOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime := daemonRLMRuntime{host: newDaemonRLMHost(value, nil), kernel: kernel}
	runtime.Close()
	if _, err := kernel.Exec(ctx, "1"); !errors.Is(err, rlm.ErrKernelClosed) {
		t.Fatalf("runtime did not close kernel: %v", err)
	}
}

func TestRLMArgumentBoundaries(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), 1.5, float64(math.MaxInt64) * 2} {
		if got := intArg(map[string]any{"value": value}, "value", 7); got != 7 {
			t.Errorf("intArg(%v) = %d", value, got)
		}
		if got := int64Arg(map[string]any{"value": value}, "value", 9); got != 9 {
			t.Errorf("int64Arg(%v) = %d", value, got)
		}
	}
	if got := utf8Prefix("界", 1); got != "" {
		t.Fatalf("UTF-8 prefix = %q", got)
	}
	if got, start := utf8Suffix("界", 1); got != "" || start != len("界") {
		t.Fatalf("UTF-8 suffix = %q at %d", got, start)
	}
	if got := errorString(errors.New("boom")); got != "boom" {
		t.Fatalf("error string = %q", got)
	}
	for status, want := range map[agent.TaskStatus]string{
		agent.TaskDone: "succeeded", agent.TaskError: "failed", agent.TaskCancelled: "cancelled", agent.TaskRunning: "running",
	} {
		if got := childStatus(status); got != want {
			t.Errorf("childStatus(%q) = %q", status, got)
		}
	}
	if got, err := childCapabilities(nil); err != nil || !slices.Equal(got, []string{"read"}) {
		t.Fatalf("default capabilities = %v, %v", got, err)
	}
	if got, err := childBudgets(nil); err != nil || got != nil {
		t.Fatalf("default budgets = %v, %v", got, err)
	}
}

func TestRLMLimitsUseContractDefaultsAndOverrides(t *testing.T) {
	defaults := rlmLimits(config.RLMConfig{})
	if defaults.Steps != 1_000_000 || defaults.HostRequests != 1_024 || defaults.Wall != 30*time.Second || defaults.MaxWorkers != 4 {
		t.Fatalf("defaults = %+v", defaults)
	}
	overrides := rlmLimits(config.RLMConfig{
		Steps: 9, HostRequests: 8, WallMillis: 7, MemoryMiB: 6,
		OutputBytes: 5, FrameBytes: 4, MaxWorkers: 3,
	})
	if overrides.Steps != 9 || overrides.HostRequests != 8 || overrides.Wall != 7*time.Millisecond ||
		overrides.MemoryBytes != 6<<20 || overrides.OutputBytes != 5 || overrides.FrameBytes != 4 || overrides.MaxWorkers != 3 {
		t.Fatalf("overrides = %+v", overrides)
	}
}

func TestConfiguredSessionModeDefaultsRLMAndDisablesToClassic(t *testing.T) {
	if got := configuredSessionMode(config.Default()); got != session.ModeRLM {
		t.Fatalf("default mode = %s", got)
	}
	disabled := false
	cfg := config.Default()
	cfg.RLM.Enabled = &disabled
	if got := configuredSessionMode(cfg); got != session.ModeClassic {
		t.Fatalf("disabled mode = %s", got)
	}
}

func TestDaemonToolServicesHonorOptionalRuntimeConfiguration(t *testing.T) {
	enabled := true
	for _, mode := range []string{"", "dedicated", "headless", "extension"} {
		cfg := config.Default()
		cfg.Browser.Enabled = &enabled
		cfg.Browser.Mode = mode
		cfg.Browser.CDPURL = "http://127.0.0.1:9222"
		cfg.Computer.Enabled = &enabled
		services := daemonToolServices(cfg, session.Meta{ID: "root"}, "model")
		if services.Browser() == nil || services.ComputerPolicy() == nil || services.Diagnostics() == nil {
			t.Fatalf("mode %q omitted configured services", mode)
		}
		if got := services.ProcessOptions().Env["WHIP_CDP_URL"]; got != cfg.Browser.CDPURL {
			t.Fatalf("mode %q CDP environment = %q", mode, got)
		}
		services.Close()
	}
	disabled := false
	cfg := config.Default()
	cfg.Browser.Enabled = &disabled
	cfg.Computer.Enabled = &disabled
	services := daemonToolServices(cfg, session.Meta{ID: "root"}, "model")
	defer services.Close()
	if services.Browser() != nil || services.ComputerPolicy() != nil || services.Diagnostics() == nil {
		t.Fatal("disabled optional services were constructed")
	}
}
