package runtime

import (
	"context"
	"fmt"
	"slices"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type workerResumption struct {
	owner *execution
	ready chan struct{}
}

// withReleasedWorker retains turn ownership while a committed cell's parent
// waits for other sessions. The caller must have released its kernel lease and
// pass the turn's execution context; this cannot run inside a live code cell.
// Returning the wait's result owns a worker permit again. Cancellation or runtime
// closure returns without one, so the caller must unwind instead of continuing.
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
	owner.worker, owner.waiting = false, true
	r.runnable--
	r.waiting++
	r.mu.Unlock()
	r.Wake()
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
	waitErr := wait(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	resume = &workerResumption{owner: owner, ready: make(chan struct{})}
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
		return waitErr
	}
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
