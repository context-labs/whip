package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
)

const MaxSessionSummaries = 32
const MaxSessionSummaryIDBytes = 256
const MaxSessionSummariesBytes = 64 << 10

// SessionNavigationSummary is advisory metadata for an explicitly requested
// root. Missing is authoritative only when the entire lookup succeeds. Queued
// counts agents with explicit queued inbox input, including running agents with
// follow-up input; future schedules and unclaimed mailbox notifications are not
// included. Questions are filled from the daemon's live registry after this read.
type SessionNavigationSummary struct {
	RootID             string `json:"root_id"`
	Missing            bool   `json:"missing"`
	Title              string `json:"title"`
	CWD                string `json:"cwd"`
	WorkspaceID        string `json:"workspace_id,omitempty"`
	RunningAgents      int64  `json:"running_agents,string"`
	QueuedAgents       int64  `json:"queued_agents,string"`
	PendingPermissions int64  `json:"pending_permissions,string"`
	PendingQuestions   int64  `json:"pending_questions,string"`
	Truncated          bool   `json:"truncated"`
}

// SessionSummaries reads one bounded working set in request order without
// hydrating transcripts or runtime roots. Partial SQL results are never returned.
func (s *Store) SessionSummaries(ctx context.Context, rootIDs []string) ([]SessionNavigationSummary, error) {
	if len(rootIDs) > MaxSessionSummaries {
		return nil, errors.New("session summaries exceed the root limit")
	}
	seen := make(map[string]bool, len(rootIDs))
	for _, id := range rootIDs {
		if id == "" || len(id) > MaxSessionSummaryIDBytes || seen[id] {
			return nil, errors.New("session summaries require distinct root IDs of 1..256 bytes")
		}
		seen[id] = true
	}
	result := make([]SessionNavigationSummary, 0, len(rootIDs))
	if len(rootIDs) == 0 {
		return result, ctx.Err()
	}
	encoded, err := json.Marshal(rootIDs)
	if err != nil {
		return nil, err
	}
	// Permissions have no root/status index. Group the requested roots once,
	// instead of rescanning the permission ledger independently for every tab.
	rows, err := s.db.QueryContext(ctx, `WITH requested AS (
 SELECT value AS id,key AS position FROM json_each(?)
), permissions AS (
 SELECT root_id,COUNT(*) AS pending FROM permission_requests
 WHERE status='pending' AND root_id IN (SELECT id FROM requested) GROUP BY root_id
)
 SELECT r.id,s.id IS NULL,substr(COALESCE(s.title,''),1,128),substr(COALESCE(s.cwd,''),1,4096),
 COALESCE(length(s.title)>128 OR length(s.cwd)>128,0),COALESCE(length(s.cwd)>4096,0),
 (SELECT COUNT(*) FROM agents a WHERE a.root_id=s.id AND a.status='running'),
 (SELECT COUNT(*) FROM agents a WHERE a.root_id=s.id AND EXISTS(
   SELECT 1 FROM inbox i WHERE i.root_id=s.id AND i.agent_id=a.id AND i.status='queued')),
 COALESCE(p.pending,0)
 FROM requested r LEFT JOIN sessions s ON s.id=r.id
 LEFT JOIN permissions p ON p.root_id=s.id ORDER BY r.position`, string(encoded))
	if err != nil {
		return nil, fmt.Errorf("read session summaries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item SessionNavigationSummary
		var pathTruncated bool
		if err := rows.Scan(&item.RootID, &item.Missing, &item.Title, &item.CWD,
			&item.Truncated, &pathTruncated, &item.RunningAgents, &item.QueuedAgents,
			&item.PendingPermissions); err != nil {
			return nil, fmt.Errorf("scan session summary: %w", err)
		}
		if !item.Missing && !pathTruncated {
			item.WorkspaceID = fmt.Sprintf("%x", sha256.Sum256([]byte(item.CWD)))
		}
		if path := []rune(item.CWD); len(path) > 128 {
			item.CWD = string(path[:128])
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read session summaries: %w", err)
	}
	return result, nil
}
