// Package runtime owns scheduling and live execution lifetimes. Durable facts
// remain in store; request contexts never own accepted execution.
package runtime

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/content"
	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/executor"
	"github.com/context-labs/whip/internal/hostview"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/shell"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
	"github.com/context-labs/whip/internal/workspace"
)

var (
	ErrOwned  = errors.New("another runtime owns this directory")
	ErrClosed = errors.New("runtime is closed")
)

type Options struct {
	Workers       int
	PollInterval  time.Duration
	EngineCommand []string
	KernelWorkers int
	// MaxActiveTurns bounds running and waiting turn goroutines together.
	// Zero defaults to 1024; at least Workers slots are reserved for progress.
	MaxActiveTurns int
}
type execution struct {
	turn             session.TurnID
	cancel           context.CancelFunc
	worker           bool
	waiting          bool
	hookNotices      []string
	executorActivity ExecutorActivity
}
type Runtime struct {
	controlMu        sync.Mutex
	controlGates     map[session.TreeID]*controlGate
	computer         *computer.Controller
	computerMu       sync.Mutex
	hostPicker       *hostview.Picker
	shells           *shell.Manager
	mcp              *mcpOwners
	languageServers  *lsp.Pool
	hostProcesses    *capability.ProcessManager
	store            *store.Store
	content          *content.Store
	runner           *runner.Runner
	tools            *tool.Dispatcher
	executors        *executor.Registry
	engineManager    *process.Manager
	kernels          map[session.SessionID]*sessionKernel
	owner            *owner
	directory        string
	host             config.Host
	configuration    *config.Authority
	options          Options
	wake             chan struct{}
	done             chan struct{}
	mu               sync.Mutex
	active           map[session.SessionID]*execution
	runnable         int
	waiting          int
	resumptions      []*workerResumption
	queueCursor      store.QueueCursor // owned only by the scheduling goroutine
	preferResumption bool
	started, closed  bool
	cancel           context.CancelFunc
	failure          error
	closeOnce        sync.Once
	closeErr         error
	epoch            string
	previewMu        sync.Mutex
	previews         map[session.SessionID]*livePreview
	workspace        *workspace.Git
	workspaceCalls   sync.WaitGroup
	workspaceSlots   chan struct{}
}

// Open acquires exclusive execution ownership before opening fresh host/storage.
// It does not start workers or recover turns until Start is called explicitly.
func Open(ctx context.Context, directory string, provider runner.Provider, options Options) (_ *Runtime, err error) {
	if directory == "" {
		return nil, fmt.Errorf("%w: runtime directory required", session.ErrInvalid)
	}
	if options.Workers == 0 {
		options.Workers = 4
	}
	if options.MaxActiveTurns == 0 {
		options.MaxActiveTurns = 1024
	}
	if options.KernelWorkers == 0 {
		options.KernelWorkers = options.Workers
	}
	if options.KernelWorkers < 1 || options.KernelWorkers > 64 {
		return nil, fmt.Errorf("%w: invalid kernel worker limit", session.ErrInvalid)
	}
	options.EngineCommand = append([]string(nil), options.EngineCommand...)
	if options.PollInterval == 0 {
		options.PollInterval = 100 * time.Millisecond
	}
	if options.Workers < 1 || options.Workers > 64 || options.MaxActiveTurns < options.Workers || options.MaxActiveTurns > 1024 || options.PollInterval < time.Millisecond || options.PollInterval > time.Second {
		return nil, fmt.Errorf("%w: invalid scheduler limits", session.ErrInvalid)
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	lock, err := acquireOwner(directory, filepath.Join(directory, "runtime.lock"))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, lock.Close())
		}
	}()
	// Only the newly acquired execution owner may remove a dead owner's socket.
	// RPC listeners never unlink a pre-existing path, so double binding is safe.
	socket := filepath.Join(directory, "runtime.sock")
	if info, statErr := os.Lstat(socket); statErr == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("runtime socket path is occupied by a non-socket")
		}
		if err := os.Remove(socket); err != nil {
			return nil, err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	host, err := config.Initialize(directory)
	if err != nil {
		return nil, err
	}
	configuration, err := config.NewAuthority(directory)
	if err != nil {
		return nil, err
	}
	database, err := store.Open(ctx, filepath.Join(directory, "state.db"))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, database.Close())
		}
	}()
	bodies, err := content.New(directory)
	if err != nil {
		return nil, err
	}
	if err := bodies.Collect(ctx, database.ContentReferenced); err != nil {
		return nil, err
	}
	if err := database.PruneUnusedContent(ctx); err != nil {
		return nil, err
	}
	r := &Runtime{
		mcp: newMCPOwners(directory), executors: executor.New(), epoch: "boot_" + rand.Text(), previews: map[session.SessionID]*livePreview{},
		engineManager: process.NewManager(options.KernelWorkers), kernels: map[session.SessionID]*sessionKernel{},
		store: database, content: bodies, owner: lock, directory: directory, host: host, configuration: configuration, options: options,
		wake: make(chan struct{}, 1), done: make(chan struct{}), active: map[session.SessionID]*execution{},
		preferResumption: true,
		workspace:        workspace.New(string(database.Identity())), workspaceSlots: make(chan struct{}, 16),
	}
	r.hostPicker = hostview.NewPicker(directory)
	r.shells = shell.NewManager()
	r.hostProcesses = capability.NewProcessManager()
	r.languageServers = lsp.NewPool(r.hostProcesses)
	defer func() {
		if err != nil {
			_ = r.hostPicker.Close()
			r.languageServers.Close()
			if r.computer != nil {
				r.computer.Close()
			}
			_ = r.hostProcesses.Close()
			r.shells.Close()
			r.executors.Close()
			_ = r.mcp.close()
			r.engineManager.Close()
		}
	}()
	r.computer, err = computer.NewController(computer.ControllerOptions{Processes: r.hostProcesses, Owner: "computer-control", Directory: directory, Environment: map[string]string{}}, host.Computer)
	if err != nil {
		return nil, err
	}
	r.tools = tool.NewDispatcher(database, database, r)
	r.runner, err = runner.New(provider, database, database, r, r, r, r, database, database)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Runtime) Identity() session.RuntimeID { return r.store.Identity() }

