package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/schedule"
	sessionstore "github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

type RecursiveRuntimeOptions struct {
	Agent          *agent.Agent
	History        []llm.Message
	Limits         rlm.Limits
	Kernels        *rlm.Manager
	KernelCommand  []string
	MCP            *mcp.Manager
	InputPrice     float64
	OutputPrice    float64
	CacheReadPrice float64
}

// RecursiveRuntime owns one tree of identical RLM agent sessions. The daemon
// root actor remains the durable serialization boundary for the whole tree.
type RecursiveRuntime struct {
	mu          sync.RWMutex
	root        *Session
	rootNode    *AgentSession
	agents      map[string]*AgentSession
	limits      rlm.Limits
	kernels     *rlm.Manager
	command     []string
	mcp         *mcp.Manager
	pricing     [3]float64
	hookMu      sync.RWMutex
	runTurnHook func(*AgentSession)
	closed      bool
}

func (runtime *RecursiveRuntime) setRunTurnHook(hook func(*AgentSession)) {
	runtime.hookMu.Lock()
	runtime.runTurnHook = hook
	runtime.hookMu.Unlock()
}

func (runtime *RecursiveRuntime) observeRunTurn(session *AgentSession) {
	runtime.hookMu.RLock()
	hook := runtime.runTurnHook
	runtime.hookMu.RUnlock()
	if hook != nil {
		hook(session)
	}
}

// AgentSession is the live model loop, Starlark kernel, and identity for one
// root or child. Root and child sessions use this exact type.
type AgentSession struct {
	runtime      *RecursiveRuntime
	root         *Session
	agent        *agent.Agent
	host         *recursiveHost
	kernel       *rlm.Kernel
	authority    capability.Authority
	id           string
	parentID     string
	name         string
	capabilities []string

	mu          sync.Mutex
	running     bool
	closed      bool
	failures    int // consecutive failed turns; drives the re-wake backoff
	cancel      context.CancelFunc
	turn        turnJournal
	emit        func(string, StreamEvent)
	interactive *daemonInteractiveRunner
	report      string // spawn report mode: "" or "notice", "message", "inline"
}

type recursiveHost struct {
	session *AgentSession
	history []llm.Message
	handle  *rlm.ContextHandle
	focused bool
	system  string
	mu      sync.Mutex
}

func NewRecursiveRuntime(options RecursiveRuntimeOptions) (*RecursiveRuntime, error) {
	if options.Agent == nil {
		return nil, errors.New("recursive runtime requires an agent")
	}
	if options.Kernels == nil {
		options.Kernels = rlm.NewManager(options.Limits.MaxWorkers)
	}
	runtime := &RecursiveRuntime{
		agents: make(map[string]*AgentSession), limits: options.Limits, kernels: options.Kernels,
		command: append([]string(nil), options.KernelCommand...), mcp: options.MCP,
		pricing: [3]float64{options.InputPrice, options.OutputPrice, options.CacheReadPrice},
	}
	node, err := runtime.newNode(options.Agent, "", "root", nil, capability.Authority{})
	if err != nil {
		return nil, err
	}
	node.host.history = append([]llm.Message(nil), options.History...)
	runtime.rootNode = node
	return runtime, nil
}

// RootSession returns the root model session. Root and descendants are the
// same concrete execution unit; only their persistence envelopes differ.
func (runtime *RecursiveRuntime) RootSession() *AgentSession { return runtime.rootNode }

// identity derives who this node is from the live tree.
func (node *AgentSession) identity() rlm.Identity {
	identity := rlm.Identity{AgentID: node.id, Name: node.name, ParentID: node.parentID, Report: node.report}
	if node.runtime == nil || node.parentID == "" {
		return identity
	}
	node.runtime.mu.RLock()
	defer node.runtime.mu.RUnlock()
	for parent := node.runtime.agents[node.parentID]; parent != nil; parent = node.runtime.agents[parent.parentID] {
		if identity.Depth == 0 {
			identity.ParentName = parent.name
		}
		identity.Depth++
		if parent.parentID == "" {
			break
		}
	}
	if identity.Depth == 0 {
		identity.Depth = 1
	}
	return identity
}

func (node *AgentSession) systemPrompt(handle *rlm.ContextHandle) string {
	return rlm.BuildPrompt(node.agent.WorkingDir, handle) + rlm.IdentityBlock(node.identity())
}

// scratchStore persists a node's Starlark scratch through the root actor. A
// node that is not bound to a root yet has nothing to load or save.
type scratchStore struct{ node *AgentSession }

func (store scratchStore) Load(ctx context.Context) (string, rlm.SnapshotManifest, error) {
	node := store.node
	if node.root == nil || node.id == "" {
		return "", rlm.SnapshotManifest{}, nil
	}
	program, encoded, err := node.root.LoadAgentScratch(ctx, node.id)
	var manifest rlm.SnapshotManifest
	if err == nil && len(encoded) > 0 {
		_ = json.Unmarshal(encoded, &manifest)
	}
	return program, manifest, err
}

// emitHostCall publishes one host call made inside a cell as a presentation
// event: module.operation, a bounded argument summary, and the duration. It
// lets a client show what a cell is doing while it runs.
func (node *AgentSession) emitHostCall(call rlm.HostCall) {
	emit := node.emit
	if emit == nil {
		return
	}
	emit("stream.cell.host", StreamEvent{
		ID: call.CallID, Name: call.Module + "." + call.Operation, Args: call.Summary,
		Text: call.Duration.Round(time.Millisecond).String(), Result: call.Err,
	})
}

// recordScratchRestore persists the restore outcome off the kernel lock; the
// event is an audit trail, so ordering against the turn does not matter.
func (node *AgentSession) recordScratchRestore(ctx context.Context, report rlm.RestoreReport) {
	root, id := node.root, node.id
	if root == nil || id == "" {
		return
	}
	go func() { _ = root.RecordScratchRestore(context.WithoutCancel(ctx), id, report) }()
}

func (store scratchStore) Save(ctx context.Context, program string, manifest rlm.SnapshotManifest) error {
	node := store.node
	if node.root == nil || node.id == "" {
		return nil
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return node.root.SaveAgentScratch(ctx, node.id, program, encoded)
}

func (runtime *RecursiveRuntime) newNode(value *agent.Agent, parentID, name string, capabilities []string, authority capability.Authority) (*AgentSession, error) {
	node := &AgentSession{
		runtime: runtime, agent: value, parentID: parentID, name: name,
		capabilities: append([]string(nil), capabilities...), authority: authority,
	}
	host := &recursiveHost{session: node}
	kernel, err := rlm.NewKernel(rlm.KernelOptions{
		Command: runtime.command, Limits: runtime.limits, Manager: runtime.kernels, Host: host, Scratch: scratchStore{node: node},
		OnRestore: node.recordScratchRestore, OnHostCall: node.emitHostCall,
	})
	if err != nil {
		return nil, err
	}
	node.host, node.kernel = host, kernel
	value.SetExclusiveTool(rlm.Tool(kernel), "rlm")
	if runtime.root != nil {
		node.bindPresentation(runtime.root)
	}
	return node, nil
}

func (runtime *RecursiveRuntime) Bind(root *Session) error {
	if root == nil {
		return errors.New("recursive runtime requires a daemon root")
	}
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		return errors.New("recursive runtime is closed")
	}
	runtime.root = root
	node := runtime.rootNode
	node.root, node.id, node.authority = root, root.AgentID(), root.authority
	node.capabilities = []string{"read", "write", "shell", "browser", "computer"}
	runtime.agents[node.id] = node
	runtime.mu.Unlock()
	if err := root.ConfigureModelPricing(runtime.pricing[0], runtime.pricing[1], runtime.pricing[2]); err != nil {
		return err
	}
	node.agent.SetModelCallBudget(agentModelBudget{node: node})
	node.agent.TransformInput = node.host.focusInput
	return runtime.restoreChildren()
}

