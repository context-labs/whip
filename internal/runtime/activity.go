package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) Activity(ctx context.Context, owner session.SessionID) (session.Activity, error) {
	return r.store.Activity(ctx, owner)
}

func (r *Runtime) InputPage(ctx context.Context, owner session.SessionID, state string, after int64, limit int) (session.InputPage, error) {
	return r.store.InputPage(ctx, owner, state, after, limit)
}

func (r *Runtime) SessionInput(ctx context.Context, owner session.SessionID, id session.InputID) (session.Input, error) {
	return r.store.SessionInput(ctx, owner, id)
}

func (r *Runtime) TurnPage(ctx context.Context, owner session.SessionID, before session.TurnID, limit int) (session.TurnPage, error) {
	return r.store.TurnPage(ctx, owner, before, limit)
}

// RecentInputText is a trusted-client editor read. No guest module exposes it.
func (r *Runtime) RecentInputText(ctx context.Context, before int64, limit int) (session.InputTextPage, error) {
	return r.store.RecentInputText(ctx, before, limit)
}
