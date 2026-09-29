package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sync"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type mcpRoot struct {
	tree    session.TreeID
	retired bool
	root    session.SessionID
	cwd     string
	actions chan struct{}
	calls   chan struct{}
	manager *mcp.Manager // guarded by mcpOwners.mu
}

// mcpOwners owns transport lifetimes independently of observing callers and
// turns. Stopping one child does not retire its siblings' borrowed connections.
type mcpOwners struct {
	humans    chan struct{}
	workers   sync.WaitGroup
	mu        sync.Mutex
	closed    bool
	roots     map[session.TreeID]*mcpRoot
	budget    *mcp.Budget
	processes *capability.ProcessManager
	ctx       context.Context
	cancel    context.CancelFunc
	calls     chan struct{}
	children  map[session.SessionID]*mcpChildCalls
}
type mcpChildCalls struct {
	slots chan struct{}
	users int
}

func newMCPOwners() *mcpOwners {
	ctx, cancel := context.WithCancel(context.Background())
	return &mcpOwners{humans: make(chan struct{}, 16), roots: map[session.TreeID]*mcpRoot{}, budget: mcp.NewBudget(), processes: capability.NewProcessManager(), ctx: ctx, cancel: cancel, calls: make(chan struct{}, 64), children: map[session.SessionID]*mcpChildCalls{}}
}

func (o *mcpOwners) close() error {
	o.mu.Lock()
	o.closed = true
	o.cancel()
	managers := make([]*mcp.Manager, 0, len(o.roots))
	for _, root := range o.roots {
		if root.manager != nil {
			managers = append(managers, root.manager)
		}
	}
	o.mu.Unlock()
	for _, manager := range managers {
		manager.Close()
	}
	o.workers.Wait()
	return o.processes.Close()
}

