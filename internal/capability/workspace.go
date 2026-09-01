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

	"golang.org/x/sync/semaphore"
)

const workspaceMutationSlots = 1 << 30

// Workspaces coordinates mutations across every daemon workspace.
type Workspaces struct {
	gate *semaphore.Weighted
	mu   sync.Mutex
	path map[string]*pathLock
}

// Workspace canonicalizes paths beneath one root using shared daemon locks.
type Workspace struct {
	root  string
	owner *Workspaces
}

type pathLock struct {
	token chan struct{}
	refs  int
}

func NewWorkspaces() *Workspaces {
	return &Workspaces{gate: semaphore.NewWeighted(workspaceMutationSlots), path: make(map[string]*pathLock)}
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

// Resolve canonicalizes every existing ancestor while allowing missing leaves.
func (w *Workspace) Resolve(path string) (string, error) {
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
			canonical = filepath.Clean(canonical)
			rel, relErr := filepath.Rel(w.root, canonical)
			if relErr != nil {
				return "", relErr
			}
			if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("path %q is outside workspace %q", path, w.root)
			}
			return canonical, nil
		}
		if !errors.Is(evalErr, os.ErrNotExist) {
			return "", fmt.Errorf("canonicalize workspace path: %w", evalErr)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("canonicalize workspace path: %w", evalErr)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// LockPath admits a path mutation alongside mutations to other canonical paths.
func (w *Workspace) LockPath(ctx context.Context, path string) (string, func(), error) {
	canonical, err := w.Resolve(path)
	if err != nil {
		return "", nil, err
	}

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
	if err := w.owner.gate.Acquire(ctx, 1); err != nil {
		<-lock.token
		w.releasePath(canonical, lock)
		return "", nil, err
	}

	release := sync.OnceFunc(func() {
		w.owner.gate.Release(1)
		<-lock.token
		w.releasePath(canonical, lock)
	})
	return canonical, release, nil
}

// LockAll excludes every path mutation for shell or otherwise unknown effects.
func (w *Workspace) LockAll(ctx context.Context) (func(), error) {
	if err := w.owner.gate.Acquire(ctx, workspaceMutationSlots); err != nil {
		return nil, err
	}
	return sync.OnceFunc(func() { w.owner.gate.Release(workspaceMutationSlots) }), nil
}

func (w *Workspace) releasePath(path string, lock *pathLock) {
	w.owner.mu.Lock()
	lock.refs--
	if lock.refs == 0 && w.owner.path[path] == lock {
		delete(w.owner.path, path)
	}
	w.owner.mu.Unlock()
}
