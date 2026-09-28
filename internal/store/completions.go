package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/context-labs/whip/internal/session"
)

// A slot is reserved before initial input admission. Deleted children with a
// pending report still occupy their parent's slot until publication succeeds.
func reserveCompletion(ctx context.Context, tx *sql.Tx, parent, child session.SessionID) error {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM completion_slots WHERE parent_id=?", parent).Scan(&count); err != nil {
		return err
	}
	if count >= session.MaxCompletionSlots {
		return fmt.Errorf("%w: parent completion slots are full", ErrLimit)
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO completion_slots (parent_id,child_id) VALUES (?,?)", parent, child)
	return err
}

// captureCompletion only runs on a newly terminal turn. Terminal retries must
// not recreate a report already published (or replace a newer pending report).
func captureCompletion(ctx context.Context, tx *sql.Tx, turn session.Turn) error {
	if turn.Kind != session.PromptInput {
		return nil
	}
	var parent session.SessionID
	err := tx.QueryRowContext(ctx, "SELECT parent_id FROM completion_slots WHERE child_id=?", turn.SessionID).Scan(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // Roots have no completion recipient.
	}
	if err != nil {
		return err
	}
	var raw string
	if err := tx.QueryRowContext(ctx, "SELECT configuration FROM session_configurations WHERE session_id=? AND revision=?", turn.SessionID, turn.ConfigRevision).Scan(&raw); err != nil {
		return err
	}
	var config session.Configuration
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return err
	}
	if config.ReportMode == session.ReportMessage && turn.State == session.Succeeded {
		return nil // A suppressed success leaves any earlier pending failure intact.
	}
	message, text, omitted, err := lastAssistantText(ctx, tx, turn.ID)
	if err != nil {
		return err
	}
	var input *session.InputID
	err = tx.QueryRowContext(ctx, "SELECT id FROM inputs WHERE turn_id=?", turn.ID).Scan(&input)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE completion_slots SET turn_id=?,input_id=?,message_id=?,state=?,failure=?,mode=?,finished_at=?,text=?,omitted_parts=? WHERE parent_id=? AND child_id=?`,
		turn.ID, input, message, turn.State, turn.Failure, config.ReportMode, turn.FinishedAt.UnixMicro(), text, omitted, parent, turn.SessionID)
	return err
}

func lastAssistantText(ctx context.Context, q querier, turn session.TurnID) (*session.MessageID, string, int64, error) {
	var id session.MessageID
	var raw string
	err := q.QueryRowContext(ctx, "SELECT id,parts FROM messages WHERE turn_id=? AND role='assistant' ORDER BY sequence DESC LIMIT 1", turn).Scan(&id, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", 0, nil
	}
	if err != nil {
		return nil, "", 0, err
	}
	var parts []session.Part
	if err := json.Unmarshal([]byte(raw), &parts); err != nil {
		return nil, "", 0, err
	}
	var text strings.Builder
	var omitted int64
	for _, part := range parts {
		if part.Type == "text" {
			text.WriteString(part.Text)
		} else {
			omitted++
		}
	}
	return &id, text.String(), omitted, nil
}

const completionColumns = `parent_id,child_id,turn_id,input_id,message_id,state,failure,mode,finished_at,length(CAST(text AS BLOB)),omitted_parts`

func scanCompletion(row scanner, text *string) (result session.CompletionMetadata, err error) {
	var finished int64
	values := []any{&result.ParentID, &result.ChildID, &result.TurnID, &result.InputID, &result.MessageID, &result.State, &result.Failure, &result.Mode, &finished, &result.TextBytes, &result.OmittedParts}
	if text != nil {
		values = append(values, text)
	}
	err = row.Scan(values...)
	result.FinishedAt = timestamp(finished)
	return result, found(err)
}

func listCompletions(ctx context.Context, q querier, parent, after session.SessionID, limit int) ([]session.CompletionMetadata, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	if after != "" && session.ValidateID(string(after)) != nil {
		return nil, session.ErrInvalid
	}
	rows, err := q.QueryContext(ctx, "SELECT "+completionColumns+" FROM completion_slots WHERE turn_id IS NOT NULL AND (?='' OR parent_id=?) AND child_id>? ORDER BY child_id LIMIT ?", parent, parent, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.CompletionMetadata{}
	size := 0
	for rows.Next() {
		item, err := scanCompletion(rows, nil)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		size += len(raw)
		if size > MaxPageBytes {
			break
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// CompletionCandidates pages metadata without hydrating report text. A runtime
// advances the child cursor even on publication pressure, wrapping at page end.
func (s *Store) CompletionCandidates(ctx context.Context, after session.SessionID, limit int) ([]session.CompletionMetadata, error) {
	return listCompletions(ctx, s.db, "", after, limit)
}

// PendingCompletions is inspection only; it neither publishes nor acknowledges.
func (s *Store) PendingCompletions(ctx context.Context, parent, after session.SessionID, limit int) ([]session.CompletionMetadata, error) {
	if err := session.ValidateID(string(parent)); err != nil {
		return nil, err
	}
	return listCompletions(ctx, s.db, parent, after, limit)
}

func pendingCompletion(ctx context.Context, q querier, parent, child session.SessionID, turn session.TurnID) (result session.Completion, err error) {
	result.CompletionMetadata, err = scanCompletion(q.QueryRowContext(ctx, "SELECT "+completionColumns+",text FROM completion_slots WHERE parent_id=? AND child_id=? AND turn_id=?", parent, child, turn), &result.Text)
	if errors.Is(err, ErrNotFound) {
		err = ErrConflict // Cleared, deleted or superseded candidate: re-list.
	}
	return result, err
}

func (s *Store) PendingCompletion(ctx context.Context, parent, child session.SessionID, turn session.TurnID) (session.Completion, error) {
	return pendingCompletion(ctx, s.db, parent, child, turn)
}

// PublishCompletion follows durable body publication. It validates the exact
// pending snapshot and registers parent-owned evidence, canonical mail and slot
// release in one transaction. Quota pressure leaves the pending snapshot intact.
func (s *Store) PublishCompletion(ctx context.Context, parent, child session.SessionID, turn session.TurnID, reference session.ContentReference) (result session.MailMetadata, err error) {
	if err := reference.Validate(); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		completion, err := pendingCompletion(ctx, tx, parent, child, turn)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(completion)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(raw)
		if reference.ID != "completion_"+string(turn) || reference.SessionID != parent || reference.MediaType != "application/json" || reference.Size != int64(len(raw)) || reference.Digest != hex.EncodeToString(digest[:]) {
			return ErrConflict
		}
		if _, err := registerContent(ctx, tx, reference); err != nil {
			return err
		}
		result, err = completionMail(ctx, tx, completion, reference.ID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE completion_slots SET turn_id=NULL,input_id=NULL,message_id=NULL,state=NULL,failure=NULL,mode=NULL,finished_at=NULL,text=NULL,omitted_parts=NULL WHERE parent_id=? AND child_id=? AND turn_id=?`, parent, child, turn); err != nil {
			return err
		}
		return releaseDeletedCompletionSlot(ctx, tx, child)
	})
	return result, err
}

