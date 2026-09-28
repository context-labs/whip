package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"

	"github.com/context-labs/whip/internal/session"
)

type CreateTree struct {
	Metadata         session.TreeMetadata
	Engine           session.Engine
	Policy           session.TreePolicy
	Definition       session.DefinitionRef
	Defaults         session.Configuration
	Overrides        session.ConfigPatch
	WorkingDirectory string
}

func validMetadata(metadata session.TreeMetadata) error {
	if metadata.Title != nil {
		return session.ValidateText(*metadata.Title, 1024)
	}
	return nil
}

func (s *Store) CreateTree(ctx context.Context, request CreateTree) (tree session.Tree, root session.Session, err error) {
	if err := request.Engine.Validate(); err != nil {
		return tree, root, err
	}
	if err := request.Policy.Validate(); err != nil {
		return tree, root, err
	}
	if err := validMetadata(request.Metadata); err != nil {
		return tree, root, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		def, err := definition(ctx, tx, request.Definition)
		if err != nil {
			return err
		}
		config, err := session.Resolve(request.Defaults, def.Document, request.Overrides)
		if err != nil {
			return err
		}
		metadata, err := encode(request.Metadata)
		if err != nil {
			return err
		}
		policy, err := encode(request.Policy)
		if err != nil {
			return err
		}
		treeID := session.TreeID(newID("tree"))
		created := now()
		if _, err := tx.ExecContext(ctx, "INSERT INTO session_trees VALUES (?,?,?,?,1,?)", treeID, metadata, request.Engine, policy, created); err != nil {
			return err
		}
		root, err = insertSession(ctx, tx, treeID, nil, request.Definition, config, request.WorkingDirectory)
		if err != nil {
			return err
		}
		tree, err = readTree(ctx, tx, treeID)
		return err
	})
	return
}

