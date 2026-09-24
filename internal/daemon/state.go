package daemon

import (
	"context"
	"slices"

	"github.com/context-labs/whip/internal/capability"
	sessionstore "github.com/context-labs/whip/internal/session"
)

func (s *Session) mutateState(ctx context.Context, callerAgentID string, payload sessionstore.RuntimePayload, action func(context.Context, sessionstore.RuntimePayload) (sessionstore.StateValue, error)) (sessionstore.StateValue, error) {
	payload.Data = slices.Clone(payload.Data)
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.StateValue, error) {
		var value sessionstore.StateValue
		err := s.consumeBudgets(actorCtx, callerAgentID, durableReservations(len(payload.Data)), func() error {
			var err error
			value, err = action(actorCtx, payload)
			return err
		})
		return value, err
	})
}

func (s *Session) GetPrivateState(ctx context.Context, callerAgentID, key string) (sessionstore.StateValue, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.StateValue, error) {
		return s.store.GetPrivateState(actorCtx, s.meta.ID, callerAgentID, key)
	})
}

func (s *Session) ListPrivateState(ctx context.Context, callerAgentID string) ([]sessionstore.StateValue, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) ([]sessionstore.StateValue, error) {
		return s.store.ListPrivateState(actorCtx, s.meta.ID, callerAgentID)
	})
}

func (s *Session) SetPrivateState(ctx context.Context, callerAgentID, key string, payload sessionstore.RuntimePayload) (sessionstore.StateValue, error) {
	return s.mutateState(ctx, callerAgentID, payload, func(actorCtx context.Context, payload sessionstore.RuntimePayload) (sessionstore.StateValue, error) {
		return s.store.SetPrivateState(actorCtx, s.meta.ID, callerAgentID, key, payload)
	})
}

func (s *Session) AppendPrivateState(ctx context.Context, callerAgentID, key string, payload sessionstore.RuntimePayload) (sessionstore.StateValue, error) {
	return s.mutateState(ctx, callerAgentID, payload, func(actorCtx context.Context, payload sessionstore.RuntimePayload) (sessionstore.StateValue, error) {
		return s.store.AppendPrivateState(actorCtx, s.meta.ID, callerAgentID, key, payload)
	})
}

func (s *Session) CompareAndSwapPrivateState(ctx context.Context, callerAgentID, key string, expectedVersion int64, payload sessionstore.RuntimePayload) (sessionstore.StateValue, error) {
	return s.mutateState(ctx, callerAgentID, payload, func(actorCtx context.Context, payload sessionstore.RuntimePayload) (sessionstore.StateValue, error) {
		return s.store.CompareAndSwapPrivateState(actorCtx, s.meta.ID, callerAgentID, key, expectedVersion, payload)
	})
}

func (s *Session) GetBlackboard(ctx context.Context, callerAgentID, key string) (sessionstore.StateValue, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.StateValue, error) {
		return s.store.GetBlackboard(actorCtx, s.meta.ID, callerAgentID, key)
	})
}

func (s *Session) SetBlackboard(ctx context.Context, callerAgentID, key string, payload sessionstore.RuntimePayload) (sessionstore.StateValue, error) {
	return s.mutateState(ctx, callerAgentID, payload, func(actorCtx context.Context, payload sessionstore.RuntimePayload) (sessionstore.StateValue, error) {
		value, err := s.store.SetBlackboard(actorCtx, s.meta.ID, callerAgentID, key, payload)
		if err == nil {
			s.wakeSubscribers(key)
		}
		return value, err
	})
}

func (s *Session) AppendBlackboard(ctx context.Context, callerAgentID, key string, payload sessionstore.RuntimePayload) (sessionstore.StateValue, error) {
	return s.mutateState(ctx, callerAgentID, payload, func(actorCtx context.Context, payload sessionstore.RuntimePayload) (sessionstore.StateValue, error) {
		value, err := s.store.AppendBlackboard(actorCtx, s.meta.ID, callerAgentID, key, payload)
		if err == nil {
			s.wakeSubscribers(key)
		}
		return value, err
	})
}

func (s *Session) CompareAndSwapBlackboard(ctx context.Context, callerAgentID, key string, expectedVersion int64, payload sessionstore.RuntimePayload) (sessionstore.StateValue, error) {
	return s.mutateState(ctx, callerAgentID, payload, func(actorCtx context.Context, payload sessionstore.RuntimePayload) (sessionstore.StateValue, error) {
		value, err := s.store.CompareAndSwapBlackboard(actorCtx, s.meta.ID, callerAgentID, key, expectedVersion, payload)
		if err == nil {
			s.wakeSubscribers(key)
		}
		return value, err
	})
}

// wakeSubscribers nudges every live subscriber of key. The subscription wake
// row was committed with the mutation; the durable inbox is the truth and a
// spurious wake is harmless.
func (s *Session) wakeSubscribers(key string) {
	ids, err := s.store.SubscribedAgents(context.Background(), s.meta.ID, key)
	if err != nil {
		return
	}
	for _, id := range ids {
		s.wakeAgent(id)
	}
}

func (s *Session) BlackboardHistory(ctx context.Context, callerAgentID, key string) ([]sessionstore.StateValue, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) ([]sessionstore.StateValue, error) {
		return s.store.BlackboardHistory(actorCtx, s.meta.ID, callerAgentID, key)
	})
}

func (s *Session) CreateBlackboardSubscription(ctx context.Context, callerAgentID, key string) (sessionstore.BlackboardSubscription, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.BlackboardSubscription, error) {
		reservations := append(durableReservations(len(key)), capability.Reservation{
			Kind: string(sessionstore.BudgetSchedulesSubscriptions), Amount: 1, Consume: true,
		})
		var subscription sessionstore.BlackboardSubscription
		err := s.consumeBudgets(actorCtx, callerAgentID, reservations, func() error {
			var err error
			subscription, err = s.store.CreateBlackboardSubscription(actorCtx, s.meta.ID, callerAgentID, key)
			return err
		})
		return subscription, err
	})
}

func (s *Session) ListBlackboardSubscriptions(ctx context.Context, callerAgentID string) ([]sessionstore.BlackboardSubscription, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) ([]sessionstore.BlackboardSubscription, error) {
		return s.store.ListBlackboardSubscriptions(actorCtx, s.meta.ID, callerAgentID)
	})
}

func (s *Session) CancelBlackboardSubscription(ctx context.Context, callerAgentID, subscriptionID string) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		return s.store.CancelBlackboardSubscription(actorCtx, s.meta.ID, callerAgentID, subscriptionID)
	})
}
