package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func workspaceGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git fixture %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func workspaceFileTest(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func workspaceRepoTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	workspaceGitTest(t, dir, "init", "-q")
	workspaceGitTest(t, dir, "config", "user.name", "Test")
	workspaceGitTest(t, dir, "config", "user.email", "test@localhost")
	workspaceFileTest(t, filepath.Join(dir, "tracked"), "before")
	workspaceGitTest(t, dir, "add", ".")
	workspaceGitTest(t, dir, "commit", "-qm", "base")
	return dir
}

func workspaceOwnerTest(t *testing.T, r *Runtime, dir string) session.Session {
	t.Helper()
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	_, owner, err := r.CreateTree(t.Context(), store.CreateTree{Engine: session.Starlark, Definition: refs[0], WorkingDirectory: dir, Overrides: session.ConfigPatch{Model: &session.ModelSelection{Provider: "test", Name: "test"}}})
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func workspaceRuntimeTest(t *testing.T, path string) *Runtime {
	t.Helper()
	return openTest(t, path, providerFunc(func(context.Context, model.Request) (model.Response, error) {
		return model.Response{}, errors.New("workspace action must not call model")
	}))
}

func workspaceRequest(owner session.SessionID, id string) session.WorkspaceRequest {
	return session.WorkspaceRequest{ID: session.WorkspaceActionID(id), SnapshotID: "snapshot", SessionID: owner}
}

func capturedWorkspaceTest(t *testing.T, r *Runtime, owner session.SessionID) session.WorkspaceResult {
	t.Helper()
	value, err := r.CaptureWorkspace(t.Context(), workspaceRequest(owner, "capture"))
	if err != nil || value.Action.State != session.WorkspaceSucceeded {
		t.Fatal(value, err)
	}
	return value
}

func waitWorkspaceFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("workspace child did not signal start")
		case <-tick.C:
		}
	}
}

func workspaceGitWrapper(t *testing.T, effect string) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	wrapper := "#!/bin/sh\ncase \" $* \" in *' checkout '*) " + effect + ";; esac\nexec '" + strings.ReplaceAll(realGit, "'", "'\\''") + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestWorkspaceRuntimeOverlayRetryReleaseAndDeletion(t *testing.T) {
	dir := workspaceRepoTest(t)
	path := t.TempDir()
	r := workspaceRuntimeTest(t, path)
	owner := workspaceOwnerTest(t, r, dir)
	capture := capturedWorkspaceTest(t, r, owner.ID)
	raw, err := json.Marshal(capture)
	if err != nil || strings.Contains(string(raw), dir) || strings.Contains(string(raw), *capture.Snapshot.ObjectID) || strings.Contains(string(raw), "GitDirectory") {
		t.Fatal("public snapshot leaked Git identity", string(raw), err)
	}
	workspaceFileTest(t, filepath.Join(dir, "tracked"), "changed")
	workspaceFileTest(t, filepath.Join(dir, "untracked"), "keep")
	request := workspaceRequest(owner.ID, "restore")
	result, err := r.RestoreWorkspace(t.Context(), request)
	if err != nil || result.Action.State != session.WorkspaceSucceeded {
		t.Fatal(result, err)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "tracked")); err != nil || string(body) != "before" {
		t.Fatal(string(body), err)
	}
	workspaceFileTest(t, filepath.Join(dir, "tracked"), "after acknowledgement")
	if retry, err := r.RestoreWorkspace(t.Context(), request); err != nil || !reflect.DeepEqual(retry.Action, result.Action) {
		t.Fatal("retry changed receipt", retry, err)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "tracked")); err != nil || string(body) != "after acknowledgement" {
		t.Fatal("retry replayed Git", string(body), err)
	}
	if err := r.DeleteSubtree(t.Context(), owner.ID); !errors.Is(err, store.ErrBusy) {
		t.Fatal("deleted retained pin", err)
	}
	release := workspaceRequest(owner.ID, "release")
	if value, err := r.ReleaseWorkspace(t.Context(), release); err != nil || value.Action.State != session.WorkspaceSucceeded || value.Snapshot.ReleasedAt == nil {
		t.Fatal(value, err)
	}
	if err := r.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = workspaceRuntimeTest(t, path)
	for _, test := range []struct {
		request session.WorkspaceRequest
		call    func(context.Context, session.WorkspaceRequest) (session.WorkspaceResult, error)
	}{
		{workspaceRequest(owner.ID, "capture"), r.CaptureWorkspace}, {request, r.RestoreWorkspace}, {release, r.ReleaseWorkspace},
	} {
		if value, err := test.call(t.Context(), test.request); err != nil || value.Action.State != session.WorkspaceSucceeded || value.Snapshot.ReleasedAt == nil {
			t.Fatal("deleted owner broke retry", value, err)
		}
	}
	if body, err := os.ReadFile(filepath.Join(dir, "untracked")); err != nil || string(body) != "keep" {
		t.Fatal("restore changed untracked file", string(body), err)
	}
}

