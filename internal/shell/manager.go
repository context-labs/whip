// Package shell owns bounded live command resources. SQL authority and durable
// operation evidence remain with runtime/store; none of these handles survives
// process restart or grants access to a human workspace terminal.
package shell

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/context-labs/whip/internal/bashrun"
	"github.com/context-labs/whip/internal/capability"
)

const (
	MaxOwners       = 128
	MaxRunning      = 64
	MaxOwnerRunning = 8
	MaxJobs         = 128
	MaxOwnerJobs    = 32
)

var (
	ErrBusy     = errors.New("shell owner is busy or changing workspace")
	ErrClosed   = errors.New("shell resource generation is closed")
	ErrLimit    = errors.New("shell resource limit reached")
	ErrNotFound = errors.New("shell job not found in this owner generation")
)

// Manager is a runtime-owned collection of independent session lifetimes.
// Capture returns a stable generation; retiring it cannot authorize a stale
// prepared operation to start against a later generation for the same owner.
type Manager struct {
	mu            sync.Mutex
	owners        map[string]*Scope
	paused        map[string]int
	running, jobs int
	closed        bool
}

type Scope struct {
	manager              *Manager
	owner                string
	ctx                  context.Context
	cancel               context.CancelFunc
	processes            *capability.ProcessManager
	jobs                 map[string]*bashrun.Job
	starting             map[string]struct{}
	interaction          *Interaction
	running, pendingJobs int
	closed               bool
	workers              sync.WaitGroup
}

func NewManager() *Manager {
	return &Manager{owners: make(map[string]*Scope), paused: make(map[string]int)}
}

func (m *Manager) Capture(owner string) (*Scope, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || owner == "" {
		return nil, ErrClosed
	}
	if m.paused[owner] != 0 {
		return nil, ErrBusy
	}
	if scope := m.owners[owner]; scope != nil {
		return scope, nil
	}
	if len(m.owners) >= MaxOwners {
		return nil, ErrLimit
	}
	ctx, cancel := context.WithCancel(context.Background())
	scope := &Scope{manager: m, owner: owner, ctx: ctx, cancel: cancel, processes: capability.NewProcessManager(), jobs: make(map[string]*bashrun.Job), starting: make(map[string]struct{})}
	m.owners[owner] = scope
	return scope, nil
}

func (s *Scope) Context() context.Context { return s.ctx }

// Reservation is acquired after consent and before SQL dispatch. It prevents
// concurrent starts from oversubscribing either processes or retained output.
type Reservation struct {
	scope       *Scope
	background  bool
	used        bool          // guarded by the manager mutex
	transferred bool          // guarded by the manager mutex
	released    bool          // guarded by the manager mutex
	finished    chan struct{} // closes when the single Run/Start invocation returns
}

func (s *Scope) Reserve(background bool) (*Reservation, error) {
	m := s.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.paused[s.owner] != 0 {
		return nil, ErrBusy
	}
	if s.closed {
		return nil, ErrClosed
	}
	if s.running >= MaxOwnerRunning || m.running >= MaxRunning {
		return nil, ErrLimit
	}
	if background {
		// Retain the newest terminal records. Running jobs are never evicted.
		for len(s.jobs)+s.pendingJobs >= MaxOwnerJobs || m.jobs >= MaxJobs {
			var only *Scope
			if len(s.jobs)+s.pendingJobs >= MaxOwnerJobs {
				only = s
			}
			if !m.evictJobLocked(only) {
				return nil, ErrLimit
			}
		}
		s.pendingJobs++
		m.jobs++
	}
	s.running++
	m.running++
	s.workers.Add(1)
	return &Reservation{scope: s, background: background, finished: make(chan struct{})}, nil
}

// Release abandons an undispatched reservation or joins a foreground call.
// A successful Start transfers release to the background completion observer.
func (r *Reservation) Release() {
	m := r.scope.manager
	m.mu.Lock()
	if r.used {
		m.mu.Unlock()
		<-r.finished
		m.mu.Lock()
	}
	if !r.transferred {
		r.releaseLocked()
	}
	m.mu.Unlock()
}

