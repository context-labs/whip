package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/llm"
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
func hookedRoot(t *testing.T, hooks *agentdef.Hooks) (*session.Store, *Daemon, *Session, *RecursiveRuntime, <-chan llm.Request) {
	t.Helper()
	requests, client := promptRuntimeProvider(t)
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	definition := agentdef.Coding()
	definition.ID = "hooked"
	definition.Tools = []agentdef.Tool{{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	definition.Children = map[string]agentdef.Child{
		"researcher": {Modules: []string{"context", "files", "agents"}, Capabilities: []string{"read"}, Tools: []string{"lookup"}, Budgets: map[string]int64{"tokens": 5000}, Report: "message"},
	}
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
	return store, owner, root, runtime, requests
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
	store, owner, root, runtime, _ := hookedRoot(t, &agentdef.Hooks{BeforeTool: &agentdef.Hook{TimeoutMillis: 2000}})
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
	store, owner, _, runtime, _ := hookedRoot(t, &agentdef.Hooks{BeforeTool: &agentdef.Hook{Operations: []string{"shell.run"}, Optional: true, TimeoutMillis: 2000}})
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
	for i := range 20 {
		parent.addHookNotice(fmt.Sprintf("notice %d", i))
	}
	if notices := parent.hookNotices(); strings.Count(notices, "\n") > maxHookNotices || !strings.Contains(notices, "more hook notices omitted") {
		t.Fatalf("notices unbounded:\n%s", notices)
	}
}

// before_spawn sees the parsed request and the resolved child, denies with a
// reason, rewrites through resolution so it cannot widen, and runs after
// before_tool when both are declared.
func TestBeforeSpawnSeesResolvedChildAndCannotWiden(t *testing.T) {
	store, owner, root, runtime, _ := hookedRoot(t, &agentdef.Hooks{BeforeTool: &agentdef.Hook{Operations: []string{"agents.spawn"}, TimeoutMillis: 2000}, BeforeSpawn: &agentdef.Hook{TimeoutMillis: 2000}})
	parent := runtime.rootNode
	decisions := captureEvents(parent)
	conn, generation := bindHookedExecutor(t, owner, store, "before_tool", "before_spawn")
	reply := func(invoke protocol.HookInvokeParams, params protocol.HookResultParams) {
		t.Helper()
		params.InvocationID, params.Generation = invoke.InvocationID, generation
		if err := owner.executors.settleHook(conn, params); err != nil {
			t.Fatal(err)
		}
	}
	// Order: before_tool on the raw call, then before_spawn on the resolution.
	outcome := execCell(t.Context(), parent, `agents.spawn(prompt="research x", name="scout", definition="researcher", budgets={"tokens": 100})`)
	first := awaitHookFromCell(t, conn, outcome)
	if first.Hook != "before_tool" || first.Operation != "agents.spawn" || first.Spawn != nil || !strings.Contains(string(first.Arguments), `"definition":"researcher"`) {
		t.Fatalf("first hook = %+v", first)
	}
	reply(first, protocol.HookResultParams{})
	second := awaitHookFromCell(t, conn, outcome)
	if second.Hook != "before_spawn" || second.Operation != "agents.spawn" || second.Spawn == nil || second.Arguments != nil {
		t.Fatalf("second hook = %+v", second)
	}
	request, resolved := second.Spawn.Request, second.Spawn.Resolved
	if request.Prompt != "research x" || request.Name != "scout" || request.Definition != "researcher" || request.Budgets["tokens"] != 100 || request.Capabilities != nil {
		t.Fatalf("spawn request = %+v", request)
	}
	if resolved.Definition != "hooked/researcher" || !slices.Equal(resolved.Modules, []string{"context", "files", "agents"}) || !slices.Equal(resolved.Capabilities, []string{"read"}) || !slices.Equal(resolved.Tools, []string{"lookup"}) || resolved.Budgets["tokens"] != 100 || resolved.Report != "message" {
		t.Fatalf("resolved child = %+v", resolved)
	}
	// Deny admits nothing.
	reply(second, protocol.HookResultParams{Decision: "deny", Reason: "no children today"})
	if cell := awaitCell(t, outcome); cell.err == nil || !strings.Contains(cell.err.Error(), "hook before_spawn denied agents.spawn: no children today") {
		t.Fatalf("deny = %v", cell.err)
	}
	if relatives, err := root.ListAgentRelatives(t.Context(), parent.id); err != nil || len(relatives.Children) != 0 {
		t.Fatalf("denied spawn admitted a child: %+v %v", relatives, err)
	}
	if events := decisions(); len(events) != 1 || events[0].event.Name != "before_spawn" || events[0].event.Text != "deny" {
		t.Fatalf("deny events = %+v", events)
	}
	// A rewrite is resolved again: a plain request becomes the named child.
	outcome = execCell(t.Context(), parent, `agents.spawn(prompt="go", name="plain")`)
	reply(awaitHookFromCell(t, conn, outcome), protocol.HookResultParams{})
	second = awaitHookFromCell(t, conn, outcome)
	if second.Spawn.Resolved.Definition != "hooked" {
		t.Fatalf("plain resolution = %+v", second.Spawn.Resolved)
	}
	// The SDK validates the notification against a non-nullable budgets object.
	if encoded, _ := json.Marshal(second.Spawn); !strings.Contains(string(encoded), `"budgets":{}`) || strings.Contains(string(encoded), `"budgets":null`) {
		t.Fatalf("spawn preview budgets are not an object: %s", encoded)
	}
	rewritten := second.Spawn.Request
	rewritten.Definition, rewritten.Name = "researcher", "scout"
	reply(second, protocol.HookResultParams{Spawn: &rewritten, Reason: "all children research"})
	cell := awaitCell(t, outcome)
	if cell.err != nil {
		t.Fatalf("rewritten spawn failed: %v", cell.err)
	}
	receipt := cell.result.Value.(map[string]any)
	runtime.mu.RLock()
	child := runtime.agents[receipt["id"].(string)]
	runtime.mu.RUnlock()
	if receipt["name"] != "scout" || receipt["report"] != "message" || child == nil || child.definition.ID != "hooked/researcher" || !slices.Equal(child.definition.Capabilities, []string{"read"}) {
		t.Fatalf("rewritten child = %+v / %+v", receipt, child)
	}
	if name, err := store.AgentDefinitionName(t.Context(), root.ID(), child.id); err != nil || name != "researcher" {
		t.Fatalf("stored child definition = %q %v", name, err)
	}
	if events := decisions(); len(events) != 2 || events[1].event.Text != "rewrite" || events[1].event.Name != "before_spawn" {
		t.Fatalf("rewrite events = %+v", events)
	}
	if notices := parent.hookNotices(); !strings.Contains(notices, "Hook before_spawn rewrote the spawn request to") || !strings.Contains(notices, "(reason: all children research)") {
		t.Fatalf("rewrite notice = %q", notices)
	}
	waitAgentIdle(t, child)
	// A rewrite that widens fails exactly as a widening request would.
	outcome = execCell(t.Context(), parent, `agents.spawn(prompt="go", name="wider", definition="researcher")`)
	reply(awaitHookFromCell(t, conn, outcome), protocol.HookResultParams{})
	second = awaitHookFromCell(t, conn, outcome)
	widened := second.Spawn.Request
	widened.Capabilities = []string{"shell"}
	reply(second, protocol.HookResultParams{Spawn: &widened})
	if cell := awaitCell(t, outcome); cell.err == nil || !strings.Contains(cell.err.Error(), `capability "shell" is not available to the parent`) {
		t.Fatalf("widening rewrite = %v", cell.err)
	}
	// The child inherits both hooks and is gated under its own identity.
	outcome = execCell(t.Context(), child, `agents.spawn(prompt="deeper", name="grandchild")`)
	first = awaitHookFromCell(t, conn, outcome)
	if first.AgentID != child.id || first.Hook != "before_tool" {
		t.Fatalf("child hook = %+v", first)
	}
	reply(first, protocol.HookResultParams{Decision: "deny", Reason: "no grandchildren"})
	if cell := awaitCell(t, outcome); cell.err == nil || !strings.Contains(cell.err.Error(), "no grandchildren") {
		t.Fatalf("child deny = %v", cell.err)
	}
}

// An optional before_spawn hook with nobody serving it proceeds with a notice.
func TestBeforeSpawnOptionalProceedsUnanswered(t *testing.T) {
	_, _, root, runtime, _ := hookedRoot(t, &agentdef.Hooks{BeforeSpawn: &agentdef.Hook{Optional: true}})
	parent := runtime.rootNode
	decisions := captureEvents(parent)
	cell := awaitCell(t, execCell(t.Context(), parent, `agents.spawn(prompt="go", name="solo", report="message")`))
	if cell.err != nil {
		t.Fatalf("optional spawn failed: %v", cell.err)
	}
	if relatives, err := root.ListAgentRelatives(t.Context(), parent.id); err != nil || len(relatives.Children) != 1 {
		t.Fatalf("spawn did not admit the child: %+v %v", relatives, err)
	}
	if events := decisions(); len(events) != 1 || events[0].event.Name != "before_spawn" || events[0].event.Text != "skipped" || !strings.Contains(events[0].event.Result, "no executor is bound") {
		t.Fatalf("skip events = %+v", events)
	}
	if notices := parent.hookNotices(); !strings.Contains(notices, "Hook before_spawn was skipped for agents.spawn") {
		t.Fatalf("skip notice = %q", notices)
	}
}

// ephemeralSystem returns the ephemeral system message of a provider request,
// which sits after the composed prompt and never enters history.
func ephemeralSystem(request llm.Request) string {
	if len(request.Messages) > 1 && request.Messages[1].Role == "system" {
		return request.Messages[1].Content
	}
	return ""
}

// turn_start contributes to the turn's requests without entering history,
// skips with a notice when unanswered, and runs for children too.
func TestTurnStartContributesEphemeralContext(t *testing.T) {
	store, owner, root, runtime, requests := hookedRoot(t, &agentdef.Hooks{TurnStart: &agentdef.Hook{TimeoutMillis: 1000}})
	parent := runtime.rootNode
	decisions := captureEvents(parent)
	// No executor yet: the turn proceeds without the contribution.
	request := submitPromptRoot(t, root, requests, "first question")
	if ephemeral := ephemeralSystem(request); strings.Contains(ephemeral, "On call") {
		t.Fatalf("unanswered hook contributed: %q", ephemeral)
	}
	if events := decisions(); len(events) != 1 || events[0].event.Name != "turn_start" || events[0].event.Text != "skipped" || !strings.Contains(events[0].event.Result, "no executor is bound") {
		t.Fatalf("skip events = %+v", events)
	}
	conn, generation := bindHookedExecutor(t, owner, store, "turn_start")
	answered := make(chan protocol.HookInvokeParams, 4)
	answering := true
	go func() {
		for invoke := range conn.hooked {
			answered <- invoke
			if !answering {
				continue
			}
			_ = owner.executors.settleHook(conn, protocol.HookResultParams{InvocationID: invoke.InvocationID, Generation: generation, Context: "On call: Sam. Open incidents: 2."})
		}
	}()
	request = submitPromptRoot(t, root, requests, "second question with a long tail")
	invoke := <-answered
	if invoke.Hook != "turn_start" || invoke.Input != "second question with a long tail" || invoke.AgentID != parent.id || invoke.TurnID == "" || invoke.Operation != "" || invoke.Spawn != nil {
		t.Fatalf("turn_start invocation = %+v", invoke)
	}
	if ephemeral := ephemeralSystem(request); !strings.Contains(ephemeral, "On call: Sam. Open incidents: 2.") {
		t.Fatalf("contribution missing from the request: %q", ephemeral)
	}
	for _, message := range parent.agent.Messages {
		if strings.Contains(message.Content, "On call: Sam") {
			t.Fatalf("contribution entered history: %+v", message)
		}
	}
	// A child turn asks under the child's identity.
	cell := awaitCell(t, execCell(t.Context(), parent, `agents.spawn(prompt="go", name="kid", report="message")`))
	if cell.err != nil {
		t.Fatal(cell.err)
	}
	childID := cell.result.Value.(map[string]any)["id"].(string)
	childRequest := readDaemonPromptRequest(t, requests)
	childInvoke := <-answered
	if childInvoke.AgentID != childID || childInvoke.Input != "go" && !strings.Contains(childInvoke.Input, "go") {
		t.Fatalf("child turn_start invocation = %+v", childInvoke)
	}
	if ephemeral := ephemeralSystem(childRequest); !strings.Contains(ephemeral, "On call: Sam") {
		t.Fatalf("child request lacks the contribution: %q", ephemeral)
	}
	runtime.mu.RLock()
	child := runtime.agents[childID]
	runtime.mu.RUnlock()
	waitAgentIdle(t, child)
	// Timeout: the turn proceeds after the hook's deadline with a skip notice.
	answering = false
	started := time.Now()
	request = submitPromptRoot(t, root, requests, "third question")
	<-answered
	if elapsed := time.Since(started); elapsed < time.Second {
		t.Fatalf("turn did not wait for the hook deadline: %s", elapsed)
	}
	if ephemeral := ephemeralSystem(request); strings.Contains(ephemeral, "On call") {
		t.Fatalf("timed-out hook contributed: %q", ephemeral)
	}
	events := decisions()
	if last := events[len(events)-1]; last.event.Text != "skipped" || !strings.Contains(last.event.Result, "timed out") {
		t.Fatalf("timeout events = %+v", events)
	}
	if cancel := <-conn.cancelled; cancel.Reason != "timeout" {
		t.Fatalf("cancel = %+v", cancel)
	}
}
