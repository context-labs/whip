package capability

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceResolve(t *testing.T) {
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "root")
	inside := filepath.Join(realRoot, "inside")
	outside := filepath.Join(parent, "outside")
	for _, dir := range []string{inside, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	rootAlias := filepath.Join(parent, "root-alias")
	if err := os.Symlink(realRoot, rootAlias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(inside, filepath.Join(realRoot, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(realRoot, "escape")); err != nil {
		t.Fatal(err)
	}

	w, err := NewWorkspaces().Open(rootAlias)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	if w.Root() != canonicalRoot {
		t.Fatalf("Root() = %q, want %q", w.Root(), canonicalRoot)
	}
	got, err := w.Resolve(filepath.Join("alias", "missing", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	canonicalInside, err := filepath.EvalSymlinks(inside)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonicalInside, "missing", "file.txt")
	if got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}

	for _, path := range []string{
		filepath.Join("escape", "new.txt"),
		filepath.Join(parent, "root-sibling", "new.txt"),
		filepath.Join("..", "outside", "new.txt"),
	} {
		if _, err := w.Resolve(path); err == nil {
			t.Errorf("Resolve(%q) accepted a path outside the workspace", path)
		}
	}
	if _, err := NewWorkspaces().Open(filepath.Join(parent, "missing")); err == nil {
		t.Fatal("NewWorkspace accepted a missing root")
	}
}

func TestWorkspaceLocks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	w, err := NewWorkspaces().Open(root)
	if err != nil {
		t.Fatal(err)
	}

	_, unlock, err := w.LockPath(context.Background(), filepath.Join(dir, "same"))
	if err != nil {
		t.Fatal(err)
	}

	different := make(chan func(), 1)
	go func() {
		_, release, lockErr := w.LockPath(context.Background(), filepath.Join(dir, "different"))
		if lockErr == nil {
			different <- release
		}
	}()
	select {
	case release := <-different:
		release()
	case <-time.After(time.Second):
		t.Fatal("different canonical paths did not overlap")
	}

	alias := make(chan func(), 1)
	go func() {
		_, release, lockErr := w.LockPath(context.Background(), filepath.Join(root, "alias", "same"))
		if lockErr == nil {
			alias <- release
		}
	}()
	assertBlocked(t, alias, "symlink alias acquired the same canonical path")

	all := make(chan func(), 1)
	go func() {
		release, lockErr := w.LockAll(context.Background())
		if lockErr == nil {
			all <- release
		}
	}()
	assertBlocked(t, all, "global mutation overlapped a path mutation")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := w.LockPath(ctx, filepath.Join(root, "alias", "same")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended LockPath error = %v, want deadline exceeded", err)
	}
	if _, err := w.LockAll(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended LockAll error = %v, want deadline exceeded", err)
	}

	unlock()
	releaseAll := <-all

	blockedPath := make(chan func(), 1)
	go func() {
		_, release, lockErr := w.LockPath(context.Background(), filepath.Join(dir, "third"))
		if lockErr == nil {
			blockedPath <- release
		}
	}()
	assertBlocked(t, blockedPath, "path mutation overlapped a global mutation")
	pathCtx, pathCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer pathCancel()
	if _, _, err := w.LockPath(pathCtx, filepath.Join(dir, "fourth")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("path behind global mutation error = %v, want deadline exceeded", err)
	}
	releaseAll()
	releaseAlias := <-alias
	releaseAlias()
	(<-blockedPath)()
}

func TestWorkspaceBarrierSpansRoots(t *testing.T) {
	workspaces := NewWorkspaces()
	one, err := workspaces.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	two, err := workspaces.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, releasePath, err := one.LockPath(context.Background(), "file")
	if err != nil {
		t.Fatal(err)
	}
	lockedAll := make(chan func(), 1)
	go func() {
		release, lockErr := two.LockAll(context.Background())
		if lockErr == nil {
			lockedAll <- release
		}
	}()
	assertBlocked(t, lockedAll, "root-wide mutation did not wait for another root's path mutation")
	releasePath()
	(<-lockedAll)()
}

func TestWorkspaceFilesystemErrors(t *testing.T) {
	root := t.TempDir()
	rootFile := filepath.Join(root, "file")
	if err := os.WriteFile(rootFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWorkspaces().Open(rootFile); err == nil {
		t.Fatal("Open accepted a file as a workspace root")
	}

	workspaces := NewWorkspaces()
	w, err := workspaces.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("loop", filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Resolve("loop"); err == nil {
		t.Fatal("Resolve accepted a symlink loop")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := w.LockPath(ctx, "new-file"); !errors.Is(err, context.Canceled) {
		t.Fatalf("LockPath error = %v, want context.Canceled", err)
	}
	if len(workspaces.path) != 0 {
		t.Fatalf("canceled LockPath leaked %d path locks", len(workspaces.path))
	}
}

func assertBlocked(t *testing.T, acquired <-chan func(), message string) {
	t.Helper()
	select {
	case release := <-acquired:
		release()
		t.Fatal(message)
	case <-time.After(50 * time.Millisecond):
	}
}
