package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func directInput(t *testing.T, s *Store, owner session.SessionID, key string) Admission {
	t.Helper()
	a, err := s.AdmitHostOperation(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key}, owner, session.HostOperation{Module: "shell", Name: "run", Arguments: json.RawMessage(`{"command":"printf ok"}`)})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestHostOperationQueueRecoveryAndDeletionReceipt(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	accepted := directInput(t, s, owner.ID, "human")
	if accepted.Input.HostOperation == nil || accepted.Input.Kind != session.HostOperationInputKind || len(accepted.Input.Parts) != 0 {
		t.Fatal(accepted)
	}
	if _, err := s.AdmitHostOperation(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "other"}, owner.ID, *accepted.Input.HostOperation); !errors.Is(err, ErrBusy) {
		t.Fatal("concurrent direct admission", err)
	}
	if retry := directInput(t, s, owner.ID, "human"); retry.Input.ID != accepted.Input.ID {
		t.Fatal("duplicate retry")
	}
	if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "compact-busy"}, Submission{SessionID: owner.ID, Source: session.UserInput, Kind: session.CompactInput}); !errors.Is(err, ErrBusy) {
		t.Fatal("maintenance overtook direct action", err)
	}
	submit(t, s, owner.ID, "queued-prompt")
	work := claim(t, s, owner.ID)
	if work.Input.ID != accepted.Input.ID || work.Turn.Kind != session.HostOperationInputKind || work.Turn.Goal != nil {
		t.Fatal(work)
	}
	spec := session.OperationSpec{ID: "direct-operation", DirectTurnID: work.Turn.ID, RequestID: "human", Capability: "shell.run", Resource: owner.WorkingDirectory, Arguments: accepted.Input.HostOperation.Arguments}
	op := admitOperation(t, s, spec)
	if op.CellID != "" || op.DirectTurnID != work.Turn.ID || op.TurnID != work.Turn.ID || op.State != session.OperationWaiting {
		t.Fatal(op)
	}
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	if dispatch, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !dispatch {
		t.Fatal(dispatch, err)
	}
	if _, err := s.Finish(t.Context(), work.Turn.ID, session.Succeeded, nil, nil); !errors.Is(err, ErrBusy) {
		t.Fatal("unfinished direct effect escaped turn", err)
	}
	if _, err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	op, err := s.Operation(t.Context(), op.ID)
	if err != nil || op.State != session.OperationUncertain {
		t.Fatal(op, err)
	}
	retry := directInput(t, s, owner.ID, "human")
	if retry.Turn.State != session.Interrupted {
		t.Fatal(retry)
	}
	for _, table := range []string{"cells", "model_attempts", "messages", "history_groups"} {
		if count(t, s, table) != 0 {
			t.Fatal("fabricated execution", table)
		}
	}
	if got, err := s.Operations(t.Context(), work.Turn.ID, "", 100); err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	retry = directInput(t, s, owner.ID, "human")
	if retry.Input != nil || retry.Receipt.DeletedAt == nil {
		t.Fatal("deleted direct work recreated", retry)
	}
	changed := *accepted.Input.HostOperation
	changed.Arguments = json.RawMessage(`{"command":"different"}`)
	if _, err := s.AdmitHostOperation(t.Context(), accepted.Receipt.RequestIdentity, owner.ID, changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestHostOperationOwnershipAndSQLGuards(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	accepted := directInput(t, s, owner.ID, "direct")
	work := claim(t, s, owner.ID)
	if _, err := s.AppendMessage(t.Context(), work.Turn.ID, session.MessageDraft{ID: "fake-message", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "fake"}}}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("direct work created conversation", err)
	}
	if _, err := s.ReserveModelAttempt(t.Context(), attemptRequest(work.Turn.ID, "fake-attempt")); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("direct work fabricated model attempt", err)
	}
	spec := session.OperationSpec{ID: "direct", DirectTurnID: work.Turn.ID, RequestID: "direct", Capability: "shell.run", Resource: owner.WorkingDirectory, Arguments: accepted.Input.HostOperation.Arguments}
	for _, capability := range []string{"user.ask", "mcp.catalog", "models.call", "agents.spawn", "tools.hidden"} {
		bad := spec
		bad.Capability = capability
		if _, err := s.AdmitOperation(t.Context(), bad); !errors.Is(err, ErrConflict) {
			t.Fatal(capability, err)
		}
	}
	if _, err := s.HostTurnSession(t.Context(), "other", work.Turn.ID); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	op := admitOperation(t, s, spec)
	mustFail(t, s, "UPDATE operations SET direct_turn_id='other' WHERE id=?", op.ID)
	mustFail(t, s, "UPDATE host_operation_inputs SET name='write' WHERE input_id=?", accepted.Input.ID)
	if _, err := s.CancelTurn(t.Context(), work.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); !errors.Is(err, ErrStopped) {
		t.Fatal("cancelled turn approved", err)
	}
	if _, err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	op, err := s.Operation(t.Context(), op.ID)
	if err != nil || op.State != session.OperationCancelled {
		t.Fatal(op, err)
	}
}