func (runtime *RecursiveRuntime) RootTool() tools.Tool { return rlm.Tool(runtime.rootNode.kernel) }

func (runtime *RecursiveRuntime) ConfigureRun(system string) {
	node := runtime.rootNode
	node.host.mu.Lock()
	node.host.system = system
	node.host.mu.Unlock()
	if system != "" {
		node.agent.SetSystemPrompt(system)
	}
}

func (runtime *RecursiveRuntime) Close() {
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		return
	}
	runtime.closed = true
	nodes := make([]*AgentSession, 0, len(runtime.agents))
	for _, node := range runtime.agents {
		nodes = append(nodes, node)
	}
	runtime.mu.Unlock()
	for _, node := range nodes {
		node.close(node != runtime.rootNode)
	}
}

func (runtime *RecursiveRuntime) WakeAgent(agentID string) {
	runtime.mu.RLock()
	node := runtime.agents[agentID]
	runtime.mu.RUnlock()
	if node != nil && node != runtime.rootNode {
		node.wake()
	}
}

func (runtime *RecursiveRuntime) HasRunningAgents() bool {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	for _, node := range runtime.agents {
		node.mu.Lock()
		running := node.running
		node.mu.Unlock()
		if running {
			return true
		}
	}
	return false
}

func (runtime *RecursiveRuntime) SetExternalPermissions(enabled bool) {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	for _, node := range runtime.agents {
		if node.agent.Services != nil {
			node.agent.Services.SetExternalPermissions(enabled)
		}
	}
}

func (runtime *RecursiveRuntime) PermissionResolver(agentID string) clientPermissionRunner {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	node := runtime.agents[agentID]
	if node == nil || node.agent.Services == nil {
		return nil
	}
	return node.agent.Services
}

func (runtime *RecursiveRuntime) ControlAgent(ctx context.Context, id, status string) error {
	if status != "stopped" && status != "deleted" {
		return errors.New("agent control requires stopped or deleted status")
	}
	if _, err := runtime.root.store.TerminalizeSubtree(ctx, runtime.root.ID(), runtime.root.AgentID(), id, status); err != nil {
		return err
	}
	runtime.closeTerminalizedAgents(id, status)
	return nil
}

func (runtime *RecursiveRuntime) CancelAgentTurn(id string) bool {
	runtime.mu.RLock()
	node := runtime.agents[id]
	runtime.mu.RUnlock()
	if node == nil || node == runtime.rootNode {
		return false
	}
	node.mu.Lock()
	defer node.mu.Unlock()
	if !node.running || node.cancel == nil {
		return false
	}
	node.cancel()
	return true
}

func (runtime *RecursiveRuntime) restoreChildren() error {
	records, err := runtime.root.LoadRetainedAgents(context.Background())
	if err != nil {
		return err
	}
	pending := append([]sessionstore.RuntimeAgent(nil), records...)
	for len(pending) > 0 {
		progress := false
		next := pending[:0]
		for _, record := range pending {
			runtime.mu.RLock()
			parent := runtime.agents[record.ParentID]
			runtime.mu.RUnlock()
			if parent == nil {
				next = append(next, record)
				continue
			}
			authority, capabilities, err := runtime.root.LoadAgentAuthority(context.Background(), record.ID)
			if err != nil {
				return err
			}
			services, err := parent.agent.Services.CloneForAuthority(runtime.root.store, runtime.root.store.Workspaces(), runtime.root.store.Processes(), authority)
			if err != nil {
				return err
			}
			arguments := map[string]any{"effort": record.Effort}
			if record.Model != "" && (record.Model != parent.agent.ModelName || record.Provider != parent.agent.Provider) {
				arguments["model"] = record.Model
				arguments["provider"] = record.Provider
			}
			child, _, _, err := cloneRuntimeAgent(parent.agent, services, arguments)
			if err != nil {
				services.Close()
				return err
			}
			if record.CWD != "" {
				child.WorkingDir = record.CWD
			}
			node, err := runtime.newNode(child, record.ParentID, record.Name, capabilities, authority)
			if err != nil {
				services.Close()
				return err
			}
			node.id, node.root = record.ID, runtime.root
			child.SetSessionID(runtime.root.ID() + "/" + record.ID)
			child.SetModelCallBudget(agentModelBudget{node: node})
			child.TransformInput = node.host.focusInput
			child.SetSystemPrompt(node.systemPrompt(nil))
			transcript, err := runtime.root.LoadAgentTranscript(context.Background(), record.ID)
			if err != nil {
				node.close(true)
				return err
			}
			node.host.history = append([]llm.Message(nil), transcript...)
			child.Messages = append(child.Messages[:1], rlm.FocusedHistory(transcript)...)
			runtime.mu.Lock()
			runtime.agents[node.id] = node
			runtime.mu.Unlock()
			progress = true
		}
		if !progress {
			return errors.New("retained agent tree contains a missing parent")
		}
		pending = next
	}
	for _, record := range records {
		pending, err := runtime.agents[record.ID].root.HasAgentWork(context.Background(), record.ID)
		if err != nil {
			return err
		}
		if pending {
			runtime.agents[record.ID].wake()
		}
	}
	return nil
}

type agentModelBudget struct{ node *AgentSession }

func (budget agentModelBudget) ReserveModelCall(ctx context.Context, amount int64) (func(llm.Usage) error, error) {
	return budget.node.root.ReserveAgentModelCall(ctx, budget.node.id, amount)
}

func (node *AgentSession) close(closeAgent bool) {
	node.mu.Lock()
	if node.closed {
		node.mu.Unlock()
		return
	}
	node.closed = true
	if node.cancel != nil {
		node.cancel()
	}
	node.mu.Unlock()
	if node.kernel != nil {
		node.kernel.Close()
	}
	if closeAgent {
		if node.agent.Services != nil {
			node.agent.Services.Close()
		}
	}
}

func (node *AgentSession) wake() {
	node.mu.Lock()
	if node.closed {
		node.mu.Unlock()
		return
	}
	if node.running {
		node.mu.Unlock()
		return
	}
	node.running = true
	node.mu.Unlock()
	node.launch()
}

func (node *AgentSession) launch() {
	if node.root.LaunchRuntimeWorker("agent "+node.id, node.run) {
		return
	}
	node.mu.Lock()
	node.running = false
	node.mu.Unlock()
}

