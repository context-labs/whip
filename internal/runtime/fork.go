package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

// Fork imports a bounded prefix into a new root with an empty REPL and fresh
// limits. Working-directory reuse does not create or restore filesystem state.
func (r *Runtime) Fork(ctx context.Context, request session.ForkRequest) (session.ForkResult, error) {
	if err := r.Err(); err != nil {
		return session.ForkResult{}, err
	}
	if result, found, err := r.store.ForkRetry(ctx, request); found || err != nil {
		return result, err
	}
	current, err := r.configuration.Snapshot(ctx)
	if err != nil {
		return session.ForkResult{}, err
	}
	return r.store.Fork(ctx, request, session.ForkDefaults{
		Resources: current.Host.Resources,
		Budgets:   session.DefaultWriteBudgets(),
	})
}
