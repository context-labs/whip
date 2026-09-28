package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) SubscribeState(ctx context.Context, actor session.SessionID, id string, request session.StateSubscribe) (session.StateSubscription, error) {
	if err := r.Err(); err != nil {
		return session.StateSubscription{}, err
	}
	value, err := r.store.SubscribeState(ctx, actor, id, request)
	if err == nil {
		r.Wake()
	}
	return value, err
}

func (r *Runtime) StateSubscriptions(ctx context.Context, actor session.SessionID, after string, limit int) ([]session.StateSubscription, error) {
	return r.store.StateSubscriptions(ctx, actor, after, limit)
}

func (r *Runtime) UnsubscribeState(ctx context.Context, actor session.SessionID, id string) (session.StateSubscription, error) {
	return r.store.UnsubscribeState(ctx, actor, id)
}
