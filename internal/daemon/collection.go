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

func (c *Client) RootCollection(ctx context.Context, params protocol.RootCollectionParams) (session.RootCollectionPage, error) {
	var page session.RootCollectionPage
	err := c.Call(ctx, "root.collection", params, &page)
	return page, err
}

func (s *Server) sessionCatalog(ctx context.Context, params protocol.SessionCatalogParams) (session.SessionCatalogPage, error) {
	return s.daemon.store.SessionCatalog(ctx, session.CatalogPageOptions{Cursor: params.Cursor, Limit: params.Limit, MaxBytes: params.MaxBytes, Search: params.Search})
}
func (c *Client) SessionCatalog(ctx context.Context, params protocol.SessionCatalogParams) (session.SessionCatalogPage, error) {
	var page session.SessionCatalogPage
	err := c.Call(ctx, "sessions.list", params, &page)
	return page, err
}
func (c *Client) SessionCatalogRevision(ctx context.Context) (session.CatalogRevision, error) {
	var revision session.CatalogRevision
	err := c.Call(ctx, "sessions.revision", protocol.EmptyParams{}, &revision)
	return revision, err
}
