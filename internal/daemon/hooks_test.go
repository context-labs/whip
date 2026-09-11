package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

type hookOutcome struct {
	decision hookDecision
	err      error
}

func invokeHook(ctx context.Context, registry *executorRegistry, invocation hookInvocation) <-chan hookOutcome {
	outcome := make(chan hookOutcome, 1)
	go func() {
		decision, err := registry.InvokeHook(ctx, invocation)
		outcome <- hookOutcome{decision, err}
	}()
	return outcome
}

func awaitHook(t *testing.T, conn *fakeExecutorConn, outcome <-chan hookOutcome) protocol.HookInvokeParams {
	t.Helper()
	select {
	case invoke := <-conn.hooked:
		return invoke
	case settled := <-outcome:
		t.Fatalf("hook settled before the executor was asked: %+v %v", settled.decision, settled.err)
	case <-time.After(5 * time.Second):
		t.Fatal("executor was not asked")
	}
	return protocol.HookInvokeParams{}
}

func awaitHookOutcome(t *testing.T, outcome <-chan hookOutcome) hookOutcome {
	t.Helper()
	select {
	case value := <-outcome:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("hook did not settle")
		return hookOutcome{}
	}
}

// The registry carries hook invocations on the same lease as tools: an empty
// reply allows unchanged, deny and rewrite replies are applied, a handler
// error is a failed decision, malformed and foreign replies are rejected, and
// the deadline, disconnect, and missing-executor paths end the wait.
func TestHookRegistrySettlesDecisions(t *testing.T) {
	registry := newExecutorRegistry()
	registry.bindWait = 50 * time.Millisecond
	base := hookInvocation{Definition: "hooked", Revision: "rev", RootID: "root", AgentID: "agent", TurnID: "turn", Hook: "before_tool", Operation: "shell.run", Arguments: json.RawMessage(`{"command":"ls"}`), PermissionMode: "automatic", Timeout: time.Second}
	if _, err := registry.InvokeHook(t.Context(), base); err == nil || !strings.Contains(err.Error(), "no executor is bound") {
		t.Fatalf("missing executor error = %v", err)
	}
	conn := newFakeExecutorConn()
	generation := registry.bind(conn, "hooked", "rev", nil, []string{"before_tool"})

	// Empty reply: allow, unchanged.
	outcome := invokeHook(t.Context(), registry, base)
	invoke := awaitHook(t, conn, outcome)
	if invoke.Hook != "before_tool" || invoke.Operation != "shell.run" || string(invoke.Arguments) != `{"command":"ls"}` || invoke.PermissionMode != "automatic" || invoke.Generation != generation || !strings.HasPrefix(invoke.InvocationID, "hook-") {
		t.Fatalf("hook invocation = %+v", invoke)
	}
	pending, err := registry.pendingFor(conn, protocol.ExecutorPendingParams{Definition: "hooked", Revision: "rev", Generation: generation})
	if err != nil || len(pending.Hooks) != 1 || pending.Hooks[0].InvocationID != invoke.InvocationID || len(pending.Invocations) != 0 {
		t.Fatalf("pending hooks = %+v %v", pending, err)
	}
	if err := registry.settle(conn, protocol.ToolResultParams{InvocationID: invoke.InvocationID, Generation: generation, Output: json.RawMessage(`1`)}); err == nil {
		t.Fatal("tool result settled a hook")
	}
	if err := registry.settleHook(conn, protocol.HookResultParams{InvocationID: invoke.InvocationID, Generation: generation}); err != nil {
		t.Fatal(err)
	}
	if settled := awaitHookOutcome(t, outcome); settled.err != nil || settled.decision.Deny || settled.decision.Failed || settled.decision.Arguments != nil || settled.decision.InvocationID != invoke.InvocationID {
		t.Fatalf("empty reply = %+v %v", settled.decision, settled.err)
	}
	if err := registry.settleHook(conn, protocol.HookResultParams{InvocationID: invoke.InvocationID, Generation: generation}); err == nil {
		t.Fatal("duplicate reply accepted")
	}

	// Deny with a reason; malformed replies are rejected and the call keeps waiting.
	outcome = invokeHook(t.Context(), registry, base)
	invoke = awaitHook(t, conn, outcome)
	for name, reply := range map[string]protocol.HookResultParams{
		"decision":   {InvocationID: invoke.InvocationID, Generation: generation, Decision: "maybe"},
		"arguments":  {InvocationID: invoke.InvocationID, Generation: generation, Arguments: json.RawMessage(`[1]`)},
		"generation": {InvocationID: invoke.InvocationID, Generation: generation + 7},
	} {
		if err := registry.settleHook(conn, reply); err == nil {
			t.Fatalf("%s reply accepted", name)
		}
	}
	if err := registry.settleHook(conn, protocol.HookResultParams{InvocationID: invoke.InvocationID, Generation: generation, Decision: "deny", Reason: "no"}); err != nil {
		t.Fatal(err)
	}
	if settled := awaitHookOutcome(t, outcome); settled.err != nil || !settled.decision.Deny || settled.decision.Failed || settled.decision.Reason != "no" {
		t.Fatalf("deny reply = %+v %v", settled.decision, settled.err)
	}

	// Rewrite, with a bounded context.
	outcome = invokeHook(t.Context(), registry, base)
	invoke = awaitHook(t, conn, outcome)
	if err := registry.settleHook(conn, protocol.HookResultParams{InvocationID: invoke.InvocationID, Generation: generation, Decision: "allow", Arguments: json.RawMessage(`{"command":"ls -la"}`), Context: strings.Repeat("x", maxHookContextBytes+10)}); err != nil {
		t.Fatal(err)
	}
	if settled := awaitHookOutcome(t, outcome); settled.err != nil || settled.decision.Deny || string(settled.decision.Arguments) != `{"command":"ls -la"}` || len(settled.decision.Context) != maxHookContextBytes {
		t.Fatalf("rewrite reply = %+v %v", settled.decision, settled.err)
	}

	// Handler error: a failed decision, not an unanswered hook.
	outcome = invokeHook(t.Context(), registry, base)
	invoke = awaitHook(t, conn, outcome)
	if err := registry.settleHook(conn, protocol.HookResultParams{InvocationID: invoke.InvocationID, Generation: generation, Error: "handler crashed"}); err != nil {
		t.Fatal(err)
	}
	if settled := awaitHookOutcome(t, outcome); settled.err != nil || !settled.decision.Deny || !settled.decision.Failed || settled.decision.Reason != "handler crashed" {
		t.Fatalf("error reply = %+v %v", settled.decision, settled.err)
	}

	// Deadline: unanswered, cancel sent, late reply rejected.
	short := base
	short.Timeout = 50 * time.Millisecond
	outcome = invokeHook(t.Context(), registry, short)
	invoke = awaitHook(t, conn, outcome)
	if settled := awaitHookOutcome(t, outcome); settled.err == nil || !strings.Contains(settled.err.Error(), "hook before_tool timed out after 50ms") {
		t.Fatalf("deadline = %+v %v", settled.decision, settled.err)
	}
	if cancel := <-conn.cancelled; cancel.InvocationID != invoke.InvocationID || cancel.Reason != "timeout" {
		t.Fatalf("cancel = %+v", cancel)
	}
	if err := registry.settleHook(conn, protocol.HookResultParams{InvocationID: invoke.InvocationID, Generation: generation}); err == nil {
		t.Fatal("late reply accepted")
	}

	// Cancelled turn.
	ctx, cancel := context.WithCancel(t.Context())
	outcome = invokeHook(ctx, registry, base)
	invoke = awaitHook(t, conn, outcome)
	cancel()
	if settled := awaitHookOutcome(t, outcome); settled.err == nil {
		t.Fatal("cancelled hook settled")
	}
	if cancelled := <-conn.cancelled; cancelled.InvocationID != invoke.InvocationID || cancelled.Reason != "cancelled" {
		t.Fatalf("cancel = %+v", cancelled)
	}

	// Disconnect: unanswered, never replayed.
	outcome = invokeHook(t.Context(), registry, base)
	awaitHook(t, conn, outcome)
	close(conn.done)
	registry.disconnect(conn)
	if settled := awaitHookOutcome(t, outcome); settled.err == nil || !strings.Contains(settled.err.Error(), "executor disconnected") {
		t.Fatalf("disconnect = %v", settled.err)
	}
	replacement := newFakeExecutorConn()
	next := registry.bind(replacement, "hooked", "rev", nil, []string{"before_tool"})
	if pending, err := registry.pendingFor(replacement, protocol.ExecutorPendingParams{Definition: "hooked", Revision: "rev", Generation: next}); err != nil || len(pending.Hooks) != 0 {
		t.Fatalf("pending after reconnect = %+v %v", pending, err)
	}
}

