package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Git fixture %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeTest(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func textTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func repositoryTest(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repository ")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "init", "-q")
	gitTest(t, dir, "config", "user.name", "Test")
	gitTest(t, dir, "config", "user.email", "test@localhost")
	for _, name := range []string{"tracked", "deleted", "restore_deleted"} {
		writeTest(t, filepath.Join(dir, name), "base")
	}
	gitTest(t, dir, "add", ".")
	gitTest(t, dir, "commit", "-qm", "base")
	return dir
}

func managerTest(t *testing.T) *Git {
	t.Helper()
	g := New("runtime_fixture")
	t.Cleanup(func() {
		if err := g.Close(); err != nil {
			t.Error(err)
		}
	})
	return g
}

func snapshotTest(t *testing.T, g *Git, dir string, id session.WorkspaceSnapshotID) session.WorkspaceSnapshot {
	t.Helper()
	ctx, cancel := g.Context(t.Context())
	defer cancel()
	binding, err := g.Inspect(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := g.Lock(ctx, binding)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	object, err := g.Capture(ctx, binding)
	if err != nil {
		t.Fatal(err)
	}
	value := session.WorkspaceSnapshot{ID: id, Binding: binding, ObjectID: &object}
	if err := g.Pin(ctx, value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestTrackedOverlayPreservesUntrackedAndLaterFilesButReplacesStaging(t *testing.T) {
	dir := repositoryTest(t)
	g := managerTest(t)
	writeTest(t, filepath.Join(dir, "tracked"), "staged")
	gitTest(t, dir, "add", "tracked")
	writeTest(t, filepath.Join(dir, "tracked"), "working")
	if err := os.Remove(filepath.Join(dir, "deleted")); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(dir, "untracked"), "untracked before")
	value := snapshotTest(t, g, dir, "opaque")
	if actual := gitTest(t, dir, "show", ":tracked"); actual != "staged" {
		t.Fatal("capture changed index", actual)
	}
	writeTest(t, filepath.Join(dir, "tracked"), "later edit")
	writeTest(t, filepath.Join(dir, "deleted"), "later recreated")
	writeTest(t, filepath.Join(dir, "later"), "new tracked file")
	gitTest(t, dir, "add", "later")
	if err := os.Remove(filepath.Join(dir, "restore_deleted")); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(dir, "untracked"), "untracked after")
	if err := g.Restore(t.Context(), value); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"tracked": "working", "deleted": "later recreated", "later": "new tracked file", "restore_deleted": "base", "untracked": "untracked after"} {
		if actual := textTest(t, filepath.Join(dir, path)); actual != want {
			t.Fatalf("overlay %s=%q want %q", path, actual, want)
		}
	}
	if actual := gitTest(t, dir, "show", ":tracked"); actual != "working" {
		t.Fatal("restore falsely preserved staging", actual)
	}
	if err := g.ValidatePin(t.Context(), value, false); err != nil {
		t.Fatal("restore consumed pin", err)
	}
	if err := g.Release(t.Context(), value); err != nil {
		t.Fatal(err)
	}
	if err := g.Release(t.Context(), value); err != nil {
		t.Fatal("absent pin cleanup failed", err)
	}
	if err := g.Restore(t.Context(), value); !errors.Is(err, ErrChanged) {
		t.Fatal("released pin restored", err)
	}
}

func TestSnapshotScopeAndPinCannotBeSubstituted(t *testing.T) {
	dir := repositoryTest(t)
	g := managerTest(t)
	scope := filepath.Join(dir, "scope [odd] ")
	if err := os.Mkdir(scope, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(scope, "inside"), "inside base")
	gitTest(t, dir, "add", ".")
	gitTest(t, dir, "commit", "-qm", "scope")
	value := snapshotTest(t, g, scope, "scope")
	writeTest(t, filepath.Join(scope, "inside"), "inside changed")
	writeTest(t, filepath.Join(dir, "tracked"), "outside changed")
	if err := g.Restore(t.Context(), value); err != nil {
		t.Fatal(err)
	}
	if textTest(t, filepath.Join(scope, "inside")) != "inside base" || textTest(t, filepath.Join(dir, "tracked")) != "outside changed" {
		t.Fatal("literal captured scope was widened")
	}
	// The opaque handle is bound to one runtime namespace and expected object.
	foreign := New("different_runtime")
	defer func() { _ = foreign.Close() }()
	if err := foreign.Restore(t.Context(), value); !errors.Is(err, ErrChanged) {
		t.Fatal("foreign runtime inherited pin", err)
	}
	gitTest(t, dir, "update-ref", g.pin(value.ID), "HEAD")
	if err := g.Restore(t.Context(), value); !errors.Is(err, ErrChanged) {
		t.Fatal("changed pin accepted", err)
	}
	if err := g.Release(t.Context(), value); !errors.Is(err, ErrChanged) {
		t.Fatal("changed pin deleted", err)
	}
	gitTest(t, dir, "update-ref", "-d", g.pin(value.ID))
	gitTest(t, dir, "update-ref", g.pin(value.ID)+"/child", *value.ObjectID)
	if err := g.ValidatePin(t.Context(), value, false); !errors.Is(err, ErrChanged) {
		t.Fatal("a descendant ref substituted for the exact pin", err)
	}
}

func TestWorkspaceBindingRejectsBareReplacementAndLinkedGitdirChanges(t *testing.T) {
	g := managerTest(t)
	bare := t.TempDir()
	gitTest(t, bare, "init", "--bare", "-q")
	if _, err := g.Inspect(t.Context(), bare); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("bare repository accepted", err)
	}
	dir := repositoryTest(t)
	linked := filepath.Join(t.TempDir(), "linked")
	gitTest(t, dir, "worktree", "add", "--detach", linked, "HEAD")
	value := snapshotTest(t, g, linked, "linked")
	if value.Binding.GitDirectory == value.Binding.CommonDirectory {
		t.Fatal("linked gitdir not captured")
	}
	commonPath := filepath.Join(value.Binding.GitDirectory, "commondir")
	originalCommon := textTest(t, commonPath)
	other := repositoryTest(t)
	writeTest(t, commonPath, filepath.Join(other, ".git")+"\n")
	if err := g.ValidatePin(t.Context(), value, false); !errors.Is(err, ErrChanged) {
		t.Fatal("changed common directory accepted", err)
	}
	writeTest(t, commonPath, originalCommon)
	// Swapping the .git link to another worktree must not reuse the old identity.
	writeTest(t, filepath.Join(linked, ".git"), "gitdir: "+filepath.Join(dir, ".git")+"\n")
	if err := g.ValidatePin(t.Context(), value, false); !errors.Is(err, ErrChanged) {
		t.Fatal("changed linked worktree accepted", err)
	}
	rootValue := snapshotTest(t, g, dir, "root")
	old := dir + "_old"
	if err := os.Rename(dir, old); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(old) })
	if err := os.Symlink(old, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Lock(t.Context(), rootValue.Binding); !errors.Is(err, ErrChanged) {
		t.Fatal("replaced root accepted", err)
	}
}

