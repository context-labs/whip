package store

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func stateOperation(t *testing.T, s *Store, owner session.Session, cell session.Cell, id, name string, args any) session.Operation {
	t.Helper()
	if _, err := s.CreateGrant(t.Context(), session.Grant{ID: session.GrantID("grant_" + id), SessionID: owner.ID, Capability: "state." + name, Resource: string(owner.TreeID)}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return admitOperation(t, s, session.OperationSpec{ID: session.OperationID(id), CellID: cell.ID, RequestID: id, Capability: "state." + name, Resource: string(owner.TreeID), Arguments: raw})
}

func TestStateOperationWriteAndSettlementAreAtomic(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := stateOperation(t, s, owner, cell, "write", "write", stateWrite(owner.ID, session.TreeState, "ignored", "key", `"evidence"`, 0))
	execTest(t, s, `CREATE TRIGGER fail_state_outcome BEFORE UPDATE ON operations WHEN NEW.id='write' AND NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'fault'); END`)
	if _, err := s.ApplyStateOperation(t.Context(), op.ID); err == nil {
		t.Fatal("fault ignored")
	}
	current, err := s.Operation(t.Context(), op.ID)
	if err != nil || current.State != session.OperationReady || count(t, s, "state_versions") != 0 || count(t, s, "content_bodies") != 0 {
		t.Fatalf("partial state survived rollback: %+v %v", current, err)
	}
	execTest(t, s, "DROP TRIGGER fail_state_outcome")
	first, err := s.ApplyStateOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.ApplyStateOperation(t.Context(), op.ID)
	if err != nil || string(first) != string(retry) || count(t, s, "state_versions") != 1 {
		t.Fatal("write replay changed result", err)
	}
	putStateTest(t, s, stateWrite(owner.ID, session.TreeState, "next", "key", `"next"`, 1))
	retry, err = s.ApplyStateOperation(t.Context(), op.ID)
	if err != nil || string(first) != string(retry) {
		t.Fatal("retry returned latest version", err)
	}
	stale := stateOperation(t, s, owner, cell, "stale", "write", stateWrite(owner.ID, session.TreeState, "ignored", "key", `true`, 1))
	if _, err := s.ApplyStateOperation(t.Context(), stale.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("stale write accepted", err)
	}
	if count(t, s, "state_versions") != 2 {
		t.Fatal("conflict mutated state")
	}
}

func TestStateOperationReadsRetainObservedVersionWithoutCopyingBody(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	initial := putStateTest(t, s, stateWrite(owner.ID, session.SessionState, "first", "key", `{"evidence":"never copy this into the ledger"}`, 0))
	op := stateOperation(t, s, owner, cell, "read", "get", session.StateKey{Scope: session.SessionState, Key: "key"})
	first, err := s.ApplyStateOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	var result session.StateValue
	if err := json.Unmarshal(first, &result); err != nil || result.ID != initial.ID {
		t.Fatalf("read %+v %v", result, err)
	}
	putStateTest(t, s, stateWrite(owner.ID, session.SessionState, "second", "key", `null`, 1))
	retry, err := s.ApplyStateOperation(t.Context(), op.ID)
	if err != nil || string(first) != string(retry) {
		t.Fatal("read retry drifted to new head", err)
	}
	_, foreign := create(t, s, session.DefaultTreePolicy())
	read := stateOperation(t, s, owner, cell, "foreign", "read", session.StateRead{ID: putStateTest(t, s, stateWrite(foreign.ID, session.SessionState, "foreign", "key", `1`, 0)).ID, Length: 10})
	if _, err := s.ApplyStateOperation(t.Context(), read.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign handle read", err)
	}
}

func TestStateOperationRechecksRevokedAuthority(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := stateOperation(t, s, owner, cell, "revoked", "write", stateWrite(owner.ID, session.SessionState, "ignored", "key", `1`, 0))
	if _, err := s.RevokeGrant(t.Context(), "grant_revoked"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyStateOperation(t.Context(), op.ID); err == nil {
		t.Fatal("revoked grant authorized state mutation")
	}
	if count(t, s, "state_versions") != 0 || count(t, s, "content_bodies") != 0 {
		t.Fatal("revoked state write retained data")
	}
}
