package store

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

// QueueCursor is an advisory scan position, not a work receipt. Retaining it
// across bounded scheduler passes prevents a blocked subtree hiding later work.
type QueueCursor struct {
	ReadyAt   int64
	SessionID session.SessionID
}

// QueuedSessions pages eligible work in admission order. Claim still atomically
// checks lifecycle and capacity; readiness can change between reads and claims.
func (s *Store) QueuedSessions(ctx context.Context, after QueueCursor, limit int) ([]QueueCursor, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `WITH ready(session_id,admitted_at) AS (
 SELECT session_id,min(created_at) FROM inputs WHERE turn_id IS NULL AND cancelled_at IS NULL GROUP BY session_id
 UNION ALL
 SELECT m.recipient_id,min(r.available_at) FROM mail m JOIN mail_revisions r ON r.mail_id=m.id AND r.revision=m.revision
 WHERE m.state='pending' AND m.deleted_at IS NULL AND r.delivery<>'next_turn' AND r.available_at<=? AND `+mailRetryAllowed+`
 GROUP BY m.recipient_id
 UNION ALL
 SELECT d.session_id,d.created_at FROM automatic_title_decisions d JOIN session_trees tree ON tree.id=d.tree_id
 WHERE d.eligible=1 AND tree.revision=d.expected_revision
 AND NOT EXISTS(SELECT 1 FROM receipts r WHERE r.client_id='automatic-title' AND r.request_id=d.tree_id)
 ), candidates AS (
 SELECT ready.session_id,min(ready.admitted_at) AS ready_at FROM ready JOIN sessions s ON s.id=ready.session_id WHERE s.lifecycle='active'
 AND NOT EXISTS(SELECT 1 FROM turns t WHERE t.session_id=s.id AND t.state IN ('running','cancelling'))
 GROUP BY ready.session_id
 ) SELECT ready_at,session_id FROM candidates
 WHERE ? OR ready_at>? OR (ready_at=? AND session_id>?)
 ORDER BY ready_at,session_id LIMIT ?`, now(), after.SessionID == "", after.ReadyAt, after.ReadyAt, after.SessionID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []QueueCursor{}
	for rows.Next() {
		var value QueueCursor
		if err := rows.Scan(&value.ReadyAt, &value.SessionID); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
