package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) RecentTrees(ctx context.Context, limit int) (session.RecentTreePage, error) {
	return r.store.RecentTrees(ctx, limit)
}

func (r *Runtime) RecentTreesPage(ctx context.Context, request session.RecentTreeList) (session.RecentTreePage, error) {
	return r.store.RecentTreesPage(ctx, request)
}
