package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
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
