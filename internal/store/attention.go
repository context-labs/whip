package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/session"
)

// Attention shares Activity's exact owner counts and a bounded SQL snapshot.
// A live turn includes its human waits even while execution permits are yielded.
// Stopped owners remain visible when they retain queued inputs or claimed work.
func (s *Store) Attention(ctx context.Context, after *session.AttentionCursor, limit, maxBytes int) (session.AttentionPage, error) {
	result := session.AttentionPage{Items: []session.AttentionItem{}}
	if err := pageLimit(limit); err != nil {
		return result, err
	}
	if maxBytes < 4096 || maxBytes > 512<<10 {
		return result, session.ErrInvalid
	}
	cursor := session.AttentionCursor{}
	if after != nil {
		cursor = *after
		if err := session.ValidateID(string(cursor.TreeID)); err != nil {
			return result, err
		}
		if err := session.ValidateID(string(cursor.SessionID)); err != nil {
			return result, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT s.tree_id,r.id,s.id,json_extract(t.metadata,'$.title')
 FROM sessions s JOIN session_trees t ON t.id=s.tree_id JOIN sessions r ON r.tree_id=s.tree_id AND r.parent_id IS NULL
 WHERE (s.tree_id,s.id)>(?,?) AND (
 EXISTS(SELECT 1 FROM turns WHERE session_id=s.id AND state IN ('running','cancelling'))
 OR EXISTS(SELECT 1 FROM inputs WHERE session_id=s.id AND turn_id IS NULL AND steered_turn_id IS NULL AND cancelled_at IS NULL)
 OR EXISTS(SELECT 1 FROM workspace_actions WHERE session_id=s.id AND state='claimed'))
 ORDER BY s.tree_id,s.id LIMIT ?`, cursor.TreeID, cursor.SessionID, limit+1)
	if err != nil {
		return result, err
	}
	defer func() { _ = rows.Close() }()
	var selected []session.AttentionItem
	for rows.Next() {
		var item session.AttentionItem
		if err := rows.Scan(&item.TreeID, &item.RootID, &item.SessionID, &item.Title); err != nil {
			_ = rows.Close()
			return result, err
		}
		selected = append(selected, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return result, err
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	used := 512
	for _, item := range selected {
		if len(result.Items) == limit {
			last := result.Items[len(result.Items)-1]
			result.NextCursor = &session.AttentionCursor{TreeID: last.TreeID, SessionID: last.SessionID}
			break
		}
		item.Activity, err = readActivity(ctx, tx, item.SessionID)
		if err != nil {
			return result, err
		}
		// Reserve for snake_case protocol keys and quoted counters beyond Go's
		// internal value encoding. Titles dominate variable presentation bytes.
		raw, err := json.Marshal(item)
		if err != nil {
			return result, err
		}
		if used+len(raw)+256 > maxBytes {
			if len(result.Items) == 0 {
				return result, fmt.Errorf("%w: attention item exceeds page byte budget", ErrLimit)
			}
			last := result.Items[len(result.Items)-1]
			result.NextCursor = &session.AttentionCursor{TreeID: last.TreeID, SessionID: last.SessionID}
			break
		}
		used += len(raw) + 256
		result.Items = append(result.Items, item)
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