func TestHostOperationAdmissionRollbackCancellationAndConcurrentRetry(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	identity := session.RequestIdentity{ClientID: "test", RequestID: "stable"}
	operation := session.HostOperation{Module: "shell", Name: "run", Arguments: json.RawMessage(`{"command":"printf ok"}`)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.AdmitHostOperation(ctx, identity, owner.ID, operation); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	execTest(t, s, `CREATE TRIGGER reject_host_payload BEFORE INSERT ON host_operation_inputs BEGIN SELECT RAISE(ABORT,'injected rollback'); END`)
	if _, err := s.AdmitHostOperation(t.Context(), identity, owner.ID, operation); err == nil {
		t.Fatal("injected failure accepted")
	}
	for _, table := range []string{"inputs", "receipts", "host_operation_inputs"} {
		if count(t, s, table) != 0 {
			t.Fatal("partial admission", table)
		}
	}
	execTest(t, s, "DROP TRIGGER reject_host_payload")
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() {
			if _, err := s.AdmitHostOperation(t.Context(), identity, owner.ID, operation); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	for _, table := range []string{"inputs", "receipts", "host_operation_inputs"} {
		if count(t, s, table) != 1 {
			t.Fatal("duplicate admission", table)
		}
	}
}

func TestHostOperationModeChangeRetiresDirectAuthority(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "automatic"}[automatic], func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			if automatic {
				setModeTest(t, s, owner.ID, "automatic", 1, session.PermissionAutomatic)
			}
			a := directInput(t, s, owner.ID, "direct")
			work := claim(t, s, owner.ID)
			op := admitOperation(t, s, session.OperationSpec{ID: "direct", DirectTurnID: work.Turn.ID, RequestID: "direct", Capability: "shell.run", Resource: owner.WorkingDirectory, Arguments: a.Input.HostOperation.Arguments})
			if automatic {
				setModeTest(t, s, owner.ID, "prompt", 2, session.PermissionPrompt)
			} else {
				setModeTest(t, s, owner.ID, "automatic", 1, session.PermissionAutomatic)
			}
			read, err := s.Operation(t.Context(), op.ID)
			if err != nil || read.State != session.OperationDenied {
				t.Fatal(read, err)
			}
			if ok, err := s.DispatchOperation(t.Context(), op.ID); ok || err != nil {
				t.Fatal(ok, err)
			}
		})
	}
}

func TestHostOperationChildCannotUseHumanOriginToBroadenDelegation(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	initial := claim(t, s, child.Session.ID)
	if _, err := s.Finish(t.Context(), initial.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	accepted := directInput(t, s, child.Session.ID, "direct")
	work := claim(t, s, child.Session.ID)
	spec := session.OperationSpec{ID: "child-direct", DirectTurnID: work.Turn.ID, RequestID: "direct", Capability: "shell.run", Resource: child.Session.WorkingDirectory, Arguments: accepted.Input.HostOperation.Arguments}
	op := admitOperation(t, s, spec)
	if op.State != session.OperationDenied || count(t, s, "permissions") != 0 {
		t.Fatal("human origin broadened child authority", op)
	}
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestHostOperationSchemaRejectsPreviousVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	execTest(t, s, "PRAGMA user_version=41")
	if _, err := Open(t.Context(), path); !errors.Is(err, ErrSchema) {
		t.Fatal(err)
	}
}
