package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

// A session without an actor lets these tests place shutdown precisely between
// durable command admission and supervised worker launch.
func controlBoundarySession(t *testing.T) *Session {
	t.Helper()
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	root := createRoot(t, store)
	meta, _, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	supervisor := newSupervisor()
	t.Cleanup(supervisor.stop)
	return &Session{store: store, meta: meta, authority: authority, supervisor: supervisor}
}

func TestClientCommandsSettleWhenWorkerAdmissionCloses(t *testing.T) {
	for _, test := range []struct {
		operation string
		payload   string
	}{
		{"session.reload", `{}`},
		{"mcp.refresh", `{}`},
		{"mcp.reconnect", `{"name":"alpha"}`},
		{"tool.call", `{"tool":"files.list","arguments":{}}`},
		{"shell.run", `{"command":"printf never"}`},
		{"history.compact", `{}`},
		{"goal.from-context", `{"window":2}`},
		{"history.rewind", `{"cut":1}`},
	} {
		t.Run(test.operation, func(t *testing.T) {
			root := controlBoundarySession(t)
			root.runner = &failingAsyncRunner{fakeRunner: &fakeRunner{}}
			if test.operation == "tool.call" {
				root.runner = NewToolRunner(tools.NewServices())
				t.Cleanup(root.runner.Close)
			}
			manager := &controlSurfaceMCP{}
			root.mcp = manager
			root.factory = func(context.Context, session.Meta, []llm.Message) (Components, error) {
				t.Error("replacement factory ran after worker admission closed")
				return Components{}, ErrStopped
			}
			admission := session.CommandAdmission{
				ClientID: "test", CommandID: "shutdown", Scope: session.CommandScopeRoot,
				RootID: root.ID(), AgentID: root.authority.AgentID, Kind: test.operation,
				RequestDigest: test.operation,
				Payload:       session.RuntimePayload{Data: []byte(test.payload), MediaType: "application/json"},
			}
			admitted, err := root.store.AdmitControlCommand(t.Context(), admission)
			if err != nil {
				t.Fatal(err)
			}
			root.supervisor.stop()
			started, err := root.executeClientCommand(
				t.Context(), admission, test.operation, json.RawMessage(test.payload), admitted,
				make(chan clientCommandReply, 1),
			)
			if err != nil || started.result.Status != "failed" || !strings.Contains(started.result.Error, ErrStopped.Error()) {
				t.Fatalf("shutdown outcome = %+v, %v", started.result, err)
			}
			if started.reply != nil || root.clientBusy || root.clientPreparing || root.clientIntegrations != 0 {
				t.Fatal("rejected worker retained a waiter or busy flag")
			}
			if len(manager.actions) != 0 {
				t.Fatalf("integration ran after shutdown: %v", manager.actions)
			}
			retried, err := root.store.AdmitControlCommand(t.Context(), admission)
			if err != nil || retried.New || retried.Command.Status != "failed" {
				t.Fatalf("terminal failure was not durable: %+v, %v", retried, err)
			}
		})
	}
}

type cancellationControlRuntime struct {
	fakeCloser
	cancelled bool
	id        string
}

func (r *cancellationControlRuntime) CancelAgentTurn(id string) bool {
	r.id = id
	return r.cancelled
}

func TestClientAgentCancellationReportsRuntimeOutcome(t *testing.T) {
	root := &Session{}
	if _, err := root.clientAgentTurnCancel("child"); err == nil {
		t.Fatal("unsupported runtime accepted cancellation")
	}
	runtime := &cancellationControlRuntime{}
	root.runtime = runtime
	for _, test := range []struct {
		cancelled bool
		want      string
	}{{false, "already idle"}, {true, "cancellation requested"}} {
		runtime.cancelled = test.cancelled
		output, err := root.clientAgentTurnCancel("child")
		if err != nil || output != test.want || runtime.id != "child" {
			t.Fatalf("cancelled=%t output=%q id=%q err=%v", test.cancelled, output, runtime.id, err)
		}
	}
}

func TestClientActionsPropagateCancelledStoreReads(t *testing.T) {
	root := controlBoundarySession(t)
	root.runner = &controlSurfaceRunner{fakeRunner: &fakeRunner{}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		operation string
		payload   string
	}{
		{"session.archive", `{"archived":true}`},
		{"permission.rules", `{}`},
		{"permission.forget", `{"id":"rule"}`},
		{"history.clear", `{}`},
		{"agent.transcript", `{"id":"missing"}`},
		{"agent.submit", `{"id":"missing","text":"never admitted"}`},
	} {
		t.Run(test.operation, func(t *testing.T) {
			_, err := root.applyClientCommand(ctx, test.operation, json.RawMessage(test.payload))
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled command returned %v", err)
			}
		})
	}
	meta, _, err := root.store.Load(root.ID())
	if err != nil || meta.Archived {
		t.Fatalf("cancelled archive mutated metadata: %+v, %v", meta, err)
	}
	if root.runner.(*controlSurfaceRunner).replaced != nil {
		t.Fatal("cancelled history clear replaced live history")
	}
}