func (node *AgentSession) run() {
	ctx, cancel := context.WithCancel(context.Background())
	node.mu.Lock()
	if node.closed {
		node.running = false
		node.mu.Unlock()
		cancel()
		return
	}
	node.cancel = cancel
	node.mu.Unlock()
	defer cancel()

	turnID := agentTurnID(node.id)
	var items []agentTurnItem
	started := false
	output, turnErr := node.RunTurn(ctx, "", nil, true, nil, nil, func(turnCtx context.Context) (string, error) {
		var err error
		_, items, err = node.root.StartAgentTurn(turnCtx, node.id, turnID)
		if err != nil {
			return "", err
		}
		started = true
		// A mailbox-triggered turn claims nothing; RunTurn supplies the digest.
		return explicitInput(items), nil
	})
	if !started {
		node.finishLiveTurn()
		// A wake that arrived while this node looked busy was dropped by
		// wake(); reconcile queued work across the tree before returning.
		node.runtime.wakeQueuedAgents()
		return
	}
	status := "succeeded"
	if errors.Is(ctx.Err(), context.Canceled) {
		status = "cancelled"
	} else if turnErr != nil {
		status = "failed"
	}
	journal := node.turnJournal()
	var ack []int64
	if status == "succeeded" {
		for _, item := range items {
			ack = append(ack, item.Seq)
		}
		ack = append(ack, journal.DeliveredInbox...)
	}
	finishErr := node.root.FinishAgentTurn(context.Background(), node.id, sessionstore.AgentTurnCommit{
		TurnID: turnID, Status: status, AcknowledgedInbox: ack, DeliveredMessages: journal.DeliveredMessages,
		Transcript: node.agent.MessagesSnapshot(), Error: errorText(turnErr),
	})
	node.finishLiveTurn()
	node.postCompletionNotice(status, output, errors.Join(turnErr, finishErr))
	switch {
	case status == "succeeded" && finishErr == nil:
		node.mu.Lock()
		node.failures = 0
		node.mu.Unlock()
		if pending, err := node.root.HasAgentWork(context.Background(), node.id); err == nil && pending {
			node.wake()
		}
		node.runtime.wakeQueuedAgents()
	case status == "failed":
		// FinishAgentTurn returned this turn's claimed items to the queue.
		// Re-wake after a backoff so a persistent provider error cannot
		// hot-loop; a cancelled turn is user intent and is not re-woken.
		node.scheduleRetryWake()
	}
	node.root.applyPendingReloadAfterAgent()
}

// scheduleRetryWake re-wakes this node after an exponential backoff
// (2s, 4s, ... capped at 64s) if it still has queued work.
func (node *AgentSession) scheduleRetryWake() {
	node.mu.Lock()
	node.failures++
	attempt := node.failures
	node.mu.Unlock()
	delay := time.Duration(1<<min(attempt, 6)) * time.Second
	time.AfterFunc(delay, func() {
		if pending, err := node.root.HasAgentWork(context.Background(), node.id); err == nil && pending {
			node.wake()
		}
	})
}

func (node *AgentSession) finishLiveTurn() {
	node.mu.Lock()
	node.running = false
	node.cancel = nil
	node.mu.Unlock()
}

// explicitInput joins the claimed inbox bodies (one, under one-at-a-time
// claiming) into the turn's input text.
func explicitInput(items []agentTurnItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(string(item.Body)); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// agentNoticePreviewBytes bounds the last-text preview a parent receives when
// a child turn ends; the full text travels as an evidence handle.
const agentNoticePreviewBytes = 160

// postCompletionNotice tells the parent how a child turn ended as an ordinary
// runtime-authored mailbox message, so child lifecycle and peer messages share
// one canonical table and one delivery path.
func (node *AgentSession) postCompletionNotice(status, output string, failure error) {
	if node.parentID == "" || node.root == nil {
		return
	}
	ctx := context.Background()
	kind := sessionstore.MessageKindAgentCompleted
	switch status {
	case "failed":
		kind = sessionstore.MessageKindAgentFailed
	case "cancelled":
		kind = sessionstore.MessageKindAgentCancelled
	}
	previewBytes := agentNoticePreviewBytes
	switch node.report {
	case "message":
		// The child reports explicitly; only failures still surface.
		if status == "succeeded" {
			return
		}
	case "inline":
		previewBytes = agentNoticeInlineBytes
	}
	preview := strings.TrimSpace(output)
	evidence := ""
	if len(preview) > previewBytes {
		if stored, err := node.root.StoreContent(ctx, node.id, sessionstore.RuntimePayload{
			Data: []byte(output), MediaType: "text/plain", Source: "agent final text",
		}); err == nil {
			evidence = stored.ReferenceID
		}
		preview = utf8PrefixRuntime(preview, previewBytes) + "…"
	}
	body := fmt.Sprintf("agent %s (%s) %s", node.name, node.id, status)
	if failure != nil {
		body += ": " + failure.Error()
	}
	if preview != "" {
		body += "\nlast text: " + preview
	}
	// One pending notice per child: a later turn replaces an unread one.
	_, _ = node.root.SendMailboxMessage(ctx, node.id, node.parentID, sessionstore.MailboxSend{
		Kind: kind, Delivery: sessionstore.MessageDeliveryQueued, Subject: node.name + " " + status,
		Body: body, EvidenceReferenceID: evidence, UpsertKey: "agent.turn:" + node.id,
	})
}

// agentNoticeInlineBytes is the preview cap for report="inline" children.
const agentNoticeInlineBytes = 4 << 10

// resolveRecipient accepts "parent", a direct relative's name, or an agent
// id. Messages travel one hop, so there is no "root" alias: below depth one it
// could only fail the relative check.
func (node *AgentSession) resolveRecipient(ctx context.Context, recipient string) (string, error) {
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return "", errors.New("recipient is required")
	}
	relatives, err := node.root.ListAgentRelatives(ctx, node.id)
	if err != nil {
		return "", err
	}
	if recipient == "parent" {
		if relatives.Parent == nil {
			return "", errors.New("the root agent has no parent")
		}
		return relatives.Parent.ID, nil
	}
	candidates := append(append([]sessionstore.RuntimeAgent(nil), relatives.Children...), relatives.Siblings...)
	if relatives.Parent != nil {
		candidates = append(candidates, *relatives.Parent)
	}
	for _, candidate := range candidates {
		if candidate.ID == recipient {
			return recipient, nil
		}
	}
	for _, candidate := range candidates {
		if candidate.Name == recipient {
			return candidate.ID, nil
		}
	}
	return recipient, nil
}

// WakeQueuedAgents re-derives readiness for every retained descendant from
// durable state and wakes those with runnable work; deferred mail arms a
// timer. It is the reconciliation point for lost in-memory wakes.
func (runtime *RecursiveRuntime) WakeQueuedAgents() { runtime.wakeQueuedAgents() }

func (runtime *RecursiveRuntime) wakeQueuedAgents() {
	runtime.mu.RLock()
	nodes := make([]*AgentSession, 0, len(runtime.agents))
	for _, node := range runtime.agents {
		if node != runtime.rootNode {
			nodes = append(nodes, node)
		}
	}
	runtime.mu.RUnlock()
	for _, node := range nodes {
		work, err := node.root.AgentWorkStatus(context.Background(), node.id)
		if err != nil {
			continue
		}
		switch {
		case work.HasExplicitInput || work.HasReadyMail:
			node.wake()
		case !work.NextDeferredAt.IsZero():
			time.AfterFunc(max(time.Until(work.NextDeferredAt), 0)+time.Second, func() {
				if pending, err := node.root.HasAgentWork(context.Background(), node.id); err == nil && pending {
					node.wake()
				}
			})
		}
	}
}

