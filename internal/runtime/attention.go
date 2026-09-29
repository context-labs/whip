package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) HostAttention(ctx context.Context, after *session.AttentionCursor, limit, maxBytes int) (session.AttentionPage, error) {
	return r.store.Attention(ctx, after, limit, maxBytes)
}