// hookedRoot registers a coding-shaped definition with one custom tool and the
// given hooks, opens a session on it, and returns the pieces a test drives.
func hookedRoot(t *testing.T, hooks *agentdef.Hooks) (*session.Store, *Daemon, *Session, *RecursiveRuntime) {
	t.Helper()
	_, client := promptRuntimeProvider(t)
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	definition := agentdef.Coding()
	definition.ID = "hooked"
	definition.Tools = []agentdef.Tool{{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	definition.Hooks = hooks
	document, err := agentdef.Encode(definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registerDefinition(t.Context(), store, document, "hooks-test"); err != nil {
		t.Fatal(err)
	}
	rootID := createDefinitionRoot(t, store, "hooked")
	// Hooks gate operations before permissions; the shell cases need no human.
	if err := store.SetPermissionMode(t.Context(), rootID, session.PermissionModeAutomatic); err != nil {
		t.Fatal(err)
	}
	owner, root, runtime := openPromptRuntime(t, store, rootID, client)
	owner.executors.bindWait = 100 * time.Millisecond
	return store, owner, root, runtime
}

// bindHookedExecutor binds a fake executor covering the hooked definition.
func bindHookedExecutor(t *testing.T, owner *Daemon, store *session.Store, hooks ...string) (*fakeExecutorConn, int64) {
	t.Helper()
	record, err := store.LatestDefinition(t.Context(), "hooked")
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeExecutorConn()
	return conn, owner.executors.bind(conn, "hooked", record.Revision, []string{"lookup"}, hooks)
}

type capturedEvent struct {
	kind  string
	event StreamEvent
}

// captureEvents records the stream events a node emits.
func captureEvents(node *AgentSession) func() []capturedEvent {
	var mu sync.Mutex
	var events []capturedEvent
	previous := node.emit
	node.emit = func(kind string, event StreamEvent) {
		mu.Lock()
		events = append(events, capturedEvent{kind, event})
		mu.Unlock()
		if previous != nil {
			previous(kind, event)
		}
	}
	return func() []capturedEvent {
		mu.Lock()
		defer mu.Unlock()
		var decisions []capturedEvent
		for _, event := range events {
			if event.kind == "stream.hook.decision" {
				decisions = append(decisions, event)
			}
		}
		return decisions
	}
}

func awaitHookFromCell(t *testing.T, conn *fakeExecutorConn, outcome <-chan cellOutcome) protocol.HookInvokeParams {
	t.Helper()
	select {
	case invoke := <-conn.hooked:
		return invoke
	case cell := <-outcome:
		t.Fatalf("cell settled before the hook was asked: %#v %v", cell.result.Value, cell.err)
	case <-time.After(10 * time.Second):
		t.Fatal("hook was not asked")
	}
	return protocol.HookInvokeParams{}
}

// A required before_tool hook gates every host operation: it fails closed
// without an executor, allows on an empty reply, denies with the reason,
// rewrites through the normal validators, and follows children.
func TestBeforeToolGatesEveryHostOperation(t *testing.T) {
	store, owner, root, runtime := hookedRoot(t, &agentdef.Hooks{BeforeTool: &agentdef.Hook{TimeoutMillis: 2000}})
	parent := runtime.rootNode
	decisions := captureEvents(parent)
	// No executor: fail closed with an error the model reads.
	if cell := awaitCell(t, execCell(t.Context(), parent, `files.list(path=".")`)); cell.err == nil || !strings.Contains(cell.err.Error(), "hook before_tool: no executor is bound for agent definition hooked") {
		t.Fatalf("missing executor = %v", cell.err)
	}
	if events := decisions(); len(events) != 1 || events[0].event.Text != "deny" || events[0].event.Args != "files.list" {
		t.Fatalf("missing executor events = %+v", events)
	}
	conn, generation := bindHookedExecutor(t, owner, store, "before_tool")
	reply := func(invoke protocol.HookInvokeParams, params protocol.HookResultParams) {
		t.Helper()
		params.InvocationID, params.Generation = invoke.InvocationID, generation
		if err := owner.executors.settleHook(conn, params); err != nil {
			t.Fatal(err)
		}
	}
	// Allow: empty reply, operation runs untouched, nothing emitted.
	outcome := execCell(t.Context(), parent, `files.list(path=".")`)
	invoke := awaitHookFromCell(t, conn, outcome)
	mode, _ := store.PermissionMode(t.Context(), root.ID())
	if invoke.Hook != "before_tool" || invoke.Operation != "files.list" || string(invoke.Arguments) != `{"path":"."}` || invoke.AgentID != parent.id || invoke.RootID != root.ID() || invoke.Definition != "hooked" || invoke.PermissionMode != mode || invoke.Spawn != nil {
		t.Fatalf("hook invocation = %+v", invoke)
	}
	reply(invoke, protocol.HookResultParams{})
	if cell := awaitCell(t, outcome); cell.err != nil {
		t.Fatalf("allowed operation failed: %v", cell.err)
	}
	if events := decisions(); len(events) != 1 {
		t.Fatalf("allow emitted an event: %+v", events)
	}
	// Deny.
	outcome = execCell(t.Context(), parent, `shell.run(command="ls")`)
	invoke = awaitHookFromCell(t, conn, outcome)
	reply(invoke, protocol.HookResultParams{Decision: "deny", Reason: "no shell"})
	if cell := awaitCell(t, outcome); cell.err == nil || !strings.Contains(cell.err.Error(), "hook before_tool denied shell.run: no shell") {
		t.Fatalf("deny = %v", cell.err)
	}
	if events := decisions(); len(events) != 2 || events[1].event.Text != "deny" || events[1].event.Args != "shell.run" || events[1].event.Result != "no shell" || events[1].event.InvocationID != invoke.InvocationID {
		t.Fatalf("deny events = %+v", events)
	}
	// Rewrite: the rewritten path is what the files handler reads.
	if err := os.WriteFile(filepath.Join(root.meta.CWD, "README.md"), []byte("hello readme"), 0o644); err != nil {
		t.Fatal(err)
	}
	outcome = execCell(t.Context(), parent, `files.read(path="secret.env")`)
	invoke = awaitHookFromCell(t, conn, outcome)
	reply(invoke, protocol.HookResultParams{Arguments: json.RawMessage(`{"path":"README.md"}`), Reason: "secrets are redacted"})
	cell := awaitCell(t, outcome)
	if encoded, _ := json.Marshal(cell.result.Value); cell.err != nil || !strings.Contains(string(encoded), "hello readme") {
		t.Fatalf("rewritten read = %s %v", encoded, cell.err)
	}
	if events := decisions(); len(events) != 3 || events[2].event.Text != "rewrite" || events[2].event.Args != "files.read" {
		t.Fatalf("rewrite events = %+v", events)
	}
	if notices := parent.hookNotices(); !strings.Contains(notices, `Hook before_tool rewrote files.read arguments to {"path":"README.md"} (reason: secrets are redacted)`) {
		t.Fatalf("rewrite notice = %q", notices)
	}
	// A rewrite to invalid arguments fails at the operation's own validator.
	outcome = execCell(t.Context(), parent, `files.read(path="README.md")`)
	invoke = awaitHookFromCell(t, conn, outcome)
	reply(invoke, protocol.HookResultParams{Arguments: json.RawMessage(`{"nope":true}`)})
	if cell := awaitCell(t, outcome); cell.err == nil {
		t.Fatal("invalid rewrite ran")
	}
	// A spawn rewrite cannot widen.
	outcome = execCell(t.Context(), parent, `agents.spawn(prompt="x", name="wide")`)
	invoke = awaitHookFromCell(t, conn, outcome)
	if invoke.Operation != "agents.spawn" || !strings.Contains(string(invoke.Arguments), `"prompt":"x"`) {
		t.Fatalf("spawn invocation = %+v", invoke)
	}
	reply(invoke, protocol.HookResultParams{Arguments: json.RawMessage(`{"prompt":"x","name":"wide","tools":["missing"]}`)})
	if cell := awaitCell(t, outcome); cell.err == nil || !strings.Contains(cell.err.Error(), `tool "missing" is not available to the parent`) {
		t.Fatalf("widening rewrite = %v", cell.err)
	}
	// Custom tools are gated too, before the tool itself is invoked.
	outcome = execCell(t.Context(), parent, `tools.lookup()`)
	invoke = awaitHookFromCell(t, conn, outcome)
	if invoke.Operation != "tools.lookup" {
		t.Fatalf("tool hook invocation = %+v", invoke)
	}
	reply(invoke, protocol.HookResultParams{})
	toolInvoke := awaitInvoke(t, conn, outcome)
	if err := owner.executors.settle(conn, protocol.ToolResultParams{InvocationID: toolInvoke.InvocationID, Generation: generation, Output: json.RawMessage(`"ok"`)}); err != nil {
		t.Fatal(err)
	}
	if cell := awaitCell(t, outcome); cell.err != nil || cell.result.Value != "ok" {
		t.Fatalf("gated tool = %#v %v", cell.result.Value, cell.err)
	}
	// A handler error denies a required hook with its text.
	outcome = execCell(t.Context(), parent, `files.list(path=".")`)
	invoke = awaitHookFromCell(t, conn, outcome)
	reply(invoke, protocol.HookResultParams{Error: "handler crashed"})
	if cell := awaitCell(t, outcome); cell.err == nil || !strings.Contains(cell.err.Error(), "hook before_tool denied files.list: handler crashed") {
		t.Fatalf("handler error = %v", cell.err)
	}
	// A child is gated under its own identity.
	outcome = execCell(t.Context(), parent, `agents.spawn(prompt="go", name="kid", report="message")`)
	invoke = awaitHookFromCell(t, conn, outcome)
	reply(invoke, protocol.HookResultParams{})
	cell = awaitCell(t, outcome)
	if cell.err != nil {
		t.Fatalf("spawn failed: %v", cell.err)
	}
	childID := cell.result.Value.(map[string]any)["id"].(string)
	runtime.mu.RLock()
	child := runtime.agents[childID]
	runtime.mu.RUnlock()
	waitAgentIdle(t, child)
	outcome = execCell(t.Context(), child, `files.list(path=".")`)
	invoke = awaitHookFromCell(t, conn, outcome)
	if invoke.AgentID != childID || invoke.Definition != "hooked" {
		t.Fatalf("child hook invocation = %+v", invoke)
	}
	reply(invoke, protocol.HookResultParams{})
	if cell := awaitCell(t, outcome); cell.err != nil {
		t.Fatalf("child allowed operation failed: %v", cell.err)
	}
	// Timeout: the deadline denies and cancels.
	outcome = execCell(t.Context(), parent, `files.list(path=".")`)
	invoke = awaitHookFromCell(t, conn, outcome)
	if cell := awaitCell(t, outcome); cell.err == nil || !strings.Contains(cell.err.Error(), "hook before_tool timed out after 2s") {
		t.Fatalf("timeout = %v", cell.err)
	}
	if cancel := <-conn.cancelled; cancel.InvocationID != invoke.InvocationID || cancel.Reason != "timeout" {
		t.Fatalf("cancel = %+v", cancel)
	}
}

// An optional, narrowed before_tool hook only asks about its operations and
// proceeds with a notice when unanswered or failing.
func TestBeforeToolOptionalAndNarrowed(t *testing.T) {
	store, owner, _, runtime := hookedRoot(t, &agentdef.Hooks{BeforeTool: &agentdef.Hook{Operations: []string{"shell.run"}, Optional: true, TimeoutMillis: 2000}})
	parent := runtime.rootNode
	decisions := captureEvents(parent)
	// Unlisted operations never reach the hook, executor or not.
	if cell := awaitCell(t, execCell(t.Context(), parent, `files.list(path=".")`)); cell.err != nil {
		t.Fatalf("unlisted operation failed: %v", cell.err)
	}
	if events := decisions(); len(events) != 0 {
		t.Fatalf("unlisted operation emitted %+v", events)
	}
	// No executor: optional proceeds with a skip.
	cell := awaitCell(t, execCell(t.Context(), parent, `shell.run(command="echo hi")`))
	if encoded, _ := json.Marshal(cell.result.Value); cell.err != nil || !strings.Contains(string(encoded), "hi") {
		t.Fatalf("optional skip did not proceed: %s %v", encoded, cell.err)
	}
	if events := decisions(); len(events) != 1 || events[0].event.Text != "skipped" || events[0].event.Args != "shell.run" || !strings.Contains(events[0].event.Result, "no executor is bound") {
		t.Fatalf("skip events = %+v", events)
	}
	if notices := parent.hookNotices(); !strings.Contains(notices, "Hook before_tool was skipped for shell.run") {
		t.Fatalf("skip notice = %q", notices)
	}
	// A handler error skips too.
	conn, generation := bindHookedExecutor(t, owner, store, "before_tool")
	outcome := execCell(t.Context(), parent, `shell.run(command="echo again")`)
	invoke := awaitHookFromCell(t, conn, outcome)
	if err := owner.executors.settleHook(conn, protocol.HookResultParams{InvocationID: invoke.InvocationID, Generation: generation, Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	cell = awaitCell(t, outcome)
	if encoded, _ := json.Marshal(cell.result.Value); cell.err != nil || !strings.Contains(string(encoded), "again") {
		t.Fatalf("optional handler error did not proceed: %s %v", encoded, cell.err)
	}
	if events := decisions(); len(events) != 2 || events[1].event.Text != "skipped" || !strings.Contains(events[1].event.Result, "boom") {
		t.Fatalf("handler error events = %+v", events)
	}
	// Notices stay bounded.
	for i := 0; i < 20; i++ {
		parent.addHookNotice(fmt.Sprintf("notice %d", i))
	}
	if notices := parent.hookNotices(); strings.Count(notices, "\n") > maxHookNotices || !strings.Contains(notices, "more hook notices omitted") {
		t.Fatalf("notices unbounded:\n%s", notices)
	}
}