func TestWorkspaceRuntimeLostAcknowledgementRecoversUncertainWithoutReplay(t *testing.T) {
	dir := workspaceRepoTest(t)
	path := t.TempDir()
	r := workspaceRuntimeTest(t, path)
	owner := workspaceOwnerTest(t, r, dir)
	capture := capturedWorkspaceTest(t, r, owner.ID)
	request := workspaceRequest(owner.ID, "restore")
	if _, claimed, err := r.store.ClaimWorkspace(t.Context(), session.WorkspaceRestore, request, capture.Snapshot.Binding); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	workspaceFileTest(t, filepath.Join(dir, "tracked"), "changed")
	if err := r.workspace.Restore(t.Context(), capture.Snapshot); err != nil {
		t.Fatal(err)
	}
	// Simulate a process loss after Git succeeds but before the SQL outcome.
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = workspaceRuntimeTest(t, path)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	workspaceFileTest(t, filepath.Join(dir, "tracked"), "edited after restart")
	value, err := r.RestoreWorkspace(t.Context(), request)
	if err != nil || value.Action.State != session.WorkspaceUncertain {
		t.Fatal(value, err)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "tracked")); err != nil || string(body) != "edited after restart" {
		t.Fatal("uncertain restore replayed", string(body), err)
	}
	if err := r.workspace.ValidatePin(t.Context(), capture.Snapshot, false); err != nil {
		t.Fatal("restart removed pin", err)
	}
	release := workspaceRequest(owner.ID, "release")
	if _, claimed, err := r.store.ClaimWorkspace(t.Context(), session.WorkspaceRelease, release, capture.Snapshot.Binding); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if err := r.workspace.Release(t.Context(), capture.Snapshot); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = workspaceRuntimeTest(t, path)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if value, err := r.ReleaseWorkspace(t.Context(), release); err != nil || value.Action.State != session.WorkspaceUncertain || value.Snapshot.ReleasedAt != nil {
		t.Fatal("ambiguous release was replayed", value, err)
	}
	if err := r.DeleteSubtree(t.Context(), owner.ID); !errors.Is(err, store.ErrBusy) {
		t.Fatal("uncertain release lost retained pin obligation", err)
	}
	// A new explicit release may confirm an absent pin; the original uncertain
	// action remains an immutable receipt and never runs again.
	release.ID = "explicit_cleanup"
	if value, err := r.ReleaseWorkspace(t.Context(), release); err != nil || value.Action.State != session.WorkspaceSucceeded || value.Snapshot.ReleasedAt == nil {
		t.Fatal(value, err)
	}
	if err := r.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceRuntimeUncertainCaptureRetainsKnownPinForExplicitRelease(t *testing.T) {
	dir := workspaceRepoTest(t)
	path := t.TempDir()
	r := workspaceRuntimeTest(t, path)
	owner := workspaceOwnerTest(t, r, dir)
	binding, err := r.workspace.Inspect(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	request := workspaceRequest(owner.ID, "capture")
	value, claimed, err := r.store.ClaimWorkspace(t.Context(), session.WorkspaceCapture, request, binding)
	if err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	object, err := r.workspace.Capture(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.store.StageWorkspaceSnapshot(t.Context(), request.ID, object); err != nil {
		t.Fatal(err)
	}
	value.Snapshot.ObjectID = &object
	if err := r.workspace.Pin(t.Context(), value.Snapshot); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = workspaceRuntimeTest(t, path)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	retry, err := r.CaptureWorkspace(t.Context(), request)
	if err != nil || retry.Action.State != session.WorkspaceUncertain || retry.Snapshot.ObjectID == nil || *retry.Snapshot.ObjectID != object {
		t.Fatal("capture lost known ambiguous pin", retry, err)
	}
	if _, err := r.RestoreWorkspace(t.Context(), workspaceRequest(owner.ID, "restore")); !errors.Is(err, store.ErrConflict) {
		t.Fatal("uncertain capture became restore authority", err)
	}
	if result, err := r.ReleaseWorkspace(t.Context(), workspaceRequest(owner.ID, "release")); err != nil || result.Action.State != session.WorkspaceSucceeded {
		t.Fatal("known ambiguous pin could not be explicitly released", result, err)
	}
}

func TestWorkspaceCallerDisconnectDoesNotCancelAcceptedRestore(t *testing.T) {
	dir := workspaceRepoTest(t)
	r := workspaceRuntimeTest(t, t.TempDir())
	owner := workspaceOwnerTest(t, r, dir)
	capturedWorkspaceTest(t, r, owner.ID)
	workspaceFileTest(t, filepath.Join(dir, "tracked"), "changed")
	workspaceGitWrapper(t, "echo started > .workspace-started; /bin/sleep 0.2")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan session.WorkspaceResult, 1)
	fail := make(chan error, 1)
	go func() {
		value, err := r.RestoreWorkspace(ctx, workspaceRequest(owner.ID, "restore"))
		done <- value
		fail <- err
	}()
	waitWorkspaceFile(t, filepath.Join(dir, ".workspace-started"))
	cancel()
	value := <-done
	if err := <-fail; err != nil || value.Action.State != session.WorkspaceSucceeded {
		t.Fatal("caller cancellation owned accepted effect", value, err)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "tracked")); err != nil || string(body) != "before" {
		t.Fatal(string(body), err)
	}
}

