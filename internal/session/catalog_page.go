package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
)

type CatalogCursor struct {
	Revision int64  `json:"revision,string"`
	Offset   int64  `json:"offset,string"`
	Search   string `json:"search,omitempty"`
}
type CatalogRevision struct {
	Revision int64 `json:"revision,string"`
}
type SessionSummary struct {
	ID          string      `json:"id"`
	Kind        SessionKind `json:"kind"`
	Title       string      `json:"title"`
	Model       string      `json:"model"`
	Provider    string      `json:"provider"`
	CWD         string      `json:"cwd"`
	WorkspaceID string      `json:"workspace_id,omitempty"`
	Pinned      bool        `json:"pinned"`
	UpdatedAt   string      `json:"updated_at"`
	Truncated   bool        `json:"truncated"`
}
type CatalogPageOptions struct {
	Cursor          *CatalogCursor
	Limit, MaxBytes int
	Search          string
}
type SessionCatalogPage struct {
	Revision   int64            `json:"revision,string"`
	Items      []SessionSummary `json:"items"`
	NextCursor *CatalogCursor   `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more"`
}

func (s *Store) SessionCatalogRevision(ctx context.Context) (CatalogRevision, error) {
	var r CatalogRevision
	err := s.db.QueryRowContext(ctx, `SELECT catalog_revision FROM runtime_schema WHERE id=1`).Scan(&r.Revision)
	return r, err
}

// SessionCatalog reads bounded metadata directly, without opening runtime roots.
// Truncated is explicit; complete fields remain available through root views.
func (s *Store) SessionCatalog(ctx context.Context, opts CatalogPageOptions) (SessionCatalogPage, error) {
	p := SessionCatalogPage{Items: []SessionSummary{}}
	if opts.Limit < 1 || opts.Limit > 128 || opts.MaxBytes < 4096 || opts.MaxBytes > 512<<10 || len(opts.Search) > 256 {
		return p, errors.New("catalog requires limit 1..128 and max_bytes 4096..524288")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = tx.QueryRowContext(ctx, `SELECT catalog_revision FROM runtime_schema WHERE id=1`).Scan(&p.Revision); err != nil {
		return p, err
	}
	var offset int64
	if opts.Cursor != nil {
		if opts.Cursor.Revision != p.Revision || opts.Cursor.Offset < 0 || opts.Cursor.Search != opts.Search {
			return p, ErrCollectionChanged
		}
		offset = opts.Cursor.Offset
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,substr(title,1,128),substr(model,1,128),substr(provider,1,128),substr(cwd,1,4096),pinned,updated_at,length(title)>128 OR length(model)>128 OR length(provider)>128 OR length(cwd)>128,length(cwd)>4096 FROM sessions
 WHERE ?='' OR instr(lower(title),lower(?))>0 OR instr(lower(cwd),lower(?))>0
 ORDER BY pinned DESC,updated_at DESC,id LIMIT ? OFFSET ?`, opts.Search, opts.Search, opts.Search, opts.Limit+1, offset)
	if err != nil {
		return p, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item SessionSummary
		var pathTruncated bool
		if err = rows.Scan(&item.ID, &item.Kind, &item.Title, &item.Model, &item.Provider, &item.CWD, &item.Pinned, &item.UpdatedAt, &item.Truncated, &pathTruncated); err != nil {
			return p, err
		}
		if !pathTruncated {
			item.WorkspaceID = fmt.Sprintf("%x", sha256.Sum256([]byte(item.CWD)))
		}
		if path := []rune(item.CWD); len(path) > 128 {
			item.CWD = string(path[:128])
		}
		if len(p.Items) == opts.Limit {
			p.HasMore = true
			break
		}
		p.Items = append(p.Items, item)
		p.NextCursor = &CatalogCursor{Revision: p.Revision, Offset: offset + int64(len(p.Items)), Search: opts.Search}
		raw, err := json.Marshal(p)
		if err != nil {
			return p, err
		}
		if len(raw) > opts.MaxBytes {
			p.Items = p.Items[:len(p.Items)-1]
			p.HasMore = true
			break
		}
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return p, err
	}
	if len(p.Items) == 0 && offset > 0 {
		return p, ErrCollectionChanged
	}
	if p.HasMore {
		p.NextCursor = &CatalogCursor{Revision: p.Revision, Offset: offset + int64(len(p.Items)), Search: opts.Search}
	} else {
		p.NextCursor = nil
	}
	if len(p.Items) == 0 && p.HasMore {
		return p, errors.New("catalog item exceeds presentation budget")
	}
	return p, tx.Commit()
}
