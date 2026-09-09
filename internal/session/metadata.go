package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const MaxSessionMetadataBytes = 64 << 10

var ErrSessionMetadataTooLarge = errors.New("session title and working directory exceed the 65536-byte metadata limit")

// SessionMetadata contains exact actionable values, never catalog truncations.
// It deliberately excludes transcripts and all live runtime state.
type SessionMetadata struct {
	RootID          string `json:"root_id"`
	Title           string `json:"title"`
	CWD             string `json:"cwd"`
	HistoryRevision int64  `json:"history_revision,string"`
	Archived        bool   `json:"archived"`
}

func (s *Store) SessionMetadata(ctx context.Context, rootID string) (SessionMetadata, error) {
	var result SessionMetadata
	if rootID == "" || len(rootID) > MaxSessionSummaryIDBytes {
		return result, errors.New("session metadata requires a root ID of 1..256 bytes")
	}
	var oversized bool
	// Bound the database-to-Go allocation before scanning. JSON escaping can
	// expand these strings further, so enforce the wire budget after encoding.
	err := s.db.QueryRowContext(ctx, `SELECT id,
 CASE WHEN length(CAST(title AS BLOB))+length(CAST(cwd AS BLOB))<=? THEN title ELSE '' END,
 CASE WHEN length(CAST(title AS BLOB))+length(CAST(cwd AS BLOB))<=? THEN cwd ELSE '' END,
 history_revision,archived,length(CAST(title AS BLOB))+length(CAST(cwd AS BLOB))>?
 FROM sessions WHERE id=?`,
		MaxSessionMetadataBytes, MaxSessionMetadataBytes, MaxSessionMetadataBytes, rootID,
	).Scan(&result.RootID, &result.Title, &result.CWD, &result.HistoryRevision, &result.Archived, &oversized)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionMetadata{}, fmt.Errorf("session %q does not exist: %w", rootID, err)
	}
	if err != nil {
		return SessionMetadata{}, fmt.Errorf("read session metadata: %w", err)
	}
	if oversized {
		return SessionMetadata{}, ErrSessionMetadataTooLarge
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return SessionMetadata{}, fmt.Errorf("encode session metadata: %w", err)
	}
	if len(encoded) > MaxSessionMetadataBytes {
		return SessionMetadata{}, ErrSessionMetadataTooLarge
	}
	return result, nil
}

// SetArchived changes catalog visibility only. It preserves recency, history,
// schedules, pending input, and runtime execution state.
func (s *Store) SetArchived(ctx context.Context, rootID string, archived bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET archived=? WHERE id=?`, archived, rootID)
	if err != nil {
		return fmt.Errorf("set session archive state: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check session archive update: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("session %q does not exist: %w", rootID, sql.ErrNoRows)
	}
	return nil
}
