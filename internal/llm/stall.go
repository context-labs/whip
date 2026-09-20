package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Stall detection. A provider stream that goes quiet is cancelled and retried
// instead of waiting for the per-attempt ceiling: opencode and pi both use an
// idle deadline (300 s) rather than a fixed total per call.
const (
	// DefaultStallTimeout bounds the wait for response headers and the gap
	// between chunks on OpenAI-compatible chat streams. Inference.net's gateway
	// reports its own no-token stall after 30 s, so this is a backstop for
	// silent transport stalls.
	DefaultStallTimeout = 2 * time.Minute
	// DefaultResponsesStallTimeout applies to the OpenAI Responses and ChatGPT
	// subscription streams, which can stay silent for minutes while a
	// reasoning model thinks without streamed summaries.
	DefaultResponsesStallTimeout = 5 * time.Minute
)

type stallKind int

const (
	chatStall stallKind = iota
	responsesStall
)

func (c *Client) stallTimeout(kind stallKind) time.Duration {
	if c.StallTimeout > 0 {
		return c.StallTimeout
	}
	if kind == responsesStall {
		return DefaultResponsesStallTimeout
	}
	return DefaultStallTimeout
}

// stallError is whip's own idle deadline: no response headers, or no bytes
// after the last chunk, for the stall timeout. It is retryable and never
// wraps context.Canceled, so callers cannot mistake it for a user cancel.
type stallError struct{ Stall time.Duration }

func (e stallError) Error() string {
	return fmt.Sprintf("provider stream stalled: no data for %s", e.Stall)
}

// ceilingError is whip's own per-attempt ceiling. It wraps
// context.DeadlineExceeded for callers that check errors.Is, but retryable()
// recognises the type first, so it is repeated within the retry budget while
// a deadline set by the caller is not.
type ceilingError struct{ Ceiling time.Duration }

func (e ceilingError) Error() string {
	return fmt.Sprintf("model call exceeded the %s per-attempt ceiling", e.Ceiling)
}

func (e ceilingError) Unwrap() error { return context.DeadlineExceeded }

// ownDeadline reports whether the context was ended by one of whip's own
// deadlines rather than by its caller.
func ownDeadline(ctx context.Context) (error, bool) {
	cause := context.Cause(ctx)
	if _, ok := errors.AsType[stallError](cause); ok {
		return cause, true
	}
	if _, ok := errors.AsType[ceilingError](cause); ok {
		return cause, true
	}
	return nil, false
}

// do sends the request with a stall watchdog: the request is cancelled when
// the provider sends nothing for the stall timeout, whether before the
// response headers or between body chunks. Errors caused by whip's own
// deadlines are returned as their typed causes.
func (c *Client) do(request *http.Request, stall time.Duration) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(request.Context())
	watchdog := &stallWatchdog{ctx: ctx, cancel: cancel, stall: stall}
	watchdog.timer = time.AfterFunc(stall, func() { cancel(stallError{Stall: stall}) })
	response, err := c.HTTP.Do(request.WithContext(ctx))
	if err != nil {
		watchdog.timer.Stop()
		cancel(nil)
		return nil, watchdog.mapError(err)
	}
	response.Body = &stallBody{ReadCloser: response.Body, watchdog: watchdog}
	return response, nil
}

type stallWatchdog struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	stall  time.Duration
	timer  *time.Timer
}

func (w *stallWatchdog) mapError(err error) error {
	if cause, ok := ownDeadline(w.ctx); ok {
		return cause
	}
	return err
}

// stallBody re-arms the watchdog on every chunk and maps a cancellation
// caused by the watchdog (or the attempt ceiling) to its typed error.
type stallBody struct {
	io.ReadCloser
	watchdog *stallWatchdog
}

func (b *stallBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.watchdog.timer.Reset(b.watchdog.stall)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		err = b.watchdog.mapError(err)
	}
	if errors.Is(err, io.EOF) {
		b.watchdog.timer.Stop()
	}
	return n, err
}

func (b *stallBody) Close() error {
	b.watchdog.timer.Stop()
	err := b.ReadCloser.Close()
	b.watchdog.cancel(nil)
	return err
}
