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
	rows, err := s.db.QueryContext(ctx, `SELECT i.session_id FROM inputs i JOIN sessions s ON s.id=i.session_id
 WHERE s.lifecycle='active' AND i.turn_id IS NULL AND i.cancelled_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM turns t WHERE t.session_id=s.id AND t.state IN ('running','cancelling'))
 GROUP BY i.session_id ORDER BY min(i.ordinal) LIMIT ?`, limit)
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
