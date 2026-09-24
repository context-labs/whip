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
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		item := InboxItem{Status: "running"}
		if err := rows.Scan(&item.AgentID); err != nil {
			return nil, err
		}
		snapshot.Inbox = append(snapshot.Inbox, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	permissionRows, err := tx.QueryContext(ctx, `SELECT DISTINCT agent_id FROM permission_requests WHERE root_id=? AND status='pending'`, rootID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = permissionRows.Close() }()
	for permissionRows.Next() {
		var permission PermissionSnapshot
		if err := permissionRows.Scan(&permission.AgentID); err != nil {
			return nil, err
		}
		snapshot.Permissions = append(snapshot.Permissions, permission)
	}
	if err := permissionRows.Err(); err != nil {
		return nil, err
	}
	if err := permissionRows.Close(); err != nil {
		return nil, err
	}
	deriveSnapshotAgentState(&snapshot)
	return snapshot.Agents, tx.Commit()
}
