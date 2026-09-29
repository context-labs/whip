package store

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/context-labs/whip/internal/session"
)

// TranscriptPage observes the revision, active tail and page in one SQL
// snapshot. Missing backward cursor starts at the actual tail, never at a
// guessed offset or a count-derived sequence. It starts no execution.
func (s *Store) TranscriptPage(ctx context.Context, request session.HistoryPageRequest) (session.TranscriptPage, error) {
	page := session.TranscriptPage{Messages: []session.Message{}}
	if err := request.Validate(); err != nil {
		return page, err
	}
	comparison, order := ">", "ASC"
	if request.Direction == "backward" {
		comparison, order = "<", "DESC"
	}
	rows, err := s.db.QueryContext(ctx, `WITH bounds AS (
 SELECT id,history_revision,
  (SELECT COALESCE(MAX(sequence),0) FROM messages WHERE session_id=s.id) AS maximum,
  (SELECT COALESCE(MAX(sequence),0) FROM messages WHERE session_id=s.id AND retired_revision IS NULL) AS active_maximum,
  (SELECT COUNT(*) FROM messages WHERE session_id=s.id AND retired_revision IS NULL) AS message_count
 FROM sessions s WHERE id=?
 ) SELECT bounds.history_revision,bounds.active_maximum,bounds.message_count,bounds.id,bounds.maximum,`+historyColumns+`
 FROM bounds LEFT JOIN messages m ON m.session_id=bounds.id AND m.retired_revision IS NULL
 AND (? IS NULL OR m.sequence`+comparison+`?) AND (? IS NULL OR bounds.history_revision=?)`+historyJoins+`
 ORDER BY m.sequence `+order+` LIMIT ?`, request.SessionID, true, request.Cursor, request.Cursor, request.ExpectedRevision, request.ExpectedRevision, request.Limit+1)
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	size, more := 0, false
	for rows.Next() {
		record, err := scanHistory(rows, true)
		if err != nil {
			return page, err
		}
		page.Snapshot = record.snapshot
		if request.ExpectedRevision != nil && *request.ExpectedRevision != page.Snapshot.Revision {
			return page, ErrConflict
		}
		if record.metadata.ID == "" {
			break
		}
		if len(page.Messages) == request.Limit {
			more = true
			break
		}
		message, err := historyMessage(record)
		if err != nil {
			return page, err
		}
		raw, err := json.Marshal(message)
		if err != nil {
			return page, err
		}
		if len(raw) > MaxPageBytes {
			return page, ErrLimit
		}
		if size+len(raw) > MaxPageBytes {
			more = true
			break
		}
		size += len(raw)
		page.Messages = append(page.Messages, message)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if page.Snapshot.SessionID == "" {
		return page, ErrNotFound
	}
	if more {
		page.NextCursor = new(page.Messages[len(page.Messages)-1].Sequence)
	}
	if request.Direction == "backward" {
		slices.Reverse(page.Messages)
	}
	return page, nil
}
