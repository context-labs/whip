package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func modeRequest(owner session.SessionID, id string, revision session.Revision, mode session.PermissionMode) session.PermissionModeRequest {
	return session.PermissionModeRequest{ID: session.PermissionModeEditID(id), SessionID: owner, ExpectedRevision: revision, Mode: mode}
}

func setModeTest(t *testing.T, s *Store, owner session.SessionID, id string, revision session.Revision, mode session.PermissionMode) session.PermissionModeEdit {
	t.Helper()
	edit, err := s.SetPermissionMode(t.Context(), modeRequest(owner, id, revision, mode))
	if err != nil {
		t.Fatal(err)
	}
	return edit
}

func TestPermissionModeReceiptsPersistOriginalRevisionAndSurviveDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	tree, owner := create(t, s, nil)
	policy, err := s.PermissionPolicy(t.Context(), owner.ID)
	if err != nil || policy.TreeID != tree.ID || policy.Mode != session.PermissionPrompt || policy.Revision != 1 {
		t.Fatalf("default: %+v %v", policy, err)
	}
	same := setModeTest(t, s, owner.ID, "same", 1, session.PermissionPrompt)
	if same.Policy != policy || same.PreviousMode != session.PermissionPrompt {
		t.Fatalf("same-value edit changed policy: %+v", same)
	}
	changed := setModeTest(t, s, owner.ID, "automatic", 1, session.PermissionAutomatic)
	if changed.Policy.Mode != session.PermissionAutomatic || changed.Policy.Revision != 2 {
		t.Fatalf("mode unchanged: %+v", changed)
	}
	_, other := create(t, s, nil)
	if policy, err := s.PermissionPolicy(t.Context(), other.ID); err != nil || policy.Revision != 1 || policy.Mode != session.PermissionPrompt {
		t.Fatal("mode leaked across trees", policy, err)
	}
	s = openTest(t, path)
	for _, expected := range []session.PermissionModeEdit{same, changed} {
		retry, err := s.SetPermissionMode(t.Context(), expected.PermissionModeRequest)
		if err != nil || retry != expected {
			t.Fatalf("retry reinterpreted original: %+v %v", retry, err)
		}
	}
	if _, err := s.SetPermissionMode(t.Context(), modeRequest(owner.ID, "stale", 1, session.PermissionPrompt)); !errors.Is(err, ErrConflict) {
		t.Fatal("stale edit accepted", err)
	}
	altered := changed.PermissionModeRequest
	altered.Mode = session.PermissionPrompt
	if _, err := s.SetPermissionMode(t.Context(), altered); !errors.Is(err, ErrConflict) {
		t.Fatal("reused edit identity", err)
	}
	if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	if retry, err := s.SetPermissionMode(t.Context(), changed.PermissionModeRequest); err != nil || retry != changed {
		t.Fatal("stopped retry changed", retry, err)
	}
	stopped := setModeTest(t, s, owner.ID, "stopped", 2, session.PermissionPrompt)
	if stopped.Policy.Revision != 3 || stopped.Policy.Mode != session.PermissionPrompt {
		t.Fatal("human policy edit requires a running worker", stopped)
	}
	if retry, err := s.SetPermissionMode(t.Context(), changed.PermissionModeRequest); err != nil || retry != changed {
		t.Fatal("stopped edit reinterpreted an old receipt", retry, err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "permission_policies") != 1 {
		t.Fatal("deleted tree retained live policy")
	}
	for _, expected := range []session.PermissionModeEdit{same, changed} {
		retry, err := s.SetPermissionMode(t.Context(), expected.PermissionModeRequest)
		if err != nil || retry != expected {
			t.Fatal("deleted retry recreated authority", retry, err)
		}
		read, err := s.PermissionModeEdit(t.Context(), owner.ID, expected.ID)
		if err != nil || read != expected {
			t.Fatal("receipt lost", read, err)
		}
		if _, err := s.PermissionModeEdit(t.Context(), other.ID, expected.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign receipt disclosed", err)
		}
	}
	mustFail(t, s, "UPDATE permission_mode_edits SET mode='prompt' WHERE id=?", changed.ID)
	mustFail(t, s, "DELETE FROM permission_mode_edits WHERE id=?", changed.ID)
}

