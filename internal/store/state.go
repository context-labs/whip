package store

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/context-labs/whip/internal/session"
)

const stateSelect = `SELECT v.id,v.tree_id,v.session_id,v.key,v.revision,v.author_id,v.digest,b.size,v.created_at
 FROM state_versions v JOIN content_bodies b ON b.digest=v.digest`

func scanState(row scanner) (value session.StateValue, err error) {
	var created int64
	err = row.Scan(&value.ID, &value.TreeID, &value.SessionID, &value.Key, &value.Revision, &value.AuthorID, &value.Digest, &value.Size, &created)
	value.CreatedAt = timestamp(created)
	return value, found(err)
}

func stateOwner(ctx context.Context, q querier, actor session.SessionID, scope session.StateScope) (session.TreeID, *session.SessionID, error) {
	if err := scope.Validate(); err != nil {
		return "", nil, err
	}
	current, err := readSession(ctx, q, actor)
	if err != nil {
		return "", nil, err
	}
	if scope == session.SessionState {
		return current.TreeID, &current.ID, nil
	}
	return current.TreeID, nil, nil
}

func readState(ctx context.Context, q querier, tree session.TreeID, owner *session.SessionID, key string) (session.StateValue, error) {
	return scanState(q.QueryRowContext(ctx, stateSelect+" WHERE v.tree_id=? AND v.session_id IS ? AND v.key=? ORDER BY v.revision DESC LIMIT 1", tree, owner, key))
}

func (s *Store) State(ctx context.Context, actor session.SessionID, scope session.StateScope, key string) (result session.StateValue, err error) {
	if err := session.ValidateStateKey(key); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		tree, owner, err := stateOwner(ctx, tx, actor, scope)
		if err != nil {
			return err
		}
		result, err = readState(ctx, tx, tree, owner, key)
		return err
	})
	return
}

// StateValue verifies handle ownership before exposing body metadata. Old
// versions remain valid handles even after the current key changes.
func (s *Store) StateValue(ctx context.Context, actor session.SessionID, id string) (result session.StateValue, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, err = stateValue(ctx, tx, actor, id)
		return err
	})
	return
}

func stateValue(ctx context.Context, q querier, actor session.SessionID, id string) (session.StateValue, error) {
	current, err := readSession(ctx, q, actor)
	if err != nil {
		return session.StateValue{}, err
	}
	return scanState(q.QueryRowContext(ctx, stateSelect+" WHERE v.id=? AND v.tree_id=? AND (v.session_id IS NULL OR v.session_id=?)", id, current.TreeID, actor))
}

func (s *Store) WriteState(ctx context.Context, request session.StateWrite) (result session.StateValue, err error) {
	if err := request.Validate(); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, err = writeState(ctx, tx, request)
		return err
	})
	return
}

func writeState(ctx context.Context, tx *sql.Tx, request session.StateWrite) (session.StateValue, error) {
	tree, owner, err := stateOwner(ctx, tx, request.SessionID, request.Scope)
	if err != nil {
		return session.StateValue{}, err
	}
	existing, err := scanState(tx.QueryRowContext(ctx, stateSelect+" WHERE v.id=?", request.ID))
	if err == nil {
		sameOwner := existing.SessionID == nil && owner == nil || existing.SessionID != nil && owner != nil && *existing.SessionID == *owner
		if !sameOwner || existing.TreeID != tree || existing.AuthorID != request.SessionID || existing.Key != request.Key || existing.Revision-1 != request.ExpectedRevision || existing.Digest != request.Digest || existing.Size != request.Size {
			return session.StateValue{}, ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return session.StateValue{}, err
	}
	current, err := readState(ctx, tx, tree, owner, request.Key)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return session.StateValue{}, err
	}
	if current.Revision != request.ExpectedRevision {
		return session.StateValue{}, ErrConflict
	}
	if current.Revision == math.MaxInt64 {
		return session.StateValue{}, ErrLimit
	}
	var count, total int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(b.size),0) FROM state_versions v
 JOIN content_bodies b ON b.digest=v.digest WHERE v.tree_id=?`, tree).Scan(&count, &total); err != nil {
		return session.StateValue{}, err
	}
	if count >= session.MaxStateVersions || total > session.MaxTreeStateBytes-request.Size {
		return session.StateValue{}, ErrLimit
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO content_bodies VALUES (?,?) ON CONFLICT DO NOTHING", request.Digest, request.Size); err != nil {
		return session.StateValue{}, err
	}
	var size int64
	if err := tx.QueryRowContext(ctx, "SELECT size FROM content_bodies WHERE digest=?", request.Digest).Scan(&size); err != nil {
		return session.StateValue{}, err
	}
	if size != request.Size {
		return session.StateValue{}, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO state_versions VALUES (?,?,?,?,?,?,?,?)", request.ID, tree, owner, request.Key, current.Revision+1, request.SessionID, request.Digest, now()); err != nil {
		return session.StateValue{}, err
	}
	value, err := scanState(tx.QueryRowContext(ctx, stateSelect+" WHERE v.id=?", request.ID))
	if err != nil {
		return value, err
	}
	if request.Scope == session.TreeState {
		err = notifyState(ctx, tx, value)
	}
	return value, err
}

func (s *Store) StateHistory(ctx context.Context, actor session.SessionID, scope session.StateScope, key string, after int64, limit int) (result []session.StateValue, err error) {
	if err := session.ValidateStateKey(key); err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, session.ErrInvalid
	}
	return s.statePage(ctx, actor, scope, key, "", after, limit)
}

func (s *Store) ListState(ctx context.Context, actor session.SessionID, scope session.StateScope, after string, limit int) ([]session.StateValue, error) {
	return s.statePage(ctx, actor, scope, "", after, 0, limit)
}

func (s *Store) statePage(ctx context.Context, actor session.SessionID, scope session.StateScope, key, afterKey string, afterRevision int64, limit int) (result []session.StateValue, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, err = listState(ctx, tx, actor, scope, key, afterKey, afterRevision, limit)
		return err
	})
	return
}

func listState(ctx context.Context, q querier, actor session.SessionID, scope session.StateScope, key, afterKey string, afterRevision int64, limit int) ([]session.StateValue, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	tree, owner, err := stateOwner(ctx, q, actor, scope)
	if err != nil {
		return nil, err
	}
	query := stateSelect + ` WHERE v.tree_id=? AND v.session_id IS ? AND v.key>? AND NOT EXISTS (
 SELECT 1 FROM state_versions n WHERE n.tree_id=v.tree_id AND n.session_id IS v.session_id AND n.key=v.key AND n.revision>v.revision) ORDER BY v.key LIMIT ?`
	args := []any{tree, owner, afterKey, limit}
	if key != "" {
		query = stateSelect + " WHERE v.tree_id=? AND v.session_id IS ? AND v.key=? AND v.revision>? ORDER BY v.revision LIMIT ?"
		args = []any{tree, owner, key, afterRevision, limit}
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.StateValue{}
	for rows.Next() {
		value, err := scanState(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
