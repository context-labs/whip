package daemon

import (
	"context"

	"github.com/context-labs/whip/internal/capability"
	sessionstore "github.com/context-labs/whip/internal/session"
)

func (s *Session) mutateState(ctx context.Context, callerAgentID string, payload sessionstore.RuntimePayload, action func(context.Context) error) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		return s.consumeBudgets(actorCtx, callerAgentID, durableReservations(len(payload.Data)), func() error { return action(actorCtx) })
	})
}

func (s *Session) GetPrivateState(ctx context.Context, callerAgentID, key string) (value sessionstore.StateValue, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		value, err = s.store.GetPrivateState(actorCtx, s.meta.ID, callerAgentID, key)
		return err
	})
	return value, err
}

func (s *Session) ListPrivateState(ctx context.Context, callerAgentID string) (values []sessionstore.StateValue, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		values, err = s.store.ListPrivateState(actorCtx, s.meta.ID, callerAgentID)
		return err
	})
	return values, err
}

func (s *Session) SetPrivateState(ctx context.Context, callerAgentID, key string, payload sessionstore.RuntimePayload) (value sessionstore.StateValue, err error) {
	err = s.mutateState(ctx, callerAgentID, payload, func(actorCtx context.Context) error {
		value, err = s.store.SetPrivateState(actorCtx, s.meta.ID, callerAgentID, key, payload)
		return err
	})
	return value, err
}

func (s *Session) AppendPrivateState(ctx context.Context, callerAgentID, key string, payload sessionstore.RuntimePayload) (value sessionstore.StateValue, err error) {
	err = s.mutateState(ctx, callerAgentID, payload, func(actorCtx context.Context) error {
		value, err = s.store.AppendPrivateState(actorCtx, s.meta.ID, callerAgentID, key, payload)
		return err
	})
	return value, err
}

func (s *Session) CompareAndSwapPrivateState(ctx context.Context, callerAgentID, key string, expectedVersion int64, payload sessionstore.RuntimePayload) (value sessionstore.StateValue, err error) {
	err = s.mutateState(ctx, callerAgentID, payload, func(actorCtx context.Context) error {
		value, err = s.store.CompareAndSwapPrivateState(actorCtx, s.meta.ID, callerAgentID, key, expectedVersion, payload)
		return err
	})
	return value, err
}

func (s *Session) GetBlackboard(ctx context.Context, callerAgentID, key string) (value sessionstore.StateValue, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		value, err = s.store.GetBlackboard(actorCtx, s.meta.ID, callerAgentID, key)
		return err
	})
	return value, err
}

func (s *Session) SetBlackboard(ctx context.Context, callerAgentID, key string, payload sessionstore.RuntimePayload) (value sessionstore.StateValue, err error) {
	err = s.mutateState(ctx, callerAgentID, payload, func(actorCtx context.Context) error {
		value, err = s.store.SetBlackboard(actorCtx, s.meta.ID, callerAgentID, key, payload)
		return err
	})
	return value, err
}

func (s *Session) AppendBlackboard(ctx context.Context, callerAgentID, key string, payload sessionstore.RuntimePayload) (value sessionstore.StateValue, err error) {
	err = s.mutateState(ctx, callerAgentID, payload, func(actorCtx context.Context) error {
		value, err = s.store.AppendBlackboard(actorCtx, s.meta.ID, callerAgentID, key, payload)
		return err
	})
	return value, err
}

func (s *Session) CompareAndSwapBlackboard(ctx context.Context, callerAgentID, key string, expectedVersion int64, payload sessionstore.RuntimePayload) (value sessionstore.StateValue, err error) {
	err = s.mutateState(ctx, callerAgentID, payload, func(actorCtx context.Context) error {
		value, err = s.store.CompareAndSwapBlackboard(actorCtx, s.meta.ID, callerAgentID, key, expectedVersion, payload)
		return err
	})
	return value, err
}

func (s *Session) BlackboardHistory(ctx context.Context, callerAgentID, key string) (values []sessionstore.StateValue, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		values, err = s.store.BlackboardHistory(actorCtx, s.meta.ID, callerAgentID, key)
		return err
	})
	return values, err
}

func (s *Session) CreateBlackboardSubscription(ctx context.Context, callerAgentID, key string) (subscription sessionstore.BlackboardSubscription, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		reservations := append(durableReservations(len(key)), capability.Reservation{
			Kind: string(sessionstore.BudgetSchedulesSubscriptions), Amount: 1, Consume: true,
		})
		return s.consumeBudgets(actorCtx, callerAgentID, reservations, func() error {
			subscription, err = s.store.CreateBlackboardSubscription(actorCtx, s.meta.ID, callerAgentID, key)
			return err
		})
	})
	return subscription, err
}

func (s *Session) ListBlackboardSubscriptions(ctx context.Context, callerAgentID string) (subscriptions []sessionstore.BlackboardSubscription, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		subscriptions, err = s.store.ListBlackboardSubscriptions(actorCtx, s.meta.ID, callerAgentID)
		return err
	})
	return subscriptions, err
}

func (s *Session) CancelBlackboardSubscription(ctx context.Context, callerAgentID, subscriptionID string) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		return s.store.CancelBlackboardSubscription(actorCtx, s.meta.ID, callerAgentID, subscriptionID)
	})
}