func TestWorkspaceLocksAreCrossManagerAndCancellationBounded(t *testing.T) {
	dir := repositoryTest(t)
	first := managerTest(t)
	second := managerTest(t)
	binding, err := first.Inspect(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := first.Lock(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if _, err := second.Lock(ctx, binding); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("second manager acquired same worktree", err)
	}
	unlock()
	other, err := second.Lock(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	other()
}

func TestWorkspaceSubprocessOutputAndCloseAreBounded(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	// Only this disposable test process sees the wrapper. The manager must reap
	// its sleeping descendant on Close instead of orphaning a Git process group.
	wrapper := "#!/bin/sh\ncase \"$*\" in *test-overflow*) /usr/bin/head -c 300000 /dev/zero;; *test-sleep*) echo started > started; /bin/sleep 30;; *) exec '" + strings.ReplaceAll(realGit, "'", "'\\''") + "' \"$@\";; esac\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	g := managerTest(t)
	if _, err := g.run(t.Context(), bin, "test-overflow"); !errors.Is(err, ErrOutputLimit) {
		t.Fatal("unbounded Git output", err)
	}
	done := make(chan error, 1)
	go func() { _, err := g.run(t.Context(), bin, "test-sleep"); done <- err }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(filepath.Join(bin, "started")); err == nil {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("child did not start")
		case <-ticker.C:
		}
	}
	started := time.Now()
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("closed manager completed sleeping process")
	}
	if time.Since(started) > time.Second {
		t.Fatal("Close failed to join process group promptly")
	}
}
