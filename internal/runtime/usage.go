package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) Usage(ctx context.Context, owner session.SessionID) (session.Usage, error) {
	return r.store.Usage(ctx, owner)
}

func (r *Runtime) TurnUsage(ctx context.Context, owner session.SessionID, turn session.TurnID) (session.TurnUsage, error) {
	return r.store.TurnUsage(ctx, owner, turn)
}
