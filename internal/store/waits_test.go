package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestChildWaitRegistrationAtomicAndScoped(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	child, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "wait_child"}, ChildRequest{ParentID: owner.ID, Parts: []session.Part{{Type: "text", Text: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateGrant(t.Context(), session.Grant{ID: "wait_grant", SessionID: owner.ID, Capability: "agents.wait_after_cell", Resource: string(owner.TreeID)})
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(ChildWait{InputIDs: []session.InputID{child.Admission.Input.ID}})
	operation := admitOperation(t, s, session.OperationSpec{ID: "wait_op", CellID: cell.ID, RequestID: "wait_op", Capability: "agents.wait_after_cell", Resource: string(owner.TreeID), Arguments: args})
	if _, err := s.db.ExecContext(t.Context(), `CREATE TRIGGER fail_wait BEFORE UPDATE ON operations WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterChildWait(t.Context(), operation.ID); err == nil {
		t.Fatal("injected settlement failure ignored")
	}
	pending, err := s.Operation(t.Context(), operation.ID)
	if err != nil || pending.State != session.OperationReady || pending.DispatchedAt != nil {
		t.Fatalf("dispatch escaped rolled-back registration: %+v %v", pending, err)
	}
	if _, err := s.db.ExecContext(t.Context(), "DROP TRIGGER fail_wait"); err != nil {
		t.Fatal(err)
	}
	registration, err := s.RegisterChildWait(t.Context(), operation.ID)
	if err != nil || registration.Boundary != "after_cell" {
		t.Fatalf("registration=%+v %v", registration, err)
	}
	retry, err := s.RegisterChildWait(t.Context(), operation.ID)
	if err != nil || !reflect.DeepEqual(registration, retry) {
		t.Fatalf("registration retry changed: %+v %v", retry, err)
	}
	targets, err := s.CellWaitInputs(t.Context(), cell.ID)
	if err != nil || !reflect.DeepEqual(targets, registration.InputIDs) {
		t.Fatalf("targets=%+v %v", targets, err)
	}
	if complete, err := s.ChildInputsComplete(t.Context(), owner.ID, targets); err != nil || complete {
		t.Fatalf("unclaimed work marked complete: %v %v", complete, err)
	}
	if _, err := s.CancelInput(t.Context(), targets[0]); err != nil {
		t.Fatal(err)
	}
	if complete, err := s.ChildInputsComplete(t.Context(), owner.ID, targets); err != nil || !complete {
		t.Fatalf("cancelled work did not resolve wait: %v %v", complete, err)
	}
	_, other := create(t, s, nil)
	otherInput := submit(t, s, other.ID, "unrelated")
	if _, err := s.ChildInputsComplete(t.Context(), owner.ID, []session.InputID{otherInput.Input.ID}); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("unrelated input accepted: %v", err)
	}
	if _, err := s.ChildInputsComplete(t.Context(), child.Session.ID, targets); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("self-cycle accepted: %v", err)
	}
	badArgs, _ := json.Marshal(ChildWait{InputIDs: []session.InputID{otherInput.Input.ID}})
	invalid := admitOperation(t, s, session.OperationSpec{ID: "bad_wait", CellID: cell.ID, RequestID: "bad_wait", Capability: "agents.wait_after_cell", Resource: string(owner.TreeID), Arguments: badArgs})
	if _, err := s.RegisterChildWait(t.Context(), invalid.ID); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("registered cross-tree wait: %v", err)
	}
	invalid, _ = s.Operation(t.Context(), invalid.ID)
	if invalid.State != session.OperationReady {
		t.Fatal("invalid wait dispatched")
	}
}
