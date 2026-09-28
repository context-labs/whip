package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func workspaceBindingTest(t *testing.T) session.WorkspaceBinding {
	t.Helper()
	root := t.TempDir()
	return session.WorkspaceBinding{Worktree: root, GitDirectory: filepath.Join(root, ".git"), CommonDirectory: filepath.Join(root, ".git"), Scope: ".", WorktreeIdentity: "1:1", GitIdentity: "1:2", CommonIdentity: "1:2", ScopeIdentity: "1:1"}
}

func workspaceRequestTest(owner session.SessionID, id string) session.WorkspaceRequest {
	return session.WorkspaceRequest{ID: session.WorkspaceActionID(id), SnapshotID: "snapshot", SessionID: owner}
}

func captureWorkspaceTest(t *testing.T, s *Store, request session.WorkspaceRequest, binding session.WorkspaceBinding) session.WorkspaceResult {
	t.Helper()
	value, claimed, err := s.ClaimWorkspace(t.Context(), session.WorkspaceCapture, request, binding)
	if err != nil || !claimed || value.Action.State != session.WorkspaceClaimed {
		t.Fatal(value, claimed, err)
	}
	if err := s.StageWorkspaceSnapshot(t.Context(), request.ID, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	value, err = s.SettleWorkspace(t.Context(), request.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestWorkspaceClaimsBlockOwnerExecutionAndRetainPinsUntilRelease(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	binding := workspaceBindingTest(t)
	request := workspaceRequestTest(owner.ID, "capture")
	value, claimed, err := s.ClaimWorkspace(t.Context(), session.WorkspaceCapture, request, binding)
	if err != nil || !claimed {
		t.Fatal(value, claimed, err)
	}
	retry, claimed, err := s.ClaimWorkspace(t.Context(), session.WorkspaceCapture, request, session.WorkspaceBinding{})
	if err != nil || claimed || !reflect.DeepEqual(value, retry) {
		t.Fatal("exact retry regained dispatch", retry, claimed, err)
	}
	changed := request
	changed.SessionID = "missing"
	if _, _, err := s.ClaimWorkspace(t.Context(), session.WorkspaceCapture, changed, binding); !errors.Is(err, ErrConflict) {
		t.Fatal("identity changed", err)
	}
	admitted := submit(t, s, owner.ID, "after_claim")
	if _, err := s.Claim(t.Context(), owner.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("turn started during workspace effect", err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("deletion discarded pin", err)
	}
	if err := s.StageWorkspaceSnapshot(t.Context(), request.ID, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if err := s.StageWorkspaceSnapshot(t.Context(), request.ID, strings.Repeat("b", 40)); !errors.Is(err, ErrConflict) {
		t.Fatal("staged commit changed", err)
	}
	if _, err := s.SettleWorkspace(t.Context(), request.ID, true); err != nil {
		t.Fatal(err)
	}
	turn := claim(t, s, owner.ID)
	if turn.Input.ID != admitted.Input.ID {
		t.Fatal("workspace claim lost queued input")
	}
	finishMailTest(t, s, turn.Turn.ID, session.Succeeded)
	release := workspaceRequestTest(owner.ID, "release")
	if _, claimed, err := s.ClaimWorkspace(t.Context(), session.WorkspaceRelease, release, binding); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if _, err := s.SettleWorkspace(t.Context(), release.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	for _, kindAndRequest := range []struct {
		kind    session.WorkspaceActionKind
		request session.WorkspaceRequest
	}{{session.WorkspaceCapture, request}, {session.WorkspaceRelease, release}} {
		if result, err := s.WorkspaceRetry(t.Context(), kindAndRequest.kind, kindAndRequest.request); err != nil || result.Action.State != session.WorkspaceSucceeded || result.Snapshot.ReleasedAt == nil {
			t.Fatal("deletion broke retained receipt", result, err)
		}
	}
	if count(t, s, "workspace_snapshots") != 1 || count(t, s, "workspace_actions") != 2 {
		t.Fatal("deleted owner erased workspace receipts")
	}
	mustFail(t, s, "DELETE FROM workspace_snapshots WHERE id=?", request.SnapshotID)
	mustFail(t, s, "DELETE FROM workspace_actions WHERE id=?", request.ID)
}

func TestWorkspaceRecoveryDoesNotReplayUncertainCaptureRestoreOrRelease(t *testing.T) {
	for _, kind := range []session.WorkspaceActionKind{session.WorkspaceCapture, session.WorkspaceRestore, session.WorkspaceRelease} {
		t.Run(string(kind), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s := openTest(t, path)
			_, owner := create(t, s, nil)
			binding := workspaceBindingTest(t)
			request := workspaceRequestTest(owner.ID, "action")
			if kind != session.WorkspaceCapture {
				captureWorkspaceTest(t, s, workspaceRequestTest(owner.ID, "capture"), binding)
			}
			if _, claimed, err := s.ClaimWorkspace(t.Context(), kind, request, binding); err != nil || !claimed {
				t.Fatal(claimed, err)
			}
			if kind == session.WorkspaceCapture {
				if err := s.StageWorkspaceSnapshot(t.Context(), request.ID, strings.Repeat("a", 40)); err != nil {
					t.Fatal(err)
				}
			}
			s = openTest(t, path)
			if _, err := s.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			result, claimed, err := s.ClaimWorkspace(t.Context(), kind, request, session.WorkspaceBinding{})
			if err != nil || claimed || result.Action.State != session.WorkspaceUncertain || result.Action.FinishedAt == nil || result.Snapshot.ObjectID == nil {
				t.Fatal("interrupted effect was dispatched", result, claimed, err)
			}
			if _, err := s.SettleWorkspace(t.Context(), request.ID, true); !errors.Is(err, ErrConflict) {
				t.Fatal("uncertainty rewritten as success", err)
			}
			if _, err := s.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			retry, err := s.WorkspaceRetry(t.Context(), kind, request)
			if err != nil || !reflect.DeepEqual(result, retry) {
				t.Fatal("recovery rewrote terminal receipt", retry, err)
			}
			if count(t, s, "turns") != 0 || count(t, s, "inputs") != 0 || count(t, s, "operations") != 0 {
				t.Fatal("human workspace action invented agent execution")
			}
		})
	}
}

func TestWorkspaceAdmissionRejectsBusyWrongScopeAndOversizedPins(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	_, other := create(t, s, nil)
	binding := workspaceBindingTest(t)
	request := workspaceRequestTest(owner.ID, "capture")
	input := submit(t, s, owner.ID, "queued")
	if _, _, err := s.ClaimWorkspace(t.Context(), session.WorkspaceCapture, request, binding); !errors.Is(err, ErrBusy) {
		t.Fatal("queued work ignored", err)
	}
	if _, err := s.CancelInput(t.Context(), input.Input.ID); err != nil {
		t.Fatal(err)
	}
	captureWorkspaceTest(t, s, request, binding)
	request.ID = "restore"
	request.SessionID = other.ID
	if _, _, err := s.ClaimWorkspace(t.Context(), session.WorkspaceRestore, request, binding); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign snapshot inherited", err)
	}
	request.SessionID = owner.ID
	changed := binding
	changed.Scope = "other"
	if _, _, err := s.ClaimWorkspace(t.Context(), session.WorkspaceRestore, request, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("restore widened snapshot scope", err)
	}
	for i := 1; i < session.MaxWorkspaceSnapshots; i++ {
		captureWorkspaceTest(t, s, session.WorkspaceRequest{ID: session.WorkspaceActionID(fmt.Sprintf("capture_%d", i)), SnapshotID: session.WorkspaceSnapshotID(fmt.Sprintf("snapshot_%d", i)), SessionID: owner.ID}, binding)
	}
	limit := session.WorkspaceRequest{ID: "limit", SnapshotID: "limit", SessionID: owner.ID}
	if _, _, err := s.ClaimWorkspace(t.Context(), session.WorkspaceCapture, limit, binding); !errors.Is(err, ErrLimit) {
		t.Fatal("unbounded live pins", err)
	}
	if count(t, s, "workspace_snapshots") != session.MaxWorkspaceSnapshots {
		t.Fatal("limit inserted snapshot")
	}
}

func TestWorkspaceLedgerSQLFailuresRollBackClaimAndSettlement(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	binding := workspaceBindingTest(t)
	request := workspaceRequestTest(owner.ID, "capture")
	execTest(t, s, `CREATE TRIGGER injected_workspace BEFORE INSERT ON workspace_actions BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, _, err := s.ClaimWorkspace(t.Context(), session.WorkspaceCapture, request, binding); err == nil {
		t.Fatal("failed claim accepted")
	}
	if count(t, s, "workspace_snapshots") != 0 {
		t.Fatal("orphan snapshot escaped failed claim")
	}
	execTest(t, s, "DROP TRIGGER injected_workspace")
	captureWorkspaceTest(t, s, request, binding)
	release := workspaceRequestTest(owner.ID, "release")
	if _, _, err := s.ClaimWorkspace(t.Context(), session.WorkspaceRelease, release, binding); err != nil {
		t.Fatal(err)
	}
	execTest(t, s, `CREATE TRIGGER injected_workspace BEFORE UPDATE ON workspace_actions BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := s.SettleWorkspace(t.Context(), release.ID, true); err == nil {
		t.Fatal("failed settlement accepted")
	}
	value, err := s.WorkspaceSnapshot(t.Context(), owner.ID, request.SnapshotID)
	if err != nil || value.ReleasedAt != nil {
		t.Fatal("release escaped failed settlement", value, err)
	}
	execTest(t, s, "DROP TRIGGER injected_workspace")
	if _, err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	valueResult, err := s.WorkspaceRetry(t.Context(), session.WorkspaceRelease, release)
	if err != nil || valueResult.Action.State != session.WorkspaceUncertain {
		t.Fatal(valueResult, err)
	}
	mustFail(t, s, "UPDATE workspace_actions SET state='succeeded' WHERE id=?", release.ID)
	mustFail(t, s, "UPDATE workspace_snapshots SET binding='{}' WHERE id=?", request.SnapshotID)
}

func TestWorkspaceSchemaRejectsPreviousVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	execTest(t, s, "PRAGMA user_version=33")
	if _, err := Open(t.Context(), path); !errors.Is(err, ErrSchema) {
		t.Fatal("pre-workspace schema accepted", err)
	}
}