func (host *recursiveHost) Call(ctx context.Context, module, operation string, arguments map[string]any) (any, error) {
	node := host.session
	if node.root == nil {
		return nil, errors.New("RLM host is not bound")
	}
	switch module {
	case "context":
		return host.context(ctx, operation, arguments)
	case "files":
		return host.files(ctx, operation, arguments)
	case "shell":
		return host.shell(ctx, operation, arguments)
	case "browser":
		if operation != "run" {
			return nil, fmt.Errorf("unknown browser operation %q", operation)
		}
		return host.invoke(ctx, "browser_exec", arguments)
	case "computer":
		if operation != "run" {
			return nil, fmt.Errorf("unknown computer operation %q", operation)
		}
		return host.invoke(ctx, "computer_exec", arguments)
	case "models":
		return host.models(ctx, operation, arguments)
	case "agents":
		return host.agents(ctx, operation, arguments)
	case "messages":
		return host.messages(ctx, operation, arguments)
	case "mcp":
		return host.mcp(ctx, operation, arguments)
	case "state":
		return host.state(ctx, operation, arguments)
	case "artifacts":
		return host.artifacts(ctx, operation, arguments)
	case "schedules":
		return host.schedules(ctx, operation, arguments)
	case "permissions":
		if operation == "request" {
			return map[string]any{"status": "invoke_operation", "message": "invoke the exact operation to create a durable permission request"}, nil
		}
		if operation == "status" {
			id, _ := stringArgument(arguments, "id")
			return node.root.InspectPermission(ctx, id)
		}
		return nil, fmt.Errorf("unknown permissions operation %q", operation)
	default:
		return nil, fmt.Errorf("unknown RLM module %q", module)
	}
}

func (host *recursiveHost) focusInput(ctx context.Context, input string) (string, error) {
	node := host.session
	host.mu.Lock()
	focused := host.focused
	host.mu.Unlock()
	if !focused {
		var handle *rlm.ContextHandle
		host.mu.Lock()
		history := append([]llm.Message(nil), host.history...)
		system := host.system
		host.mu.Unlock()
		if len(history) > 0 {
			data, err := rlm.MarshalHistory(history)
			if err != nil {
				return "", err
			}
			value, err := node.root.StoreContent(ctx, node.id, sessionstore.RuntimePayload{Data: data, MediaType: "application/json", Source: "full conversation history"})
			if err != nil {
				return "", err
			}
			handle = &rlm.ContextHandle{ReferenceID: value.ReferenceID, Size: value.Size, Source: value.Source}
		}
		if system == "" {
			system = rlm.BuildPrompt(node.agent.WorkingDir, handle) + rlm.IdentityBlock(node.identity())
		} else if node.parentID != "" {
			// An explicit -system override stays verbatim for the root only.
			system += rlm.IdentityBlock(node.identity())
		}
		node.agent.SetSystemPrompt(system)
		host.mu.Lock()
		host.handle, host.history, host.focused = handle, nil, true
		host.mu.Unlock()
	}
	if len(input) <= sessionstore.InlineValueLimit {
		return input, nil
	}
	value, err := node.root.StoreContent(ctx, node.id, sessionstore.RuntimePayload{Data: []byte(input), MediaType: "text/plain", Source: "agent input"})
	if err != nil {
		return "", err
	}
	const head, tail = 4 << 10, 2 << 10
	prefix := utf8PrefixRuntime(input, head)
	suffix, suffixStart := utf8SuffixRuntime(input, tail)
	return fmt.Sprintf("%s\n\n[Input continues in context handle %s; size=%d; shown spans=0:%d,%d:%d.]\n\n%s",
		prefix, value.ReferenceID, value.Size, len(prefix), suffixStart, len(input), suffix), nil
}

func (host *recursiveHost) context(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	reference, _ := stringArgument(arguments, "handle")
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
	node := host.session
	switch operation {
	case "inspect":
		_, metadata, err := node.root.ReadContent(ctx, node.id, reference, 0, 0)
		return contentMetadataMap(metadata), err
	case "read":
		offset := int64Argument(arguments, "offset", 0)
		length := min(intArgument(arguments, "length", sessionstore.InlineValueLimit), sessionstore.InlineValueLimit)
		body, metadata, err := node.root.ReadContent(ctx, node.id, reference, offset, length)
		if err != nil {
			return nil, err
		}
		return map[string]any{"text": string(body), "source": metadata.Source, "handle": reference, "span": map[string]any{"start": offset, "end": offset + int64(len(body))}, "size": metadata.Size}, nil
	case "search":
		query, _ := stringArgument(arguments, "query")
		if query == "" {
			return nil, errors.New("query is required")
		}
		return host.searchContent(ctx, reference, query)
	default:
		return nil, fmt.Errorf("unknown context operation %q", operation)
	}
}