type reloadBoundaryRunner struct {
	*fakeRunner
	replaceErr error
}

func (r *reloadBoundaryRunner) CanReplace() error { return r.replaceErr }
func (r *reloadBoundaryRunner) ContextAudit() ContextAuditResult {
	return ContextAuditResult{}
}

func TestClientDeferredReloadPreservesLifecycleBoundaries(t *testing.T) {
	t.Run("active runner defers replacement", func(t *testing.T) {
		root := controlBoundarySession(t)
		root.runner = &reloadBoundaryRunner{fakeRunner: &fakeRunner{}, replaceErr: errors.New("active tool")}
		root.reloadPending = true
		root.startPendingReload()
		if !root.reloadPending || root.clientBusy || root.clientPreparing {
			t.Fatal("runner rejected replacement but reload did not remain pending")
		}
	})
	t.Run("shutdown releases preparation state", func(t *testing.T) {
		root := controlBoundarySession(t)
		root.runner = &fakeRunner{}
		root.reloadPending = true
		root.supervisor.stop()
		root.startPendingReload()
		if root.clientBusy || root.clientPreparing {
			t.Fatal("shutdown left deferred reload busy")
		}
	})
	t.Run("missing factory settles automatic failure", func(t *testing.T) {
		root := controlBoundarySession(t)
		root.runner = &fakeRunner{}
		root.reloadPending = true
		root.startPendingReload()
		root.supervisor.wait()
		events := root.supervisor.take()
		if len(events) != 1 || events[0].client == nil || events[0].client.err == nil {
			t.Fatalf("reload completion = %+v", events)
		}
		if _, err := root.completeClientCommand(events[0].client); err != nil {
			t.Fatal(err)
		}
		if root.clientBusy || root.clientPreparing || root.reloadPending {
			t.Fatal("automatic failure retained pending or busy state")
		}
	})
}

func (r *cancellationControlRuntime) ControlAgent(context.Context, string, string) error {
	return session.ErrAgentTerminal
}

func TestClientControlRejectsInvalidTargetsAndInputs(t *testing.T) {
	root := controlBoundarySession(t)
	root.runner = &reloadBoundaryRunner{fakeRunner: &fakeRunner{}}
	root.runtime = &cancellationControlRuntime{cancelled: true}
	root.mcp = &controlSurfaceMCP{}
	if isClientOperation("not.a.command") {
		t.Fatal("unknown command was allowed")
	}
	for _, test := range []struct {
		operation string
		payload   string
		want      string
	}{
		{"mcp.attach", `{"servers":7}`, "invalid MCP attachment"},
		{"session.fork", `{"expected_revision":"999"}`, "revision"},
		{"agent.submit", `{"id":"","text":"hello"}`, "requires an agent"},
		{"agent.submit", `{"id":"` + root.authority.AgentID + `","text":"hello"}`, "use submit"},
		{"mcp.reconnect", `{"name":""}`, "name is required"},
	} {
		t.Run(test.operation+test.want, func(t *testing.T) {
			_, err := root.applyClientCommand(t.Context(), test.operation, json.RawMessage(test.payload))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid control returned %v, want %q", err, test.want)
			}
		})
	}
	if _, err := applyMCPAction(root.mcp.(*controlSurfaceMCP), "mcp.unknown", "alpha"); err == nil {
		t.Fatal("unknown MCP action was accepted")
	}
	for _, test := range []struct {
		operation string
		payload   string
		want      string
	}{
		{"agent.control", `{"id":"child"}`, "stopped"},
		{"mcp.reconnect", `{"name":"alpha"}`, "alpha: reconnect"},
	} {
		output, err := root.applyClientCommand(t.Context(), test.operation, json.RawMessage(test.payload))
		if err != nil || output != test.want {
			t.Fatalf("%s returned %q, %v", test.operation, output, err)
		}
	}
	output, err := root.applyClientCommand(t.Context(), "context.audit", json.RawMessage(`{}`))
	want, _ := json.Marshal(ContextAuditResult{})
	if err != nil || output != string(want) {
		t.Fatalf("context audit = %q, %v", output, err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("WHIPCODE_HOME"), "config.json"), []byte("{invalid configuration"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := root.applyClientCommand(t.Context(), "provider.catalogs", json.RawMessage(`{}`)); err == nil {
		t.Fatal("catalog lookup accepted malformed configuration")
	}
	root.clientBusy = true
	if _, err := root.applyClientCommand(t.Context(), "history.compact.undo", json.RawMessage(`{}`)); err == nil {
		t.Fatal("compaction undo was admitted while busy")
	}
}