func TestPermissionModeTransitionsRetireOnlyPendingAuthority(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	pending := admitOperation(t, s, operationSpec(cell, "pending"))
	same := setModeTest(t, s, owner.ID, "same", 1, session.PermissionPrompt)
	if same.Policy.Revision != 1 {
		t.Fatal("same mode bumped revision")
	}
	if operation, err := s.Operation(t.Context(), pending.ID); err != nil || operation.State != session.OperationWaiting {
		t.Fatal("same mode retired approval", operation, err)
	}
	setModeTest(t, s, owner.ID, "enable", 1, session.PermissionAutomatic)
	if permission, err := readPermission(t.Context(), s.db, pending.ID); err != nil || permission.State != session.PermissionDenied {
		t.Fatal("obsolete prompt remained pending", permission, err)
	}
	if _, err := s.ResolvePermission(t.Context(), pending.ID, true); !errors.Is(err, ErrConflict) {
		t.Fatal("obsolete approval gained authority", err)
	}
	ready := admitOperation(t, s, operationSpec(cell, "policy-ready"))
	dispatched := admitOperation(t, s, operationSpec(cell, "policy-dispatched"))
	if ready.GrantID != nil || ready.PermissionRevision == nil || *ready.PermissionRevision != 2 || ready.State != session.OperationReady {
		t.Fatalf("automatic authority absent: %+v", ready)
	}
	if allowed, err := s.DispatchOperation(t.Context(), dispatched.ID); err != nil || !allowed {
		t.Fatal("automatic operation did not dispatch", allowed, err)
	}
	standingGrant(t, s, owner.ID, "standing")
	granted := admitOperation(t, s, operationSpec(cell, "granted"))
	if granted.PermissionRevision != nil || granted.GrantID == nil {
		t.Fatal("explicit grant replaced by mode", granted)
	}
	setModeTest(t, s, owner.ID, "disable", 2, session.PermissionPrompt)
	if allowed, err := s.DispatchOperation(t.Context(), ready.ID); err != nil || allowed {
		t.Fatal("retired policy dispatched", allowed, err)
	}
	if after, err := s.Operation(t.Context(), ready.ID); err != nil || after.State != session.OperationDenied || after.PermissionRevision == nil {
		t.Fatal("retirement lost evidence", after, err)
	}
	if allowed, err := s.DispatchOperation(t.Context(), granted.ID); err != nil || !allowed {
		t.Fatal("mode change invalidated explicit grant", allowed, err)
	}
	if _, err := s.SettleOperation(t.Context(), dispatched.ID, session.OperationResult{State: session.OperationSucceeded}); err != nil {
		t.Fatal("dispatched work was retroactively invalidated", err)
	}
	retry := admitOperation(t, s, operationSpec(cell, "policy-ready"))
	if retry.State != session.OperationDenied || retry.PermissionRevision == nil || *retry.PermissionRevision != 2 {
		t.Fatal("retry reselected policy", retry)
	}
	if count(t, s, "grants") != 1 || count(t, s, "permissions") != 1 {
		t.Fatal("mode fabricated grants or permission rows")
	}
}

func TestPermissionModeChildDisplayNeverWidensDelegation(t *testing.T) {
	s := fresh(t)
	root, _ := operationCell(t, s)
	setModeTest(t, s, root.ID, "enable", 1, session.PermissionAutomatic)
	issuer := standingGrant(t, s, root.ID, "issuer")
	request := childRequest(root.ID)
	request.GrantIDs = []session.GrantID{}
	child := spawnChildTest(t, s, "child", request)
	cell := childOperationCell(t, s, child.Session.ID)
	policy, err := s.PermissionPolicy(t.Context(), child.Session.ID)
	if err != nil || policy.Mode != session.PermissionAutomatic || policy.TreeID != root.TreeID {
		t.Fatal("child did not inherit displayed policy", policy, err)
	}
	if op := admitOperation(t, s, operationSpec(cell, "without-delegation")); op.State != session.OperationDenied || op.PermissionRevision != nil {
		t.Fatal("automatic mode widened child", op)
	}
	grant := session.Grant{ID: "delegated", SessionID: child.Session.ID, Capability: issuer.Capability, Resource: issuer.Resource, IssuerID: &issuer.ID}
	if _, err := s.CreateGrant(t.Context(), grant); err != nil {
		t.Fatal(err)
	}
	ready := admitOperation(t, s, operationSpec(cell, "delegated"))
	if ready.State != session.OperationReady || ready.PermissionRevision != nil {
		t.Fatal("live delegation rejected", ready)
	}
	wrong := operationSpec(cell, "scope")
	wrong.Resource = "/elsewhere"
	if op := admitOperation(t, s, wrong); op.State != session.OperationDenied {
		t.Fatal("mode bypassed exact scope", op)
	}
	if _, err := s.SetPermissionMode(t.Context(), modeRequest(child.Session.ID, "child-edit", 2, session.PermissionPrompt)); !errors.Is(err, ErrConflict) {
		t.Fatal("child changed whole-tree policy", err)
	}
	if _, err := s.RevokeGrant(t.Context(), issuer.ID); err != nil {
		t.Fatal(err)
	}
	if allowed, err := s.DispatchOperation(t.Context(), ready.ID); err != nil || allowed {
		t.Fatal("mode bypassed revoked delegation", allowed, err)
	}
	if op := admitOperation(t, s, operationSpec(cell, "after-revoke")); op.State != session.OperationDenied {
		t.Fatal("mode restored revoked authority", op)
	}
	setModeTest(t, s, root.ID, "disable", 2, session.PermissionPrompt)
	if policy, err := s.PermissionPolicy(t.Context(), child.Session.ID); err != nil || policy.Mode != session.PermissionPrompt || policy.Revision != 3 {
		t.Fatal("child policy copy became stale", policy, err)
	}
}

