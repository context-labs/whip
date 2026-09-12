package capability

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceLockRevalidatesAfterWaiting(t *testing.T) {
	for _, confined := range []bool{false, true} {
		name := "canonical"
		if confined {
			name = "confined"
		}
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			inside := filepath.Join(base, "inside")
			outside := filepath.Join(base, "outside")
			if err := os.Mkdir(inside, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			workspaces := NewWorkspaces()
			workspace, err := workspaces.Open(inside)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(workspace.Root(), "target")
			lockPath := workspace.LockCanonicalPath
			if confined {
				lockPath = workspace.LockPath
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, release, err := lockPath(ctx, target)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			done := make(chan error, 1)
			go func() {
				_, unlock, lockErr := lockPath(ctx, target)
				if unlock != nil {
					unlock()
				}
				done <- lockErr
			}()
			for {
				workspaces.mu.Lock()
				waiting := workspaces.path[target].refs == 2
				workspaces.mu.Unlock()
				if waiting {
					break
				}
				if ctx.Err() != nil {
					t.Fatal("second mutation did not wait for the path lock")
				}
				time.Sleep(time.Millisecond)
			}
			if err := os.Symlink(outside, target); err != nil {
				t.Fatal(err)
			}
			release()
			if err := <-done; !errors.Is(err, ErrStaleAdmission) {
				t.Fatalf("retargeted mutation error = %v, want stale admission", err)
			}
			workspaces.mu.Lock()
			remaining := len(workspaces.path)
			workspaces.mu.Unlock()
			if remaining != 0 {
				t.Fatalf("stale mutation leaked %d path locks", remaining)
			}
		})
	}
}

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

func TestWorkspaceCanonicalizeOutsideRoot(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	outside := filepath.Join(parent, "sibling")
	for _, dir := range []string{root, outside} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	w, err := NewWorkspaces().Open(root)
	if err != nil {
		t.Fatal(err)
	}
	canonicalOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{name: "absolute directory", path: outside, want: canonicalOutside},
		{
			name: "relative sibling with missing leaves",
			path: filepath.Join("..", "sibling", "missing", "file.txt"),
			want: filepath.Join(canonicalOutside, "missing", "file.txt"),
		},
		{
			name: "symlink with missing leaves",
			path: filepath.Join("alias", "missing", "file.txt"),
			want: filepath.Join(canonicalOutside, "missing", "file.txt"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := w.Canonicalize(test.path)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("Canonicalize(%q) = %q, want %q", test.path, got, test.want)
			}
			if _, err := w.Resolve(test.path); err == nil {
				t.Fatal("Resolve accepted an outside path")
			}
			if _, release, err := w.LockPath(t.Context(), test.path); err == nil {
				release()
				t.Fatal("LockPath accepted an outside path")
			}
		})
	}
}

func TestWorkspaceRejectsDanglingSymlink(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(parent, "missing"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	w, err := NewWorkspaces().Open(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"alias", filepath.Join("alias", "new-file")} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			if _, err := w.Canonicalize(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Canonicalize(%q) error = %v, want unresolved symlink error", path, err)
			}
			if _, err := w.Resolve(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Resolve(%q) error = %v, want unresolved symlink error", path, err)
			}
		})
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

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := w.LockPath(ctx, filepath.Join(root, "alias", "same")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended LockPath error = %v, want deadline exceeded", err)
	}

	unlock()
	releaseAlias := <-alias
	releaseAlias()
}

func TestWorkspaceCanonicalLocksSharedAcrossProjects(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	firstRoot := filepath.Join(parent, "first")
	secondRoot := filepath.Join(parent, "second")
	shared := filepath.Join(parent, "shared")
	for _, dir := range []string{firstRoot, secondRoot, shared} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(shared, filepath.Join(secondRoot, "alias")); err != nil {
		t.Fatal(err)
	}
	workspaces := NewWorkspaces()
	first, err := workspaces.Open(firstRoot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := workspaces.Open(secondRoot)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "shared", "missing", "file.txt")
	alias := filepath.Join("alias", "missing", "file.txt")
	canonical, unlock, err := first.LockCanonicalPath(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unlock)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, release, err := second.LockCanonicalPath(ctx, alias); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("same canonical path lock error = %v, want deadline exceeded", err)
	}
	_, releaseOther, err := second.LockCanonicalPath(t.Context(), filepath.Join("alias", "other"))
	if err != nil {
		t.Fatal(err)
	}
	releaseOther()

	unlock()
	got, release, err := second.LockCanonicalPath(t.Context(), alias)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if got != canonical {
		t.Fatalf("second workspace locked %q, want %q", got, canonical)
	}
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
	if _, err := w.Canonicalize("loop"); err == nil {
		t.Fatal("Canonicalize accepted a symlink loop")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := w.LockPath(ctx, "new-file"); !errors.Is(err, context.Canceled) {
		t.Fatalf("LockPath error = %v, want context.Canceled", err)
	}
	if len(workspaces.path) != 0 {
		t.Fatalf("canceled LockPath leaked %d path locks", len(workspaces.path))
	}
	if _, _, err := w.LockCanonicalPath(ctx, "new-file"); !errors.Is(err, context.Canceled) {
		t.Fatalf("LockCanonicalPath error = %v, want context.Canceled", err)
	}
	if len(workspaces.path) != 0 {
		t.Fatalf("canceled LockCanonicalPath leaked %d path locks", len(workspaces.path))
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
