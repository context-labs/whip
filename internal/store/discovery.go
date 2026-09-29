package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

type TreeList struct {
	Search           string
	After            session.TreeID
	Archived, Pinned *bool
	Limit            int
	ExpectedRevision *session.Revision
}

type TreePage struct {
	Revision session.Revision
	Items    []session.TreeSummary
	Next     *session.TreeID
}

// Trees reads the catalog revision and bounded metadata in one SQL snapshot.
// A captured revision rejects changed membership or metadata across page calls.
// Cursors remain ID keysets; callers restart from the beginning after a conflict.
func (s *Store) Trees(ctx context.Context, request TreeList) (TreePage, error) {
	result := TreePage{Items: []session.TreeSummary{}}
	if len(request.Search) > 256 || !utf8.ValidString(request.Search) || strings.ContainsRune(request.Search, 0) {
		return result, session.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pageLimit(request.Limit); err != nil {
		return result, err
	}
	if request.After != "" {
		if err := session.ValidateID(string(request.After)); err != nil {
			return result, err
		}
	}
	if request.ExpectedRevision != nil && *request.ExpectedRevision < 1 {
		return result, session.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `WITH page AS (
 SELECT t.id,t.metadata,t.engine,t.revision,t.created_at,s.id AS root_id,c.working_directory
 FROM session_trees t JOIN sessions s ON s.tree_id=t.id AND s.parent_id IS NULL
 JOIN session_configurations c ON c.session_id=s.id AND c.revision=s.config_revision
 WHERE t.id>? AND (? IS NULL OR json_extract(t.metadata,'$.archived')=?)
 AND (? IS NULL OR json_extract(t.metadata,'$.pinned')=?)
 AND (?='' OR instr(lower(COALESCE(json_extract(t.metadata,'$.title'),'')),lower(?))>0 OR instr(lower(s.id),lower(?))>0 OR instr(lower(t.id),lower(?))>0 OR instr(lower(c.working_directory),lower(?))>0)
 AND (? IS NULL OR ?=(SELECT revision FROM tree_catalog WHERE singleton=1))
 ORDER BY t.id LIMIT ?)
 SELECT c.revision,COALESCE(p.id,''),COALESCE(p.metadata,''),COALESCE(p.engine,''),
 COALESCE(p.revision,0),COALESCE(p.created_at,0),COALESCE(p.root_id,''),COALESCE(p.working_directory,'')
 FROM tree_catalog c LEFT JOIN page p ON TRUE WHERE c.singleton=1 ORDER BY p.id`,
		request.After, request.Archived, request.Archived, request.Pinned, request.Pinned,
		request.Search, request.Search, request.Search, request.Search, request.Search,
		request.ExpectedRevision, request.ExpectedRevision, request.Limit+1)
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
		if err := rows.Scan(&result.Revision, &value.ID, &raw, &value.Engine, &value.Revision, &created, &value.RootID, &value.WorkingDirectory); err != nil {
			return result, err
		}
		if request.ExpectedRevision != nil && *request.ExpectedRevision != result.Revision {
			return result, ErrConflict
		}
		if value.ID == "" {
			break
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
