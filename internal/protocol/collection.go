package protocol

import "github.com/context-labs/whip/internal/session"

type RootCollectionParams struct {
	RootID     string                    `json:"root_id"`
	Collection string                    `json:"collection"`
	Cursor     *session.CollectionCursor `json:"cursor,omitempty"`
	Limit      int                       `json:"limit"`
	MaxBytes   int                       `json:"max_bytes"`
}

type SessionCatalogParams struct {
	Search   string                 `json:"search,omitempty"`
	Status   string                 `json:"status,omitempty"`
	Cursor   *session.CatalogCursor `json:"cursor,omitempty"`
	Limit    int                    `json:"limit"`
	MaxBytes int                    `json:"max_bytes"`
}
