package session

import (
	"context"
	"encoding/json"
	"errors"
)

type AttentionRoot struct {
	RootID             string
	Title              string
	ActiveAgents       int64
	PendingPermissions int64
}

// AttentionRoots reads only navigation metadata. Live question IDs are supplied
// by the daemon because questions end with their running turn, including restart.
func (s *Store) AttentionRoots(ctx context.Context, afterID string, questionRoots []string, limit int) ([]AttentionRoot, error) {
	if limit < 1 || limit > 129 || len(questionRoots) > 10000 || len(afterID) > 256 {
		return nil, errors.New("invalid attention bounds")
	}
	questions, err := json.Marshal(questionRoots)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,substr(s.title,1,128),
 (SELECT COUNT(*) FROM agents a WHERE a.root_id=s.id AND (a.status='running' OR EXISTS(SELECT 1 FROM inbox i WHERE i.root_id=s.id AND i.agent_id=a.id AND i.status='queued'))),
 (SELECT COUNT(*) FROM permission_requests p WHERE p.root_id=s.id AND p.status='pending')
 FROM sessions s WHERE s.id>? AND (
 EXISTS(SELECT 1 FROM agents a WHERE a.root_id=s.id AND a.status='running') OR
 EXISTS(SELECT 1 FROM inbox i WHERE i.root_id=s.id AND i.status='queued') OR
 EXISTS(SELECT 1 FROM permission_requests p WHERE p.root_id=s.id AND p.status='pending') OR
 s.id IN (SELECT value FROM json_each(?))) ORDER BY s.id LIMIT ?`, afterID, string(questions), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []AttentionRoot{}
	for rows.Next() {
		var item AttentionRoot
		if err := rows.Scan(&item.RootID, &item.Title, &item.ActiveAgents, &item.PendingPermissions); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
