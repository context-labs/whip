package store

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

// HistoryGroup reads identity and provenance without treating copied history as
// locally executed work. Native groups expose their actual turn.
func (s *Store) HistoryGroup(ctx context.Context, owner session.SessionID, id session.HistoryGroupID) (result session.HistoryGroup, err error) {
	var created int64
	var sourceOwner *session.SessionID
	var sourceGroup *session.HistoryGroupID
	err = s.db.QueryRowContext(ctx, "SELECT id,session_id,turn_id,source_session_id,source_group_id,created_at FROM history_groups WHERE session_id=? AND id=?", owner, id).
		Scan(&result.ID, &result.SessionID, &result.TurnID, &sourceOwner, &sourceGroup, &created)
	if err != nil {
		return result, found(err)
	}
	result.CreatedAt = timestamp(created)
	if sourceOwner != nil {
		result.Source = &session.HistoryGroupSource{SessionID: *sourceOwner, GroupID: *sourceGroup}
	}
	return result, nil
}

// HistoryEdit is immutable evidence. This boundary does not expose an edit or
// fork command; admission and atomic retirement belong to their own operation.
func (s *Store) HistoryEdit(ctx context.Context, owner session.SessionID, id session.HistoryEditID) (result session.HistoryEdit, err error) {
	var created int64
	err = s.db.QueryRowContext(ctx, "SELECT id,session_id,digest,expected_revision,revision,observed_through,keep_through,created_at FROM history_edits WHERE session_id=? AND id=?", owner, id).
		Scan(&result.ID, &result.SessionID, &result.Digest, &result.ExpectedRevision, &result.Revision, &result.ObservedThrough, &result.KeepThrough, &created)
	result.CreatedAt = timestamp(created)
	return result, found(err)
}