// ProcessEpoch identifies this live runtime generation, never a durable session.
func (r *Runtime) ProcessEpoch() string { return r.epoch }

// HostConfiguration is the single host declaration authority shared with the
// command's account/provider services. Snapshot reads never create a second cache.
func (r *Runtime) HostConfiguration() *config.Authority { return r.configuration }

func (r *Runtime) SocketPath() string    { return filepath.Join(r.directory, "runtime.sock") }
func (r *Runtime) Done() <-chan struct{} { return r.done }
func (r *Runtime) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}
	if r.started {
		return errors.New("runtime already started")
	}
	if len(r.workspaceSlots) != 0 {
		return store.ErrBusy
	}
	if _, err := r.store.Recover(ctx); err != nil {
		return err
	}
	ctx, r.cancel = context.WithCancel(ctx)
	r.started = true
	go r.run(ctx)
	return nil
}

func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		started := r.started
		if r.cancel != nil {
			r.cancel()
		}
		r.mu.Unlock()
		pickerErr := r.hostPicker.Close()
		r.executors.Close()
		r.shells.Close()
		workspaceErr := r.workspace.Close()
		mcpErr := r.mcp.close()
		r.workspaceCalls.Wait()
		r.languageServers.Close()
		r.computer.Close()
		_ = r.hostProcesses.Close()
		r.engineManager.Close()
		if started {
			<-r.done
		}
		for _, entry := range r.kernels {
			entry.kernel.Close()
		}
		r.previewMu.Lock()
		clear(r.previews)
		r.previewMu.Unlock()
		r.closeErr = errors.Join(pickerErr, workspaceErr, mcpErr, r.store.Close(), r.owner.Close())
	})
	return r.closeErr
}
func (r *Runtime) Err() error { r.mu.Lock(); defer r.mu.Unlock(); return r.failure }
func (r *Runtime) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Runtime) cancelTurn(id session.TurnID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, active := range r.active {
		if active.turn == id {
			active.cancel()
			return
		}
	}
}

func (r *Runtime) run(ctx context.Context) {
	defer close(r.done)
	var workers sync.WaitGroup
	var completionCursor session.SessionID
	var scheduleCursor scheduleScan
	ticker := time.NewTicker(r.options.PollInterval)
	defer ticker.Stop()
	defer workers.Wait()
	for {
		// Delivery progresses even when all execution slots are occupied. This
		// cursor is disposable; pending outcomes remain authoritative in SQLite.
		var err error
		scheduleCursor, err = r.admitSchedules(ctx, scheduleCursor)
		if err == nil {
			completionCursor, err = r.publishCompletions(ctx, completionCursor)
		}
		if err == nil {
			err = r.schedule(ctx, &workers)
		}
		if err != nil && ctx.Err() == nil {
			r.mu.Lock()
			if r.failure == nil {
				r.failure = err
			}
			r.cancel()
			r.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		case <-ticker.C:
		}
	}
}

