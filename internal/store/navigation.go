package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/session"
)

// TreeSummaries reads exact root identities, metadata and all descendant activity
// in one bounded SQL snapshot. Missing roots include child IDs; no body is loaded.
func (s *Store) TreeSummaries(ctx context.Context, roots []session.SessionID) (session.TreeNavigationPage, error) {
	result := session.TreeNavigationPage{Items: []session.TreeNavigation{}, Missing: []session.SessionID{}}
	if len(roots) < 1 || len(roots) > 64 {
		return result, session.ErrInvalid
	}
	wanted := make(map[session.SessionID]bool, len(roots))
	for _, id := range roots {
		if err := session.ValidateID(string(id)); err != nil {
			return result, err
		}
		if wanted[id] {
			return result, session.ErrInvalid
		}
		wanted[id] = true
	}
	raw, err := json.Marshal(roots)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `WITH selected AS (
 SELECT r.id,r.tree_id,r.config_revision FROM sessions r
 WHERE r.parent_id IS NULL AND r.id IN (SELECT value FROM json_each(?))
 ), scoped_sessions AS (
 SELECT s.id,s.tree_id FROM sessions s JOIN selected r ON r.tree_id=s.tree_id
 ), owned_operations AS (
 SELECT o.id,o.state,s.tree_id FROM operations o LEFT JOIN cells c ON c.id=o.cell_id
 JOIN turns t ON t.id=COALESCE(c.turn_id,o.direct_turn_id)
 JOIN scoped_sessions s ON s.id=t.session_id
 ) SELECT t.id,t.metadata,t.engine,t.revision,t.created_at,r.id,c.working_directory,
 (SELECT COUNT(*) FROM turns v JOIN scoped_sessions s ON s.id=v.session_id WHERE s.tree_id=t.id AND v.state IN ('running','cancelling')),
 (SELECT COUNT(*) FROM inputs i JOIN scoped_sessions s ON s.id=i.session_id WHERE s.tree_id=t.id AND i.turn_id IS NULL AND i.steered_turn_id IS NULL AND i.cancelled_at IS NULL),
 (SELECT COUNT(*) FROM permissions p JOIN owned_operations o ON o.id=p.operation_id WHERE o.tree_id=t.id AND p.state='pending'),
 (SELECT COUNT(*) FROM questions q JOIN owned_operations o ON o.id=q.operation_id WHERE o.tree_id=t.id AND o.state='dispatched' AND q.close_reason IS NULL AND q.deadline>?),
 (SELECT COUNT(*) FROM workspace_actions a JOIN scoped_sessions s ON s.id=a.session_id WHERE s.tree_id=t.id AND a.state='claimed')
 FROM selected r JOIN session_trees t ON t.id=r.tree_id
 JOIN session_configurations c ON c.session_id=r.id AND c.revision=r.config_revision
 ORDER BY r.id`, string(raw), now())
	if err != nil {
		return result, err
	}
	defer func() { _ = rows.Close() }()
	used := 512
	for rows.Next() {
		var item session.TreeNavigation
		var metadata string
		var created int64
		if err := rows.Scan(&item.ID, &metadata, &item.Engine, &item.Revision, &created, &item.RootID, &item.WorkingDirectory, &item.ActiveTurnCount, &item.QueuedInputCount, &item.PendingPermissionCount, &item.PendingQuestionCount, &item.ActiveWorkspaceActionCount); err != nil {
			return result, err
		}
		if err := json.Unmarshal([]byte(metadata), &item.Metadata); err != nil {
			return result, err
		}
		item.CreatedAt = timestamp(created)
		used += len(metadata) + len(item.WorkingDirectory) + 1024
		if used > 512<<10 {
			return result, ErrLimit
		}
		result.Items = append(result.Items, item)
		delete(wanted, item.RootID)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	for _, id := range roots {
		if wanted[id] {
			result.Missing = append(result.Missing, id)
		}
	}
	return result, nil
}
