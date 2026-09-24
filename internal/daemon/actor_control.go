package daemon

import (
	"context"
	"sync"
)

// routeControlOwnedValue is for bounded, cancellation-aware database claims
// whose result transfers settlement responsibility to the caller. Once the
// claim starts, observe its actual result even if cancellation wins the reply
// race. Before entry, abandonment prevents the claim from starting later.
// General controls use routeControlValue and may stop waiting immediately.
func routeControlOwnedValue[T any](s *Session, ctx context.Context, control func(context.Context) (T, error)) (T, error) {
	var mu sync.Mutex
	var value T
	var callErr error
	completed, abandoned := false, false
	routeErr := s.routeControl(ctx, func(actorCtx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if abandoned {
			return ErrStopped
		}
		value, callErr = control(actorCtx)
		completed = true
		return callErr
	})
	mu.Lock()
	defer mu.Unlock()
	if completed {
		return value, callErr
	}
	abandoned = true
	return value, routeErr
}
