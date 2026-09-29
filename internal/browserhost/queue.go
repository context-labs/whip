package browserhost

import (
	"context"
	"slices"
	"sync"
)

// A whole batch owns its tab, including its permission-authorized native calls.
// Transfers acquire sorted tabs immediately and never partially queue a handoff.
type tabQueue struct {
	mu     sync.Mutex
	active bool
	queue  []chan struct{}
}

func (t *tabQueue) acquire(ctx context.Context, immediate bool) (func(), error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	t.mu.Lock()
	if !t.active {
		t.active = true
		t.mu.Unlock()
		return t.release, nil
	}
	if immediate || len(t.queue) >= 4 {
		t.mu.Unlock()
		return nil, ErrBusy
	}
	ready := make(chan struct{})
	t.queue = append(t.queue, ready)
	t.mu.Unlock()
	select {
	case <-ready:
		if e := ctx.Err(); e != nil {
			t.release()
			return nil, e
		}
		return t.release, nil
	case <-ctx.Done():
		t.mu.Lock()
		i := slices.Index(t.queue, ready)
		if i >= 0 {
			t.queue = slices.Delete(t.queue, i, i+1)
			t.mu.Unlock()
		} else {
			t.mu.Unlock()
			t.release()
		}
		return nil, ctx.Err()
	}
}

func (t *tabQueue) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.queue) == 0 {
		t.active = false
		return
	}
	next := t.queue[0]
	t.queue = t.queue[1:]
	close(next)
}

func (t *tabQueue) idle() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.active && len(t.queue) == 0
}
