package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func dispatchFixture(t *testing.T) (*store.Store, *Dispatcher, session.Session, session.Turn, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	_, _, ref, err := session.CanonicalDefinition(session.Builtins()[0])
	if err != nil {
		t.Fatal(err)
	}
	_, root, err := db.CreateTree(t.Context(), store.CreateTree{Engine: session.Starlark, Policy: session.DefaultTreePolicy(), Definition: ref, WorkingDirectory: t.TempDir(), Overrides: session.ConfigPatch{Model: &session.ModelSelection{Provider: "scripted", Name: "scripted"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Admit(t.Context(), session.RequestIdentity{ClientID: "fixture", RequestID: "input"}, store.Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "execute"}}}); err != nil {
		t.Fatal(err)
	}
	claim, err := db.Claim(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppendMessage(t.Context(), claim.Turn.ID, session.MessageDraft{ID: "call-message", Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{"code":"files.write(path='note.txt',content='once')"}`)}}}}); err != nil {
		t.Fatal(err)
	}
	if _, dispatch, err := db.BeginCell(t.Context(), session.CellSpec{ID: "cell", TurnID: claim.Turn.ID, CallMessageID: "call-message", CallID: "call"}); err != nil || !dispatch {
		t.Fatalf("begin cell: %v %v", dispatch, err)
	}
	return db, NewDispatcher(db, db, nil), root, claim.Turn, path
}

func invokeWrite(root session.Session, request string) Invocation {
	return Invocation{SessionID: root.ID, CellID: "cell", RequestID: request, Module: "files", Name: "write", Arguments: map[string]any{"path": "note.txt", "content": "once"}}
}

func awaitOperation(t *testing.T, db *store.Store, turn session.TurnID, request string, state session.OperationState) session.Operation {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		values, err := db.Operations(ctx, turn, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			if value.RequestID == request && value.State == state {
				return value
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("operation %s did not reach %s: %+v", request, state, values)
		case <-ticker.C:
		}
	}
}

func TestOneUsePermissionDoesNotGrantTheNextInvocation(t *testing.T) {
	db, dispatcher, root, turn, _ := dispatchFixture(t)
	for _, step := range []struct {
		id       string
		approved bool
	}{{"first", true}, {"second", false}} {
		done := make(chan error, 1)
		go func() { _, _, err := dispatcher.Call(t.Context(), invokeWrite(root, step.id)); done <- err }()
		pending := awaitOperation(t, db, turn.ID, step.id, session.OperationWaiting)
		if step.id == "first" {
			if _, err := os.Stat(filepath.Join(root.WorkingDirectory, "note.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("effect before approval: %v", err)
			}
		}
		if _, err := db.ResolvePermission(t.Context(), pending.ID, step.approved); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if (err == nil) != step.approved {
				t.Fatalf("approval=%v error=%v", step.approved, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("permission did not release handler")
		}
	}
	data, err := os.ReadFile(filepath.Join(root.WorkingDirectory, "note.txt"))
	if err != nil || string(data) != "once" {
		t.Fatalf("write=%q error=%v", data, err)
	}
	operations, err := db.Operations(t.Context(), turn.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range operations {
		if op.RequestID == "second" && (op.State != session.OperationDenied || op.DispatchedAt != nil) {
			t.Fatalf("denial dispatched: %+v", op)
		}
	}
}

func TestRevocationWhileWaitingForPathLockPreventsDispatch(t *testing.T) {
	db, dispatcher, root, turn, _ := dispatchFixture(t)
	grant, err := db.CreateGrant(t.Context(), session.Grant{ID: "standing", SessionID: root.ID, Capability: "files.write", Resource: root.WorkingDirectory})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := dispatcher.files.Prepare(root.WorkingDirectory, "files.write", invokeWrite(root, "held").Arguments)
	if err != nil {
		t.Fatal(err)
	}
	release, err := prepared.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	done := make(chan error, 1)
	go func() { _, _, err := dispatcher.Call(t.Context(), invokeWrite(root, "revoked")); done <- err }()
	admitted := awaitOperation(t, db, turn.ID, "revoked", session.OperationReady)
	if _, err := db.RevokeGrant(t.Context(), grant.ID); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revoked operation succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("revoked operation remained blocked")
	}
	if _, err := os.Stat(filepath.Join(root.WorkingDirectory, "note.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("revoked write happened: %v", err)
	}
	outcome, err := db.Operation(t.Context(), admitted.ID)
	if err != nil || outcome.State != session.OperationDenied || outcome.DispatchedAt != nil {
		t.Fatalf("revocation evidence=%+v error=%v", outcome, err)
	}
}

func TestPendingPermissionCancellationLeavesNoEffect(t *testing.T) {
	db, dispatcher, root, turn, _ := dispatchFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, _, err := dispatcher.Call(ctx, invokeWrite(root, "cancel")); done <- err }()
	pending := awaitOperation(t, db, turn.ID, "cancel", session.OperationWaiting)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled permission wait leaked")
	}
	outcome, err := db.Operation(t.Context(), pending.ID)
	if err != nil || outcome.State != session.OperationCancelled || outcome.DispatchedAt != nil {
		t.Fatalf("cancel evidence=%+v error=%v", outcome, err)
	}
	permissions, err := db.Permissions(t.Context(), root.ID, "", 100)
	if err != nil || len(permissions) != 1 || permissions[0].State != session.PermissionCancelled {
		t.Fatalf("permission=%+v error=%v", permissions, err)
	}
	if _, err := db.ResolvePermission(t.Context(), pending.ID, true); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("late approval revived cancellation: %v", err)
	}
}

func TestEffectSettlementRetryDoesNotRepeatFilesystemMutation(t *testing.T) {
	db, dispatcher, root, turn, path := dispatchFixture(t)
	if _, err := db.CreateGrant(t.Context(), session.Grant{ID: "standing", SessionID: root.ID, Capability: "files.write", Resource: root.WorkingDirectory}); err != nil {
		t.Fatal(err)
	}
	fault, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fault.Close() }()
	if _, err := fault.ExecContext(t.Context(), `CREATE TRIGGER fail_operation BEFORE UPDATE ON operations WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'injected settlement failure'); END`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, _, err := dispatcher.Call(t.Context(), invokeWrite(root, "retry-write")); done <- err }()
	admitted := awaitOperation(t, db, turn.ID, "retry-write", session.OperationDispatched)
	target := filepath.Join(root.WorkingDirectory, "note.txt")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		raw, err := os.ReadFile(target)
		if err == nil && string(raw) == "once" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("write never happened")
		case <-time.After(time.Millisecond):
		}
	}
	if err := os.WriteFile(target, []byte("external change after effect"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fault.ExecContext(t.Context(), "DROP TRIGGER fail_operation"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SQL settlement did not recover")
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "external change after effect" {
		t.Fatalf("settlement replayed effect: %q %v", raw, err)
	}
	outcome, err := db.Operation(t.Context(), admitted.ID)
	if err != nil || outcome.State != session.OperationSucceeded {
		t.Fatalf("outcome=%+v error=%v", outcome, err)
	}
	if _, _, err := dispatcher.Call(t.Context(), invokeWrite(root, "retry-write")); err == nil {
		t.Fatal("duplicate dispatch identity reran handler")
	}
}

type operationReadFault struct {
	Ledger
	failure error
}

func (f *operationReadFault) Operation(ctx context.Context, id session.OperationID) (session.Operation, error) {
	if f.failure != nil {
		err := f.failure
		f.failure = nil
		return session.Operation{}, err
	}
	return f.Ledger.Operation(ctx, id)
}

func TestPermissionReadFailureCancelsTheAdmittedWaiter(t *testing.T) {
	db, dispatcher, root, turn, _ := dispatchFixture(t)
	injected := errors.New("injected transient operation read failure")
	dispatcher.ledger = &operationReadFault{Ledger: db, failure: injected}
	_, id, err := dispatcher.Call(t.Context(), invokeWrite(root, "read-failure"))
	if !errors.Is(err, injected) {
		t.Fatalf("failure = %v", err)
	}
	outcome, err := db.Operation(t.Context(), id)
	if err != nil || outcome.State != session.OperationCancelled || outcome.DispatchedAt != nil {
		t.Fatalf("stranded operation: %+v %v", outcome, err)
	}
	permissions, err := db.Permissions(t.Context(), root.ID, "", 100)
	if err != nil || len(permissions) != 1 || permissions[0].State != session.PermissionCancelled {
		t.Fatalf("stranded permission: %+v %v", permissions, err)
	}
	if _, err := os.Stat(filepath.Join(root.WorkingDirectory, "note.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected effect: %v", err)
	}
	if _, err := db.SettleCell(t.Context(), "cell", session.CellFailed, session.ToolResult{CallID: "call", Output: "host read failed", IsError: true}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Finish(t.Context(), turn.ID, session.Failed, new("host read failed"), nil); err != nil {
		t.Fatal(err)
	}
}
