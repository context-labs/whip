package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

const maxHistoryScanMessages = 100

// HistorySnapshot reads one consistent raw boundary without executing work or
// observing mail. Sequences are source coordinates, never compacted positions.
func (s *Store) HistorySnapshot(ctx context.Context, owner session.SessionID) (result session.HistorySnapshot, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT s.id,COALESCE(MAX(m.sequence),0),COUNT(m.id)
		FROM sessions s LEFT JOIN messages m ON m.session_id=s.id WHERE s.id=? GROUP BY s.id`, owner).
		Scan(&result.SessionID, &result.ThroughSequence, &result.MessageCount)
	return result, found(err)
}

// ContextTail selects recent message-bearing turns without loading their bodies.
// Its boundary is a raw sequence and does not change a context selection.
func (s *Store) ContextTail(ctx context.Context, owner session.SessionID, through int64, keepTurns int) (int64, error) {
	if through < 0 || keepTurns < 1 || keepTurns > 4 {
		return 0, fmt.Errorf("%w: context tail requires a nonnegative snapshot and 1 to 4 turns", session.ErrInvalid)
	}
	var maximum, after int64
	err := s.db.QueryRowContext(ctx, `WITH bounds AS (
		SELECT id,(SELECT COALESCE(MAX(sequence),0) FROM messages WHERE session_id=s.id) AS maximum FROM sessions s WHERE id=?
	), latest AS (
		SELECT DISTINCT turn_id FROM messages WHERE session_id=? AND sequence<=? ORDER BY sequence DESC LIMIT ?
	)
	SELECT maximum,COALESCE((SELECT MIN((SELECT MIN(sequence) FROM messages WHERE turn_id=latest.turn_id AND session_id=? AND sequence<=?))-1 FROM latest),0)
	FROM bounds`, owner, owner, through, keepTurns, owner, through).Scan(&maximum, &after)
	if err != nil {
		return 0, found(err)
	}
	if through > maximum {
		return 0, fmt.Errorf("%w: history snapshot is beyond retained history", session.ErrInvalid)
	}
	return after, nil
}

// Ordinary metadata reads ask SQLite for byte lengths without loading payloads.
// Mail parts are a deterministic projection of their immutable revision, so only
// those bounded source bodies must be rendered to obtain an exact byte count.
const historyColumns = `COALESCE(m.id,''),COALESCE(m.turn_id,''),m.input_id,
	COALESCE(m.sequence,0),COALESCE(m.role,''),COALESCE(length(CAST(COALESCE(m.parts,i.parts) AS BLOB)),0),
	CASE WHEN ? THEN COALESCE(m.parts,i.parts) END,COALESCE(m.created_at,0),
	m.mail_id,m.mail_revision,m.mail_presentation,r.subject,r.body,mail.source_kind,mail.source_id,r.evidence_ref`

const historyJoins = ` LEFT JOIN inputs i ON i.id=m.input_id
	LEFT JOIN mail_revisions r ON r.mail_id=m.mail_id AND r.revision=m.mail_revision
	LEFT JOIN mail ON mail.id=m.mail_id`

type historyRecord struct {
	metadata session.HistoryMetadata
	parts    []byte
	created  int64
	maximum  int64
}

func scanHistory(row scanner) (value historyRecord, err error) {
	var raw sql.NullString
	var mailID *session.MailID
	var revision sql.NullInt64
	var presentation, subject, body, sourceKind, sourceID sql.NullString
	var evidence *string
	err = row.Scan(&value.metadata.SessionID, &value.maximum, &value.metadata.ID, &value.metadata.TurnID, &value.metadata.InputID,
		&value.metadata.Sequence, &value.metadata.Role, &value.metadata.PartsBytes, &raw, &value.created,
		&mailID, &revision, &presentation, &subject, &body, &sourceKind, &sourceID, &evidence)
	if err != nil {
		return value, err
	}
	if mailID != nil {
		value.metadata.Mail = &session.MailRef{ID: *mailID, Revision: revision.Int64, Presentation: session.MailPresentation(presentation.String)}
		value.parts, err = json.Marshal(mailParts(*value.metadata.Mail, session.MailSource{Kind: sourceKind.String, ID: sourceID.String}, subject.String, body.String, evidence))
		value.metadata.PartsBytes = int64(len(value.parts))
	} else if raw.Valid {
		value.parts = []byte(raw.String)
	}
	return value, err
}

func historyWindow(after, through int64, limit int) error {
	if after < 0 || through < 0 || after > through {
		return fmt.Errorf("%w: history requires 0 <= after <= through_sequence", session.ErrInvalid)
	}
	return pageLimit(limit)
}

func (s *Store) historyRows(ctx context.Context, owner session.SessionID, after, through int64, limit int, bodies bool) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, `WITH bounds AS (
		SELECT id,(SELECT COALESCE(MAX(sequence),0) FROM messages WHERE session_id=s.id) AS maximum FROM sessions s WHERE id=?
	) SELECT bounds.id,bounds.maximum,`+historyColumns+`
	FROM bounds LEFT JOIN messages m ON m.session_id=bounds.id AND m.sequence>? AND m.sequence<=?`+historyJoins+`
	ORDER BY m.sequence LIMIT ?`, owner, bodies, after, through, limit)
}

func (s *Store) HistoryMetadata(ctx context.Context, owner session.SessionID, after, through int64, limit int) (session.HistoryMetadataPage, error) {
	result := session.HistoryMetadataPage{Items: []session.HistoryMetadata{}, ThroughSequence: through}
	if err := historyWindow(after, through, limit); err != nil {
		return result, err
	}
	rows, err := s.historyRows(ctx, owner, after, through, limit+1, false)
	if err != nil {
		return result, err
	}
	defer func() { _ = rows.Close() }()
	exists := false
	for rows.Next() {
		value, err := scanHistory(rows)
		if err != nil {
			return result, err
		}
		exists = true
		if through > value.maximum {
			return result, fmt.Errorf("%w: history snapshot is beyond retained history", session.ErrInvalid)
		}
		if value.metadata.ID == "" {
			break
		}
		if len(result.Items) == limit {
			result.NextAfter = new(result.Items[len(result.Items)-1].Sequence)
			break
		}
		result.Items = append(result.Items, value.metadata)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if !exists {
		return result, ErrNotFound
	}
	return result, nil
}

// HistoryRange preserves full raw messages within a fixed snapshot. Only the
// selected page is hydrated; content references remain references.
func (s *Store) HistoryRange(ctx context.Context, owner session.SessionID, after, through int64, limit int) ([]session.Message, error) {
	if err := historyWindow(after, through, limit); err != nil {
		return nil, err
	}
	rows, err := s.historyRows(ctx, owner, after, through, limit, true)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.Message{}
	exists, size := false, 0
	for rows.Next() {
		value, err := scanHistory(rows)
		if err != nil {
			return nil, err
		}
		exists = true
		if through > value.maximum {
			return nil, fmt.Errorf("%w: history snapshot is beyond retained history", session.ErrInvalid)
		}
		if value.metadata.ID == "" {
			break
		}
		message := session.Message{
			ID: value.metadata.ID, SessionID: owner, TurnID: value.metadata.TurnID, InputID: value.metadata.InputID,
			Mail: value.metadata.Mail, Sequence: value.metadata.Sequence, Role: value.metadata.Role, CreatedAt: timestamp(value.created),
		}
		if err := json.Unmarshal(value.parts, &message.Parts); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}
		if size+len(raw) > MaxPageBytes {
			break
		}
		size += len(raw)
		result = append(result, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	return result, nil
}

func (s *Store) ReadHistoryMessage(ctx context.Context, owner session.SessionID, id session.MessageID, offset int64, length int) (session.HistoryRead, error) {
	if offset < 0 || length < 1 || length > session.MaxHistoryReadBytes {
		return session.HistoryRead{}, fmt.Errorf("%w: history reads require a nonnegative offset and length from 1 to 65536", session.ErrInvalid)
	}
	value, err := scanHistory(s.db.QueryRowContext(ctx, `SELECT m.session_id,m.sequence,`+historyColumns+` FROM messages m`+historyJoins+` WHERE m.session_id=? AND m.id=?`, true, owner, id))
	if err != nil {
		return session.HistoryRead{}, found(err)
	}
	if offset > value.metadata.PartsBytes {
		return session.HistoryRead{}, fmt.Errorf("%w: offset exceeds message parts size", session.ErrInvalid)
	}
	end := min(offset+int64(length), value.metadata.PartsBytes)
	result := session.HistoryRead{Message: value.metadata, Offset: offset, Data: bytes.Clone(value.parts[offset:end])}
	if end < value.metadata.PartsBytes {
		result.NextOffset = &end
	}
	return result, nil
}

// SearchHistory searches literal, case-sensitive text without hydrating content
// bodies. Each call searches at most 100 messages and 4 MiB of serialized parts.
func (s *Store) SearchHistory(ctx context.Context, owner session.SessionID, after, through int64, query string, limit int) (session.HistorySearchPage, error) {
	result := session.HistorySearchPage{Matches: []session.HistoryMatch{}, ThroughSequence: through}
	if err := historyWindow(after, through, limit); err != nil {
		return result, err
	}
	if !utf8.ValidString(query) || strings.ContainsRune(query, 0) || strings.TrimSpace(query) == "" || len(query) > session.MaxHistoryQueryBytes {
		return result, fmt.Errorf("%w: history query must be UTF-8 text from 1 to 256 bytes", session.ErrInvalid)
	}
	rows, err := s.historyRows(ctx, owner, after, through, maxHistoryScanMessages+1, true)
	if err != nil {
		return result, err
	}
	defer func() { _ = rows.Close() }()
	exists := false
	last := after
	for rows.Next() {
		value, err := scanHistory(rows)
		if err != nil {
			return result, err
		}
		exists = true
		if through > value.maximum {
			return result, fmt.Errorf("%w: history snapshot is beyond retained history", session.ErrInvalid)
		}
		if value.metadata.ID == "" {
			break
		}
		if result.ScannedMessages == maxHistoryScanMessages || result.ScannedBytes+value.metadata.PartsBytes > MaxPageBytes || len(result.Matches) == limit {
			result.NextAfter = &last
			break
		}
		var parts []session.Part
		if err := json.Unmarshal(value.parts, &parts); err != nil {
			return result, err
		}
		result.ScannedMessages++
		result.ScannedBytes += value.metadata.PartsBytes
		last = value.metadata.Sequence
		if match := historyMatch(value.metadata, parts, query); match != nil {
			result.Matches = append(result.Matches, *match)
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if !exists {
		return result, ErrNotFound
	}
	return result, nil
}

func historyMatch(metadata session.HistoryMetadata, parts []session.Part, query string) *session.HistoryMatch {
	for index, part := range parts {
		field, text := "text", part.Text
		if part.Call != nil {
			field, text = "arguments", string(part.Call.Arguments)
		} else if part.Result != nil {
			field, text = "output", part.Result.Output
		}
		offset := strings.Index(text, query)
		if offset < 0 {
			continue
		}
		start := max(0, offset-128)
		for start > 0 && !utf8.RuneStart(text[start]) {
			start--
		}
		end := min(len(text), start+512)
		for end < len(text) && !utf8.RuneStart(text[end]) {
			end--
		}
		return &session.HistoryMatch{Message: metadata, PartIndex: index, Field: field, Offset: int64(offset), Snippet: text[start:end], Truncated: start > 0 || end < len(text)}
	}
	return nil
}