func (host *recursiveHost) searchContent(ctx context.Context, reference, query string) (any, error) {
	const maxScan = 8 << 20
	var offset int64
	var matches []map[string]any
	var metadata sessionstore.ContentMetadata
	for offset < maxScan && len(matches) < 20 {
		body, current, err := host.session.root.ReadContent(ctx, host.session.id, reference, offset, sessionstore.MaxContentRead)
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

func (host *recursiveHost) files(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	tool := operation
	switch operation {
	case "read", "write":
	case "patch":
		tool = "edit"
		arguments = cloneArguments(arguments)
		arguments["old_string"] = arguments["old"]
		arguments["new_string"] = arguments["new"]
	case "list":
		path, _ := stringArgument(arguments, "path")
		if path == "" {
			path = "."
		}
		arguments, tool = map[string]any{"path": path, "_rlm_mode": "list"}, "read"
	case "search":
		query, _ := stringArgument(arguments, "query")
		path, _ := stringArgument(arguments, "path")
		if query == "" {
			return nil, errors.New("query is required")
		}
		if path == "" {
			path = "."
		}
		arguments, tool = map[string]any{"path": path, "query": query, "_rlm_mode": "search"}, "read"
	default:
		return nil, fmt.Errorf("unknown files operation %q", operation)
	}
	return host.invoke(ctx, tool, arguments)
}

// maxJobWaitMS bounds shell.wait like agents.wait: the wait is a host call the
// cell clock does not charge, but a blocked cell still holds a kernel slot.
const maxJobWaitMS = 25000

func (host *recursiveHost) shell(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	node := host.session
	switch operation {
	case "run":
		return host.invoke(ctx, "bash", arguments)
	case "read":
		return host.context(ctx, "read", arguments)
	case "start":
		data, err := json.Marshal(arguments)
		if err != nil {
			return nil, err
		}
		output, err := node.agent.Services.Invoke(ctx, "shell_start", data)
		if err != nil {
			return nil, err
		}
		var status tools.JobStatus
		if err := json.Unmarshal([]byte(output), &status); err != nil {
			return nil, err
		}
		return host.jobView(ctx, status, false)
	case "poll", "kill", "wait":
		id, _ := stringArgument(arguments, "id")
		status, ok := node.agent.Services.JobStatus(id)
		if !ok {
			return nil, fmt.Errorf("no such job %q (jobs do not survive a daemon restart)", id)
		}
		if operation == "kill" {
			if err := node.agent.Services.KillJob(id); err != nil {
				return nil, err
			}
		}
		timedOut := false
		if operation == "wait" && status.Running {
			job, _ := node.agent.Services.Job(id)
			timeout := time.Duration(min(max(intArgument(arguments, "timeout_ms", 10000), 0), maxJobWaitMS)) * time.Millisecond
			select {
			case <-job.Done():
			case <-time.After(timeout):
				timedOut = true
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if operation == "kill" {
			if job, _ := node.agent.Services.Job(id); job != nil {
				select {
				case <-job.Done():
				case <-time.After(2 * time.Second):
				}
			}
		}
		status, _ = node.agent.Services.JobStatus(id)
		view, err := host.jobView(ctx, status, !status.Running)
		if err == nil && operation == "wait" {
			view["timed_out"] = timedOut
		}
		return view, err
	case "tail":
		id, _ := stringArgument(arguments, "id")
		job, ok := node.agent.Services.Job(id)
		if !ok {
			return nil, fmt.Errorf("no such job %q (jobs do not survive a daemon restart)", id)
		}
		limit := min(max(intArgument(arguments, "bytes", 4096), 1), sessionstore.InlineValueLimit)
		text, total := job.Output(limit)
		return map[string]any{"id": id, "running": job.Running(), "tail": text, "bytes": total}, nil
	case "list":
		statuses := node.agent.Services.Jobs()
		result := make([]any, 0, len(statuses))
		for _, status := range statuses {
			view, err := host.jobView(ctx, status, false)
			if err != nil {
				return nil, err
			}
			result = append(result, view)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unknown shell operation %q", operation)
	}
}

// jobView renders a job for the model; a finished job carries its output as
// inline text or, above the inline limit, as a handle with a preview.
func (host *recursiveHost) jobView(ctx context.Context, status tools.JobStatus, withOutput bool) (map[string]any, error) {
	view := map[string]any{
		"id": status.ID, "pid": status.PID, "command": status.Command, "running": status.Running,
		"bytes": status.Bytes, "started_at": status.StartedAt.UTC().Format(time.RFC3339),
	}
	if status.Exit != "" {
		view["exit"] = status.Exit
	}
	if status.Killed {
		view["killed"] = true
	}
	if !status.EndedAt.IsZero() {
		view["ended_at"] = status.EndedAt.UTC().Format(time.RFC3339)
	}
	if withOutput {
		if job, ok := host.session.agent.Services.Job(status.ID); ok {
			text, _ := job.Output(0)
			bounded, err := host.boundedText(ctx, "shell job output", text)
			if err != nil {
				return nil, err
			}
			maps.Copy(view, bounded)
		}
	}
	return view, nil
}

func (host *recursiveHost) invoke(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	data, err := json.Marshal(arguments)
	if err != nil {
		return nil, err
	}
	output, callErr := host.session.agent.Services.Invoke(ctx, operation, data)
	result, boundErr := host.boundedText(ctx, operation+" output", output)
	if callErr != nil {
		return result, callErr
	}
	return result, boundErr
}

func (host *recursiveHost) models(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	node := host.session
	call := func(prompt string, maxTokens int) map[string]any {
		output, usage, err := node.complete(ctx, prompt, maxTokens)
		result, boundErr := host.boundedText(ctx, "stateless model output", output)
		if result == nil {
			result = make(map[string]any)
		}
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
		prompt, _ := stringArgument(arguments, "prompt")
		if prompt == "" {
			return nil, errors.New("prompt is required")
		}
		return call(prompt, intArgument(arguments, "max_tokens", 0)), nil
	case "batch":
		items, ok := arguments["prompts"].([]any)
		if !ok || len(items) == 0 {
			return nil, errors.New("prompts must be a non-empty list")
		}
		results := make([]map[string]any, len(items))
		maxTokens := intArgument(arguments, "max_tokens", 0)
		var calls sync.WaitGroup
		for index, item := range items {
			prompt, ok := item.(string)
			if !ok {
				results[index] = map[string]any{"error": "prompt is not a string"}
				continue
			}
			calls.Go(func() { results[index] = call(prompt, maxTokens) })
		}
		calls.Wait()
		return results, nil
	default:
		return nil, fmt.Errorf("unknown models operation %q", operation)
	}
}

func (host *recursiveHost) agents(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	node := host.session
	switch operation {
	case "spawn":
		prompt, _ := stringArgument(arguments, "prompt")
		if prompt == "" {
			return nil, errors.New("prompt is required")
		}
		name, _ := stringArgument(arguments, "name")
		if name == "" {
			name = "agent-" + randomRuntimeSuffix()
		}
		return node.runtime.spawn(ctx, node, name, prompt, arguments)
	case "submit":
		id, _ := stringArgument(arguments, "id")
		text, _ := stringArgument(arguments, "text")
		delivery, _ := stringArgument(arguments, "delivery")
		return node.runtime.submit(ctx, node, id, text, delivery)
	case "wait":
		ids, err := stringListArgument(arguments, "ids")
		if err != nil {
			return nil, err
		}
		return node.runtime.wait(ctx, node, ids, intArgument(arguments, "timeout_ms", 10000))
	case "list":
		return node.root.ListAgentRelatives(ctx, node.id)
	case "inspect":
		id, _ := stringArgument(arguments, "id")
		return node.runtime.inspect(ctx, node, id)
	case "stop", "delete":
		id, _ := stringArgument(arguments, "id")
		return node.runtime.terminalize(ctx, node, id, operation)
	default:
		return nil, fmt.Errorf("unknown agents operation %q", operation)
	}
}

func (runtime *RecursiveRuntime) spawn(ctx context.Context, parent *AgentSession, name, prompt string, arguments map[string]any) (any, error) {
	capabilities, err := requestedCapabilities(arguments["capabilities"], parent.capabilities)
	if err != nil {
		return nil, err
	}
	budgets, err := requestedBudgets(arguments["budgets"])
	if err != nil {
		return nil, err
	}
	id := parent.root.ID() + ":" + randomRuntimeSuffix()
	authority := capability.Authority{
		RootID: parent.root.ID(), AgentID: id,
		Files: capability.Reference{ID: "files:" + id, Generation: 1},
		Shell: capability.Reference{ID: "shell:" + id, Generation: 1},
	}
	delegations := capabilityDelegations(parent, authority, capabilities)
	services, err := parent.agent.Services.CloneForAuthority(parent.root.store, parent.root.store.Workspaces(), parent.root.store.Processes(), authority)
	if err != nil {
		return nil, err
	}
	child, modelName, providerName, err := cloneRuntimeAgent(parent.agent, services, arguments)
	if err != nil {
		services.Close()
		return nil, err
	}
	node, err := runtime.newNode(child, parent.id, name, capabilities, authority)
	if err != nil {
		services.Close()
		return nil, err
	}
	report, _ := stringArgument(arguments, "report")
	switch report {
	case "", "notice", "message", "inline":
	default:
		node.close(true)
		return nil, fmt.Errorf("unknown report mode %q (notice, message, or inline)", report)
	}
	node.id, node.root, node.report = id, parent.root, report
	child.SetSessionID(parent.root.ID() + "/" + id)
	child.SetModelCallBudget(agentModelBudget{node: node})
	child.TransformInput = node.host.focusInput
	child.SetSystemPrompt(node.systemPrompt(nil))
	task := fmt.Sprintf("[task from parent %s (%s)]\n\n%s", parent.name, parent.id, prompt)
	if err := parent.root.AdmitAgent(ctx, sessionstore.AgentAdmission{
		ParentAgentID: parent.id, ChildAgentID: id, Name: name,
		Model: modelName, Provider: providerName, Effort: child.Effort, CWD: child.WorkingDir,
		Prompt:       sessionstore.RuntimePayload{Data: []byte(task), MediaType: "text/plain", Source: "initial agent prompt"},
		Capabilities: delegations, Budgets: budgets,
	}); err != nil {
		node.close(true)
		return nil, err
	}
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		node.close(true)
		_ = parent.root.TerminalizeSubtree(ctx, parent.id, id, "deleted")
		return nil, errors.New("recursive runtime is closed")
	}
	runtime.agents[id] = node
	runtime.mu.Unlock()
	node.wake()
	effectiveBudgets, _ := parent.root.InspectBudgets(ctx, parent.id, id)
	return map[string]any{
		"id": id, "name": name, "parent_id": parent.id, "status": "queued",
		"effective_capabilities": capabilities, "effective_budgets": effectiveBudgets,
	}, nil
}

func cloneRuntimeAgent(parent *agent.Agent, services *tools.Services, arguments map[string]any) (*agent.Agent, string, string, error) {
	client, modelID := parent.Client, parent.Model
	contextLimit, maxTokens, effort, vision := parent.ContextLimit, parent.MaxTokens, parent.Effort, parent.Vision
	modelName, _ := stringArgument(arguments, "model")
	providerName, _ := stringArgument(arguments, "provider")
	if modelName == "" && providerName != "" {
		return nil, "", "", errors.New("provider override requires a model override")
	}
	requestedEffort, _ := stringArgument(arguments, "effort")
	effectiveModel, effectiveProvider := parent.ModelName, parent.Provider
	if modelName != "" {
		if parent.ResolveModel == nil {
			return nil, "", "", errors.New("model overrides are unavailable")
		}
		resolved, err := parent.ResolveModel(modelName, providerName)
		if err != nil {
			return nil, "", "", err
		}
		client, modelID = resolved.Client, resolved.Model
		effectiveModel, effectiveProvider = resolved.ModelName, resolved.Provider
		if effectiveModel == "" {
			effectiveModel = modelName
		}
		if effectiveProvider == "" {
			return nil, "", "", errors.New("model override resolved without a provider")
		}
		if resolved.ContextLimit > 0 {
			contextLimit = resolved.ContextLimit
		}
		if resolved.MaxTokens > 0 {
			maxTokens = resolved.MaxTokens
		}
		if resolved.Effort != "" {
			effort = resolved.Effort
		}
		vision = resolved.Vision
	}
	if requestedEffort != "" {
		effort = requestedEffort
	}
	copyClient := *client
	child := agent.NewRuntime(&copyClient, modelID, maxTokens, rlm.BuildPrompt(parent.WorkingDir, nil), services)
	child.ModelName, child.Provider = effectiveModel, effectiveProvider
	child.ContextLimit, child.Effort = contextLimit, effort
	child.Vision = vision
	child.Temperature, child.TopP = parent.Temperature, parent.TopP
	child.CompactClient, child.CompactModel, child.CompactThreshold = parent.CompactClient, parent.CompactModel, parent.CompactThreshold
	child.WorkingDir = parent.WorkingDir
	child.ResolveModel = parent.ResolveModel
	return child, child.ModelName, child.Provider, nil
}

func capabilityDelegations(parent *AgentSession, child capability.Authority, names []string) []sessionstore.CapabilityDelegation {
	var fileOps, shellOps []string
	for _, name := range names {
		switch name {
		case "read":
			fileOps = append(fileOps, "read")
		case "write":
			fileOps = append(fileOps, "read", "write", "edit", "workspace.write")
		case "shell":
			shellOps = append(shellOps, "bash", "shell_start", "workspace_process")
		case "browser":
			shellOps = append(shellOps, "browser_exec")
		case "computer":
			shellOps = append(shellOps, "computer_exec")
		}
	}
	sort.Strings(fileOps)
	fileOps = slices.Compact(fileOps)
	sort.Strings(shellOps)
	shellOps = slices.Compact(shellOps)
	var result []sessionstore.CapabilityDelegation
	if len(fileOps) > 0 {
		result = append(result, sessionstore.CapabilityDelegation{ID: child.Files.ID, Issuer: parent.authority.Files, AgentID: child.AgentID, Operations: fileOps, Scopes: []string{parent.root.WorkingDirectory()}})
	}
	if len(shellOps) > 0 {
		result = append(result, sessionstore.CapabilityDelegation{ID: child.Shell.ID, Issuer: parent.authority.Shell, AgentID: child.AgentID, Operations: shellOps})
	}
	return result
}

// submit lets a parent send follow-up work to a direct child. The default
// delivery is steer: the text joins the child's running turn at its next loop
// boundary, or starts a turn if the child is idle.
func (runtime *RecursiveRuntime) submit(ctx context.Context, caller *AgentSession, id, text, delivery string) (any, error) {
	text = strings.TrimSpace(text)
	if id == "" || text == "" {
		return nil, errors.New("submit requires id and text")
	}
	kind := "steer"
	switch delivery {
	case "", "steer":
	case "queued":
		kind = "submit"
	default:
		return nil, fmt.Errorf("unknown delivery %q (steer or queued)", delivery)
	}
	relatives, err := caller.root.ListAgentRelatives(ctx, caller.id)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(relatives.Children, func(child sessionstore.RuntimeAgent) bool { return child.ID == id }) {
		return nil, sessionstore.ErrAgentAccess
	}
	seq, err := caller.root.SubmitAgentInput(ctx, caller.id, id, kind, text, "parent follow-up")
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "inbox_seq": seq, "kind": kind, "status": "queued"}, nil
}

// maxAgentWaitMS bounds how long a blocked cell may hold its kernel pool slot.
// Host-call time is not charged to the cell clock, so this is pool fairness,
// not deadline safety.
// ponytail: fixed cap; make it configurable if pools grow beyond a handful of slots.
const maxAgentWaitMS = 25000

// wait polls direct children until each is idle with no runnable work, or the
// timeout passes. It is bounded so a kernel cell cannot block forever.
func (runtime *RecursiveRuntime) wait(ctx context.Context, caller *AgentSession, ids []string, timeoutMS int) (any, error) {
	if len(ids) == 0 {
		return nil, errors.New("wait requires ids")
	}
	timeout := time.Duration(min(max(timeoutMS, 0), maxAgentWaitMS)) * time.Millisecond
	deadline := time.Now().Add(timeout)
	for {
		relatives, err := caller.root.ListAgentRelatives(ctx, caller.id)
		if err != nil {
			return nil, err
		}
		result := make(map[string]any, len(ids))
		settled := true
		for _, id := range ids {
			index := slices.IndexFunc(relatives.Children, func(child sessionstore.RuntimeAgent) bool { return child.ID == id })
			if index < 0 {
				return nil, sessionstore.ErrAgentAccess
			}
			child := relatives.Children[index]
			work, err := caller.root.AgentWorkStatus(ctx, id)
			if err != nil {
				return nil, err
			}
			busy := child.Status == "running" || child.Status == "queued" || work.HasExplicitInput || work.HasReadyMail
			if child.LifecyclePhase == "terminal" {
				busy = false
			}
			settled = settled && !busy
			result[id] = map[string]any{"status": child.Status, "busy": busy, "pending_mail": child.PendingMail}
		}
		if settled || time.Now().After(deadline) {
			return map[string]any{"agents": result, "settled": settled, "timed_out": !settled, "timeout_ms": int(timeout / time.Millisecond)}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (runtime *RecursiveRuntime) inspect(ctx context.Context, caller *AgentSession, id string) (any, error) {
	if id == "" {
		return nil, errors.New("agent id is required")
	}
	relatives, err := caller.root.ListAgentRelatives(ctx, caller.id)
	if err != nil {
		return nil, err
	}
	var found *sessionstore.RuntimeAgent
	if relatives.Parent != nil && relatives.Parent.ID == id {
		found = relatives.Parent
	}
	for _, values := range [][]sessionstore.RuntimeAgent{relatives.Children, relatives.Siblings} {
		for index := range values {
			if values[index].ID == id {
				value := values[index]
				found = &value
			}
		}
	}
	if found == nil {
		return nil, sessionstore.ErrAgentAccess
	}
	summary, _ := caller.root.MailboxSummary(ctx, id)
	budgets, _ := caller.root.InspectBudgets(ctx, caller.id, id)
	return map[string]any{
		"id": found.ID, "name": found.Name, "parent_id": found.ParentID, "status": found.Status,
		"model": found.Model, "provider": found.Provider, "effort": found.Effort, "cwd": found.CWD,
		"unread_messages": summary.UnreadCount, "budgets": budgets,
	}, nil
}

func (runtime *RecursiveRuntime) terminalize(ctx context.Context, caller *AgentSession, id, operation string) (any, error) {
	runtime.mu.RLock()
	target := runtime.agents[id]
	runtime.mu.RUnlock()
	if target == nil || target == runtime.rootNode {
		return nil, sessionstore.ErrAgentAccess
	}
	status := "stopped"
	if operation == "delete" {
		status = "deleted"
	}
	if err := caller.root.TerminalizeSubtree(ctx, caller.id, id, status); err != nil {
		return nil, err
	}
	runtime.closeTerminalizedAgents(id, status)
	return map[string]any{"id": id, "status": status}, nil
}

func (runtime *RecursiveRuntime) closeTerminalizedAgents(id, status string) {
	runtime.mu.Lock()
	var subtree []*AgentSession
	queue := []string{id}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if value := runtime.agents[current]; value != nil {
			subtree = append(subtree, value)
		}
		for childID, value := range runtime.agents {
			if value.parentID == current {
				queue = append(queue, childID)
			}
		}
	}
	if status == "deleted" {
		for _, value := range subtree {
			delete(runtime.agents, value.id)
		}
	}
	runtime.mu.Unlock()
	for _, value := range subtree {
		value.close(true)
	}
}

func (host *recursiveHost) messages(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	node := host.session
	switch operation {
	case "send":
		recipient, _ := stringArgument(arguments, "recipient")
		recipient, err := node.resolveRecipient(ctx, recipient)
		if err != nil {
			return nil, err
		}
		subject, _ := stringArgument(arguments, "subject")
		body, _ := stringArgument(arguments, "body")
		evidence, _ := stringArgument(arguments, "evidence_handle")
		delivery, _ := stringArgument(arguments, "delivery")
		message, err := node.root.SendMailboxMessage(ctx, node.id, recipient, sessionstore.MailboxSend{
			Subject: subject, Body: body, EvidenceReferenceID: evidence, Delivery: delivery,
		})
		return map[string]any{
			"id": message.ID, "recipient": recipient, "delivery": message.Delivery, "status": message.Status, "created_at": message.CreatedAt,
		}, err
	case "list":
		status, _ := stringArgument(arguments, "status")
		sender, _ := stringArgument(arguments, "sender")
		messages, err := node.root.ListMailboxMessages(ctx, node.id, status, sender, intArgument(arguments, "limit", 50))
		if err != nil {
			return nil, err
		}
		result := make([]map[string]any, 0, len(messages))
		for _, message := range messages {
			result = append(result, map[string]any{
				"id": message.ID, "sender": message.SenderAgentID, "kind": message.Kind, "delivery": message.Delivery,
				"subject": message.Subject, "excerpt": message.Excerpt, "size": message.Body.Size,
				"evidence_handle": message.EvidenceReferenceID, "status": message.Status, "created_at": message.CreatedAt,
			})
		}
		return result, nil
	case "read":
		id, _ := stringArgument(arguments, "id")
		message, body, err := node.root.ReadMailboxMessage(ctx, node.id, id)
		if err != nil {
			return nil, err
		}
		node.recordDelivered(message.ID)
		return map[string]any{
			"id": message.ID, "sender": message.SenderAgentID, "kind": message.Kind, "delivery": message.Delivery,
			"subject": message.Subject, "body": string(body),
			"evidence_handle": message.EvidenceReferenceID, "status": message.Status, "created_at": message.CreatedAt,
		}, nil
	case "complete", "ack":
		ids, err := stringListArgument(arguments, "ids")
		if err != nil {
			return nil, err
		}
		count, err := node.root.CompleteMailboxMessages(ctx, node.id, ids)
		return map[string]any{"completed": count}, err
	case "defer":
		id, _ := stringArgument(arguments, "id")
		until := time.Time{}
		if value, ok := stringArgument(arguments, "until"); ok && value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return nil, fmt.Errorf("until must be RFC3339: %w", err)
			}
			until = parsed
		} else if seconds := intArgument(arguments, "seconds", 0); seconds > 0 {
			until = time.Now().Add(time.Duration(seconds) * time.Second)
		}
		if until.IsZero() {
			return nil, errors.New("defer requires until (RFC3339) or seconds")
		}
		err := node.root.DeferMailboxMessage(ctx, node.id, id, until)
		return map[string]any{"id": id, "available_at": until.UTC().Format(time.RFC3339)}, err
	default:
		return nil, fmt.Errorf("unknown messages operation %q", operation)
	}
}

func (host *recursiveHost) mcp(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	manager := host.session.runtime.mcp
	if manager == nil {
		return nil, errors.New("no MCP servers are configured")
	}
	switch operation {
	case "list_servers":
		servers := manager.Statuses()
		result := make([]map[string]any, 0, len(servers))
		for _, server := range servers {
			result = append(result, map[string]any{"name": server.Name, "status": server.Status.String(), "error": server.Err, "tools": server.Tools, "source": server.Source})
		}
		return result, nil
	case "list_tools":
		server, _ := stringArgument(arguments, "server")
		return manager.ListTools(server)
	case "call":
		server, _ := stringArgument(arguments, "server")
		tool, _ := stringArgument(arguments, "tool")
		args, ok := arguments["arguments"].(map[string]any)
		if !ok {
			return nil, errors.New("arguments must be a dictionary")
		}
		body, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		output, err := manager.Call(ctx, server, tool, body)
		result, boundErr := host.boundedText(ctx, "MCP "+server+"."+tool+" output", output)
		if err != nil {
			return result, err
		}
		return result, boundErr
	default:
		return nil, fmt.Errorf("unknown mcp operation %q", operation)
	}
}

func (host *recursiveHost) state(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	node := host.session
	key, _ := stringArgument(arguments, "key")
	payload, err := runtimeStatePayload(arguments["value"])
	if err != nil {
		return nil, err
	}
	switch operation {
	case "private_get":
		return node.root.GetPrivateState(ctx, node.id, key)
	case "private_list":
		return node.root.ListPrivateState(ctx, node.id)
	case "private_set":
		return node.root.SetPrivateState(ctx, node.id, key, payload)
	case "private_append":
		return node.root.AppendPrivateState(ctx, node.id, key, payload)
	case "private_cas":
		return node.root.CompareAndSwapPrivateState(ctx, node.id, key, int64Argument(arguments, "version", 0), payload)
	case "blackboard_get":
		return node.root.GetBlackboard(ctx, node.id, key)
	case "blackboard_set":
		return node.root.SetBlackboard(ctx, node.id, key, payload)
	case "blackboard_append":
		return node.root.AppendBlackboard(ctx, node.id, key, payload)
	case "blackboard_cas":
		return node.root.CompareAndSwapBlackboard(ctx, node.id, key, int64Argument(arguments, "version", 0), payload)
	case "blackboard_history":
		return node.root.BlackboardHistory(ctx, node.id, key)
	case "subscribe":
		return node.root.CreateBlackboardSubscription(ctx, node.id, key)
	case "subscriptions":
		return node.root.ListBlackboardSubscriptions(ctx, node.id)
	case "cancel_subscription":
		id, _ := stringArgument(arguments, "id")
		return map[string]any{"cancelled": id}, node.root.CancelBlackboardSubscription(ctx, node.id, id)
	default:
		return nil, fmt.Errorf("unknown state operation %q", operation)
	}
}

func (host *recursiveHost) artifacts(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	node := host.session
	switch operation {
	case "put":
		text, _ := stringArgument(arguments, "text")
		source, _ := stringArgument(arguments, "source")
		if source == "" {
			source = "agent artifact"
		}
		value, err := node.root.StoreContent(ctx, node.id, sessionstore.RuntimePayload{Data: []byte(text), MediaType: "text/plain", Source: source})
		return runtimeValueMap(value), err
	case "inspect":
		return host.context(ctx, "inspect", arguments)
	case "read":
		return host.context(ctx, "read", arguments)
	default:
		return nil, fmt.Errorf("unknown artifacts operation %q", operation)
	}
}

func (host *recursiveHost) schedules(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	node := host.session
	switch operation {
	case "create":
		expression, _ := stringArgument(arguments, "schedule")
		prompt, _ := stringArgument(arguments, "prompt")
		if _, err := schedule.Parse(expression); err != nil {
			return nil, err
		}
		id, err := node.root.AddSchedule(ctx, expression, prompt, time.Now())
		return map[string]any{"id": id}, err
	case "list":
		return node.root.ListSchedules(ctx)
	case "cancel":
		id := intArgument(arguments, "id", 0)
		return map[string]any{"cancelled": id}, node.root.CancelSchedule(ctx, id)
	default:
		return nil, fmt.Errorf("unknown schedules operation %q", operation)
	}
}

func (host *recursiveHost) boundedText(ctx context.Context, source, value string) (map[string]any, error) {
	if len(value) <= sessionstore.InlineValueLimit {
		return map[string]any{"output": value}, nil
	}
	node := host.session
	stored, err := node.root.StoreContent(ctx, node.id, sessionstore.RuntimePayload{Data: []byte(value), MediaType: "text/plain", Source: source})
	if err != nil {
		return nil, err
	}
	const preview = 2 << 10
	prefix := utf8PrefixRuntime(value, preview)
	suffix, _ := utf8SuffixRuntime(value, preview)
	return map[string]any{
		"handle": stored.ReferenceID, "size": stored.Size, "source": stored.Source,
		"preview": prefix + "\n... [handle-backed remainder] ...\n" + suffix,
	}, nil
}

func requestedCapabilities(value any, inherited []string) ([]string, error) {
	if value == nil {
		return append([]string(nil), inherited...), nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("capabilities must be a list")
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		name, ok := item.(string)
		if !ok || !slices.Contains(inherited, name) {
			return nil, fmt.Errorf("capability %q is not available to the parent", name)
		}
		if !slices.Contains(result, name) {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result, nil
}

func requestedBudgets(value any) ([]sessionstore.BudgetLimit, error) {
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
	result := make([]sessionstore.BudgetLimit, 0, len(keys))
	for _, key := range keys {
		limit, ok := items[key].(float64)
		if !ok || limit < 0 || limit != float64(int64(limit)) {
			return nil, fmt.Errorf("budget %q must be a non-negative integer", key)
		}
		result = append(result, sessionstore.BudgetLimit{Kind: sessionstore.BudgetKind(key), Limit: int64(limit)})
	}
	return result, nil
}

func runtimeStatePayload(value any) (sessionstore.RuntimePayload, error) {
	data, err := json.Marshal(value)
	return sessionstore.RuntimePayload{Data: data, MediaType: "application/json", Source: "agent state"}, err
}

func runtimeValueMap(value sessionstore.RuntimeValue) map[string]any {
	return map[string]any{"inline": string(value.Inline), "handle": value.ReferenceID, "digest": value.Digest, "size": value.Size, "media_type": value.MediaType, "source": value.Source}
}

func contentMetadataMap(value sessionstore.ContentMetadata) map[string]any {
	return map[string]any{"handle": value.ReferenceID, "digest": value.Digest, "size": value.Size, "media_type": value.MediaType, "source": value.Source}
}

func cloneArguments(arguments map[string]any) map[string]any {
	result := make(map[string]any, len(arguments))
	maps.Copy(result, arguments)
	return result
}

func stringArgument(arguments map[string]any, key string) (string, bool) {
	value, ok := arguments[key].(string)
	return value, ok
}

func stringListArgument(arguments map[string]any, key string) ([]string, error) {
	items, ok := arguments[key].([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("%s must be a non-empty list", key)
	}
	result := make([]string, len(items))
	for index, item := range items {
		value, ok := item.(string)
		if !ok || value == "" {
			return nil, fmt.Errorf("%s must contain message ids", key)
		}
		result[index] = value
	}
	return result, nil
}

func intArgument(arguments map[string]any, key string, fallback int) int {
	value, ok := arguments[key].(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) || value < math.MinInt || value > math.MaxInt {
		return fallback
	}
	return int(value)
}

func int64Argument(arguments map[string]any, key string, fallback int64) int64 {
	value, ok := arguments[key].(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) || value < math.MinInt64 || value > math.MaxInt64 {
		return fallback
	}
	return int64(value)
}

func utf8PrefixRuntime(value string, bytes int) string {
	end := min(len(value), bytes)
	for end > 0 && end < len(value) && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

func utf8SuffixRuntime(value string, bytes int) (string, int) {
	start := max(0, len(value)-bytes)
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:], start
}

func randomRuntimeSuffix() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(value[:])
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
