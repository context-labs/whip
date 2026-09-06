package session

import (
	"context"
	"encoding/json"
	"errors"
)

type CatalogCursor struct {
	Revision int64 `json:"revision,string"`
	Offset   int64 `json:"offset,string"`
}
type CatalogRevision struct {
	Revision int64 `json:"revision,string"`
}
type SessionSummary struct {
	ID        string      `json:"id"`
	Kind      SessionKind `json:"kind"`
	Title     string      `json:"title"`
	Model     string      `json:"model"`
	Provider  string      `json:"provider"`
	CWD       string      `json:"cwd"`
	Pinned    bool        `json:"pinned"`
	UpdatedAt string      `json:"updated_at"`
	Truncated bool        `json:"truncated"`
}
type CatalogPageOptions struct {
	Cursor          *CatalogCursor
	Limit, MaxBytes int
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
	if opts.Limit < 1 || opts.Limit > 128 || opts.MaxBytes < 4096 || opts.MaxBytes > 512<<10 {
		return p, errors.New("catalog requires limit 1..128 and max_bytes 4096..524288")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT catalog_revision FROM runtime_schema WHERE id=1`).Scan(&p.Revision); err != nil {
		return p, err
	}
	var offset int64
	if opts.Cursor != nil {
		if opts.Cursor.Revision != p.Revision || opts.Cursor.Offset < 0 {
			return p, ErrCollectionChanged
		}
		offset = opts.Cursor.Offset
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,substr(title,1,128),substr(model,1,128),substr(provider,1,128),substr(cwd,1,128),pinned,updated_at,length(title)>128 OR length(model)>128 OR length(provider)>128 OR length(cwd)>128 FROM sessions ORDER BY pinned DESC,updated_at DESC,id LIMIT ? OFFSET ?`, opts.Limit+1, offset)
	if err != nil {
		return p, err
	}
	for rows.Next() {
		var item SessionSummary
		if err = rows.Scan(&item.ID, &item.Kind, &item.Title, &item.Model, &item.Provider, &item.CWD, &item.Pinned, &item.UpdatedAt, &item.Truncated); err != nil {
			rows.Close()
			return p, err
		}
		if len(p.Items) == opts.Limit {
			p.HasMore = true
			break
		}
		p.Items = append(p.Items, item)
		p.NextCursor = &CatalogCursor{Revision: p.Revision, Offset: offset + int64(len(p.Items))}
		raw, err := json.Marshal(p)
		if err != nil {
			rows.Close()
			return p, err
		}
		if len(raw) > opts.MaxBytes {
			p.Items = p.Items[:len(p.Items)-1]
			p.HasMore = true
			break
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return p, err
	}
	if len(p.Items) == 0 && offset > 0 {
		return p, ErrCollectionChanged
	}
	if p.HasMore {
		p.NextCursor = &CatalogCursor{Revision: p.Revision, Offset: offset + int64(len(p.Items))}
	} else {
		p.NextCursor = nil
	}
	if len(p.Items) == 0 && p.HasMore {
		return p, errors.New("catalog item exceeds presentation budget")
	}
	return p, tx.Commit()
}