func completionBody(value session.Completion) (string, error) {
	limit := 160
	if value.Mode == session.ReportInline {
		limit = 4 << 10
	}
	notice := session.CompletionNotice{CompletionMetadata: value.CompletionMetadata}
	notice.Preview, notice.TextTruncated = childText(value.Text, limit)
	if value.Failure != nil {
		failure, truncated := childText(*value.Failure, 1024)
		notice.Failure, notice.FailureTruncated = &failure, truncated
	}
	for {
		raw, err := json.Marshal(notice)
		if err != nil {
			return "", err
		}
		if len(raw) <= session.MaxMailBodyBytes {
			return string(raw), nil
		}
		// JSON escaping can expand a valid 4 KiB preview beyond the mail limit.
		// The other bounded fields fit alone, so shortening only the preview
		// terminates and the full original evidence remains available.
		notice.Preview, _ = childText(notice.Preview, len(notice.Preview)/2)
		notice.TextTruncated = true
	}
}

func completionMail(ctx context.Context, tx *sql.Tx, value session.Completion, evidence string) (session.MailMetadata, error) {
	body, err := completionBody(value)
	if err != nil {
		return session.MailMetadata{}, err
	}
	source := session.MailSource{Kind: "completion", ID: string(value.ChildID)}
	current, err := scanMail(tx.QueryRowContext(ctx, mailSelect+" WHERE m.source_kind='completion' AND m.source_id=? AND m.recipient_id=? AND m.deleted_at IS NULL AND m.state='pending' AND r.revision=m.revision ORDER BY m.created_at DESC LIMIT 1", value.ChildID, value.ParentID))
	spec := session.MailSpec{RecipientID: value.ParentID, Delivery: session.MailQueued, Subject: "Child completion", Body: body, EvidenceRef: &evidence}
	if err == nil {
		spec.ID, spec.AvailableAt = current.ID, &current.AvailableAt
		mail, err := replaceMail(ctx, tx, spec, current)
		return mail.MailMetadata, err
	}
	if !errors.Is(err, ErrNotFound) {
		return session.MailMetadata{}, err
	}
	spec.ID = session.MailID("mail_completion_" + string(value.TurnID))
	if err := spec.Validate(); err != nil {
		return session.MailMetadata{}, err
	}
	if err := mailCapacity(ctx, tx, source, spec.RecipientID, spec.ID, true); err != nil {
		return session.MailMetadata{}, err
	}
	digest, err := requestDigest("completion", spec)
	if err != nil {
		return session.MailMetadata{}, err
	}
	created := now()
	if _, err := tx.ExecContext(ctx, "INSERT INTO mail VALUES (?,?,?,?,?,1,'pending',?,NULL)", spec.ID, source.Kind, source.ID, spec.RecipientID, digest, created); err != nil {
		return session.MailMetadata{}, err
	}
	if err := insertMailRevision(ctx, tx, spec, 1, created); err != nil {
		return session.MailMetadata{}, err
	}
	mail, err := readMail(ctx, tx, spec.RecipientID, spec.ID)
	return mail.MailMetadata, err
}

// Source identities have no foreign key: pending deleted-source evidence remains
// owned by the surviving parent. Empty orphan slots need no retained state.
func releaseDeletedCompletionSlot(ctx context.Context, tx *sql.Tx, child session.SessionID) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM completion_slots WHERE child_id=? AND turn_id IS NULL AND NOT EXISTS(SELECT 1 FROM sessions WHERE id=child_id)", child)
	return err
}
