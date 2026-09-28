package store

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/session"
)

type TreeList struct {
	After            session.TreeID
	Archived, Pinned *bool
	Limit            int
}

type TreePage struct {
	Items []session.TreeSummary
	Next  *session.TreeID
}

// Trees reads a live ID-ordered page. Cursors survive deletion, but do not freeze
// catalog membership or metadata across calls. Restart from the beginning to refresh.
func (s *Store) Trees(ctx context.Context, request TreeList) (TreePage, error) {
	result := TreePage{Items: []session.TreeSummary{}}
	if err := pageLimit(request.Limit); err != nil {
		return result, err
	}
	if request.After != "" {
		if err := session.ValidateID(string(request.After)); err != nil {
			return result, err
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.metadata,t.engine,t.revision,t.created_at,s.id
 FROM session_trees t JOIN sessions s ON s.tree_id=t.id AND s.parent_id IS NULL
 WHERE t.id>? AND (? IS NULL OR json_extract(t.metadata,'$.archived')=?)
 AND (? IS NULL OR json_extract(t.metadata,'$.pinned')=?) ORDER BY t.id LIMIT ?`,
		request.After, request.Archived, request.Archived, request.Pinned, request.Pinned, request.Limit+1)
	if err != nil {
		return result, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if len(result.Items) == request.Limit {
			result.Next = new(result.Items[len(result.Items)-1].ID)
			break
		}
		var value session.TreeSummary
		var raw string
		var created int64
		if err := rows.Scan(&value.ID, &raw, &value.Engine, &value.Revision, &created, &value.RootID); err != nil {
			return result, err
		}
		if err := json.Unmarshal([]byte(raw), &value.Metadata); err != nil {
			return result, err
		}
		value.CreatedAt = timestamp(created)
		result.Items = append(result.Items, value)
	}
	return result, rows.Err()
}

type DefinitionPage struct {
	Items []session.DefinitionSummary
	Next  *session.DefinitionRef
}

// DefinitionSummaries omits configuration bodies and includes every immutable
// revision. A hash is identity, not chronology; callers select an explicit ref.
func (s *Store) DefinitionSummaries(ctx context.Context, after *session.DefinitionRef, limit int) (DefinitionPage, error) {
	result := DefinitionPage{Items: []session.DefinitionSummary{}}
	if err := pageLimit(limit); err != nil {
		return result, err
	}
	cursor := session.DefinitionRef{}
	if after != nil {
		if err := after.Validate(); err != nil {
			return result, err
		}
		cursor = *after
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,revision,json_extract(document,'$.name'),created_at
 FROM definition_revisions WHERE (id,revision)>(?,?) ORDER BY id,revision LIMIT ?`, cursor.ID, cursor.Revision, limit+1)
	if err != nil {
		return result, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if len(result.Items) == limit {
			result.Next = new(result.Items[len(result.Items)-1].Ref)
			break
		}
		var value session.DefinitionSummary
		var created int64
		if err := rows.Scan(&value.Ref.ID, &value.Ref.Revision, &value.Name, &created); err != nil {
			return result, err
		}
		value.CreatedAt = timestamp(created)
		result.Items = append(result.Items, value)
	}
	return result, rows.Err()
}