// Evict globally only when global retention is full. Local pressure cannot
// discard another owner's evidence to bypass the requesting owner's own cap.
func (m *Manager) evictJobLocked(only *Scope) bool {
	var chosen *Scope
	var oldestID string
	var oldest *bashrun.Job
	for _, scope := range m.owners {
		if only != nil && scope != only {
			continue
		}
		for id, job := range scope.jobs {
			if job.Running() {
				continue
			}
			if oldest == nil || job.Started.Before(oldest.Started) || job.Started.Equal(oldest.Started) && scope.owner+"/"+id < chosen.owner+"/"+oldestID {
				chosen, oldestID, oldest = scope, id, job
			}
		}
	}
	if chosen == nil {
		return false
	}
	delete(chosen.jobs, oldestID)
	m.jobs--
	return true
}

func (r *Reservation) releaseLocked() {
	if r.released {
		return
	}
	r.released = true
	s := r.scope
	s.running--
	s.manager.running--
	if r.background && !r.transferred {
		s.pendingJobs--
		s.manager.jobs--
	}
	s.workers.Done()
}

func (r *Reservation) options(options bashrun.Options) bashrun.Options {
	options.RootID, options.Processes = r.scope.owner, r.scope.processes
	return options
}

func (r *Reservation) Run(ctx context.Context, options bashrun.Options) (bashrun.Result, error) {
	m := r.scope.manager
	m.mu.Lock()
	if r.scope.closed || r.released || r.used || r.background {
		m.mu.Unlock()
		return bashrun.Result{}, ErrClosed
	}
	r.used = true
	m.mu.Unlock()
	defer close(r.finished)
	return bashrun.Run(ctx, r.options(options)), nil
}

// Start publishes only after the process starts. The caller's context bounds
// admission; the process belongs to its session generation until exit/kill.
func (r *Reservation) Start(ctx context.Context, id string, options bashrun.Options) (*bashrun.Job, error) {
	s, m := r.scope, r.scope.manager
	m.mu.Lock()
	_, starting := s.starting[id]
	if s.closed || r.released || r.used || !r.background || id == "" || s.jobs[id] != nil || starting {
		m.mu.Unlock()
		return nil, ErrClosed
	}
	r.used = true
	s.starting[id] = struct{}{}
	m.mu.Unlock()
	defer close(r.finished)
	job, err := bashrun.Start(ctx, r.options(options))
	m.mu.Lock()
	delete(s.starting, id)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if s.closed {
		m.mu.Unlock()
		_ = job.Kill()
		return nil, ErrClosed
	}
	s.jobs[id] = job
	s.pendingJobs--
	r.transferred = true
	m.mu.Unlock()
	go func() {
		<-job.Done()
		m.mu.Lock()
		r.releaseLocked()
		m.mu.Unlock()
	}()
	return job, nil
}

func (s *Scope) Job(id string) (*bashrun.Job, error) {
	m := s.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	job := s.jobs[id]
	if job == nil {
		return nil, ErrNotFound
	}
	return job, nil
}

func (s *Scope) JobIDs() ([]string, error) {
	m := s.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	ids := make([]string, 0, len(s.jobs))
	for id := range s.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// Owners returns a bounded inventory for post-deletion cleanup, without
// consulting SQL or retaining session/configuration projections.
func (m *Manager) Owners() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.owners))
	for id := range m.owners {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (s *Scope) retireLocked() {
	s.closed = true
	s.cancel()
}

func (s *Scope) join() {
	_ = s.processes.Close()
	s.workers.Wait()
}

func (m *Manager) Retire(owner string) {
	m.mu.Lock()
	s := m.owners[owner]
	if s != nil && !s.closed {
		s.retireLocked()
	}
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.join()
	m.mu.Lock()
	if m.owners[owner] == s {
		m.jobs -= len(s.jobs)
		delete(m.owners, owner)
	}
	m.mu.Unlock()
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	scopes := make([]*Scope, 0, len(m.owners))
	for _, scope := range m.owners {
		scope.retireLocked()
		scopes = append(scopes, scope)
	}
	m.mu.Unlock()
	for _, scope := range scopes {
		scope.join()
	}
	m.mu.Lock()
	m.owners = make(map[string]*Scope)
	m.jobs = 0
	m.mu.Unlock()
}

// PauseIdle prevents new reservations for an exact owner set while a human
// workspace control commits. It never stops a running job to make the check pass.
func (m *Manager) PauseIdle(owners []string) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	for _, owner := range owners {
		if m.paused[owner] != 0 {
			return nil, ErrBusy
		}
		if scope := m.owners[owner]; scope != nil && scope.running != 0 {
			return nil, ErrBusy
		}
	}
	for _, owner := range owners {
		m.paused[owner]++
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			for _, owner := range owners {
				delete(m.paused, owner)
			}
		})
	}, nil
}
