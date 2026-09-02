package workflow

import (
	"context"
	"sync"
	"time"
)

// RunStatus is the lifecycle of a managed run.
type RunStatus string

const (
	RunRunning  RunStatus = "running"
	RunComplete RunStatus = "complete"
	RunError    RunStatus = "error"
	RunStopped  RunStatus = "stopped"
)

// AgentSnapshot is the observable state of one agent() call.
type AgentSnapshot struct {
	Index  int
	Label  string
	Phase  string
	Model  string
	Status RunStatus // running | complete | error
	Error  string
}

// Snapshot is the observable state of one run (manager.ts WorkflowSnapshot).
type Snapshot struct {
	RunID    string
	Name     string
	Status   RunStatus
	Phase    string
	Phases   []string
	Agents   []AgentSnapshot
	Logs     []string
	Result   any    // set on complete
	Error    string // set on error/stopped
	Duration time.Duration
}

// ManagedRun is one run under the manager. Done is closed exactly once when
// the run settles — closing a channel broadcasts to every waiter at once
// (the same close-to-broadcast shape as BackgroundTask.Done).
type ManagedRun struct {
	ID         string
	Name       string
	Status     RunStatus
	ScriptPath string // where the script was persisted (edit + re-invoke target)
	Done       chan struct{}

	ctx    context.Context
	cancel context.CancelFunc

	mu   sync.Mutex
	snap Snapshot
}

// Manager owns workflow runs: starts them in the background, exposes live
// snapshots, persists each run (script + journal) for resume, and fires
// OnSettle so the caller (the agent wiring) can fan the result back into the
// conversation. Port of manager.ts WorkflowManager.
type Manager struct {
	runner Runner
	cwd    string

	mu   sync.Mutex
	runs map[string]*ManagedRun

	// OnSettle fires (from the run goroutine) when a run completes, errors,
	// or is stopped: the agent wiring steers the result into the parent.
	OnSettle func(run *ManagedRun)
}

// NewManager builds a Manager. runner is the subagent bridge; cwd is the
// script-visible working directory.
func NewManager(runner Runner, cwd string) *Manager {
	return &Manager{runner: runner, cwd: cwd, runs: map[string]*ManagedRun{}}
}

// Start launches a script in the background and returns its run (the tool
// returns immediately with the run id). resumeFromRunID replays the prior
// run's unchanged agent() prefix from its journal. Port of
// manager.ts startInBackground.
func (m *Manager) Start(script string, args any, resumeFromRunID string) (*ManagedRun, error) {
	meta, _, err := Parse(script) // fail fast: a broken script never starts a run
	if err != nil {
		return nil, err
	}
	runID := resumeFromRunID
	if runID == "" {
		runID = GenerateRunID()
	}
	scriptPath := PersistScript(meta.Name, runID, script)

	ctx, cancel := context.WithCancel(context.Background())
	run := &ManagedRun{
		ID: runID, Name: meta.Name, Status: RunRunning, ScriptPath: scriptPath,
		Done: make(chan struct{}), ctx: ctx, cancel: cancel,
		snap: Snapshot{RunID: runID, Name: meta.Name, Status: RunRunning},
	}
	m.mu.Lock()
	m.runs[runID] = run
	m.mu.Unlock()

	persisted := &PersistedRun{
		RunID: runID, Name: meta.Name, ScriptPath: scriptPath,
		Status: string(RunRunning), Args: args, StartedAt: time.Now().UnixMilli(),
	}
	SaveRun(persisted)

	var resume map[int]JournalEntry
	if resumeFromRunID != "" {
		resume = JournalMap(LoadRun(resumeFromRunID))
	}

	go m.execute(run, script, args, persisted, resume)
	return run, nil
}

