package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) Budgets(ctx context.Context, id session.SessionID) ([]session.Budget, error) {
	return r.store.Budgets(ctx, id)
}

func (r *Runtime) SetBudget(ctx context.Context, id session.SessionID, revision int64, limit session.BudgetLimit) (session.Budget, error) {
	return r.store.SetBudget(ctx, id, revision, limit)
}