func (r *Runtime) mcpRoot(ctx context.Context, current session.Session, create bool) (*mcpRoot, error) {
	root, err := r.store.Root(ctx, current.TreeID)
	if err != nil {
		return nil, err
	}
	cwd, err := filepath.EvalSymlinks(root.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	r.mcp.mu.Lock()
	defer r.mcp.mu.Unlock()
	if r.mcp.closed {
		return nil, ErrClosed
	}
	if entry := r.mcp.roots[root.TreeID]; entry != nil {
		if entry.root != root.ID || entry.cwd != cwd {
			return nil, store.ErrConflict
		}
		return entry, nil
	}
	if !create {
		return nil, nil //nolint:nilnil // Metadata may observe a root with no live resources.
	}
	if len(r.mcp.roots) >= mcp.MaxRootManagers {
		return nil, store.ErrLimit
	}
	entry := &mcpRoot{tree: root.TreeID, root: root.ID, cwd: cwd, actions: make(chan struct{}, 1), calls: make(chan struct{}, 16)}
	r.mcp.roots[root.TreeID] = entry
	return entry, nil
}

func (r *Runtime) mcpManager(entry *mcpRoot) *mcp.Manager {
	if entry == nil {
		return nil
	}
	r.mcp.mu.Lock()
	defer r.mcp.mu.Unlock()
	if entry.retired {
		return nil
	}
	return entry.manager
}

func takeMCPSlot(ctx context.Context, slot chan struct{}) (func(), error) {
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *Runtime) mcpCallSlots(ctx context.Context, current session.Session, root *mcpRoot) (func(), error) {
	hostDone, err := takeMCPSlot(ctx, r.mcp.calls)
	if err != nil {
		return nil, err
	}
	rootDone, err := takeMCPSlot(ctx, root.calls)
	if err != nil {
		hostDone()
		return nil, err
	}
	if current.ParentID == nil {
		return func() { rootDone(); hostDone() }, nil
	}
	r.mcp.mu.Lock()
	bucket := r.mcp.children[current.ID]
	if bucket == nil {
		bucket = &mcpChildCalls{slots: make(chan struct{}, 4)}
		r.mcp.children[current.ID] = bucket
	}
	bucket.users++
	r.mcp.mu.Unlock()
	remove := func() {
		r.mcp.mu.Lock()
		bucket.users--
		if bucket.users == 0 {
			delete(r.mcp.children, current.ID)
		}
		r.mcp.mu.Unlock()
		rootDone()
		hostDone()
	}
	childDone, err := takeMCPSlot(ctx, bucket.slots)
	if err != nil {
		remove()
		return nil, err
	}
	return func() { childDone(); remove() }, nil
}

type mcpDiscovery struct {
	config   mcp.Filtered
	identity string
	trusted  bool
	cwd      string
}

func (r *Runtime) discoverMCP(ctx context.Context, current session.Session) (mcpDiscovery, error) {
	if current.ParentID != nil {
		return mcpDiscovery{}, store.ErrConflict
	}
	cwd, err := filepath.EvalSymlinks(current.WorkingDirectory)
	if err != nil {
		return mcpDiscovery{}, err
	}
	snapshot, err := r.configuration.Snapshot(ctx)
	if err != nil {
		return mcpDiscovery{}, err
	}
	native := mcp.NativeConfigs(snapshot.Host.MCP.Servers, filepath.Join(r.directory, config.FileName))
	filtered := mcp.Select(mcp.LoadMergedFiltered(cwd, native, mcp.ImportPolicyFrom(&snapshot.Host.MCP.Imports)), current.Config.MCPServers.Allowed())
	if len(filtered.Merged) > mcp.MaxServers || len(filtered.Blocked) > 256 {
		return mcpDiscovery{}, store.ErrLimit
	}
	trusted := true
	trust := map[string]bool{}
	for name, value := range filtered.Merged {
		trust[name] = value.Trusted
		if !value.Disabled() && !value.Trusted {
			trusted = false
		}
	}
	identity, err := json.Marshal(struct {
		Root    session.SessionID
		Cwd     string
		Configs map[string]mcp.ServerConfig
		Trust   map[string]bool
	}{current.ID, cwd, filtered.Merged, trust})
	if err != nil {
		return mcpDiscovery{}, err
	}
	if err := ctx.Err(); err != nil {
		return mcpDiscovery{}, err
	}
	return mcpDiscovery{config: filtered, identity: fmt.Sprintf("%x", sha256.Sum256(identity)), trusted: trusted, cwd: cwd}, nil
}

// applyMCPDiscovery requires the root action slot. New managers contain no
// entries until the caller has authorized this exact discovered connection set.
func (r *Runtime) applyMCPDiscovery(ctx context.Context, entry *mcpRoot, discovery mcpDiscovery) (mcp.RefreshResult, error) {
	if discovery.cwd != entry.cwd {
		return mcp.RefreshResult{}, store.ErrConflict
	}
	r.mcp.mu.Lock()
	if r.mcp.closed || entry.retired || r.mcp.roots[entry.tree] != entry {
		r.mcp.mu.Unlock()
		return mcp.RefreshResult{}, ErrClosed
	}
	manager := entry.manager
	if manager == nil {
		var err error
		manager, err = mcp.NewBoundedManager(nil, r.mcp.budget)
		if err != nil {
			r.mcp.mu.Unlock()
			return mcp.RefreshResult{}, err
		}
		manager.SetProcessOptions(r.mcp.processes, string(entry.root), entry.cwd, nil)
		manager.Start(r.mcp.ctx)
		entry.manager = manager
	}
	r.mcp.mu.Unlock()
	if err := manager.SetBlocked(discovery.config.Blocked); err != nil {
		return mcp.RefreshResult{}, err
	}
	if err := manager.SetSourceErrors(discovery.config.Errs); err != nil {
		return mcp.RefreshResult{}, err
	}
	result, err := manager.AddServers(ctx, discovery.config.Merged)
	result.Blocked = mcp.ServerStatuses(manager.Blocked())
	result.SourceErrors = mcp.ServerStatuses(manager.SourceErrors())
	return result, err
}

func (r *Runtime) cleanupDeletedMCP(ctx context.Context) error {
	r.mcp.mu.Lock()
	roots := maps.Clone(r.mcp.roots)
	r.mcp.mu.Unlock()
	for tree, entry := range roots {
		if _, err := r.store.Session(ctx, entry.root); err == nil {
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		r.mcp.mu.Lock()
		if r.mcp.roots[tree] == entry {
			entry.retired = true
			delete(r.mcp.roots, tree)
		}
		manager := entry.manager
		r.mcp.mu.Unlock()
		if manager != nil {
			manager.Close()
		}
	}
	return nil
}

func sortedMCPNames(configs map[string]mcp.ServerConfig) []string {
	names := slices.Collect(maps.Keys(configs))
	slices.Sort(names)
	return names
}
