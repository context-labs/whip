package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func denyTest(t *testing.T, db *Store, owner session.SessionID, id string, revision session.Revision, deny bool) session.PermissionDenialEdit {
	t.Helper()
	edit, _, err := db.ApplyPermissionDenial(t.Context(), session.PermissionDenialRequest{ID: id, SessionID: owner, ExpectedRevision: revision, DenyInteractive: deny})
	if err != nil {
		t.Fatal(err)
	}
	return edit
}

func TestPermissionDenialRetainsModeAndExactReceiptsAcrossRestartDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	db := openTest(t, path)
	_, root := create(t, db, nil)
	same := denyTest(t, db, root.ID, "same", 1, false)
	if same.Policy.Revision != 1 || same.Policy.DenyInteractive {
		t.Fatal(same)
	}
	denied := denyTest(t, db, root.ID, "Deny.Mixed-Case", 1, true)
	mode := setModeTest(t, db, root.ID, "automatic", 2, session.PermissionAutomatic)
	if !mode.Policy.DenyInteractive || mode.Policy.Mode != session.PermissionAutomatic {
		t.Fatal("mode silently cleared denial", mode)
	}
	cleared := denyTest(t, db, root.ID, "clear", 3, false)
	if cleared.Policy.DenyInteractive || cleared.Policy.Mode != session.PermissionAutomatic {
		t.Fatal("clear changed mode", cleared)
	}
	if again, err := db.PermissionModeEdit(t.Context(), root.ID, mode.ID); err != nil || again != mode {
		t.Fatal("mode receipt reinterpreted denial", again, err)
	}
	db = openTest(t, path)
	for _, expected := range []session.PermissionDenialEdit{same, denied, cleared} {
		actual, changed, err := db.ApplyPermissionDenial(t.Context(), expected.PermissionDenialRequest)
		if err != nil || changed || actual != expected {
			t.Fatal("retry changed original", actual, changed, err)
		}
	}
	altered := denied.PermissionDenialRequest
	altered.DenyInteractive = false
	if _, _, err := db.ApplyPermissionDenial(t.Context(), altered); !errors.Is(err, ErrConflict) {
		t.Fatal("changed payload reused identity", err)
	}
	if _, _, err := db.ApplyPermissionDenial(t.Context(), session.PermissionDenialRequest{ID: "stale", SessionID: root.ID, ExpectedRevision: 1, DenyInteractive: true}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale policy accepted", err)
	}
	if _, err := db.SetLifecycle(t.Context(), root.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	stopped := denyTest(t, db, root.ID, "stopped", 4, true)
	if stopped.Policy.Revision != 5 {
		t.Fatal(stopped)
	}
	if err := db.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	actual, changed, err := db.ApplyPermissionDenial(t.Context(), denied.PermissionDenialRequest)
	if err != nil || changed || actual != denied {
		t.Fatal("deleted retry changed policy", actual, changed, err)
	}
	if _, err := db.PermissionDenialEdit(t.Context(), "foreign", denied.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	mustFail(t, db, "UPDATE permission_denial_edits SET deny_interactive=0 WHERE id=?", denied.ID)
	mustFail(t, db, "DELETE FROM permission_denial_edits WHERE id=?", denied.ID)
}

func TestPermissionDenialSettlesPendingAndAutomaticButKeepsExplicitAuthority(t *testing.T) {
	db := fresh(t)
	root, cell := operationCell(t, db)
	pending := admitOperation(t, db, operationSpec(cell, "pending"))
	denyTest(t, db, root.ID, "deny", 1, true)
	if value, err := db.Operation(t.Context(), pending.ID); err != nil || value.State != session.OperationDenied {
		t.Fatal(value, err)
	}
	if _, err := db.ResolvePermission(t.Context(), pending.ID, true); !errors.Is(err, ErrConflict) {
		t.Fatal("late approval bypassed denial", err)
	}
	setModeTest(t, db, root.ID, "automatic", 2, session.PermissionAutomatic)
	if op := admitOperation(t, db, operationSpec(cell, "denied")); op.State != session.OperationDenied || op.PermissionRevision != nil {
		t.Fatal("automatic bypass", op)
	}
	if count(t, db, "permissions") != 1 {
		t.Fatal("denial created a new human prompt")
	}
	issuer := standingGrant(t, db, root.ID, "issuer")
	granted := admitOperation(t, db, operationSpec(cell, "granted"))
	if granted.GrantID == nil || granted.State != session.OperationReady {
		t.Fatal("standing grant rejected", granted)
	}
	if okay, err := db.DispatchOperation(t.Context(), granted.ID); err != nil || !okay {
		t.Fatal(okay, err)
	}
	request := childRequest(root.ID)
	request.GrantIDs = []session.GrantID{}
	child := spawnChildTest(t, db, "child", request)
	childCell := childOperationCell(t, db, child.Session.ID)
	if policy, err := db.PermissionPolicy(t.Context(), child.Session.ID); err != nil || !policy.DenyInteractive {
		t.Fatal(policy, err)
	}
	if op := admitOperation(t, db, operationSpec(childCell, "missing")); op.State != session.OperationDenied {
		t.Fatal(op)
	}
	delegated := session.Grant{ID: "delegated", SessionID: child.Session.ID, Capability: issuer.Capability, Resource: issuer.Resource, IssuerID: &issuer.ID}
	if _, err := db.CreateGrant(t.Context(), delegated); err != nil {
		t.Fatal(err)
	}
	op := admitOperation(t, db, operationSpec(childCell, "delegated"))
	if op.State != session.OperationReady {
		t.Fatal(op)
	}
	if _, _, err := db.ApplyPermissionDenial(t.Context(), session.PermissionDenialRequest{ID: "child-edit", SessionID: child.Session.ID, ExpectedRevision: 3}); !errors.Is(err, ErrConflict) {
		t.Fatal("child edited tree policy", err)
	}
	if _, err := db.RevokeGrant(t.Context(), issuer.ID); err != nil {
		t.Fatal(err)
	}
	if okay, err := db.DispatchOperation(t.Context(), op.ID); err != nil || okay {
		t.Fatal("denial clear restored a revoked grant", okay, err)
	}
	denyTest(t, db, root.ID, "clear", 3, false)
	ready := admitOperation(t, db, operationSpec(cell, "automatic-ready"))
	dispatched := admitOperation(t, db, operationSpec(cell, "automatic-dispatched"))
	if okay, err := db.DispatchOperation(t.Context(), dispatched.ID); err != nil || !okay {
		t.Fatal(okay, err)
	}
	denyTest(t, db, root.ID, "deny-again", 4, true)
	if okay, err := db.DispatchOperation(t.Context(), ready.ID); err != nil || okay {
		t.Fatal("stale automatic authority dispatched", okay, err)
	}
	if _, err := db.SettleOperation(t.Context(), dispatched.ID, session.OperationResult{State: session.OperationSucceeded}); err != nil {
		t.Fatal("already dispatched result lost", err)
	}
}

func TestPermissionDenialRollsBackSettlementAndPolicyWithReceiptFailure(t *testing.T) {
	db := fresh(t)
	root, cell := operationCell(t, db)
	op := admitOperation(t, db, operationSpec(cell, "pending"))
	if _, err := db.db.ExecContext(t.Context(), `CREATE TRIGGER fail_denial BEFORE INSERT ON permission_denial_edits BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := db.ApplyPermissionDenial(t.Context(), session.PermissionDenialRequest{ID: "rollback", SessionID: root.ID, ExpectedRevision: 1, DenyInteractive: true}); err == nil || changed {
		t.Fatal(changed, err)
	}
	if policy, err := db.PermissionPolicy(t.Context(), root.ID); err != nil || policy.DenyInteractive || policy.Revision != 1 {
		t.Fatal(policy, err)
	}
	if value, err := db.Operation(t.Context(), op.ID); err != nil || value.State != session.OperationWaiting {
		t.Fatal(value, err)
	}
}