func TestPermissionModeMutationRollbackAndValidation(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := admitOperation(t, s, operationSpec(cell, "pending"))
	before, err := s.PermissionPolicy(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := modeRequest(owner.ID, "enable", 1, session.PermissionAutomatic)
	for _, table := range []string{"permission_policies", "operations", "permissions", "permission_mode_edits"} {
		action := "UPDATE"
		if table == "permission_mode_edits" {
			action = "INSERT"
		}
		execTest(t, s, fmt.Sprintf("CREATE TRIGGER mode_fault BEFORE %s ON %s BEGIN SELECT RAISE(ABORT,'injected'); END", action, table))
		if result, err := s.SetPermissionMode(t.Context(), request); err == nil || result.ID != "" {
			t.Fatal("failed write returned edit", result, err)
		}
		policy, err := s.PermissionPolicy(t.Context(), owner.ID)
		if err != nil || policy != before {
			t.Fatal("rollback changed mode", policy, err)
		}
		if operation, err := s.Operation(t.Context(), op.ID); err != nil || operation.State != session.OperationWaiting {
			t.Fatal("rollback retired operation", operation, err)
		}
		if permission, err := readPermission(t.Context(), s.db, op.ID); err != nil || permission.State != session.PermissionPending {
			t.Fatal("rollback closed prompt", permission, err)
		}
		if count(t, s, "permission_mode_edits") != 0 {
			t.Fatal("rollback kept receipt")
		}
		execTest(t, s, "DROP TRIGGER mode_fault")
	}
	for _, mode := range []session.PermissionMode{"", "Ask", "full_access", " automatic"} {
		request.Mode = mode
		if _, err := s.SetPermissionMode(t.Context(), request); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("invalid mode accepted", mode, err)
		}
	}
}

