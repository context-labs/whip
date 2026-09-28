package runtime

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type workerResumption struct {
	owner *execution
	ready chan struct{}
	done  <-chan struct{}
	err   error
}

// withReleasedWorker retains turn ownership while a committed cell's parent
// waits for other sessions. The caller must have released its kernel lease and
// pass the turn's execution context; this cannot run inside a live code cell.
// Returning the wait's result owns a worker permit again. Cancellation, closure or
// refused resumption returns without one; execution must unwind in those cases.
func (r *Runtime) withReleasedWorker(ctx context.Context, turnID session.TurnID, wait func(context.Context) error) (result error) {
	if wait == nil {
		return fmt.Errorf("%w: worker release requires a wait function", session.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrClosed
	}
	var owner *execution
	for _, active := range r.active {
		if active.turn == turnID {
			owner = active
			break
		}
	}
	if owner == nil {
		r.mu.Unlock()
		return store.ErrNotFound
	}
	if !owner.worker || owner.waiting {
		r.mu.Unlock()
		return store.ErrBusy
	}
	// A wait must leave room for runnable descendants. Once this bound is
	// reached, fail before yielding rather than deadlocking an all-waiting set.
	if r.waiting >= r.options.MaxActiveTurns-r.options.Workers {
		r.mu.Unlock()
		return fmt.Errorf("%w: runtime waiting-turn capacity exhausted", store.ErrLimit)
	}
	owner.waiting = true
	r.waiting++
	r.mu.Unlock()
	var resume *workerResumption
	defer func() {
		r.mu.Lock()
		if resume != nil {
			if index := slices.Index(r.resumptions, resume); index >= 0 {
				r.resumptions = slices.Delete(r.resumptions, index, index+1)
			}
		}
		if err := ctx.Err(); err != nil {
			result = err
			if owner.worker {
				owner.worker = false
				r.runnable--
			}
		}
		owner.waiting = false
		r.waiting--
		r.mu.Unlock()
		r.Wake()
	}()
	// Keep the physical worker until SQL confirms all dispatched work has settled
	// and its execution permission has been released. Never yield during I/O.
	if err := r.store.YieldTurn(ctx, turnID); err != nil {
		return err
	}
	r.mu.Lock()
	owner.worker = false
	r.runnable--
	r.mu.Unlock()
	r.Wake()
	if err := ctx.Err(); err != nil {
		return err
	}
	waitErr := wait(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	resume = &workerResumption{owner: owner, ready: make(chan struct{}), done: ctx.Done()}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrClosed
	}
	r.resumptions = append(r.resumptions, resume)
	r.mu.Unlock()
	r.Wake()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-resume.ready:
		if err := ctx.Err(); err != nil {
			return err
		}
		if resume.err != nil {
			return resume.err
		}
		return waitErr
	}
}

// resumeWorker owns a provisional host slot while SQL acquires scoped permission.
// A blocked waiter stays in FIFO order, while this pass can try unrelated work.
func (r *Runtime) resumeWorker(ctx context.Context, resume *workerResumption) error {
	err := r.store.ResumeTurn(ctx, resume.owner.turn)
	r.mu.Lock()
	index := slices.Index(r.resumptions, resume)
	cancelled := false
	select {
	case <-resume.done:
		cancelled = true
	default:
	}
	if err == nil && index >= 0 && !cancelled && !r.closed && ctx.Err() == nil {
		r.resumptions = slices.Delete(r.resumptions, index, index+1)
		resume.owner.worker = true
		r.preferResumption = false
		close(resume.ready)
		r.mu.Unlock()
		return nil
	}
	r.runnable--
	if index >= 0 && (!errors.Is(err, store.ErrLimit) || cancelled || r.closed || ctx.Err() != nil) {
		r.resumptions = slices.Delete(r.resumptions, index, index+1)
		resume.err = err
		if cancelled || ctx.Err() != nil {
			resume.err = context.Canceled
		} else if r.closed {
			resume.err = ErrClosed
		}
		close(resume.ready)
	}
	r.mu.Unlock()
	if err == nil {
		// Cancellation can remove the waiter while acquisition commits. It cannot
		// receive execution; release that unused permission without its context.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = r.store.YieldTurn(cleanup, resume.owner.turn)
	}
	if errors.Is(err, store.ErrLimit) || errors.Is(err, store.ErrStopped) || errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return err
}

// finishExecution cannot block on the scheduler during shutdown. The runtime
// WaitGroup still joins every worker after durable turn settlement completes.
func (r *Runtime) finishExecution(id session.SessionID, owner *execution, err error, cancelled bool) {
	r.mu.Lock()
	if r.active[id] == owner {
		if owner.worker {
			owner.worker = false
			r.runnable--
		}
		delete(r.active, id)
	}
	if err != nil && !cancelled && r.failure == nil {
		r.failure = err
		r.cancel()
	}
	r.mu.Unlock()
	r.Wake()
}
