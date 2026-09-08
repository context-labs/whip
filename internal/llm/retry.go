package llm

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// retryPolicy is the one transport-retry loop every provider client uses.
// Transient failures (transport errors, 429, 5xx, server-side stream faults)
// are retried with exponential backoff, honouring Retry-After when the server
// sends one. A retry is skipped when stop() reports the caller has already
// seen output (a retry would replay it), when the error is permanent, or when
// the budget is spent. onRetry, when set, is told about each retry so the UI
// can show "retrying in Ns" instead of looking hung.
type retryPolicy struct {
	attempts int
	onRetry  func(RetryEvent)
}

func newRetryPolicy(maxRetries int, onRetry func(RetryEvent)) retryPolicy {
	if maxRetries <= 0 {
		maxRetries = DefaultMaxAttempts
	}
	return retryPolicy{attempts: maxRetries, onRetry: onRetry}
}

// run calls once() up to p.attempts times. stop may be nil.
func (p retryPolicy) run(ctx context.Context, once func() error, stop func() bool) error {
	var last error
	for attempt := 1; attempt <= p.attempts; attempt++ {
		err := once()
		if err == nil {
			return nil
		}
		last = err
		if (stop != nil && stop()) || !retryable(err) || attempt == p.attempts {
			break
		}
		delay := retryDelay(attempt, err)
		if p.onRetry != nil {
			p.onRetry(RetryEvent{Attempt: attempt, Max: p.attempts, Delay: delay, Err: err})
		}
		if serr := sleep(ctx, delay); serr != nil {
			return serr
		}
	}
	return last
}

// maxRetryAfter caps how long a Retry-After header may hold a turn. A server
// asking for longer is telling us the window is exhausted, not congested;
// that surfaces to the user (see retryable) instead of a silent multi-minute
// stall.
const maxRetryAfter = 60 * time.Second

// retryDelay is the backoff schedule, raised to Retry-After when the server
// asked for a longer pause.
func retryDelay(attempt int, err error) time.Duration {
	d := backoff(attempt)
	if he, ok := errors.AsType[*HTTPError](err); ok && he.RetryAfter > d {
		d = he.RetryAfter
	}
	return d
}

// newHTTPError builds the error for a non-2xx response: status, a bounded
// slice of the body, and the parsed Retry-After header (seconds or HTTP-date).
func newHTTPError(resp *http.Response, body string) *HTTPError {
	return &HTTPError{Status: resp.Status, Body: strings.TrimSpace(body), RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
}

func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(max(secs, 0)) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0)
	}
	return 0
}
