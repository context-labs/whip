package capability

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Workspaces coordinates same-path file mutations across every daemon
// workspace. Shell commands take no lock: they run concurrently with each
// other and with edits, and their authority is checked at admission instead.
type Workspaces struct {
	mu   sync.Mutex
	path map[string]*pathLock
}

// Workspace resolves relative paths from one root using shared daemon locks.
type Workspace struct {
	root  string
	owner *Workspaces
}

type pathLock struct {
	token chan struct{}
	refs  int
}

func NewWorkspaces() *Workspaces {
	return &Workspaces{path: make(map[string]*pathLock)}
}

// Open requires an existing directory and resolves its symlinks once.
func (w *Workspaces) Open(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("canonicalize workspace root: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return nil, fmt.Errorf("stat workspace root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root is not a directory: %s", canonical)
	}
	return &Workspace{root: filepath.Clean(canonical), owner: w}, nil
}

func (w *Workspace) Root() string { return w.root }

// Resolve canonicalizes a path and confines it to the workspace root.
func (w *Workspace) Resolve(path string) (string, error) {
	canonical, err := w.Canonicalize(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(w.root, canonical)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(w.root, path)
		}
		return "", fmt.Errorf("path %q is outside workspace %q", path, w.root)
	}
	return canonical, nil
}

// Canonicalize resolves relative paths from the workspace root and canonicalizes
// every existing ancestor while allowing missing leaves. It does not authorize
// access: callers must check the resulting path against the effective grant scope.
func (w *Workspace) Canonicalize(path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(w.root, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	current := filepath.Clean(abs)
	var missing []string
	for {
		canonical, evalErr := filepath.EvalSymlinks(current)
		if evalErr == nil {
			for _, m := range slices.Backward(missing) {
				canonical = filepath.Join(canonical, m)
			}
			return filepath.Clean(canonical), nil
		}
		if !errors.Is(evalErr, os.ErrNotExist) {
			return "", fmt.Errorf("canonicalize workspace path: %w", evalErr)
		}
		info, statErr := os.Lstat(current)
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("inspect workspace path: %w", statErr)
		}
		// A dangling symlink is an existing component with an unresolved target,
		// not a missing leaf that can safely be appended to its parent.
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("canonicalize workspace path: unresolved symlink %q: %w", current, evalErr)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("canonicalize workspace path: %w", evalErr)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// LockPath confines the path to the workspace and serializes its mutations.
func (w *Workspace) LockPath(ctx context.Context, path string) (string, func(), error) {
	canonical, err := w.Resolve(path)
	if err != nil {
		return "", nil, err
	}
	return w.lockCanonicalPath(ctx, canonical)
}

// LockCanonicalPath canonicalizes a path and serializes mutations to it across
// daemon workspaces. Locking does not authorize access; callers must validate the
// returned canonical path against the operation's admission before mutating it.
func (w *Workspace) LockCanonicalPath(ctx context.Context, path string) (string, func(), error) {
	canonical, err := w.Canonicalize(path)
	if err != nil {
		return "", nil, err
	}
	return w.lockCanonicalPath(ctx, canonical)
}

func (w *Workspace) lockCanonicalPath(ctx context.Context, canonical string) (string, func(), error) {
	w.owner.mu.Lock()
	lock := w.owner.path[canonical]
	if lock == nil {
		lock = &pathLock{token: make(chan struct{}, 1)}
		w.owner.path[canonical] = lock
	}
	lock.refs++
	w.owner.mu.Unlock()

	select {
	case lock.token <- struct{}{}:
	case <-ctx.Done():
		w.releasePath(canonical, lock)
		return "", nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-lock.token
		w.releasePath(canonical, lock)
		return "", nil, err
	}

	release := sync.OnceFunc(func() {
		<-lock.token
		w.releasePath(canonical, lock)
	})
	// A queued mutation may have waited while a target or ancestor became a
	// symlink. The lock and admission must still refer to the same real path.
	current, err := w.Canonicalize(canonical)
	if err != nil || current != canonical {
		release()
		return "", nil, errors.Join(ErrStaleAdmission, err)
	}
	return canonical, release, nil
}

func (w *Workspace) releasePath(path string, lock *pathLock) {
	w.owner.mu.Lock()
	lock.refs--
	if lock.refs == 0 && w.owner.path[path] == lock {
		delete(w.owner.path, path)
	}
	w.owner.mu.Unlock()
}
