package tool

import "errors"

// FatalError means an accepted host operation has unresolved persistence or
// accounting. The process boundary must stop the cell, never turn this error
// into a catchable guest exception or publish a checkpoint.
type FatalError struct{ cause error }

func (e *FatalError) Error() string { return e.cause.Error() }
func (e *FatalError) Unwrap() error { return e.cause }
func (*FatalError) FatalHostError() {}

// Fatal preserves the underlying failure for recovery and diagnostics.
func Fatal(err error) error {
	if err == nil {
		return nil
	}
	if isFatal(err) {
		return err
	}
	return &FatalError{cause: err}
}

func isFatal(err error) bool {
	_, ok := errors.AsType[interface {
		error
		FatalHostError()
	}](err)
	return ok
}
