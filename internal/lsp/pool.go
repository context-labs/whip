package lsp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/context-labs/whip/internal/capability"
)

const maxManagers = 128

type managed struct {
	manager *Manager
	cwd     string
	config  [32]byte
}

// Pool bounds all language-server processes and retains clients only for callers
// with standing workspace authority. Configuration is supplied by the host's
// existing authority for each dispatch; this pool does not own configuration.
type Pool struct {
	mu         sync.Mutex
	processes  *capability.ProcessManager
	slots      chan struct{}
	retained   map[string]*managed
	active     map[*Manager]struct{}
	closed     bool
	generation uint64
	calls      sync.WaitGroup
	done       chan struct{}
}

func NewPool(processes *capability.ProcessManager) *Pool {
	return &Pool{processes: processes, slots: make(chan struct{}, 16), retained: map[string]*managed{}, active: map[*Manager]struct{}{}, done: make(chan struct{})}
}

// Generation captures the lifetime in which an operation may create clients.
// Revocation or stop invalidates it before joining existing clients.
func (p *Pool) Generation() uint64 { p.mu.Lock(); defer p.mu.Unlock(); return p.generation }

// Diagnostics holds one manager reference until the call finishes. Nonretained
// calls use an isolated manager whose processes are joined before returning.
func (p *Pool) Diagnostics(ctx context.Context, owner, cwd, path, text string, config map[string]Config, retain bool, generation uint64, identity os.FileInfo) (Result, error) {
	if p.processes == nil {
		return Result{}, errors.New("contained language server processes are unavailable")
	}
	if owner == "" || !filepath.IsAbs(cwd) || identity == nil {
		return Result{}, errors.New("language server owner and absolute workspace are required")
	}
	if err := ValidateConfig(config); err != nil {
		return Result{}, err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return Result{}, err
	}
	digest := sha256.Sum256(encoded)
	p.mu.Lock()
	if p.closed || p.generation != generation {
		p.mu.Unlock()
		return Result{}, errors.New("language server pool closed or authority changed")
	}
	entry := p.retained[owner]
	var retired *Manager
	if entry != nil && (entry.cwd != cwd || entry.config != digest) {
		delete(p.retained, owner)
		retired, entry = entry.manager, nil
	}
	if !retain {
		entry = nil
	}
	if entry == nil {
		if len(p.active) >= maxManagers {
			p.mu.Unlock()
			if retired != nil {
				p.closeManager(retired)
			}
			return Result{State: "unavailable", Reason: "language server manager capacity reached"}, nil
		}
		manager := NewManager(FromConfigMap(config))
		manager.slots = p.slots
		manager.SetProcessOptions(p.processes, owner, cwd, nil)
		manager.workspaceInfo = identity
		entry = &managed{manager: manager, cwd: cwd, config: digest}
		p.active[manager] = struct{}{}
		if retain {
			p.retained[owner] = entry
		}
	}
	p.calls.Add(1)
	p.mu.Unlock()
	defer p.calls.Done()
	if retired != nil {
		p.closeManager(retired)
	}
	if !retain {
		defer p.closeManager(entry.manager)
	}
	return entry.manager.Diagnostics(ctx, path, text)
}

func (p *Pool) closeManager(manager *Manager) {
	manager.Close()
	p.mu.Lock()
	delete(p.active, manager)
	p.mu.Unlock()
}

// RetireAll synchronously closes clients when authority changes or execution is
// stopped/deleted. Closing every bounded manager also covers delegated grants.
func (p *Pool) RetireAll() {
	p.mu.Lock()
	p.generation++
	managers := make([]*Manager, 0, len(p.active))
	for manager := range p.active {
		managers = append(managers, manager)
	}
	clear(p.retained)
	p.mu.Unlock()
	var joined sync.WaitGroup
	for _, manager := range managers {
		joined.Go(func() { p.closeManager(manager) })
	}
	joined.Wait()
}

func (p *Pool) Statuses(owner string, config map[string]Config) []Status {
	p.mu.Lock()
	entry := p.retained[owner]
	p.mu.Unlock()
	if entry != nil {
		return entry.manager.Statuses()
	}
	manager := NewManager(FromConfigMap(config))
	defer manager.Close()
	return manager.Statuses()
}

func (p *Pool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		<-p.done
		return
	}
	p.closed = true
	p.mu.Unlock()
	p.RetireAll()
	p.calls.Wait()
	close(p.done)
}

// Retire joins a retained workspace client before the owner uses a new cwd.
// The runtime excludes new turn claims and checks the tree idle around this call.
func (p *Pool) Retire(owner string) {
	p.mu.Lock()
	entry := p.retained[owner]
	delete(p.retained, owner)
	p.mu.Unlock()
	if entry != nil {
		p.closeManager(entry.manager)
	}
}
