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