func TestWorkspaceCloseJoinsPartialEffectAndRecordsUncertainty(t *testing.T) {
	dir := workspaceRepoTest(t)
	path := t.TempDir()
	r := workspaceRuntimeTest(t, path)
	owner := workspaceOwnerTest(t, r, dir)
	capturedWorkspaceTest(t, r, owner.ID)
	workspaceGitWrapper(t, "echo partial > tracked; echo started > .workspace-started; /bin/sleep 30; exit 77")
	request := workspaceRequest(owner.ID, "restore")
	done := make(chan session.WorkspaceResult, 1)
	fail := make(chan error, 1)
	go func() { value, err := r.RestoreWorkspace(t.Context(), request); done <- value; fail <- err }()
	waitWorkspaceFile(t, filepath.Join(dir, ".workspace-started"))
	started := time.Now()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("runtime Close did not join owned Git promptly")
	}
	value := <-done
	if err := <-fail; err != nil || value.Action.State != session.WorkspaceUncertain {
		t.Fatal("partial effect lost outcome before SQL closed", value, err)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "tracked")); err != nil || string(body) != "partial\n" {
		t.Fatal("partial effect fixture did not execute", string(body), err)
	}
	r = workspaceRuntimeTest(t, path)
	workspaceFileTest(t, filepath.Join(dir, "tracked"), "manual repair")
	if value, err := r.RestoreWorkspace(t.Context(), request); err != nil || value.Action.State != session.WorkspaceUncertain {
		t.Fatal("uncertain receipt lost", value, err)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "tracked")); err != nil || string(body) != "manual repair" {
		t.Fatal("uncertain action replayed", string(body), err)
	}
}

func TestWorkspaceConcurrentCaptureRetriesHaveOnePinAndChangedIdentityConflicts(t *testing.T) {
	dir := workspaceRepoTest(t)
	r := workspaceRuntimeTest(t, t.TempDir())
	owner := workspaceOwnerTest(t, r, dir)
	request := workspaceRequest(owner.ID, "capture")
	results := make(chan session.WorkspaceResult, 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { value, err := r.CaptureWorkspace(t.Context(), request); results <- value; failures <- err })
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	for value := range results {
		if value.Action.State != session.WorkspaceSucceeded && value.Action.State != session.WorkspaceClaimed {
			t.Fatal(value)
		}
	}
	if snapshots, err := r.WorkspaceSnapshots(t.Context(), owner.ID, "", 100); err != nil || len(snapshots) != 1 {
		t.Fatal(snapshots, err)
	}
	request.SnapshotID = "different"
	if _, err := r.CaptureWorkspace(t.Context(), request); !errors.Is(err, store.ErrConflict) {
		t.Fatal("request identity changed", err)
	}
	other := workspaceOwnerTest(t, r, workspaceRepoTest(t))
	if _, err := r.RestoreWorkspace(t.Context(), workspaceRequest(other.ID, "foreign")); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("snapshot crossed session scope", err)
	}
}
