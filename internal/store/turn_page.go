package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/context-labs/whip/internal/session"
)

// TurnPage reads one owner's metadata without inputs, messages, or worker
// hydration. A cursor names an immutable row in that owner; states remain live.
func (s *Store) TurnPage(ctx context.Context, owner session.SessionID, before session.TurnID, limit int) (session.TurnPage, error) {
	page := session.TurnPage{Items: []session.Turn{}}
	if err := session.ValidateID(string(owner)); err != nil {
		return page, err
	}
	if before != "" {
		if err := session.ValidateID(string(before)); err != nil {
			return page, err
		}
	}
	if err := pageLimit(limit); err != nil {
		return page, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer func() { _ = tx.Rollback() }()
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sessions WHERE id=?)", owner).Scan(&exists); err != nil {
		return page, err
	}
	if !exists {
		return page, ErrNotFound
	}
	query := "SELECT " + turnColumns + " FROM turns WHERE session_id=?"
	args := []any{owner}
	if before != "" {
		var started int64
		if err := tx.QueryRowContext(ctx, "SELECT started_at FROM turns WHERE id=? AND session_id=?", before, owner).Scan(&started); err != nil {
			return page, found(err)
		}
		query += " AND (started_at,id)<(?,?)"
		args = append(args, started, before)
	}
	query += " ORDER BY started_at DESC,id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if len(page.Items) == limit {
			page.NextCursor = new(page.Items[len(page.Items)-1].ID)
			break
		}
		turn, err := scanTurn(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, turn)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if err := rows.Close(); err != nil {
		return page, err
	}
	if err := tx.Commit(); err != nil {
		return page, err
	}
	return page, nil
}
