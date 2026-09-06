package session

import "context"

// RootAgentViews reads the recursive tree without loading transcripts or stream
// history. Large trees are also available through RootCollectionPage.
func (s *Store) RootAgentViews(ctx context.Context, rootID string) ([]RuntimeAgent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	snapshot := RootSnapshot{RootID: rootID}
	if err := readSnapshotAgents(ctx, tx, rootID, &snapshot); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT agent_id FROM inbox WHERE root_id=? AND status='running'`, rootID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		item := InboxItem{Status: "running"}
		if err := rows.Scan(&item.AgentID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		snapshot.Inbox = append(snapshot.Inbox, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT DISTINCT agent_id FROM permission_requests WHERE root_id=? AND status='pending'`, rootID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var permission PermissionSnapshot
		if err := rows.Scan(&permission.AgentID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		snapshot.Permissions = append(snapshot.Permissions, permission)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	deriveSnapshotAgentState(&snapshot)
	return snapshot.Agents, tx.Commit()
}
