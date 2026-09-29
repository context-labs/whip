package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) Usage(ctx context.Context, owner session.SessionID) (session.Usage, error) {
	return r.store.Usage(ctx, owner)
}