func insertSession(ctx context.Context, tx *sql.Tx, tree session.TreeID, parent *session.SessionID, ref session.DefinitionRef, config session.Configuration, cwd string) (session.Session, error) {
	if !filepath.IsAbs(cwd) || session.ValidateText(cwd, 4096) != nil {
		return session.Session{}, fmt.Errorf("%w: working directory must be an absolute path", session.ErrInvalid)
	}
	for _, child := range config.Children {
		if _, err := definition(ctx, tx, child); err != nil {
			return session.Session{}, fmt.Errorf("child definition: %w", err)
		}
	}
	raw, err := encode(config)
	if err != nil {
		return session.Session{}, err
	}
	id := session.SessionID(newID("session"))
	created := now()
	if _, err := tx.ExecContext(ctx, "INSERT INTO sessions VALUES (?,?,?,?,?,1,?,'active',?)", id, tree, parent, ref.ID, ref.Revision, filepath.Clean(cwd), created); err != nil {
		return session.Session{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO session_configurations VALUES (?,1,?,?)", id, raw, created); err != nil {
		return session.Session{}, err
	}
	return readSession(ctx, tx, id)
}

type SpawnSession struct {
	ParentID session.SessionID
	// Nil inherits the parent's effective configuration and definition origin.
	// A specified revision applies its defaults before explicit overrides.
	Definition       *session.DefinitionRef
	Overrides        session.ConfigPatch
	WorkingDirectory string
}

func (s *Store) SpawnSession(ctx context.Context, request SpawnSession) (result session.Session, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		parent, err := readSession(ctx, tx, request.ParentID)
		if err != nil {
			return err
		}
		if parent.Lifecycle != session.Active {
			return ErrStopped
		}
		tree, err := readTree(ctx, tx, parent.TreeID)
		if err != nil {
			return err
		}
		var count, depth int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sessions WHERE tree_id=?", tree.ID).Scan(&count); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `WITH RECURSIVE ancestors(id,parent_id) AS (
   SELECT id,parent_id FROM sessions WHERE id=? UNION ALL
   SELECT s.id,s.parent_id FROM sessions s JOIN ancestors a ON a.parent_id=s.id
  ) SELECT count(*) FROM ancestors`, parent.ID).Scan(&depth); err != nil {
			return err
		}
		if count >= tree.Policy.MaxSessions || depth > tree.Policy.MaxDepth {
			return ErrLimit
		}
		ref := parent.Definition
		var doc session.DefinitionDocument
		if request.Definition != nil {
			ref = *request.Definition
			def, err := definition(ctx, tx, ref)
			if err != nil {
				return err
			}
			doc = def.Document
		}
		config, err := session.Resolve(parent.Config, doc, request.Overrides)
		if err != nil {
			return err
		}
		cwd := request.WorkingDirectory
		if cwd == "" {
			cwd = parent.WorkingDirectory
		}
		result, err = insertSession(ctx, tx, parent.TreeID, &parent.ID, ref, config, cwd)
		return err
	})
	return
}

const sessionSelect = `SELECT s.id,s.tree_id,s.parent_id,s.definition_id,s.definition_revision,
 s.config_revision,s.working_directory,s.lifecycle,s.created_at,c.configuration
 FROM sessions s JOIN session_configurations c ON c.session_id=s.id AND c.revision=s.config_revision`

type scanner interface{ Scan(...any) error }

func scanSession(row scanner) (result session.Session, err error) {
	var raw string
	var created int64
	err = row.Scan(&result.ID, &result.TreeID, &result.ParentID, &result.Definition.ID, &result.Definition.Revision,
		&result.ConfigRevision, &result.WorkingDirectory, &result.Lifecycle, &created, &raw)
	if err != nil {
		return result, found(err)
	}
	result.CreatedAt = timestamp(created)
	err = json.Unmarshal([]byte(raw), &result.Config)
	return
}

func readSession(ctx context.Context, q querier, id session.SessionID) (session.Session, error) {
	return scanSession(q.QueryRowContext(ctx, sessionSelect+" WHERE s.id=?", id))
}

func (s *Store) Session(ctx context.Context, id session.SessionID) (session.Session, error) {
	return readSession(ctx, s.db, id)
}

func (s *Store) Root(ctx context.Context, id session.TreeID) (session.Session, error) {
	return scanSession(s.db.QueryRowContext(ctx, sessionSelect+" WHERE s.tree_id=? AND s.parent_id IS NULL", id))
}

func (s *Store) Sessions(ctx context.Context, tree session.TreeID, after session.SessionID, limit int) ([]session.Session, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, sessionSelect+" WHERE s.tree_id=? AND s.id>? ORDER BY s.id LIMIT ?", tree, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.Session{}
	size := 0
	for rows.Next() {
		value, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		raw, err := encode(value)
		if err != nil {
			return nil, err
		}
		size += len(raw)
		if size > MaxPageBytes {
			break
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func readTree(ctx context.Context, q querier, id session.TreeID) (result session.Tree, err error) {
	var metadata, policy string
	var created int64
	err = q.QueryRowContext(ctx, "SELECT id,metadata,engine,policy,revision,created_at FROM session_trees WHERE id=?", id).
		Scan(&result.ID, &metadata, &result.Engine, &policy, &result.Revision, &created)
	if err != nil {
		return result, found(err)
	}
	result.CreatedAt = timestamp(created)
	if err = json.Unmarshal([]byte(metadata), &result.Metadata); err != nil {
		return
	}
	err = json.Unmarshal([]byte(policy), &result.Policy)
	return
}

func (s *Store) Tree(ctx context.Context, id session.TreeID) (session.Tree, error) {
	return readTree(ctx, s.db, id)
}

func (s *Store) UpdateTree(ctx context.Context, id session.TreeID, expected session.Revision, metadata session.TreeMetadata) (result session.Tree, err error) {
	if err := validMetadata(metadata); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		current, err := readTree(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Revision != expected || expected == math.MaxInt64 {
			return ErrConflict
		}
		raw, err := encode(metadata)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE session_trees SET metadata=?,revision=revision+1 WHERE id=?", raw, id); err != nil {
			return err
		}
		result, err = readTree(ctx, tx, id)
		return err
	})
	return
}

func (s *Store) Configuration(ctx context.Context, id session.SessionID, revision session.Revision) (result session.Configuration, err error) {
	var raw string
	err = s.db.QueryRowContext(ctx, "SELECT configuration FROM session_configurations WHERE session_id=? AND revision=?", id, revision).Scan(&raw)
	if err != nil {
		return result, found(err)
	}
	err = json.Unmarshal([]byte(raw), &result)
	return
}

func (s *Store) UpdateConfiguration(ctx context.Context, id session.SessionID, expected session.Revision, patch session.ConfigPatch) (result session.Session, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		current, err := readSession(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.ConfigRevision != expected || expected == math.MaxInt64 {
			return ErrConflict
		}
		config, err := session.Resolve(current.Config, session.DefinitionDocument{}, patch)
		if err != nil {
			return err
		}
		for _, child := range config.Children {
			if _, err := definition(ctx, tx, child); err != nil {
				return err
			}
		}
		raw, err := encode(config)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO session_configurations VALUES (?,?,?,?)", id, expected+1, raw, now()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE sessions SET config_revision=? WHERE id=?", expected+1, id); err != nil {
			return err
		}
		result, err = readSession(ctx, tx, id)
		return err
	})
	return
}
