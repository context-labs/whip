package daemon

import (
	"context"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func (s *Server) rootCollection(ctx context.Context, params protocol.RootCollectionParams) (session.RootCollectionPage, error) {
	return s.daemon.store.RootCollectionPage(ctx, params.RootID, params.Collection, session.CollectionPageOptions{
		Cursor: params.Cursor, Limit: params.Limit, MaxBytes: params.MaxBytes,
	})
}

func (s *Server) sessionCatalog(ctx context.Context, params protocol.SessionCatalogParams) (session.SessionCatalogPage, error) {
	return s.daemon.store.SessionCatalog(ctx, session.CatalogPageOptions{Cursor: params.Cursor, Limit: params.Limit, MaxBytes: params.MaxBytes, Search: params.Search, Status: params.Status})
}
