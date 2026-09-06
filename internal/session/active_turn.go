package session

import (
	"context"
	"database/sql"
	"errors"
)

// ActiveTurn returns the exact durable cancellation target for one agent.
func (s *Store) ActiveTurn(ctx context.Context, rootID, agentID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM turns WHERE root_id=? AND agent_id=? AND status='running'`, rootID, agentID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func readSnapshotTurns(ctx context.Context, tx *sql.Tx, snapshot *RootSnapshot) error {
	snapshot.ActiveTurns = map[string]string{}
	rows, err := tx.QueryContext(ctx, `SELECT agent_id,id FROM turns WHERE root_id=? AND status='running' ORDER BY agent_id LIMIT ?`, snapshot.RootID, snapshot.collectionLimit())
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var agentID, turnID string
		if err := rows.Scan(&agentID, &turnID); err != nil {
			return err
		}
		snapshot.ActiveTurns[agentID] = turnID
	}
	return rows.Err()
}
