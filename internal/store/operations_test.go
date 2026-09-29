package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func operationCell(t *testing.T, s *Store) (session.Session, session.Cell) {
	t.Helper()
	_, owner := create(t, s, nil)
	key := string(owner.ID)
	submit(t, s, owner.ID, key)
	turn := claim(t, s, owner.ID).Turn
	message, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{
		ID: session.MessageID("call_" + key), Role: session.Assistant,
		Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: []byte(`{"code":"1"}`)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cell, dispatch, err := s.BeginCell(t.Context(), session.CellSpec{ID: session.CellID("cell_" + key), TurnID: turn.ID, CallMessageID: message.ID, CallID: "call"})
	if err != nil || !dispatch {
		t.Fatalf("begin cell: %v %v", dispatch, err)
	}
	return owner, cell
}

func operationSpec(cell session.Cell, id string) session.OperationSpec {
	return session.OperationSpec{ID: session.OperationID(id), CellID: cell.ID, RequestID: id, Capability: "filesystem.read", Resource: "/workspace", Arguments: json.RawMessage(`{"path":"file"}`)}
}

func admitOperation(t *testing.T, s *Store, spec session.OperationSpec) session.Operation {
	t.Helper()
	operation, err := s.AdmitOperation(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func standingGrant(t *testing.T, s *Store, owner session.SessionID, id string) session.Grant {
	t.Helper()
	grant, err := s.CreateGrant(t.Context(), session.Grant{ID: session.GrantID(id), SessionID: owner, Capability: "filesystem.read", Resource: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func TestOperationAdmissionAndImmutableRetry(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	spec := operationSpec(cell, "op")
	spec.Arguments = json.RawMessage(`{ "path": "file" }`)
	op := admitOperation(t, s, spec)
	if op.State != session.OperationWaiting || op.GrantID != nil || op.SessionID != owner.ID || op.TurnID != cell.TurnID {
		t.Fatalf("wrong admission: %+v", op)
	}
	retry := admitOperation(t, s, spec)
	if !reflect.DeepEqual(op, retry) || count(t, s, "permissions") != 1 {
		t.Fatal("retry changed admitted intent or duplicated permission")
	}
	changed := spec
	changed.Arguments = json.RawMessage(`{"path":"other"}`)
	if _, err := s.AdmitOperation(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed intent: %v", err)
	}
	changed = spec
	changed.ID = "duplicate-request"
	if _, err := s.AdmitOperation(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("reused cell request: %v", err)
	}
	if dispatch, err := s.DispatchOperation(t.Context(), op.ID); err != nil || dispatch {
		t.Fatalf("waiting dispatched: %v %v", dispatch, err)
	}
	for range 2 {
		permission, err := s.ResolvePermission(t.Context(), op.ID, true)
		if err != nil || permission.State != session.PermissionApproved {
			t.Fatalf("approval: %+v %v", permission, err)
		}
	}
	approved, err := s.Operation(t.Context(), op.ID)
	if err != nil || approved.State != session.OperationReady || approved.GrantID == nil {
		t.Fatalf("approval did not prepare operation: %+v %v", approved, err)
	}
	grants, err := s.Grants(t.Context(), owner.ID, "", 100)
	if err != nil || len(grants) != 1 || grants[0].OperationID == nil || *grants[0].OperationID != op.ID || grants[0].Resource != spec.Resource || grants[0].Capability != spec.Capability {
		t.Fatalf("approval did not bind exact intent: %+v %v", grants, err)
	}
	if next := admitOperation(t, s, operationSpec(cell, "next")); next.State != session.OperationWaiting {
		t.Fatal("one-use grant authorized a different operation")
	}
	if _, err := s.ResolvePermission(t.Context(), op.ID, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("approval was rewritten: %v", err)
	}
	if _, err := s.CreateGrant(t.Context(), grants[0]); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("forged one-use creation accepted: %v", err)
	}
}

func TestOperationConcurrentSingleDispatchAndSettlement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	owner, cell := operationCell(t, s)
	standingGrant(t, s, owner.ID, "standing")
	spec := operationSpec(cell, "op")
	var wins atomic.Int32
	var workers sync.WaitGroup
	for i := range 12 {
		workers.Go(func() {
			db := s
			if i%2 != 0 {
				db = other
			}
			if _, err := db.AdmitOperation(t.Context(), spec); err != nil {
				t.Error(err)
				return
			}
			dispatch, err := db.DispatchOperation(t.Context(), spec.ID)
			if err != nil {
				t.Error(err)
			}
			if dispatch {
				wins.Add(1)
			}
		})
	}
	workers.Wait()
	if wins.Load() != 1 || count(t, s, "operations") != 1 || count(t, s, "permissions") != 0 {
		t.Fatalf("dispatches=%d operations=%d", wins.Load(), count(t, s, "operations"))
	}
	if _, err := s.SettleOperation(t.Context(), spec.ID, session.OperationResult{State: session.OperationCancelled}); !errors.Is(err, ErrConflict) {
		t.Fatalf("dispatched operation relabeled cancelled: %v", err)
	}
	result := session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(`{ "value": "<done>" }`)}
	for range 2 {
		op, err := s.SettleOperation(t.Context(), spec.ID, result)
		if err != nil || op.State != session.OperationSucceeded || op.DispatchedAt == nil || op.FinishedAt == nil {
			t.Fatalf("settle: %+v %v", op, err)
		}
	}
	if _, err := other.SettleOperation(t.Context(), spec.ID, session.OperationResult{State: session.OperationUncertain}); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed settlement: %v", err)
	}
	if dispatch, err := other.DispatchOperation(t.Context(), spec.ID); err != nil || dispatch {
		t.Fatalf("terminal dispatch: %v %v", dispatch, err)
	}
}

func TestOperationGrantScopeAndRevocation(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	_, other := create(t, s, nil)
	foreign := standingGrant(t, s, other.ID, "foreign")
	if op := admitOperation(t, s, operationSpec(cell, "foreign-test")); op.State != session.OperationWaiting {
		t.Fatal("foreign session authorized operation")
	}
	for _, grant := range []session.Grant{
		{ID: "wrong-cap", SessionID: owner.ID, Capability: "filesystem.write", Resource: "/workspace"},
		{ID: "wrong-path", SessionID: owner.ID, Capability: "filesystem.read", Resource: "/workspace/subdir"},
	} {
		if _, err := s.CreateGrant(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
	}
	if op := admitOperation(t, s, operationSpec(cell, "scope-test")); op.State != session.OperationWaiting {
		t.Fatal("nonexact scope authorized operation")
	}
	grant := standingGrant(t, s, owner.ID, "standing")
	ready := admitOperation(t, s, operationSpec(cell, "ready"))
	dispatched := admitOperation(t, s, operationSpec(cell, "dispatched"))
	if dispatch, err := s.DispatchOperation(t.Context(), dispatched.ID); err != nil || !dispatch {
		t.Fatalf("dispatch: %v %v", dispatch, err)
	}
	var revoked session.Grant
	for range 2 {
		var err error
		revoked, err = s.RevokeGrant(t.Context(), grant.ID)
		if err != nil || revoked.RevokedAt == nil {
			t.Fatalf("revoke: %+v %v", revoked, err)
		}
	}
	for id, want := range map[session.OperationID]session.OperationState{ready.ID: session.OperationDenied, dispatched.ID: session.OperationDispatched} {
		op, err := s.Operation(t.Context(), id)
		if err != nil || op.State != want {
			t.Fatalf("revocation state: %+v %v", op, err)
		}
	}
	retried, err := s.CreateGrant(t.Context(), grant)
	if err != nil || !reflect.DeepEqual(retried, revoked) {
		t.Fatalf("stable ID reactivated grant: %+v %v", retried, err)
	}
	grant.Resource = "/different"
	if _, err := s.CreateGrant(t.Context(), grant); !errors.Is(err, ErrConflict) {
		t.Fatalf("grant scope changed: %v", err)
	}
	if op := admitOperation(t, s, operationSpec(cell, "after-revoke")); op.State != session.OperationWaiting {
		t.Fatal("revoked grant reused")
	}
	// A forged persisted association still cannot cross the dispatch gate.
	execTest(t, s, `INSERT INTO operations (id,cell_id,request_id,capability,resource,arguments,state,grant_id,created_at)
 VALUES ('forged',?,'forged','filesystem.read','/workspace','{}','ready',?,?)`, cell.ID, foreign.ID, now())
	if dispatch, err := s.DispatchOperation(t.Context(), "forged"); dispatch || !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign persisted grant dispatched: %v %v", dispatch, err)
	}
	if _, err := s.CreateGrant(t.Context(), session.Grant{ID: "missing", SessionID: "missing", Capability: "filesystem.read", Resource: "/workspace"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("grant to missing owner: %v", err)
	}
}

func TestOperationDenialAndCancellationBlockSettlement(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	denied := admitOperation(t, s, operationSpec(cell, "denied"))
	for range 2 {
		permission, err := s.ResolvePermission(t.Context(), denied.ID, false)
		if err != nil || permission.State != session.PermissionDenied {
			t.Fatalf("deny: %+v %v", permission, err)
		}
	}
	pending := admitOperation(t, s, operationSpec(cell, "pending"))
	if _, err := s.SettleCell(t.Context(), cell.ID, session.CellSucceeded, session.ToolResult{CallID: "call", Output: "done"}, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("cell settled before operation: %v", err)
	}
	if _, err := s.Finish(t.Context(), cell.TurnID, session.Failed, new("failed"), nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("turn settled before operation: %v", err)
	}
	if _, err := s.SettleOperation(t.Context(), pending.ID, session.OperationResult{State: session.OperationSucceeded}); !errors.Is(err, ErrConflict) {
		t.Fatalf("undispatched operation succeeded: %v", err)
	}
	if _, err := s.SettleOperation(t.Context(), pending.ID, session.OperationResult{State: session.OperationCancelled}); err != nil {
		t.Fatal(err)
	}
	permissions, err := s.Permissions(t.Context(), owner.ID, "", 100)
	if err != nil || len(permissions) != 2 || permissions[0].State != session.PermissionDenied || permissions[1].State != session.PermissionCancelled {
		t.Fatalf("unresolved cancellation: %+v %v", permissions, err)
	}
	if _, err := s.ResolvePermission(t.Context(), pending.ID, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("late approval: %v", err)
	}
	if _, err := s.SettleCell(t.Context(), cell.ID, session.CellSucceeded, session.ToolResult{CallID: "call", Output: "done"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdmitOperation(t.Context(), operationSpec(cell, "after-cell")); !errors.Is(err, ErrStopped) {
		t.Fatalf("admitted after cell: %v", err)
	}
	if _, err := s.Finish(t.Context(), cell.TurnID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOperationTransactionRollback(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	spec := operationSpec(cell, "op")
	execTest(t, s, `CREATE TRIGGER fail_permission BEFORE INSERT ON permissions BEGIN SELECT RAISE(ABORT,'fault'); END`)
	if _, err := s.AdmitOperation(t.Context(), spec); err == nil || count(t, s, "operations") != 0 {
		t.Fatal("partial admission survived rollback")
	}
	execTest(t, s, "DROP TRIGGER fail_permission")
	admitOperation(t, s, spec)
	execTest(t, s, `CREATE TRIGGER fail_permission BEFORE UPDATE ON permissions BEGIN SELECT RAISE(ABORT,'fault'); END`)
	if _, err := s.ResolvePermission(t.Context(), spec.ID, true); err == nil || count(t, s, "grants") != 0 {
		t.Fatal("partial approval survived rollback")
	}
	if _, err := s.SettleOperation(t.Context(), spec.ID, session.OperationResult{State: session.OperationCancelled}); err == nil {
		t.Fatal("cancellation ignored prompt failure")
	}
	op, err := s.Operation(t.Context(), spec.ID)
	if err != nil || op.State != session.OperationWaiting {
		t.Fatalf("failed transaction changed operation: %+v %v", op, err)
	}
	execTest(t, s, "DROP TRIGGER fail_permission")
	if _, err := s.ResolvePermission(t.Context(), spec.ID, true); err != nil {
		t.Fatal(err)
	}
	op, err = s.Operation(t.Context(), spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	execTest(t, s, `CREATE TRIGGER fail_operation BEFORE UPDATE ON operations BEGIN SELECT RAISE(ABORT,'fault'); END`)
	if dispatch, err := s.DispatchOperation(t.Context(), spec.ID); err == nil || dispatch {
		t.Fatalf("failed dispatch authorized effect: %v %v", dispatch, err)
	}
	if _, err := s.RevokeGrant(t.Context(), *op.GrantID); err == nil {
		t.Fatal("partial revoke succeeded")
	}
	grants, err := s.Grants(t.Context(), owner.ID, "", 100)
	if err != nil || len(grants) != 1 || grants[0].RevokedAt != nil {
		t.Fatalf("failed revoke retained revocation: %+v %v", grants, err)
	}
	execTest(t, s, "DROP TRIGGER fail_operation")
	execTest(t, s, `CREATE TRIGGER fail_cell BEFORE UPDATE ON cells BEGIN SELECT RAISE(ABORT,'fault'); END`)
	if _, err := s.Recover(t.Context()); err == nil {
		t.Fatal("recovery ignored cell failure")
	}
	op, err = s.Operation(t.Context(), spec.ID)
	if err != nil || op.State != session.OperationReady {
		t.Fatalf("partial recovery retained operation outcome: %+v %v", op, err)
	}
}

func TestOperationRecoveryBeforeCellsAndDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	owner, cell := operationCell(t, s)
	admitOperation(t, s, operationSpec(cell, "waiting"))
	admitOperation(t, s, operationSpec(cell, "approved"))
	if _, err := s.ResolvePermission(t.Context(), "approved", true); err != nil {
		t.Fatal(err)
	}
	standingGrant(t, s, owner.ID, "standing")
	admitOperation(t, s, operationSpec(cell, "ready"))
	admitOperation(t, s, operationSpec(cell, "dispatched"))
	if dispatch, err := s.DispatchOperation(t.Context(), "dispatched"); err != nil || !dispatch {
		t.Fatalf("dispatch: %v %v", dispatch, err)
	}
	other := openTest(t, path)
	execTest(t, s, `CREATE TRIGGER recovery_order BEFORE UPDATE ON cells
 WHEN EXISTS(SELECT 1 FROM operations WHERE cell_id=OLD.id AND finished_at IS NULL)
 BEGIN SELECT RAISE(ABORT,'operation must settle before cell'); END`)
	for range 2 {
		if _, err := other.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	for id, state := range map[session.OperationID]session.OperationState{"waiting": session.OperationCancelled, "approved": session.OperationCancelled, "ready": session.OperationCancelled, "dispatched": session.OperationUncertain} {
		op, err := s.Operation(t.Context(), id)
		if err != nil || op.State != state || op.Result == nil || op.FinishedAt == nil {
			t.Fatalf("recovery %s: %+v %v", id, op, err)
		}
		if dispatch, err := s.DispatchOperation(t.Context(), id); err != nil || dispatch {
			t.Fatalf("recovery replay %s: %v %v", id, dispatch, err)
		}
	}
	permissions, err := s.Permissions(t.Context(), owner.ID, "", 100)
	if err != nil || len(permissions) != 2 || permissions[0].State != session.PermissionApproved || permissions[1].State != session.PermissionCancelled {
		t.Fatalf("recovery left prompt: %+v %v", permissions, err)
	}
	cells, err := s.Cells(t.Context(), cell.TurnID, "", 100)
	if err != nil || len(cells) != 1 || cells[0].State != session.CellUncertain || cells[0].ResultMessageID == nil {
		t.Fatalf("recovery cell evidence: %+v %v", cells, err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"operations", "permissions", "grants", "cells"} {
		if count(t, s, table) != 0 {
			t.Fatalf("deletion retained %s", table)
		}
	}
}

func TestOperationAuthorityRaces(t *testing.T) {
	for _, scenario := range []string{"approve_deny", "approve_cancel", "approve_stop", "dispatch_revoke", "dispatch_stop", "dispatch_recover", "approve_recover"} {
		t.Run(scenario, func(t *testing.T) {
			for range 4 {
				path := filepath.Join(t.TempDir(), "runtime.db")
				s, other := openTest(t, path), openTest(t, path)
				owner, cell := operationCell(t, s)
				ready := strings.HasPrefix(scenario, "dispatch_")
				if ready {
					standingGrant(t, s, owner.ID, "standing")
				}
				admitOperation(t, s, operationSpec(cell, "op"))
				start := make(chan struct{})
				var leftErr, rightErr error
				var dispatched bool
				var workers sync.WaitGroup
				workers.Go(func() {
					<-start
					if ready {
						dispatched, leftErr = s.DispatchOperation(t.Context(), "op")
					} else {
						_, leftErr = s.ResolvePermission(t.Context(), "op", true)
					}
				})
				workers.Go(func() {
					<-start
					switch scenario {
					case "approve_deny":
						_, rightErr = other.ResolvePermission(t.Context(), "op", false)
					case "approve_cancel":
						_, rightErr = other.SettleOperation(t.Context(), "op", session.OperationResult{State: session.OperationCancelled})
					case "approve_stop", "dispatch_stop":
						_, rightErr = other.CancelTurn(t.Context(), cell.TurnID)
					case "dispatch_revoke":
						_, rightErr = other.RevokeGrant(t.Context(), "standing")
					case "dispatch_recover", "approve_recover":
						_, rightErr = other.Recover(t.Context())
					}
				})
				close(start)
				workers.Wait()
				if leftErr != nil && !errors.Is(leftErr, ErrConflict) && !errors.Is(leftErr, ErrStopped) {
					t.Fatalf("unexpected authorization error: %v", leftErr)
				}
				if rightErr != nil && (scenario != "approve_deny" || !errors.Is(rightErr, ErrConflict)) {
					t.Fatalf("unexpected competing result: %v", rightErr)
				}
				op, err := s.Operation(t.Context(), "op")
				if err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "approve_deny":
					if (leftErr == nil) == (rightErr == nil) || (op.State != session.OperationReady && op.State != session.OperationDenied) {
						t.Fatalf("decision was not exclusive: %s %v %v", op.State, leftErr, rightErr)
					}
				case "approve_cancel":
					if op.State != session.OperationCancelled {
						t.Fatalf("cancel lost to approval: %s", op.State)
					}
				case "approve_stop", "dispatch_stop":
					if again, err := other.DispatchOperation(t.Context(), "op"); again || (err != nil && !errors.Is(err, ErrStopped)) {
						t.Fatalf("stop failed gate: %v %v", again, err)
					}
					state := session.OperationCancelled
					if dispatched {
						state = session.OperationUncertain
					}
					if _, err := s.SettleOperation(t.Context(), "op", session.OperationResult{State: state}); err != nil {
						t.Fatalf("cancel cleanup: %v", err)
					}
				case "dispatch_revoke":
					want := session.OperationDenied
					if dispatched {
						want = session.OperationDispatched
					}
					if op.State != want {
						t.Fatalf("revocation rewrote effect evidence: %s dispatch=%v", op.State, dispatched)
					}
				case "dispatch_recover", "approve_recover":
					want := session.OperationCancelled
					if dispatched {
						want = session.OperationUncertain
					}
					if op.State != want {
						t.Fatalf("incorrect recovery: %s dispatch=%v", op.State, dispatched)
					}
					if again, err := s.DispatchOperation(t.Context(), "op"); again || err != nil {
						t.Fatalf("recovery permitted replay: %v %v", again, err)
					}
				}
				permissions, err := s.Permissions(t.Context(), owner.ID, "", 100)
				if err != nil {
					t.Fatal(err)
				}
				for _, permission := range permissions {
					if permission.State == session.PermissionPending {
						t.Fatalf("race left unresolved permission: %+v", permission)
					}
				}
			}
		})
	}
}

func TestOperationBoundsAndLists(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	_, other := create(t, s, nil)
	for i := range 5 {
		spec := operationSpec(cell, fmt.Sprintf("op%d", i))
		spec.Arguments = json.RawMessage(`{"value":"` + strings.Repeat("a", session.MaxDocumentBytes-20) + `"}`)
		admitOperation(t, s, spec)
	}
	page, err := s.Operations(t.Context(), cell.TurnID, "", 100)
	if err != nil || len(page) == 0 || len(page) >= 5 {
		t.Fatalf("byte page bound: %d %v", len(page), err)
	}
	next, err := s.Operations(t.Context(), cell.TurnID, page[len(page)-1].ID, 1)
	if err != nil || len(next) != 1 || next[0].ID <= page[len(page)-1].ID {
		t.Fatalf("operation cursor: %+v %v", next, err)
	}
	permissions, err := s.Permissions(t.Context(), owner.ID, "op1", 2)
	if err != nil || len(permissions) != 2 || permissions[0].OperationID != "op2" {
		t.Fatalf("permission cursor: %+v %v", permissions, err)
	}
	foreign, err := s.Permissions(t.Context(), other.ID, "", 100)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("permission owner: %+v %v", foreign, err)
	}
	for _, id := range []string{"a", "b", "c"} {
		standingGrant(t, s, owner.ID, id)
	}
	grants, err := s.Grants(t.Context(), owner.ID, "a", 1)
	if err != nil || len(grants) != 1 || grants[0].ID != "b" {
		t.Fatalf("grant cursor: %+v %v", grants, err)
	}
	if rows, err := s.Grants(t.Context(), other.ID, "", 100); err != nil || len(rows) != 0 {
		t.Fatalf("grant owner: %+v %v", rows, err)
	}
	if rows, err := s.Cells(t.Context(), cell.TurnID, cell.ID, 100); err != nil || len(rows) != 0 {
		t.Fatalf("cell cursor: %+v %v", rows, err)
	}
	if _, err := s.Operations(t.Context(), cell.TurnID, "", 101); err == nil {
		t.Fatal("unbounded operation list")
	}
	if _, err := s.Grants(t.Context(), owner.ID, "", 0); err == nil {
		t.Fatal("unbounded grant list")
	}
	if _, err := s.Permissions(t.Context(), owner.ID, "", 101); err == nil {
		t.Fatal("unbounded permission list")
	}
	if _, err := s.Cells(t.Context(), cell.TurnID, "", 101); err == nil {
		t.Fatal("unbounded cell list")
	}
	// Fill bounded metadata cheaply; the public admissions above exercise the
	// normal transaction path, while these rows put the limits at their edges.
	execTest(t, s, `WITH RECURSIVE n(i) AS (SELECT 5 UNION ALL SELECT i+1 FROM n WHERE i<1023)
 INSERT INTO operations (id,cell_id,request_id,capability,resource,arguments,state,created_at)
 SELECT 'limit_op_'||i,?,'limit_request_'||i,'filesystem.read','/workspace','{}','waiting',? FROM n`, cell.ID, now())
	if _, err := s.AdmitOperation(t.Context(), operationSpec(cell, "overflow")); !errors.Is(err, ErrLimit) {
		t.Fatalf("operation quota: %v", err)
	}
	execTest(t, s, `WITH RECURSIVE n(i) AS (SELECT 3 UNION ALL SELECT i+1 FROM n WHERE i<1023)
 INSERT INTO grants (id,session_id,capability,resource,created_at)
 SELECT 'limit_grant_'||i,?,'filesystem.read','/workspace',? FROM n`, owner.ID, now())
	if _, err := s.CreateGrant(t.Context(), session.Grant{ID: "overflow", SessionID: owner.ID, Capability: "filesystem.read", Resource: "/workspace"}); !errors.Is(err, ErrLimit) {
		t.Fatalf("grant quota: %v", err)
	}
	if _, err := s.ResolvePermission(t.Context(), "op0", true); !errors.Is(err, ErrLimit) {
		t.Fatalf("one-use approval bypassed grant quota: %v", err)
	}
	if op, err := s.Operation(t.Context(), "op0"); err != nil || op.State != session.OperationWaiting {
		t.Fatalf("failed quota mutated intent: %+v %v", op, err)
	}
}

func TestPendingPermissionsFilterBeforePageLimit(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	_, other := create(t, s, nil)
	for i := range 102 {
		id := fmt.Sprintf("permission_%03d", i)
		operation := admitOperation(t, s, operationSpec(cell, id))
		if i < 100 {
			if _, err := s.ResolvePermission(t.Context(), operation.ID, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	all, err := s.Permissions(t.Context(), owner.ID, "", 100)
	if err != nil || len(all) != 100 || all[0].State != session.PermissionDenied {
		t.Fatalf("unfiltered contract changed: %d %v", len(all), err)
	}
	pending, err := s.PermissionsFiltered(t.Context(), owner.ID, "", 1, true)
	if err != nil || len(pending) != 1 || pending[0].OperationID != "permission_100" {
		t.Fatalf("pending hidden behind history: %+v %v", pending, err)
	}
	next, err := s.PermissionsFiltered(t.Context(), owner.ID, pending[0].OperationID, 1, true)
	if err != nil || len(next) != 1 || next[0].OperationID != "permission_101" {
		t.Fatalf("pending exclusive cursor: %+v %v", next, err)
	}
	foreign, err := s.PermissionsFiltered(t.Context(), other.ID, "", 100, true)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("pending owner: %+v %v", foreign, err)
	}
	if _, err := s.ResolvePermission(t.Context(), pending[0].OperationID, false); err != nil {
		t.Fatal(err)
	}
	remaining, err := s.PermissionsFiltered(t.Context(), owner.ID, "", 100, true)
	if err != nil || len(remaining) != 1 || remaining[0].OperationID != next[0].OperationID {
		t.Fatalf("resolved still pending: %+v %v", remaining, err)
	}
}
