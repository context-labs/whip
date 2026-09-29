package tool

import (
	"context"
	"errors"
)

// SettledFailure marks a confirmed failed effect, such as a remote tool's
// completed error response. It preserves bounded returned evidence without
// classifying the outcome as uncertain. Transport failures must remain ordinary
// errors. Cancellation and deadlines cannot be marked as confirmed outcomes.
func SettledFailure(cause error) error {
	if cause == nil || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return cause
	}
	return &settledFailure{cause: cause}
}

type settledFailure struct{ cause error }

func (e *settledFailure) Error() string { return e.cause.Error() }
func (e *settledFailure) Unwrap() error { return e.cause }
func isSettledFailure(err error) bool   { var failure *settledFailure; return errors.As(err, &failure) }
