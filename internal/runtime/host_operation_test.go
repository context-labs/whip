package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/executor"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func directRuntime(t *testing.T) (*Runtime, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	r := openTest(t, t.TempDir(), providerFunc(func(context.Context, model.Request) (model.Response, error) {
		calls.Add(1)
		return model.Response{}, errors.New("model must not run")
	}))
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r, &calls
}

func admitDirect(t *testing.T, r *Runtime, owner session.SessionID, key, module, name, args string) store.Admission {
	t.Helper()
	a, err := r.AdmitHostOperation(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key}, owner, session.HostOperation{Module: module, Name: name, Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestHostOperationBothEnginesModelFreeAndObserverCancellation(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			r, calls := directRuntime(t)
			refs, err := r.Builtins()
			if err != nil {
				t.Fatal(err)
			}
			_, root, err := r.CreateTree(t.Context(), store.CreateTree{Engine: engine, Definition: refs[0], WorkingDirectory: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if !root.Config.Model.Equal(session.ModelSelection{}) {
				t.Fatal("model-free root fabricated selection", root.Config.Model)
			}
			if _, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "prompt"}, store.Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "hello"}}}); !errors.Is(err, session.ErrInvalid) {
				t.Fatal("model-free prompt accepted", err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			a, err := r.AdmitHostOperation(ctx, session.RequestIdentity{ClientID: "test", RequestID: "direct"}, root.ID, session.HostOperation{Module: "shell", Name: "run", Arguments: json.RawMessage(`{"command":"printf model-free"}`)})
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			permission := awaitRuntimeFilePermission(t, r, root.ID, "direct", "shell.run")
			if permission.CellID != "" || permission.DirectTurnID == "" {
				t.Fatal(permission)
			}
			if _, err := r.ResolvePermission(t.Context(), permission.ID, true); err != nil {
				t.Fatal(err)
			}
			finished := waitTest(t, r, "direct", terminal)
			if finished.Turn.State != session.Succeeded {
				t.Fatal(finished)
			}
			ops, err := r.Operations(t.Context(), finished.Turn.ID, "", 10)
			if err != nil || len(ops) != 1 || !strings.Contains(string(ops[0].Result.Value), "model-free") {
				t.Fatal(ops, err)
			}
			if attempts, err := r.ModelAttempts(t.Context(), finished.Turn.ID, "", 10); err != nil || len(attempts) != 0 {
				t.Fatal(attempts, err)
			}
			if cells, err := r.Cells(t.Context(), finished.Turn.ID, "", 10); err != nil || len(cells) != 0 {
				t.Fatal(cells, err)
			}
			if history, err := r.History(t.Context(), root.ID, 0, 10); err != nil || len(history) != 0 {
				t.Fatal(history, err)
			}
			if calls.Load() != 0 {
				t.Fatal("provider ran")
			}
			if retry := admitDirect(t, r, root.ID, "direct", "shell", "run", `{"command":"printf model-free"}`); retry.Input.ID != a.Input.ID {
				t.Fatal("retry duplicated work")
			}
			// Explicit model configuration enables future prompts; it does not alter
			// the completed direct turn's captured model-free configuration.
			updated, err := r.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "test", Name: "test"}})
			if err != nil {
				t.Fatal(err)
			}
			if updated.Config.Model.Provider != "test" {
				t.Fatal(updated)
			}
		})
	}
}

func TestHostOperationQueuesPromptsAndExplicitCancellationSettles(t *testing.T) {
	r, _ := directRuntime(t)
	root := createTest(t, r)
	admitDirect(t, r, root.ID, "direct", "shell", "run", `{"command":"sleep 30"}`)
	permission := awaitRuntimeFilePermission(t, r, root.ID, "direct", "shell.run")
	queued := submitTest(t, r, root.ID, "after")
	if queued.Turn != nil {
		t.Fatal("prompt overtook direct work")
	}
	if _, err := r.AdmitHostOperation(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "busy"}, root.ID, session.HostOperation{Module: "files", Name: "read", Arguments: json.RawMessage(`{"path":"file"}`)}); !errors.Is(err, store.ErrBusy) {
		t.Fatal(err)
	}
	if _, err := r.ResolvePermission(t.Context(), permission.ID, true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		operation, err := r.Operation(t.Context(), permission.ID)
		if err != nil {
			t.Fatal(err)
		}
		if operation.State == session.OperationDispatched {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not dispatched")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := r.CancelTurn(t.Context(), permission.TurnID); err != nil {
		t.Fatal(err)
	}
	finished := waitTest(t, r, "direct", terminal)
	if finished.Turn.State != session.Cancelled {
		t.Fatal(finished)
	}
	op, err := r.Operation(t.Context(), permission.ID)
	if err != nil || op.State != session.OperationUncertain {
		t.Fatal(op, err)
	}
	waitTest(t, r, "after", terminal)
}

