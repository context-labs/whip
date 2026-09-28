// Package runtime owns scheduling and live execution lifetimes. Durable facts
// remain in store; request contexts never own accepted execution.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

var (
	ErrOwned  = errors.New("another runtime owns this directory")
	ErrClosed = errors.New("runtime is closed")
)

type Options struct {
	Workers      int
	PollInterval time.Duration
}
type execution struct {
	turn   session.TurnID
	cancel context.CancelFunc
}
type completion struct {
	session session.SessionID
	err     error
}
type Runtime struct {
	store           *store.Store
	runner          *runner.Runner
	owner           *owner
	directory       string
	host            config.Host
	options         Options
	wake            chan struct{}
	done            chan struct{}
	mu              sync.Mutex
	active          map[session.SessionID]execution
	started, closed bool
	cancel          context.CancelFunc
	failure         error
	closeOnce       sync.Once
	closeErr        error
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
	if options.PollInterval == 0 {
		options.PollInterval = 100 * time.Millisecond
	}
	if options.Workers < 1 || options.Workers > 64 || options.PollInterval < time.Millisecond || options.PollInterval > time.Second {
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
	database, err := store.Open(ctx, filepath.Join(directory, "state.db"))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, database.Close())
		}
	}()
	loop, err := runner.New(provider, database, database)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		store: database, runner: loop, owner: lock, directory: directory, host: host, options: options,
		wake: make(chan struct{}, 1), done: make(chan struct{}), active: map[session.SessionID]execution{},
	}, nil
}
func (r *Runtime) Identity() session.RuntimeID { return r.store.Identity() }
func (r *Runtime) SocketPath() string          { return filepath.Join(r.directory, "runtime.sock") }
func (r *Runtime) Done() <-chan struct{}       { return r.done }
func (r *Runtime) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}
	if r.started {
		return errors.New("runtime already started")
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
		if started {
			<-r.done
		}
		r.closeErr = errors.Join(r.store.Close(), r.owner.Close())
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
	// At most Workers completions can be pending. A worker never waits for a
	// scheduler which is joining it during shutdown.
	completed := make(chan completion, r.options.Workers)
	ticker := time.NewTicker(r.options.PollInterval)
	defer ticker.Stop()
	defer workers.Wait()
	for {
		if err := r.schedule(ctx, &workers, completed); err != nil && ctx.Err() == nil {
			r.mu.Lock()
			r.failure = err
			r.cancel()
			r.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case result := <-completed:
			r.mu.Lock()
			delete(r.active, result.session)
			if result.err != nil && ctx.Err() == nil {
				r.failure = result.err
				r.cancel()
			}
			r.mu.Unlock()
		case <-r.wake:
		case <-ticker.C:
		}
	}
}

func (r *Runtime) schedule(ctx context.Context, workers *sync.WaitGroup, completed chan<- completion) error {
	r.mu.Lock()
	available := r.options.Workers - len(r.active)
	r.mu.Unlock()
	if available == 0 {
		return nil
	}
	pending, err := r.store.QueuedSessions(ctx, r.options.Workers)
	if err != nil {
		return err
	}
	for _, id := range pending {
		if available == 0 {
			break
		}
		r.mu.Lock()
		_, busy := r.active[id]
		r.mu.Unlock()
		if busy {
			continue
		}
		claim, err := r.store.Claim(ctx, id)
		if errors.Is(err, store.ErrBusy) || errors.Is(err, store.ErrNoWork) || errors.Is(err, store.ErrStopped) || errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		workerCtx, cancel := context.WithCancel(ctx)
		r.mu.Lock()
		r.active[id] = execution{turn: claim.Turn.ID, cancel: cancel}
		r.mu.Unlock()
		available--
		workers.Go(func() {
			defer cancel()
			err := r.execute(workerCtx, claim)
			completed <- completion{session: id, err: err}
		})
	}
	return nil
}

func (r *Runtime) execute(ctx context.Context, claim store.Claim) error {
	current, err := r.store.Turn(ctx, claim.Turn.ID)
	if err != nil && ctx.Err() == nil {
		return err
	}
	outcome := runner.Outcome{State: session.Cancelled}
	if err == nil && current.State != session.Cancelling {
		outcome, err = r.runner.Run(ctx, claim.Turn, claim.Configuration)
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
