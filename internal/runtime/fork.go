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
	return r.store.Fork(ctx, request, session.ForkDefaults{
		Resources: r.host.Resources,
		Budgets:   session.DefaultWriteBudgets(),
	})
}
