package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/session"
)

// CellPage follows immutable cell ordinals, never lexical provider/cell IDs.
// A before cursor is scoped to the exact turn; settled states may update in place.
func (s *Store) CellPage(ctx context.Context, turn session.TurnID, before session.CellID, limit int) (session.CellPage, error) {
	page := session.CellPage{Items: []session.Cell{}}
	if err := session.ValidateID(string(turn)); err != nil {
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
	if _, err := readTurn(ctx, tx, turn); err != nil {
		return page, err
	}
	query := cellSelect + " WHERE c.turn_id=?"
	args := []any{turn}
	if before != "" {
		var ordinal int64
		if err := tx.QueryRowContext(ctx, "SELECT ordinal FROM cells WHERE id=? AND turn_id=?", before, turn).Scan(&ordinal); err != nil {
			return page, found(err)
		}
		query += " AND c.ordinal<?"
		args = append(args, ordinal)
	}
	query += " ORDER BY c.ordinal DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	size := 0
	for rows.Next() {
		value, err := scanCell(rows)
		if err != nil {
			return page, err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return page, err
		}
		size += len(raw)
		if len(page.Items) == limit || size > MaxPageBytes {
			if len(page.Items) == 0 {
				return page, ErrLimit
			}
			page.NextCursor = new(page.Items[len(page.Items)-1].ID)
			break
		}
		page.Items = append(page.Items, value)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if err := rows.Close(); err != nil {
		return page, err
	}
	return page, tx.Commit()
}
