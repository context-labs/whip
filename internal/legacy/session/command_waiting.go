package session

import "context"

// CommandPermissionWaiting derives the human gate from the same durable
// operation records used by capability admission. Unrelated child permissions
// do not label a root command as waiting.
func (s *Store) CommandPermissionWaiting(ctx context.Context, record CommandRecord) (bool, error) {
	var waiting bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM permission_requests p WHERE p.root_id=? AND p.agent_id=? AND p.status='pending')`, record.RootID, record.RootID).Scan(&waiting)
	return waiting, err
}
