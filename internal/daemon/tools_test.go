package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
)

// toolingDefinition registers a coding-shaped definition with custom tools
// (two by default) and returns its id.
func toolingDefinition(t *testing.T, store *session.Store, tools ...agentdef.Tool) string {
	t.Helper()
	definition := agentdef.Coding()
	definition.ID = "tooling"
	definition.Tools = tools
	if len(tools) == 0 {
		definition.Tools = []agentdef.Tool{
			{Name: "lookup", Description: "Fetch a ticket by id", InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`)},
			{Name: "other", Description: "Another tool", InputSchema: json.RawMessage(`{"type":"object"}`)},
		}
	}
	document, err := agentdef.Encode(definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registerDefinition(t.Context(), store, document, "tools-test"); err != nil {
		t.Fatal(err)
	}
	return definition.ID
}

// A root receives a tools grant for its definition's tools; children narrow it
// by name through the spawn argument, never widen it, and keep the narrowing
// across a daemon restart.
func TestToolsGrantFollowsDefinitionAndChildNarrowing(t *testing.T) {
	_, client := promptRuntimeProvider(t)
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createDefinitionRoot(t, store, toolingDefinition(t, store))
	owner, root, runtime := openPromptRuntime(t, store, rootID, client)
	parent := runtime.rootNode
	if !slices.Equal(parent.definition.ToolNames(), []string{"lookup", "other"}) {
		t.Fatalf("root tools = %v", parent.definition.ToolNames())
	}
	if err := store.AuthorizeCapability(t.Context(), rootID, rootID, root.authority.Tools, "tools.lookup", ""); err != nil {
		t.Fatalf("root tools grant missing: %v", err)
	}
	narrow := spawnMCPChild(t, parent, map[string]any{"name": "narrow", "tools": []any{"lookup"}})
	none := spawnMCPChild(t, parent, map[string]any{"name": "none", "tools": []any{}})
	inherit := spawnMCPChild(t, parent, map[string]any{"name": "inherit"})
	if !slices.Equal(narrow.definition.ToolNames(), []string{"lookup"}) || len(none.definition.ToolNames()) != 0 || !slices.Equal(inherit.definition.ToolNames(), []string{"lookup", "other"}) {
		t.Fatalf("child tools = %v / %v / %v", narrow.definition.ToolNames(), none.definition.ToolNames(), inherit.definition.ToolNames())
	}
	if tools, err := store.LoadAgentTools(t.Context(), rootID, narrow.id); err != nil || !slices.Equal(tools, []string{"lookup"}) {
		t.Fatalf("narrow child grant = %v %v", tools, err)
	}
	if _, err := parent.host.Call(t.Context(), "agents", "spawn", map[string]any{"prompt": "no", "name": "wide", "tools": []any{"missing"}}); err == nil || err.Error() != `tool "missing" is not available to the parent` {
		t.Fatalf("widening spawn error = %v", err)
	}
	if _, err := narrow.host.Call(t.Context(), "agents", "spawn", map[string]any{"prompt": "no", "name": "regain", "tools": []any{"other"}}); err == nil || err.Error() != `tool "other" is not available to the parent` {
		t.Fatalf("child regained a narrowed tool: %v", err)
	}
	for _, node := range []*AgentSession{narrow, none, inherit} {
		waitAgentIdle(t, node)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, restored := openPromptRuntime(t, openStore(t, path), rootID, client)
	restored.mu.RLock()
	defer restored.mu.RUnlock()
	for _, want := range []struct {
		id    string
		tools []string
	}{{narrow.id, []string{"lookup"}}, {none.id, nil}, {inherit.id, []string{"lookup", "other"}}} {
		node := restored.agents[want.id]
		if node == nil || !slices.Equal(node.definition.ToolNames(), want.tools) {
			t.Fatalf("restored child %s tools = %v, want %v", want.id, node, want.tools)
		}
	}
}

// fakeExecutorConn stands in for an SDK executor's connection.
type fakeExecutorConn struct {
	invoked   chan protocol.ToolInvokeParams
	hooked    chan protocol.HookInvokeParams
	cancelled chan protocol.ToolCancelParams
	done      chan struct{}
}

func newFakeExecutorConn() *fakeExecutorConn {
	return &fakeExecutorConn{invoked: make(chan protocol.ToolInvokeParams, 8), hooked: make(chan protocol.HookInvokeParams, 8), cancelled: make(chan protocol.ToolCancelParams, 8), done: make(chan struct{})}
}

func (c *fakeExecutorConn) notify(method string, params any) bool {
	raw, err := json.Marshal(params)
	if err != nil {
		return false
	}
	switch method {
	case "tool.invoke":
		var invoke protocol.ToolInvokeParams
		_ = json.Unmarshal(raw, &invoke)
		c.invoked <- invoke
	case "hook.invoke":
		var invoke protocol.HookInvokeParams
		_ = json.Unmarshal(raw, &invoke)
		c.hooked <- invoke
	case "tool.cancel", "hook.cancel":
		var cancel protocol.ToolCancelParams
		_ = json.Unmarshal(raw, &cancel)
		c.cancelled <- cancel
	}
	return true
}

func (c *fakeExecutorConn) finished() <-chan struct{} { return c.done }

// bindFakeExecutor binds a fake executor for the registered tooling definition.
func bindFakeExecutor(t *testing.T, owner *Daemon, store *session.Store, tools ...string) (*fakeExecutorConn, string, int64) {
	t.Helper()
	record, err := store.LatestDefinition(t.Context(), "tooling")
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeExecutorConn()
	return conn, record.Revision, owner.executors.bind(conn, "tooling", record.Revision, tools, nil)
}

type cellOutcome struct {
	result rlm.Result
	err    error
}

// execCell runs one cell without blocking the test.
func execCell(ctx context.Context, node *AgentSession, code string) <-chan cellOutcome {
	outcome := make(chan cellOutcome, 1)
	go func() {
		result, err := node.kernel.Exec(ctx, code)
		outcome <- cellOutcome{result, err}
	}()
	return outcome
}

// awaitInvoke waits for the executor to be invoked; a cell that settles first
// failed before reaching the executor.
func awaitInvoke(t *testing.T, conn *fakeExecutorConn, outcome <-chan cellOutcome) protocol.ToolInvokeParams {
	t.Helper()
	select {
	case invoke := <-conn.invoked:
		return invoke
	case cell := <-outcome:
		t.Fatalf("cell settled before the executor was invoked: %#v %v", cell.result.Value, cell.err)
	case <-time.After(10 * time.Second):
		t.Fatal("executor was not invoked")
	}
	return protocol.ToolInvokeParams{}
}

func awaitCell(t *testing.T, outcome <-chan cellOutcome) cellOutcome {
	t.Helper()
	select {
	case value := <-outcome:
		return value
	case <-time.After(10 * time.Second):
		t.Fatal("cell did not settle")
		return cellOutcome{}
	}
}

// A validated invocation reaches the bound executor with its identity, the
// result enters the cell as a JSON value, and late or duplicate settlement is
// rejected.
func TestCustomToolInvocationRoundTrip(t *testing.T) {
	_, client := promptRuntimeProvider(t)
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createDefinitionRoot(t, store, toolingDefinition(t, store))
	owner, _, runtime := openPromptRuntime(t, store, rootID, client)
	parent := runtime.rootNode
	conn, revision, generation := bindFakeExecutor(t, owner, store, "lookup", "other")
	outcome := execCell(t.Context(), parent, `tools.lookup(id="7")`)
	invoke := awaitInvoke(t, conn, outcome)
	if invoke.Tool != "lookup" || string(invoke.Input) != `{"id":"7"}` || invoke.RootID != rootID || invoke.AgentID != parent.id || invoke.Definition != "tooling" || invoke.Revision != revision || invoke.Generation != generation || invoke.InvocationID == "" || invoke.DeadlineMillis <= time.Now().UnixMilli() {
		t.Fatalf("invocation = %+v", invoke)
	}
	if err := owner.executors.report(conn, protocol.ToolProgressParams{InvocationID: invoke.InvocationID, Generation: generation, Text: "working"}); err != nil {
		t.Fatalf("progress rejected: %v", err)
	}
	if err := owner.executors.settle(conn, protocol.ToolResultParams{InvocationID: invoke.InvocationID, Generation: generation, Output: json.RawMessage(`{"ticket":"7","count":3}`)}); err != nil {
		t.Fatalf("result rejected: %v", err)
	}
	cell := awaitCell(t, outcome)
	if cell.err != nil {
		t.Fatalf("cell failed: %v", cell.err)
	}
	value, _ := cell.result.Value.(map[string]any)
	if value["ticket"] != "7" || fmt.Sprint(value["count"]) != "3" {
		t.Fatalf("cell value = %#v", cell.result.Value)
	}
	if err := owner.executors.settle(conn, protocol.ToolResultParams{InvocationID: invoke.InvocationID, Generation: generation, Output: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("duplicate result accepted")
	}
	if err := owner.executors.report(conn, protocol.ToolProgressParams{InvocationID: invoke.InvocationID, Generation: generation, Text: "late"}); err == nil {
		t.Fatal("late progress accepted")
	}
	// A handler error fails the call with the handler's text.
	outcome = execCell(t.Context(), parent, `tools.other()`)
	invoke = awaitInvoke(t, conn, outcome)
	if err := owner.executors.settle(conn, protocol.ToolResultParams{InvocationID: invoke.InvocationID, Generation: generation, Error: "upstream unavailable"}); err != nil {
		t.Fatal(err)
	}
	if cell := awaitCell(t, outcome); cell.err == nil || !strings.Contains(cell.err.Error(), "upstream unavailable") {
		t.Fatalf("handler error not surfaced: %v", cell.err)
	}
	// Arguments are validated against the schema before any executor sees them.
	if cell := awaitCell(t, execCell(t.Context(), parent, `tools.lookup(id=7)`)); cell.err == nil || !strings.Contains(cell.err.Error(), "invalid tool arguments") {
		t.Fatalf("schema violation error = %v", cell.err)
	}
	select {
	case invoke := <-conn.invoked:
		t.Fatalf("invalid arguments reached the executor: %+v", invoke)
	default:
	}
}

// Without a bound executor a call fails closed after a bounded wait; a deadline,
// a cancelled turn, or a disconnected executor settles the call without replay.
func TestCustomToolFailureSemantics(t *testing.T) {
	_, client := promptRuntimeProvider(t)
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createDefinitionRoot(t, store, toolingDefinition(t, store,
		agentdef.Tool{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)},
		agentdef.Tool{Name: "slow", InputSchema: json.RawMessage(`{"type":"object"}`), TimeoutMillis: 100},
	))
	owner, _, runtime := openPromptRuntime(t, store, rootID, client)
	owner.executors.bindWait = 50 * time.Millisecond
	parent := runtime.rootNode
	if cell := awaitCell(t, execCell(t.Context(), parent, `tools.lookup()`)); cell.err == nil || !strings.Contains(cell.err.Error(), "no executor is bound for agent definition tooling") {
		t.Fatalf("missing executor error = %v", cell.err)
	}
	conn, revision, generation := bindFakeExecutor(t, owner, store, "lookup", "slow")
	// Deadline.
	outcome := execCell(t.Context(), parent, `tools.slow()`)
	invoke := awaitInvoke(t, conn, outcome)
	if cell := awaitCell(t, outcome); cell.err == nil || !strings.Contains(cell.err.Error(), "timed out after 100ms") {
		t.Fatalf("deadline error = %v", cell.err)
	}
	if cancel := <-conn.cancelled; cancel.InvocationID != invoke.InvocationID || cancel.Reason != "timeout" {
		t.Fatalf("cancel = %+v", cancel)
	}
	if err := owner.executors.settle(conn, protocol.ToolResultParams{InvocationID: invoke.InvocationID, Generation: generation, Output: json.RawMessage(`1`)}); err == nil {
		t.Fatal("late result accepted after timeout")
	}
	// Cancelled turn.
	ctx, cancel := context.WithCancel(t.Context())
	outcome = execCell(ctx, parent, `tools.lookup()`)
	invoke = awaitInvoke(t, conn, outcome)
	cancel()
	if cell := awaitCell(t, outcome); cell.err == nil {
		t.Fatal("cancelled call succeeded")
	}
	if cancelled := <-conn.cancelled; cancelled.InvocationID != invoke.InvocationID || cancelled.Reason != "cancelled" {
		t.Fatalf("cancel = %+v", cancelled)
	}
	// Executor disconnect after dispatch: the call fails and is never replayed.
	outcome = execCell(t.Context(), parent, `tools.lookup()`)
	invoke = awaitInvoke(t, conn, outcome)
	close(conn.done)
	owner.executors.disconnect(conn)
	if cell := awaitCell(t, outcome); cell.err == nil || !strings.Contains(cell.err.Error(), "executor disconnected") {
		t.Fatalf("disconnect error = %v", cell.err)
	}
	replacement := newFakeExecutorConn()
	next := owner.executors.bind(replacement, "tooling", revision, []string{"lookup", "slow"}, nil)
	pending, err := owner.executors.pendingFor(replacement, protocol.ExecutorPendingParams{Definition: "tooling", Revision: revision, Generation: next})
	if err != nil || len(pending.Invocations) != 0 || len(pending.Hooks) != 0 {
		t.Fatalf("pending after reconnect = %v %v", pending, err)
	}
	if _, err := owner.executors.pendingFor(conn, protocol.ExecutorPendingParams{Definition: "tooling", Revision: revision, Generation: generation}); err == nil {
		t.Fatal("stale lease listed pending invocations")
	}
	// A pending call is listed for the holder and a replacing bind fails it.
	outcome = execCell(t.Context(), parent, `tools.lookup()`)
	invoke = awaitInvoke(t, replacement, outcome)
	pending, err = owner.executors.pendingFor(replacement, protocol.ExecutorPendingParams{Definition: "tooling", Revision: revision, Generation: next})
	if err != nil || len(pending.Invocations) != 1 || pending.Invocations[0].InvocationID != invoke.InvocationID {
		t.Fatalf("pending = %+v %v", pending, err)
	}
	owner.executors.bind(newFakeExecutorConn(), "tooling", revision, []string{"lookup", "slow"}, nil)
	if cell := awaitCell(t, outcome); cell.err == nil || !strings.Contains(cell.err.Error(), "replaced") {
		t.Fatalf("replaced executor error = %v", cell.err)
	}
}

// A child calls only the tools its parent left it, at the host boundary and in
// the ledger; its invocations carry its own identity.
func TestCustomToolChildNarrowingIsEnforced(t *testing.T) {
	_, client := promptRuntimeProvider(t)
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createDefinitionRoot(t, store, toolingDefinition(t, store))
	owner, _, runtime := openPromptRuntime(t, store, rootID, client)
	conn, _, generation := bindFakeExecutor(t, owner, store, "lookup", "other")
	child := spawnMCPChild(t, runtime.rootNode, map[string]any{"name": "narrow", "tools": []any{"other"}})
	waitAgentIdle(t, child)
	// The child's kernel never installed the tool; the host refuses it too.
	if cell := awaitCell(t, execCell(t.Context(), child, `tools.lookup(id="1")`)); cell.err == nil || !strings.Contains(cell.err.Error(), "has no .lookup attribute") {
		t.Fatalf("kernel binding error = %v", cell.err)
	}
	if _, err := child.host.Call(t.Context(), "tools", "lookup", map[string]any{"id": "1"}); err == nil || err.Error() != `tool "lookup" is not available to this agent` {
		t.Fatalf("host gate error = %v", err)
	}
	if _, err := child.agent.Services.InvokeTool(t.Context(), "lookup", "", json.RawMessage(`{"id":"1"}`)); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("ledger allowed a narrowed-away tool: %v", err)
	}
	outcome := execCell(t.Context(), child, `tools.other()`)
	invoke := awaitInvoke(t, conn, outcome)
	if invoke.AgentID != child.id || invoke.Tool != "other" {
		t.Fatalf("child invocation = %+v", invoke)
	}
	if err := owner.executors.settle(conn, protocol.ToolResultParams{InvocationID: invoke.InvocationID, Generation: generation, Output: json.RawMessage(`"ok"`)}); err != nil {
		t.Fatal(err)
	}
	if cell := awaitCell(t, outcome); cell.err != nil || cell.result.Value != "ok" {
		t.Fatalf("child cell = %#v %v", cell.result.Value, cell.err)
	}
}

// executor.bind resolves the definition revision and requires handlers for
// exactly the declared tools.
func TestExecutorBindValidatesDefinitionAndHandlers(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	toolingDefinition(t, store)
	record, err := store.LatestDefinition(t.Context(), "tooling")
	if err != nil {
		t.Fatal(err)
	}
	for name, params := range map[string]protocol.ExecutorBindParams{
		"unknown":          {Definition: "missing", Revision: "x", Tools: []string{"a"}},
		"built-in-rev":     {Definition: "coding", Revision: "x", Tools: []string{"a"}},
		"no-tools":         {Definition: "junior-developer", Tools: []string{"a"}},
		"missing-revision": {Definition: "tooling", Tools: []string{"lookup", "other"}},
		"missing-handler":  {Definition: "tooling", Revision: record.Revision, Tools: []string{"lookup"}},
		"extra-handler":    {Definition: "tooling", Revision: record.Revision, Tools: []string{"lookup", "other", "extra"}},
	} {
		if _, _, err := executorCoverage(t.Context(), store, params); err == nil {
			t.Fatalf("%s bind accepted", name)
		}
	}
	tools, hooks, err := executorCoverage(t.Context(), store, protocol.ExecutorBindParams{Definition: "tooling", Revision: record.Revision, Tools: []string{"other", "lookup"}})
	if err != nil || !slices.Equal(tools, []string{"lookup", "other"}) || hooks != nil {
		t.Fatalf("bind coverage = %v %v %v", tools, hooks, err)
	}
	if _, _, err := executorCoverage(t.Context(), store, protocol.ExecutorBindParams{Definition: "tooling", Revision: record.Revision, Tools: []string{"other", "lookup"}, Hooks: []string{"before_tool"}}); err == nil {
		t.Fatal("undeclared hook accepted")
	}
	// A definition with hooks and no tools binds on its hooks alone, and every
	// declared hook must be covered.
	hooked := agentdef.Coding()
	hooked.ID = "hooked"
	hooked.Hooks = &agentdef.Hooks{BeforeTool: &agentdef.Hook{}, TurnStart: &agentdef.Hook{Optional: true}}
	document, err := agentdef.Encode(hooked)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := registerDefinition(t.Context(), store, document, "hooks-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := executorCoverage(t.Context(), store, protocol.ExecutorBindParams{Definition: "hooked", Revision: registered.Revision, Hooks: []string{"before_tool"}}); err == nil || !strings.Contains(err.Error(), `no handler for hook "turn_start"`) {
		t.Fatalf("partial hook coverage = %v", err)
	}
	tools, hooks, err = executorCoverage(t.Context(), store, protocol.ExecutorBindParams{Definition: "hooked", Revision: registered.Revision, Hooks: []string{"turn_start", "before_tool"}})
	if err != nil || len(tools) != 0 || !slices.Equal(hooks, []string{"before_tool", "turn_start"}) {
		t.Fatalf("hook coverage = %v %v %v", tools, hooks, err)
	}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	server, err := NewServer(value, ServerOptions{BuildID: "test", Generation: 3})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	connection := &serverConn{ctx: t.Context(), server: server, client: InitializeParams{ClientID: "executor", ClientKind: "automation"}}
	raw, _ := json.Marshal(protocol.ToolResultParams{InvocationID: "nope", Generation: 1, Output: json.RawMessage(`1`)})
	if result, failure := server.handle(connection, rpcMessage{Method: "tool.result", Params: raw}); result != nil || failure == nil || failure.Code != -32009 {
		t.Fatalf("foreign result = %v, %+v", result, failure)
	}
	raw, _ = json.Marshal(protocol.ExecutorBindParams{Definition: "tooling", Revision: record.Revision, Tools: []string{"lookup", "other"}})
	result, failure := server.handle(connection, rpcMessage{Method: "executor.bind", Params: raw})
	if failure != nil || result.(protocol.ExecutorBindResult).Generation != 1 {
		t.Fatalf("bind = %+v, %+v", result, failure)
	}
	server.unregister(connection)
	raw, _ = json.Marshal(protocol.ExecutorPendingParams{Definition: "tooling", Revision: record.Revision, Generation: 1})
	if _, failure := server.handle(connection, rpcMessage{Method: "executor.pending", Params: raw}); failure == nil || failure.Code != -32003 {
		t.Fatalf("lease survived disconnect: %+v", failure)
	}
}

// A tool's declared output schema is enforced on every result: a value that
// does not match settles the call as an error the cell reads; a matching one
// returns as usual.
func TestCustomToolOutputSchemaIsEnforced(t *testing.T) {
	_, client := promptRuntimeProvider(t)
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createDefinitionRoot(t, store, toolingDefinition(t, store,
		agentdef.Tool{Name: "lookup", Description: "Fetch a ticket", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"}},"required":["id","title"]}`)},
	))
	owner, _, runtime := openPromptRuntime(t, store, rootID, client)
	parent := runtime.rootNode
	conn, _, generation := bindFakeExecutor(t, owner, store, "lookup")
	outcome := execCell(t.Context(), parent, `tools.lookup()`)
	invoke := awaitInvoke(t, conn, outcome)
	if err := owner.executors.settle(conn, protocol.ToolResultParams{InvocationID: invoke.InvocationID, Generation: generation, Output: json.RawMessage(`{"id":"42"}`)}); err != nil {
		t.Fatal(err)
	}
	if cell := awaitCell(t, outcome); cell.err == nil || !strings.Contains(cell.err.Error(), "tool lookup returned a value that does not match its output schema") {
		t.Fatalf("mismatch accepted: %#v %v", cell.result.Value, cell.err)
	}
	outcome = execCell(t.Context(), parent, `tools.lookup()`)
	invoke = awaitInvoke(t, conn, outcome)
	if err := owner.executors.settle(conn, protocol.ToolResultParams{InvocationID: invoke.InvocationID, Generation: generation, Output: json.RawMessage(`{"id":"42","title":"Login page times out"}`)}); err != nil {
		t.Fatal(err)
	}
	cell := awaitCell(t, outcome)
	if value, _ := cell.result.Value.(map[string]any); cell.err != nil || value["title"] != "Login page times out" {
		t.Fatalf("matching result rejected: %#v %v", cell.result.Value, cell.err)
	}
}
