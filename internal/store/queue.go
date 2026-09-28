package store

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

// QueuedSessions returns eligible work in admission order. Reading this list
// does not claim input; Claim remains the single atomic execution boundary.
func (s *Store) QueuedSessions(ctx context.Context, limit int) ([]session.SessionID, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `WITH ready(session_id,admitted_at) AS (
 SELECT session_id,min(created_at) FROM inputs WHERE turn_id IS NULL AND cancelled_at IS NULL GROUP BY session_id
 UNION ALL
 SELECT m.recipient_id,min(r.available_at) FROM mail m JOIN mail_revisions r ON r.mail_id=m.id AND r.revision=m.revision
 WHERE m.state='pending' AND m.deleted_at IS NULL AND r.delivery<>'next_turn' AND r.available_at<=? AND `+mailRetryAllowed+`
 GROUP BY m.recipient_id
 ) SELECT ready.session_id FROM ready JOIN sessions s ON s.id=ready.session_id WHERE s.lifecycle='active'
 AND NOT EXISTS(SELECT 1 FROM turns t WHERE t.session_id=s.id AND t.state IN ('running','cancelling'))
 GROUP BY ready.session_id ORDER BY min(ready.admitted_at),ready.session_id LIMIT ?`, now(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.SessionID{}
	for rows.Next() {
		var id session.SessionID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}
