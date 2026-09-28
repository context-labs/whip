package process

import "errors"

// FatalHostError is implemented by host-owned failures whose accounting or
// operation settlement remains unresolved. It never crosses the guest wire:
// the parent terminates the worker instead of sending a catchable host response.
type FatalHostError interface {
	error
	FatalHostError()
}

func fatalHostError(err error) bool {
	_, ok := errors.AsType[FatalHostError](err)
	return ok
}
