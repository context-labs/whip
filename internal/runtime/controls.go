package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

// SetWorkingDirectory is an explicit human session control. It does not grant
// filesystem authority, move files, reset the REPL, or change historical turns.
func (r *Runtime) SetWorkingDirectory(ctx context.Context, request session.WorkspaceSetRequest) (session.ControlEdit, error) {
	done, err := r.beginWorkspace()
	if err != nil {
		return session.ControlEdit{}, err
	}
	defer done()
	if prior, err := r.store.WorkspaceSetRetry(ctx, request); err == nil {
		return prior, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return session.ControlEdit{}, err
	}
	ctx, cancel := r.workspace.Context(ctx)
	defer cancel()
	current, err := r.store.Session(ctx, request.SessionID)
	if err != nil {
		return session.ControlEdit{}, err
	}
	if current.ConfigRevision != request.ExpectedRevision {
		return session.ControlEdit{}, store.ErrConflict
	}
	path := request.Path
	if path == "" {
		path = current.WorkingDirectory
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(current.WorkingDirectory, path)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return session.ControlEdit{}, fmt.Errorf("%w: resolve working directory: %w", session.ErrInvalid, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return session.ControlEdit{}, fmt.Errorf("%w: inspect working directory: %w", session.ErrInvalid, err)
	}
	if !info.IsDir() {
		return session.ControlEdit{}, session.ErrInvalid
	}
	root, err := r.store.Root(ctx, current.TreeID)
	if err != nil {
		return session.ControlEdit{}, err
	}
	entry, err := r.controlMCPRoot(root)
	if err != nil {
		return session.ControlEdit{}, err
	}
	// Never wait for an unrelated connection effect to finish while holding the
	// scheduler gate. The existing per-root action slot serializes MCP mutations.
	releaseMCP, err := takeMCPSlot(ctx, entry.actions)
	if err != nil {
		return session.ControlEdit{}, err
	}
	defer releaseMCP()
	gate, drop := r.controlGate(current.TreeID)
	defer drop()
	if !gate.mu.TryLock() {
		return session.ControlEdit{}, store.ErrBusy
	}
	defer gate.mu.Unlock()
	owners, err := r.store.ControlOwners(ctx, current.TreeID)
	if err != nil {
		return session.ControlEdit{}, err
	}
	names := make([]string, len(owners))
	for i, id := range owners {
		names[i] = string(id)
	}
	release, err := r.shells.PauseIdle(names)
	if err != nil {
		return session.ControlEdit{}, store.ErrBusy
	}
	defer release()
	result, err := r.store.SetWorkingDirectory(ctx, request, path, owners)
	if err != nil {
		return result, err
	}
	// SQL has committed. Join old captured-cwd caches even if this observer leaves;
	// Close waits for beginWorkspace while the database remains available.
	if path != current.WorkingDirectory {
		r.shells.Retire(string(current.ID))
		r.languageServers.Retire(string(current.ID))
		if current.ParentID == nil {
			r.retireControlMCP(entry)
		}
	}
	r.Wake()
	return result, nil
}

func (r *Runtime) ConfigureRun(ctx context.Context, request session.RunConfigureRequest) (session.ControlEdit, error) {
	done, err := r.beginWorkspace()
	if err != nil {
		return session.ControlEdit{}, err
	}
	defer done()
	return r.store.ConfigureRun(ctx, request)
}

// controlMCPRoot needs no filesystem read: changing away from a deleted old
// directory must remain possible. It only reserves an in-memory action slot.
func (r *Runtime) controlMCPRoot(root session.Session) (*mcpRoot, error) {
	r.mcp.mu.Lock()
	defer r.mcp.mu.Unlock()
	if r.mcp.closed {
		return nil, ErrClosed
	}
	if entry := r.mcp.roots[root.TreeID]; entry != nil {
		if entry.retired {
			return nil, store.ErrBusy
		}
		return entry, nil
	}
	if len(r.mcp.roots) >= mcp.MaxRootManagers {
		return nil, store.ErrLimit
	}
	entry := &mcpRoot{tree: root.TreeID, root: root.ID, cwd: root.WorkingDirectory, actions: make(chan struct{}, 1), calls: make(chan struct{}, 16)}
	r.mcp.roots[root.TreeID] = entry
	return entry, nil
}

func (r *Runtime) retireControlMCP(entry *mcpRoot) {
	r.mcp.mu.Lock()
	entry.retired = true
	manager := entry.manager
	r.mcp.mu.Unlock()
	if manager != nil {
		manager.Close()
	}
	r.mcp.mu.Lock()
	if r.mcp.roots[entry.tree] == entry {
		delete(r.mcp.roots, entry.tree)
	}
	r.mcp.mu.Unlock()
}

// Only one tree is paused. The scheduler never waits on external teardown and
// continues claiming unrelated trees; empty gates are removed after their users.
type controlGate struct {
	mu    sync.RWMutex
	users int
}

func (r *Runtime) controlGate(tree session.TreeID) (*controlGate, func()) {
	r.controlMu.Lock()
	if r.controlGates == nil {
		r.controlGates = make(map[session.TreeID]*controlGate)
	}
	gate := r.controlGates[tree]
	if gate == nil {
		gate = &controlGate{}
		r.controlGates[tree] = gate
	}
	gate.users++
	r.controlMu.Unlock()
	return gate, func() {
		r.controlMu.Lock()
		defer r.controlMu.Unlock()
		gate.users--
		if gate.users == 0 {
			delete(r.controlGates, tree)
		}
	}
}

func (r *Runtime) claimControlled(ctx context.Context, id session.SessionID) (store.Claim, error) {
	current, err := r.store.Session(ctx, id)
	if err != nil {
		return store.Claim{}, err
	}
	gate, drop := r.controlGate(current.TreeID)
	defer drop()
	if !gate.mu.TryRLock() {
		return store.Claim{}, store.ErrBusy
	}
	defer gate.mu.RUnlock()
	return r.store.Claim(ctx, id)
}