func TestHostOperationRequiredOptionalHooksAndCustomProvenance(t *testing.T) {
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "optional"}[optional], func(t *testing.T) {
			r, _ := directRuntime(t)
			document := executorDefinition()
			document.Defaults.Hooks = map[string]session.HookDeclaration{"before_tool": {Optional: optional}}
			root, definition := hookRoot(t, r, session.Starlark, document)
			peer, lease := executorPeer(t, r, definition)
			admitDirect(t, r, root.ID, "direct", "tools", "lookup", `{"id":"original"}`)
			before := executorNext(t, peer)
			if !before.Invocation.Request.HostOperation || before.Invocation.Request.CellID != "" || before.Invocation.Request.TurnID == "" {
				t.Fatal(before)
			}
			if !optional {
				settleHook(t, peer, before, executor.Result{Decision: "deny", Reason: "required policy"})
				finished := waitTest(t, r, "direct", terminal)
				ops, err := r.Operations(t.Context(), finished.Turn.ID, "", 10)
				if err != nil || len(ops) != 0 || finished.Turn.State != session.Failed {
					t.Fatal(finished, ops, err)
				}
				return
			}
			settleHook(t, peer, before, executor.Result{Arguments: json.RawMessage(`{"id":"rewritten"}`)})
			permission := awaitRuntimeFilePermission(t, r, root.ID, "direct", "tools.lookup")
			if !strings.Contains(string(permission.Arguments), "rewritten") {
				t.Fatal(permission)
			}
			if _, err := r.ResolvePermission(t.Context(), permission.ID, true); err != nil {
				t.Fatal(err)
			}
			invoke := executorNext(t, peer)
			if !invoke.Invocation.Request.HostOperation || invoke.Invocation.Request.CellID != "" || invoke.Invocation.Lease.Definition != definition.Ref {
				t.Fatal(invoke)
			}
			if err := peer.Settle(lease.Epoch, lease.Generation, invoke.ID, executor.Tool, executor.Result{Value: json.RawMessage(`{"value":7}`)}); err != nil {
				t.Fatal(err)
			}
			if finished := waitTest(t, r, "direct", terminal); finished.Turn.State != session.Succeeded {
				t.Fatal(finished)
			}
		})
	}
}

func TestHostOperationFileScopeAndCapturedModules(t *testing.T) {
	r, _ := directRuntime(t)
	root := createTest(t, r)
	if err := os.WriteFile(filepath.Join(root.WorkingDirectory, "file"), []byte("scoped"), 0o600); err != nil {
		t.Fatal(err)
	}
	admitDirect(t, r, root.ID, "direct", "files", "read", `{"path":"file"}`)
	permission := awaitRuntimeFilePermission(t, r, root.ID, "direct", "files.read")
	if _, err := r.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Modules: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ResolvePermission(t.Context(), permission.ID, true); err != nil {
		t.Fatal(err)
	}
	finished := waitTest(t, r, "direct", terminal)
	if finished.Turn.State != session.Succeeded {
		t.Fatal(finished)
	}
	admitDirect(t, r, root.ID, "disabled", "files", "read", `{"path":"file"}`)
	if finished := waitTest(t, r, "disabled", terminal); finished.Turn.State != session.Failed {
		t.Fatal("disabled module ran", finished)
	}
}

func TestHostOperationModelFreeOwnerCannotDispatchAutonomousModelWork(t *testing.T) {
	r, calls := directRuntime(t)
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	_, root, err := r.CreateTree(t.Context(), store.CreateTree{Engine: session.Starlark, Definition: refs[0], WorkingDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "compact"}, store.Submission{SessionID: root.ID, Source: session.UserInput, Kind: session.CompactInput}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("model-free compaction admitted", err)
	}
	if _, err := r.CreateSchedule(t.Context(), root.ID, "schedule", session.ScheduleSpec{Expression: "@every 1s", Parts: []session.Part{{Type: "text", Text: "scheduled"}}}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("model-free scheduled prompt admitted", err)
	}
	if _, err := r.SendMail(t.Context(), session.MailSpec{ID: "mail", SenderID: root.ID, RecipientID: root.ID, Delivery: session.MailQueued, Subject: "wake", Body: "observe"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		history, err := r.History(t.Context(), root.ID, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) > 0 {
			turn, err := r.Turn(t.Context(), history[0].TurnID)
			if err != nil {
				t.Fatal(err)
			}
			if turn.State.Terminal() {
				if turn.State != session.Failed || turn.Failure == nil || !strings.Contains(*turn.Failure, "no configured model") {
					t.Fatal(turn)
				}
				attempts, err := r.ModelAttempts(t.Context(), turn.ID, "", 10)
				if err != nil || len(attempts) != 0 {
					t.Fatal(attempts, err)
				}
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("mail turn did not fail closed")
		}
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != 0 || r.Err() != nil {
		t.Fatal("model-free work contacted provider or stopped runtime", calls.Load(), r.Err())
	}
}