func (r *Runtime) schedule(ctx context.Context, workers *sync.WaitGroup) error {
	r.mu.Lock()
	available := r.options.Workers - r.runnable
	resumes := slices.Clone(r.resumptions)
	r.mu.Unlock()
	if available == 0 {
		return nil
	}
	const pageSize = 100
	pending, err := r.store.QueuedSessions(ctx, r.queueCursor, pageSize)
	if err != nil {
		return err
	}
	index, resumeIndex := 0, 0
	defer func() {
		if index == len(pending) && len(pending) < pageSize {
			r.queueCursor = store.QueueCursor{}
		}
	}()
	for {
		r.mu.Lock()
		if r.closed || ctx.Err() != nil || r.runnable >= r.options.Workers {
			r.mu.Unlock()
			return ctx.Err()
		}
		for index < len(pending) && r.active[pending[index].SessionID] != nil {
			r.queueCursor = pending[index]
			index++
		}
		for resumeIndex < len(resumes) && !slices.Contains(r.resumptions, resumes[resumeIndex]) {
			resumeIndex++
		}
		fresh := index < len(pending) && len(r.active) < r.options.MaxActiveTurns
		resuming := resumeIndex < len(resumes)
		if !fresh && !resuming {
			r.mu.Unlock()
			return nil
		}
		// Reserve the host slot before SQL without holding the scheduler mutex.
		// Each blocked resumption/candidate is examined at most once this pass.
		r.runnable++
		if resuming && (r.preferResumption || !fresh) {
			resume := resumes[resumeIndex]
			resumeIndex++
			r.mu.Unlock()
			if err := r.resumeWorker(ctx, resume); err != nil {
				return err
			}
			continue
		}
		candidate := pending[index]
		index++
		r.queueCursor = candidate
		r.mu.Unlock()
		claim, err := r.claimControlled(ctx, candidate.SessionID)
		if err != nil {
			r.mu.Lock()
			r.runnable--
			r.mu.Unlock()
		}
		if errors.Is(err, store.ErrBusy) || errors.Is(err, store.ErrNoWork) || errors.Is(err, store.ErrStopped) || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrLimit) {
			continue
		}
		if err != nil {
			return err
		}
		workerCtx, cancel := context.WithCancel(ctx)
		active := &execution{turn: claim.Turn.ID, cancel: cancel, worker: true}
		r.mu.Lock()
		r.active[candidate.SessionID] = active
		r.preferResumption = true
		r.mu.Unlock()
		workers.Go(func() {
			defer cancel()
			err := r.execute(workerCtx, claim)
			r.finishExecution(candidate.SessionID, active, err, ctx.Err() != nil)
		})
	}
}

func (r *Runtime) execute(ctx context.Context, claim store.Claim) error {
	current, err := r.store.Turn(ctx, claim.Turn.ID)
	if err != nil && ctx.Err() == nil {
		return err
	}
	outcome := runner.Outcome{State: session.Cancelled}
	if err == nil && current.State != session.Cancelling {
		if claim.Turn.Kind == session.HostOperationInputKind {
			outcome, err = r.runHostOperation(ctx, claim)
		} else if modelErr := claim.Configuration.Model.Validate(); modelErr != nil {
			outcome = runner.Failure(fmt.Errorf("session has no configured model: %w", modelErr))
		} else {
			outcome, err = r.runner.Run(ctx, claim.Turn, claim.Configuration)
		}
	}
	if err != nil && ctx.Err() == nil {
		return r.settle(ctx, claim.Turn.ID, runner.Failure(err))
	}
	if ctx.Err() != nil {
		outcome = runner.Outcome{State: session.Interrupted, Failure: new("runtime execution interrupted")}
	}
	return r.settle(ctx, claim.Turn.ID, outcome)
}

func (r *Runtime) settle(parent context.Context, id session.TurnID, outcome runner.Outcome) error {
	// Cleanup has its own bounded lifetime; an observer or cancelled turn cannot
	// abort durable settlement. This loop retries SQL only, never Run/provider work.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		current, err := r.store.Turn(ctx, id)
		if err == nil {
			if current.State == session.Cancelling {
				outcome = runner.Outcome{State: session.Cancelled}
			}
			_, err = r.store.Finish(ctx, id, outcome.State, outcome.Failure, nil)
		}
		if err == nil {
			return nil
		}
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, session.ErrInvalid) {
			return err
		}
		last = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("settle turn %s: %w", id, errors.Join(ctx.Err(), last))
		case <-ticker.C:
		}
	}
}
