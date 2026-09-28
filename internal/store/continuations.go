package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

func readContinuation(ctx context.Context, q querier, id session.MessageID) (*session.ModelContinuation, error) {
	var raw *string
	if err := q.QueryRowContext(ctx, "SELECT model_continuation FROM messages WHERE id=?", id).Scan(&raw); err != nil {
		return nil, found(err)
	}
	return decodeContinuation(raw)
}

//nolint:nilnil // No private provider state is a valid message value.
func decodeContinuation(raw *string) (*session.ModelContinuation, error) {
	if raw == nil {
		return nil, nil
	}
	var result session.ModelContinuation
	if err := json.Unmarshal([]byte(*raw), &result); err != nil {
		return nil, err
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return &result, nil
}

// Continuations is a host-only projection for an already selected ordinary
// context. Public history queries never select this column. Check lengths in a
// consistent snapshot before reading any blobs; overflow returns no partial map.
func (s *Store) Continuations(ctx context.Context, owner session.SessionID, ids []session.MessageID) (map[session.MessageID]session.ModelContinuation, error) {
	if len(ids) > 100 {
		return nil, fmt.Errorf("%w: too many continuation identities", session.ErrInvalid)
	}
	result := map[session.MessageID]session.ModelContinuation{}
	if len(ids) == 0 {
		return result, nil
	}
	seen := map[session.MessageID]bool{}
	for _, id := range ids {
		if session.ValidateID(string(id)) != nil || seen[id] {
			return nil, fmt.Errorf("%w: invalid continuation identities", session.ErrInvalid)
		}
		seen[id] = true
	}
	identities, err := encode(ids)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT id,COALESCE(length(CAST(model_continuation AS BLOB)),0) FROM messages
 WHERE session_id=? AND retired_revision IS NULL AND id IN (SELECT value FROM json_each(?))`, owner, identities)
	if err != nil {
		return nil, err
	}
	size, count := 0, 0
	for rows.Next() {
		var id session.MessageID
		var length int
		if err := rows.Scan(&id, &length); err != nil {
			_ = rows.Close()
			return nil, err
		}
		count++
		size += length
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if count != len(ids) {
		return nil, ErrNotFound
	}
	if size > MaxPageBytes {
		return nil, session.ErrContinuationLimit
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,model_continuation FROM messages
 WHERE session_id=? AND retired_revision IS NULL AND id IN (SELECT value FROM json_each(?))`, owner, identities)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id session.MessageID
		var raw *string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		value, err := decodeContinuation(raw)
		if err != nil {
			return nil, err
		}
		if value != nil {
			result[id] = *value
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
