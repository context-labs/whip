package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/session"
)

// RecentTrees is an advisory fresh read. Activity comes from canonical creation,
// configuration, input, message and turn timestamps throughout each live tree.
// CatalogRevision describes membership/metadata only: activity can change while
// it stays equal. There is deliberately no cursor or promise of a frozen order.
func (s *Store) RecentTrees(ctx context.Context, limit int) (session.RecentTreePage, error) {
	result := session.RecentTreePage{Items: []session.RecentTree{}}
	if err := pageLimit(limit); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `WITH clocks(session_id,at) AS (
 SELECT id,created_at FROM sessions
 UNION ALL SELECT session_id,created_at FROM session_configurations
 UNION ALL SELECT session_id,created_at FROM inputs
 UNION ALL SELECT session_id,created_at FROM messages
 UNION ALL SELECT session_id,MAX(started_at,COALESCE(finished_at,started_at)) FROM turns
 ), activity AS (
 SELECT s.tree_id,MAX(c.at) AS at FROM clocks c JOIN sessions s ON s.id=c.session_id GROUP BY s.tree_id
 ), page AS (
 SELECT t.id,t.metadata,t.engine,t.revision,t.created_at,s.id AS root_id,c.working_directory,
 json_extract(c.configuration,'$.model') AS model,MAX(t.created_at,a.at) AS activity_at
 FROM session_trees t JOIN sessions s ON s.tree_id=t.id AND s.parent_id IS NULL
 JOIN session_configurations c ON c.session_id=s.id AND c.revision=s.config_revision
 JOIN activity a ON a.tree_id=t.id ORDER BY activity_at DESC,t.id DESC LIMIT ?
 ) SELECT catalog.revision,COALESCE(p.id,''),COALESCE(p.metadata,''),COALESCE(p.engine,''),
 COALESCE(p.revision,0),COALESCE(p.created_at,0),COALESCE(p.root_id,''),COALESCE(p.working_directory,''),
 COALESCE(p.model,''),COALESCE(p.activity_at,0)
 FROM tree_catalog catalog LEFT JOIN page p ON TRUE WHERE catalog.singleton=1
 ORDER BY p.activity_at DESC,p.id DESC`, limit+1)
	if err != nil {
		return result, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if len(result.Items) == limit {
			result.HasMore = true
			break
		}
		var item session.RecentTree
		var metadata, model string
		var created, activity int64
		if err := rows.Scan(&result.CatalogRevision, &item.ID, &metadata, &item.Engine, &item.Revision, &created, &item.RootID, &item.WorkingDirectory, &model, &activity); err != nil {
			return result, err
		}
		if item.ID == "" {
			continue
		}
		if err := json.Unmarshal([]byte(metadata), &item.Metadata); err != nil {
			return result, err
		}
		if err := json.Unmarshal([]byte(model), &item.Model); err != nil {
			return result, err
		}
		item.CreatedAt, item.LastActivityAt = timestamp(created), timestamp(activity)
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}
