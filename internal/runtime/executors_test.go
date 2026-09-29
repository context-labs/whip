package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/executor"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func executorDefinition() session.DefinitionDocument {
	return session.DefinitionDocument{ID: "custom", Name: "Custom", Defaults: session.ConfigPatch{
		Modules: []string{}, Tools: map[string]session.ToolDeclaration{"lookup": {
			InputSchema:  json.RawMessage(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}},"additionalProperties":false}`),
			OutputSchema: json.RawMessage(`{"type":"object","required":["value"],"properties":{"value":{"type":"integer"}},"additionalProperties":false}`),
		}},
	}}
}

func executorPeer(t *testing.T, r *Runtime, definition session.DefinitionRevision) (*executor.Peer, executor.Lease) {
	t.Helper()
	peer, err := r.ExecutorPeer()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(peer.Close)
	lease, err := peer.Bind(t.Context(), definition.Document, executor.Coverage{Tools: slices.Sorted(maps.Keys(definition.Document.Defaults.Tools)), Hooks: slices.Sorted(maps.Keys(definition.Document.Defaults.Hooks))})
	if err != nil {
		t.Fatal(err)
	}
	return peer, lease
}

func executorNext(t *testing.T, peer *executor.Peer) executor.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	event, err := peer.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func executorRoot(t *testing.T, r *Runtime, engine session.Engine) (session.Session, session.DefinitionRevision) {
	t.Helper()
	definition, err := r.RegisterDefinition(t.Context(), executorDefinition())
	if err != nil {
		t.Fatal(err)
	}
	_, root, err := r.CreateTree(t.Context(), store.CreateTree{
		Engine: engine, Definition: definition.Ref, WorkingDirectory: t.TempDir(),
		Overrides: session.ConfigPatch{Model: &session.ModelSelection{Provider: "test", Name: "test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return root, definition
}

func TestBothEnginesCustomExecutorPublicationFollowsPermissionAndDispatch(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := "print(tools.lookup(id=\"item\")[\"value\"])"
			if engine == session.QuickJS {
				code = "console.log((await tools.lookup({id:'item'})).value)"
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"run": code}))
			root, definition := executorRoot(t, r, engine)
			old, oldLease := executorPeer(t, r, definition)
			submitTest(t, r, root.ID, "run")
			permission := awaitRuntimeFilePermission(t, r, root.ID, "run", "tools.lookup")
			pending, err := old.Pending(oldLease, "")
			if err != nil || len(pending.Items) != 0 {
				t.Fatal("permission wait captured or published to an executor", pending, err)
			}
			// A permission wait can outlive a holder: capture the current lease only
			// after approval, without sending work to the retired connection.
			peer, lease := executorPeer(t, r, definition)
			if _, err := r.ResolvePermission(t.Context(), permission.ID, true); err != nil {
				t.Fatal(err)
			}
			event := executorNext(t, peer)
			invocation := event.Invocation
			if event.Type != "invoke" || invocation.Request.SessionID != root.ID || invocation.Request.TurnID != permission.TurnID || invocation.Request.OperationID != permission.ID || invocation.Lease.Generation != lease.Generation || invocation.Lease.Definition != definition.Ref {
				t.Fatal("executor scope changed", event)
			}
			operation, err := r.Operation(t.Context(), permission.ID)
			if err != nil || operation.State != session.OperationDispatched || operation.DispatchedAt == nil {
				t.Fatal("receiver ran before durable dispatch", operation, err)
			}
			if err := peer.Settle(lease.Epoch, lease.Generation, invocation.ID, executor.Tool, executor.Result{Value: json.RawMessage(`{"value":7}`)}); err != nil {
				t.Fatal(err)
			}
			admitted := waitTestWithin(t, r, "run", terminal, 30*time.Second)
			operation, err = r.Operation(t.Context(), permission.ID)
			if err != nil || admitted.Turn.State != session.Succeeded || operation.State != session.OperationSucceeded || string(operation.Result.Value) != `{"value":7}` {
				t.Fatal("custom result did not settle", admitted.Turn, operation, err)
			}
			history, err := r.History(t.Context(), root.ID, 0, 100)
			if err != nil || !strings.Contains(history[len(history)-1].Parts[0].Text, `7\n`) {
				t.Fatal("engine did not receive custom result", history, err)
			}
		})
	}
}

func TestCustomExecutorConfirmedFailureUncertainDisconnectAndSchemaEvidence(t *testing.T) {
	for _, finish := range []string{"failed", "schema", "disconnect", "cancel"} {
		t.Run(finish, func(t *testing.T) {
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"run": "tools.lookup(id=\"item\")"}))
			root, definition := executorRoot(t, r, session.Starlark)
			peer, lease := executorPeer(t, r, definition)
			if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "custom", SessionID: root.ID, Capability: "tools.lookup", Resource: definition.Ref.ID + "@" + definition.Ref.Revision}); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, root.ID, "run")
			event := executorNext(t, peer)
			invocation := event.Invocation
			want := session.OperationFailed
			switch finish {
			case "failed":
				if err := peer.Settle(lease.Epoch, lease.Generation, invocation.ID, executor.Tool, executor.Result{Failure: "handler declined"}); err != nil {
					t.Fatal(err)
				}
			case "schema":
				if err := peer.Settle(lease.Epoch, lease.Generation, invocation.ID, executor.Tool, executor.Result{Value: json.RawMessage(`{"value":"bad"}`)}); err != nil {
					t.Fatal(err)
				}
			case "disconnect":
				peer.Close()
				want = session.OperationUncertain
			case "cancel":
				if _, err := r.CancelTurn(t.Context(), invocation.Request.TurnID); err != nil {
					t.Fatal(err)
				}
				want = session.OperationUncertain
				if notice := executorNext(t, peer); notice.Type != "cancel" || notice.ID != invocation.ID {
					t.Fatal("executor cancellation missing", notice)
				}
			}
			waitTestWithin(t, r, "run", terminal, 30*time.Second)
			operation, err := r.Operation(t.Context(), invocation.Request.OperationID)
			if err != nil || operation.State != want || operation.Result == nil || operation.Result.Failure == nil {
				t.Fatal("wrong custom failure evidence", operation, err)
			}
			if finish == "schema" && string(operation.Result.Value) != `{"value":"bad"}` {
				t.Fatal("observed invalid output was lost", operation.Result)
			}
			if err := peer.Settle(lease.Epoch, lease.Generation, invocation.ID, executor.Tool, executor.Result{Value: json.RawMessage(`{"value":7}`)}); !errors.Is(err, executor.ErrConflict) {
				t.Fatal("late completion revived operation", err)
			}
		})
	}
}

func TestCustomExecutorAbsentAndUnownedDeclarationsRemainUnavailable(t *testing.T) {
	r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"run": "tools.lookup(id=\"item\")", "unowned": "tools.lookup(id=\"item\")"}))
	root, definition := executorRoot(t, r, session.Starlark)
	if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "custom", SessionID: root.ID, Capability: "tools.lookup", Resource: definition.Ref.ID + "@" + definition.Ref.Revision}); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, root.ID, "run")
	admission := waitTestWithin(t, r, "run", terminal, 30*time.Second)
	operations, err := r.Operations(t.Context(), admission.Turn.ID, "", 100)
	if err != nil || len(operations) != 1 || operations[0].State != session.OperationCancelled || operations[0].DispatchedAt != nil {
		t.Fatal("missing executor dispatched work", operations, err)
	}
	base, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	_, unowned, err := r.CreateTree(t.Context(), store.CreateTree{
		Engine: session.Starlark, Definition: base[0], WorkingDirectory: t.TempDir(),
		Overrides: session.ConfigPatch{Model: &session.ModelSelection{Provider: "test", Name: "test"}, Tools: executorDefinition().Defaults.Tools},
	})
	if err != nil {
		t.Fatal(err)
	}
	bindingCellFailure(t, r, unowned.ID, "unowned", "custom tool executor unavailable")
}
