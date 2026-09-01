package daemon

import (
	"context"

	sessionstore "github.com/context-labs/whip/internal/session"
)

func (s *Session) InspectBudgets(ctx context.Context, callerAgentID, targetAgentID string) (states []sessionstore.BudgetState, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		states, err = s.store.InspectBudgetsFor(actorCtx, s.meta.ID, callerAgentID, targetAgentID)
		return err
	})
	return states, err
}

func (s *Session) CapBudget(ctx context.Context, callerAgentID, targetAgentID string, kind sessionstore.BudgetKind, limit int64) (state sessionstore.BudgetState, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		state, err = s.store.CapBudget(actorCtx, s.meta.ID, callerAgentID, targetAgentID, kind, limit)
		return err
	})
	return state, err
}
