package session

import "context"

// CheckHistoryRevision rejects actions based on a transcript that has since
// been destructively edited. Call under root admission before applying a cut.
func (s *Store) CheckHistoryRevision(ctx context.Context, rootID string, expected int64) error {
	var actual int64
	if err := s.db.QueryRowContext(ctx, `SELECT history_revision FROM sessions WHERE id=?`, rootID).Scan(&actual); err != nil {
		return err
	}
	if actual != expected {
		return ErrHistoryRevision
	}
	return nil
}
