package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func (r *Runtime) Trees(ctx context.Context, request store.TreeList) (store.TreePage, error) {
	return r.store.Trees(ctx, request)
}

func (r *Runtime) DefinitionSummaries(ctx context.Context, after *session.DefinitionRef, limit int) (store.DefinitionPage, error) {
	return r.store.DefinitionSummaries(ctx, after, limit)
}

func (r *Runtime) TreeCatalog(ctx context.Context) (session.Revision, error) {
	return r.store.TreeCatalog(ctx)
}