func (m *Manager) execute(run *ManagedRun, script string, args any, persisted *PersistedRun, resume map[int]JournalEntry) {
	events := Events{
		OnPhase: func(title string) {
			run.mu.Lock()
			run.snap.Phase = title
			run.mu.Unlock()
		},
		OnLog: func(msg string) {
			run.mu.Lock()
			run.snap.Logs = append(run.snap.Logs, msg)
			run.mu.Unlock()
		},
		OnAgentStart: func(index int, label, phase, model string) {
			run.mu.Lock()
			run.snap.Agents = append(run.snap.Agents, AgentSnapshot{
				Index: index, Label: label, Phase: phase, Model: model, Status: RunRunning,
			})
			for _, p := range phasesOf(run.snap.Agents) {
				if !contains(run.snap.Phases, p) {
					run.snap.Phases = append(run.snap.Phases, p)
				}
			}
			run.mu.Unlock()
		},
		OnAgentEnd: func(index int, label, phase string, result any, tokens int, errStr string) {
			run.mu.Lock()
			for i := range run.snap.Agents {
				if run.snap.Agents[i].Index == index {
					if errStr != "" {
						run.snap.Agents[i].Status = RunError
						run.snap.Agents[i].Error = errStr
					} else {
						run.snap.Agents[i].Status = RunComplete
					}
					break
				}
			}
			run.mu.Unlock()
		},
		OnJournal: func(e JournalEntry) {
			persisted.Journal = append(persisted.Journal, e)
			SaveRun(persisted) // incremental: resume sees completed calls so far
		},
	}

	result, err := Run(run.ctx, script, Options{
		RunID: runID(run), Cwd: m.cwd, Args: args,
		Run: m.runner, Events: events, ResumeJournal: resume,
	})

	run.mu.Lock()
	switch {
	case err != nil && run.ctx.Err() == context.Canceled:
		run.Status, run.snap.Status = RunStopped, RunStopped
		run.snap.Error = err.Error()
		persisted.Status, persisted.Error = string(RunStopped), err.Error()
	case err != nil:
		run.Status, run.snap.Status = RunError, RunError
		run.snap.Error = err.Error()
		persisted.Status, persisted.Error = string(RunError), err.Error()
	default:
		run.Status, run.snap.Status = RunComplete, RunComplete
		run.snap.Result = result.Value
		run.snap.Duration = result.Duration
		run.snap.Phases = result.Phases
		persisted.Status, persisted.Result = string(RunComplete), result.Value
	}
	persisted.FinishedAt = time.Now().UnixMilli()
	run.mu.Unlock()
	SaveRun(persisted)

	if m.OnSettle != nil {
		m.OnSettle(run)
	}
	close(run.Done) // broadcast to all waiters
}

func runID(run *ManagedRun) string { return run.ID }

// Get returns one run, or nil if unknown.
func (m *Manager) Get(id string) *ManagedRun {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs[id]
}

// List returns all runs, newest first.
func (m *Manager) List() []*ManagedRun {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*ManagedRun, 0, len(m.runs))
	for _, r := range m.runs {
		out = append(out, r)
	}
	return out
}

// Snapshot returns a copy of the run's observable state.
func (m *Manager) Snapshot(id string) (Snapshot, bool) {
	r := m.Get(id)
	if r == nil {
		return Snapshot{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := r.snap
	snap.Agents = append([]AgentSnapshot(nil), r.snap.Agents...)
	snap.Phases = append([]string(nil), r.snap.Phases...)
	snap.Logs = append([]string(nil), r.snap.Logs...)
	return snap, true
}

// Stop cancels a running run (its in-flight agents' ctx dies with it).
// Returns false if unknown or already settled.
func (m *Manager) Stop(id string) bool {
	r := m.Get(id)
	if r == nil {
		return false
	}
	r.mu.Lock()
	running := r.Status == RunRunning
	r.mu.Unlock()
	if !running {
		return false
	}
	r.cancel()
	return true
}

func phasesOf(agents []AgentSnapshot) []string {
	var out []string
	for _, a := range agents {
		if a.Phase != "" && !contains(out, a.Phase) {
			out = append(out, a.Phase)
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
