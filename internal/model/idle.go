package model

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

const (
	chatIdleTimeout      = 2 * time.Minute
	responsesIdleTimeout = 5 * time.Minute
)

var errProviderIdle = errors.New("provider idle deadline")

// idleWatchdog owns one timer goroutine for a single HTTP attempt. Progress only
// moves the monotonic deadline; the timer wakes again at that deadline. This
// avoids stale timer callbacks racing a reset or outliving Execute.
type idleWatchdog struct {
	mu       sync.Mutex
	deadline time.Time
	timeout  time.Duration
	cancel   context.CancelCauseFunc
	done     chan struct{}
}

func watchIdle(ctx context.Context, timeout time.Duration) (context.Context, *idleWatchdog) {
	ctx, cancel := context.WithCancelCause(ctx)
	w := &idleWatchdog{deadline: time.Now().Add(timeout), timeout: timeout, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(w.done)
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				w.mu.Lock()
				remaining := time.Until(w.deadline)
				if remaining <= 0 {
					cancel(errProviderIdle)
				}
				w.mu.Unlock()
				if remaining <= 0 {
					return
				}
				timer.Reset(remaining)
			}
		}
	}()
	return ctx, w
}

func (w *idleWatchdog) progress() {
	w.mu.Lock()
	w.deadline = time.Now().Add(w.timeout)
	w.mu.Unlock()
}

func (w *idleWatchdog) finish(ctx context.Context, response *Response, err *error) {
	w.cancel(nil)
	<-w.done
	// A fully decoded completion (or confirmed HTTP rejection) wins over a
	// simultaneous idle deadline. Only interrupted I/O is a stall. Caller
	// cancellation/deadline causes retain their existing error identity.
	if errors.Is(*err, context.Canceled) && errors.Is(context.Cause(ctx), errProviderIdle) {
		response.Parts, response.Continuation = nil, nil
		*err = streamError("provider stalled waiting for response data; outcome is unknown")
	}
}

type idleReader struct {
	reader   io.Reader
	watchdog *idleWatchdog
}

func (r idleReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.watchdog.progress()
	}
	return n, err
}