func TestPermissionModeDispatchRaceSettlesAtOneBoundary(t *testing.T) {
	for range 8 {
		s := fresh(t)
		owner, cell := operationCell(t, s)
		setModeTest(t, s, owner.ID, "enable", 1, session.PermissionAutomatic)
		operation := admitOperation(t, s, operationSpec(cell, "race"))
		var allowed bool
		var dispatchErr, editErr error
		var workers sync.WaitGroup
		workers.Go(func() { allowed, dispatchErr = s.DispatchOperation(t.Context(), operation.ID) })
		workers.Go(func() {
			_, editErr = s.SetPermissionMode(t.Context(), modeRequest(owner.ID, "disable", 2, session.PermissionPrompt))
		})
		workers.Wait()
		if dispatchErr != nil || editErr != nil {
			t.Fatal("race error", dispatchErr, editErr)
		}
		saved, err := s.Operation(t.Context(), operation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if allowed && saved.State != session.OperationDispatched || !allowed && saved.State != session.OperationDenied {
			t.Fatal("race split authority", allowed, saved)
		}
	}
}

func TestPermissionModeForkCapturesFreshDefaultAndRetryNeverReselects(t *testing.T) {
	s := fresh(t)
	_, source := create(t, s, nil)
	setModeTest(t, s, source.ID, "enable", 1, session.PermissionAutomatic)
	request := session.ForkRequest{ID: "fork", SessionID: source.ID, ExpectedHistoryRevision: 1, ExpectedConfigRevision: 1}
	defaults := forkDefaultsTest()
	first, err := s.Fork(t.Context(), request, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if policy, err := s.PermissionPolicy(t.Context(), first.Root.ID); err != nil || policy.Mode != session.PermissionPrompt || policy.Revision != 1 {
		t.Fatal("fork copied source authority", policy, err)
	}
	defaults.PermissionMode = session.PermissionAutomatic
	retry, err := s.Fork(t.Context(), request, defaults)
	if err != nil || !reflect.DeepEqual(first, retry) {
		t.Fatal("fork retry changed", retry, err)
	}
	if policy, err := s.PermissionPolicy(t.Context(), first.Root.ID); err != nil || policy.Mode != session.PermissionPrompt {
		t.Fatal("retry applied new default", policy, err)
	}
	request.ID = "second"
	second, err := s.Fork(t.Context(), request, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if policy, err := s.PermissionPolicy(t.Context(), second.Root.ID); err != nil || policy.Mode != session.PermissionAutomatic || policy.Revision != 1 {
		t.Fatal("fresh fork missed default", policy, err)
	}
}

func TestPermissionModeConcurrentEditsAndImmutableAdmissionAuthority(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	var results [2]session.PermissionModeEdit
	var failures [2]error
	var workers sync.WaitGroup
	for i := range 2 {
		workers.Go(func() {
			results[i], failures[i] = s.SetPermissionMode(t.Context(), modeRequest(owner.ID, fmt.Sprintf("edit-%d", i), 1, session.PermissionAutomatic))
		})
	}
	workers.Wait()
	accepted := 0
	for i, err := range failures {
		if err == nil {
			accepted++
			retry, err := s.SetPermissionMode(t.Context(), results[i].PermissionModeRequest)
			if err != nil || retry != results[i] {
				t.Fatal("lost acknowledgement retry changed", retry, err)
			}
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if accepted != 1 || count(t, s, "permission_mode_edits") != 1 {
		t.Fatal("CAS allowed competing edits", accepted)
	}
	ready := admitOperation(t, s, operationSpec(cell, "automatic"))
	mustFail(t, s, "UPDATE operations SET permission_revision=3,state='dispatched',dispatched_at=1 WHERE id=?", ready.ID)
	mustFail(t, s, "UPDATE permission_policies SET revision=revision+1 WHERE tree_id=?", owner.TreeID)
	mustFail(t, s, "UPDATE permission_policies SET mode='prompt' WHERE tree_id=?", owner.TreeID)
	setModeTest(t, s, owner.ID, "disable", 2, session.PermissionPrompt)
	setModeTest(t, s, owner.ID, "reenable", 3, session.PermissionAutomatic)
	if operation := admitOperation(t, s, operationSpec(cell, "automatic")); operation.State != session.OperationDenied || operation.PermissionRevision == nil || *operation.PermissionRevision != 2 {
		t.Fatal("ABA policy change resurrected old authority", operation)
	}
	if next := admitOperation(t, s, operationSpec(cell, "new-automatic")); next.State != session.OperationReady || next.PermissionRevision == nil || *next.PermissionRevision != 4 {
		t.Fatal("new operation missed latest policy revision", next)
	}
}

func TestPermissionModeDoesNotOverrideIntrinsicQuestionRestrictions(t *testing.T) {
	s := fresh(t)
	root, cell := operationCell(t, s)
	setModeTest(t, s, root.ID, "enable", 1, session.PermissionAutomatic)
	spec := questionSpec(t, cell, "question", false)
	wrong := spec
	wrong.Resource = "another-session"
	if _, err := s.AdmitOperation(t.Context(), wrong); err == nil {
		t.Fatal("automatic mode accepted foreign human question resource")
	}
	rootQuestion := admitOperation(t, s, spec)
	if rootQuestion.PermissionRevision != nil || rootQuestion.GrantID != nil || rootQuestion.State != session.OperationReady {
		t.Fatal("intrinsic question captured policy authority", rootQuestion)
	}
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	childCell := childOperationCell(t, s, child.Session.ID)
	denied := admitOperation(t, s, questionSpec(t, childCell, "child-question", false))
	if denied.State != session.OperationDenied || denied.PermissionRevision != nil {
		t.Fatal("automatic mode bypassed root-only human interaction", denied)
	}
	if count(t, s, "permissions") != 0 || count(t, s, "questions") != 0 {
		t.Fatal("question admission created approval or fake waiter")
	}
}
