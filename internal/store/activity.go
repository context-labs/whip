package store

import (
	"context"
	"database/sql"

	"github.com/context-labs/whip/internal/session"
)

// Activity reads all facts in one statement without claiming work or hydrating
// inputs. Pending direct human operations and cell operations share ownership.
func (s *Store) Activity(ctx context.Context, owner session.SessionID) (session.Activity, error) {
	return readActivity(ctx, s.db, owner)
}

func readActivity(ctx context.Context, q querier, owner session.SessionID) (session.Activity, error) {
	value := session.Activity{}
	if err := session.ValidateID(string(owner)); err != nil {
		return value, err
	}
	turn := session.Turn{SessionID: owner}
	var started sql.NullInt64
	var goalID *session.GoalID
	var goalRevision sql.NullInt64
	err := q.QueryRowContext(ctx, `WITH owned_operations AS (
 SELECT o.id,o.state FROM operations o LEFT JOIN cells c ON c.id=o.cell_id
 JOIN turns t ON t.id=COALESCE(c.turn_id,o.direct_turn_id) WHERE t.session_id=?
 ) SELECT s.id,s.lifecycle,
 COALESCE(t.id,''),COALESCE(t.config_revision,0),COALESCE(t.history_revision,0),COALESCE(t.state,''),t.started_at,
 COALESCE(i.kind,'prompt'),t.goal_id,t.goal_revision,i.id,
 (SELECT COUNT(*) FROM inputs WHERE session_id=s.id AND turn_id IS NULL AND steered_turn_id IS NULL AND cancelled_at IS NULL),
 (SELECT COUNT(*) FROM permissions p JOIN owned_operations o ON o.id=p.operation_id WHERE p.state='pending'),
 (SELECT COUNT(*) FROM questions q JOIN owned_operations o ON o.id=q.operation_id WHERE o.state='dispatched' AND q.close_reason IS NULL AND q.deadline>?),
 EXISTS(SELECT 1 FROM turn_permits WHERE turn_id=t.id),
 (SELECT id FROM workspace_actions WHERE session_id=s.id AND state='claimed')
 FROM sessions s LEFT JOIN turns t ON t.session_id=s.id AND t.state IN ('running','cancelling')
 LEFT JOIN inputs i ON i.turn_id=t.id WHERE s.id=?`, owner, now(), owner).Scan(
		&value.SessionID, &value.Lifecycle, &turn.ID, &turn.ConfigRevision, &turn.HistoryRevision, &turn.State, &started,
		&turn.Kind, &goalID, &goalRevision, &value.ActiveInputID, &value.QueuedInputCount, &value.PendingPermissionCount,
		&value.PendingQuestionCount, &value.ExecutionPermit, &value.ActiveWorkspaceActionID)
	if err != nil {
		return value, found(err)
	}
	if turn.ID != "" {
		turn.StartedAt = timestamp(started.Int64)
		if goalID != nil {
			turn.Goal = &session.GoalRef{ID: *goalID, Revision: goalRevision.Int64}
		}
		value.ActiveTurn = &turn
	}
	return value, nil
}

func (s *Store) InputPage(ctx context.Context, owner session.SessionID, state string, after int64, limit int) (session.InputPage, error) {
	page := session.InputPage{Items: []session.InputSummary{}}
	if err := session.ValidateID(string(owner)); err != nil {
		return page, err
	}
	if state != "queued" && state != "all" || after < 0 {
		return page, session.ErrInvalid
	}
	if err := pageLimit(limit); err != nil {
		return page, err
	}
	rows, err := s.db.QueryContext(ctx, `WITH selected AS (
 SELECT i.*,
 COALESCE((SELECT json_extract(value,'$.text') FROM json_each(i.parts) WHERE json_extract(value,'$.type')='text' LIMIT 1),'') AS first_text,
 (SELECT COUNT(*) FROM json_each(i.parts) WHERE json_extract(value,'$.type')='text') AS text_count,
 (SELECT COUNT(*) FROM json_each(i.parts) WHERE json_extract(value,'$.type')='content') AS attachments
 FROM inputs i WHERE i.session_id=? AND i.ordinal>? AND (?='all' OR (i.turn_id IS NULL AND i.steered_turn_id IS NULL AND i.cancelled_at IS NULL))
 ORDER BY i.ordinal LIMIT ?
 ) SELECT s.id,COALESCE(i.id,''),COALESCE(i.ordinal,0),COALESCE(i.source,''),COALESCE(i.kind,''),COALESCE(i.turn_id,i.steered_turn_id),i.cancelled_at,i.created_at,
 COALESCE(substr(i.first_text,1,512),''),COALESCE(length(i.first_text)>512 OR i.text_count>1,0),COALESCE(i.attachments,0),
 (SELECT id FROM input_steering target WHERE target.input_id=i.id),(SELECT turn_id FROM input_steering target WHERE target.input_id=i.id),COALESCE(i.steered_turn_id IS NOT NULL,0)
 FROM sessions s LEFT JOIN selected i ON i.session_id=s.id WHERE s.id=? ORDER BY i.ordinal`, owner, after, state, limit+1, owner)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	foundOwner := false
	for rows.Next() {
		var item session.InputSummary
		var created, cancelled sql.NullInt64
		var steeringID *session.InputSteeringID
		var targetTurn *session.TurnID
		var consumed bool
		if err := rows.Scan(&item.SessionID, &item.ID, &item.Ordinal, &item.Source, &item.Kind, &item.TurnID, &cancelled, &created, &item.TextPreview, &item.PreviewTruncated, &item.AttachmentCount, &steeringID, &targetTurn, &consumed); err != nil {
			return page, err
		}
		foundOwner = true
		if item.ID == "" {
			break
		}
		if len(page.Items) == limit {
			page.NextCursor = new(page.Items[len(page.Items)-1].Ordinal)
			break
		}
		if steeringID != nil && targetTurn != nil {
			item.Steering = &session.InputSteeringRef{ID: *steeringID, TurnID: *targetTurn, Consumed: consumed}
		}
		item.CreatedAt = timestamp(created.Int64)
		item.State = session.Queued
		if item.TurnID != nil {
			item.State = session.Claimed
		} else if cancelled.Valid {
			item.State = session.InputCancelled
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if !foundOwner {
		return page, ErrNotFound
	}
	return page, nil
}

func (s *Store) SessionInput(ctx context.Context, owner session.SessionID, id session.InputID) (session.Input, error) {
	value, err := s.Input(ctx, id)
	if err == nil && value.SessionID != owner {
		return session.Input{}, ErrNotFound
	}
	return value, err
}
