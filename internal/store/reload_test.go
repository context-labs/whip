package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func captureReload(t *testing.T, s *Store, root session.Session, id string) session.ReloadEdit {
	t.Helper()
	host := root.Config.Clone()
	host.Compaction.ThresholdPercent = 75
	value, err := s.AdmitReload(t.Context(), session.ReloadRequest{ID: id, SessionID: root.ID, ExpectedRevision: root.ConfigRevision}, host, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestReloadCapturedDeferredBoundaryAndExactRestartRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, root := create(t, s, nil)
	grant := standingGrant(t, s, root.ID, "grant")
	submit(t, s, root.ID, "running")
	running := claim(t, s, root.ID)
	child := controlChild(t, s, root.ID, "child")
	captured := captureReload(t, s, root, "Reload.Mixed")
	if captured.State != session.ReloadPending || captured.Configuration.Compaction.ThresholdPercent != 75 {
		t.Fatal(captured)
	}
	if _, changed, err := s.ApplyReload(t.Context(), captured.ID); !errors.Is(err, ErrBusy) || changed {
		t.Fatal(changed, err)
	}
	// Existing parent work must be able to make progress through a queued child.
	childTurn := claim(t, s, child.Session.ID)
	if _, err := s.Finish(t.Context(), childTurn.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), running.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	submit(t, s, root.ID, "queued")
	if _, err := s.Claim(t.Context(), root.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("new work crossed reload boundary", err)
	}
	s = openTest(t, path)
	exact, err := s.AdmitReload(t.Context(), captured.ReloadRequest, session.Configuration{}, "not a current revision")
	if err != nil || !reflect.DeepEqual(exact, captured) {
		t.Fatal(exact, err)
	}
	applied, changed, err := s.ApplyReload(t.Context(), captured.ID)
	if err != nil || !changed || applied.State != session.ReloadApplied || *applied.Revision != 2 {
		t.Fatal(applied, changed, err)
	}
	next := claim(t, s, root.ID)
	if next.Configuration.Compaction.ThresholdPercent != 75 || next.Turn.ConfigRevision != 2 {
		t.Fatal(next)
	}
	old, err := s.Configuration(t.Context(), root.ID, running.Turn.ConfigRevision)
	if err != nil || old.Compaction.ThresholdPercent != 50 {
		t.Fatal(old, err)
	}
	retained, err := s.Session(t.Context(), child.Session.ID)
	if err != nil || retained.ConfigRevision != 1 || retained.Config.Compaction.ThresholdPercent != 50 {
		t.Fatal(retained, err)
	}
	found, err := s.Grants(t.Context(), root.ID, "", 100)
	if err != nil || len(found) != 1 || found[0].ID != grant.ID || found[0].RevokedAt != nil {
		t.Fatal(found, err)
	}
	again, changed, err := s.ApplyReload(t.Context(), captured.ID)
	if err != nil || changed || !reflect.DeepEqual(again, applied) {
		t.Fatal(again, changed, err)
	}
	changedRequest := captured.ReloadRequest
	changedRequest.ExpectedRevision++
	if _, err := s.ReloadRetry(t.Context(), changedRequest); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.ReloadEdit(t.Context(), child.Session.ID, captured.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.AdmitReload(t.Context(), session.ReloadRequest{ID: "child", SessionID: child.Session.ID, ExpectedRevision: 1}, root.Config, strings.Repeat("a", 64)); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestReloadOverridesSurviveManualEditsRunWorkspaceForkAndRollback(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	explicit := root.Config.Compaction
	updated, err := s.UpdateConfiguration(t.Context(), root.ID, 1, session.ConfigPatch{Compaction: &explicit, AutomaticTitle: new(false)})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.ConfigureRun(t.Context(), session.RunConfigureRequest{ID: "run", SessionID: root.ID, ExpectedRevision: 2, Configuration: session.RunConfiguration{System: "keep"}})
	if err != nil {
		t.Fatal(err)
	}
	directory, err := setDirectory(t, s, session.WorkspaceSetRequest{ID: "cwd", SessionID: root.ID, ExpectedRevision: 3, Path: root.WorkingDirectory}, root.WorkingDirectory)
	if err != nil {
		t.Fatal(err)
	}
	captured := captureReload(t, s, *directory.Session, "captured")
	if captured.Configuration.Compaction.ThresholdPercent != 50 || captured.Configuration.AutomaticTitle || captured.Configuration.Run.System != run.Session.Config.Run.System {
		t.Fatal(captured)
	}
	execTest(t, s, `CREATE TRIGGER fail_reload BEFORE UPDATE ON session_reloads WHEN NEW.state='applied' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, _, err := s.ApplyReload(t.Context(), captured.ID); err == nil {
		t.Fatal("expected rollback")
	}
	still, err := s.Session(t.Context(), root.ID)
	if err != nil || still.ConfigRevision != 4 {
		t.Fatal(still, err)
	}
	execTest(t, s, "DROP TRIGGER fail_reload")
	if _, _, err := s.ApplyReload(t.Context(), captured.ID); err != nil {
		t.Fatal(err)
	}
	fork, err := s.Fork(t.Context(), session.ForkRequest{ID: "fork", SessionID: root.ID, ExpectedConfigRevision: 5, ExpectedHistoryRevision: 1}, session.ForkDefaults{Budgets: session.DefaultWriteBudgets()})
	if err != nil {
		t.Fatal(err)
	}
	fields, err := reloadOverrides(t.Context(), s.db, fork.Root.ID, 1)
	if err != nil || fields != session.ExplicitReloadOverrides(session.ConfigPatch{Compaction: &explicit, AutomaticTitle: new(false)}) {
		t.Fatal(fields, err, updated)
	}
	mustFail(t, s, "UPDATE session_configurations SET override_fields=0 WHERE session_id=?", root.ID)
	mustFail(t, s, "UPDATE session_reloads SET host_revision=? WHERE id=?", strings.Repeat("b", 64), captured.ID)
	mustFail(t, s, "DELETE FROM session_reloads WHERE id=?", captured.ID)
}

func TestReloadConflictsCancellationDeletionAndStorageBounds(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	captured := captureReload(t, s, root, "conflict")
	if _, err := s.UpdateConfiguration(t.Context(), root.ID, 1, session.ConfigPatch{AutomaticTitle: new(false)}); err != nil {
		t.Fatal(err)
	}
	result, changed, err := s.ApplyReload(t.Context(), captured.ID)
	if err != nil || changed || result.State != session.ReloadConflicted {
		t.Fatal(result, changed, err)
	}
	root, err = s.Session(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	captured = captureReload(t, s, root, "cancel")
	var group sync.WaitGroup
	group.Go(func() {
		if _, _, err := s.ApplyReload(t.Context(), captured.ID); err != nil {
			t.Error(err)
		}
	})
	group.Go(func() {
		if _, err := s.CancelReload(t.Context(), root.ID, captured.ID); err != nil {
			t.Error(err)
		}
	})
	group.Wait()
	result, err = s.ReloadEdit(t.Context(), root.ID, captured.ID)
	if err != nil || result.State == session.ReloadPending {
		t.Fatal(result, err)
	}
	before := result.State
	result, err = s.CancelReload(t.Context(), root.ID, captured.ID)
	if err != nil || result.State != before {
		t.Fatal(result, err)
	}
	root, err = s.Session(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	captured = captureReload(t, s, root, "deleted")
	if err := s.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	result, err = s.ReloadRetry(t.Context(), captured.ReloadRequest)
	if err != nil || result.State != session.ReloadUnavailable {
		t.Fatal(result, err)
	}
	_, root = create(t, s, nil)
	for i := range 64 {
		request := session.ReloadRequest{ID: strings.Repeat("x", i+1), SessionID: root.ID, ExpectedRevision: 1}
		if _, err := s.AdmitReload(t.Context(), request, root.Config, strings.Repeat("a", 64)); err != nil {
			t.Fatal(i, err)
		}
		if _, err := s.CancelReload(t.Context(), root.ID, request.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AdmitReload(t.Context(), session.ReloadRequest{ID: "overlimit", SessionID: root.ID, ExpectedRevision: 1}, root.Config, strings.Repeat("a", 64)); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}
