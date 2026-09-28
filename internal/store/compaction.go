package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"

	"github.com/context-labs/whip/internal/session"
)

const compactionColumns = `id,session_id,COALESCE(turn_id,''),COALESCE(attempt_id,''),base_id,expected_revision,through_sequence,pinned_message_ids,length(CAST(text AS BLOB)),created_at,history_revision,source_session_id,source_compaction_id`

func scanCompaction(row scanner, withText bool) (result session.Compaction, err error) {
	var pins string
	var created int64
	var sourceOwner *session.SessionID
	var sourceID *session.CompactionID
	args := []any{&result.ID, &result.SessionID, &result.TurnID, &result.AttemptID, &result.BaseID, &result.ExpectedRevision, &result.ThroughSequence, &pins, &result.TextBytes, &created, &result.HistoryRevision, &sourceOwner, &sourceID}
	if withText {
		args = append(args, &result.Text)
	}
	if err := row.Scan(args...); err != nil {
		return result, found(err)
	}
	result.CreatedAt = timestamp(created)
	if sourceOwner != nil {
		result.Source = &session.CompactionSource{SessionID: *sourceOwner, CompactionID: *sourceID}
	}
	err = json.Unmarshal([]byte(pins), &result.PinnedMessageIDs)
	return
}

func readContextHead(ctx context.Context, q querier, owner session.SessionID) (result session.ContextHead, err error) {
	err = q.QueryRowContext(ctx, `SELECT s.id,COALESCE(h.revision,0),h.compaction_id FROM sessions s
 LEFT JOIN context_heads h ON h.session_id=s.id WHERE s.id=?`, owner).
		Scan(&result.SessionID, &result.Revision, &result.CompactionID)
	return result, found(err)
}

func (s *Store) ContextHead(ctx context.Context, owner session.SessionID) (session.ContextHead, error) {
	return readContextHead(ctx, s.db, owner)
}

func readCompaction(ctx context.Context, q querier, owner session.SessionID, id session.CompactionID) (session.Compaction, error) {
	return scanCompaction(q.QueryRowContext(ctx, "SELECT "+compactionColumns+",text FROM compactions WHERE session_id=? AND id=?", owner, id), true)
}

func (s *Store) Compaction(ctx context.Context, owner session.SessionID, id session.CompactionID) (session.Compaction, error) {
	return readCompaction(ctx, s.db, owner, id)
}

func (s *Store) Compactions(ctx context.Context, owner session.SessionID, after session.CompactionID, limit int) ([]session.CompactionMetadata, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	if _, err := readContextHead(ctx, s.db, owner); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+compactionColumns+" FROM compactions WHERE session_id=? AND id>? ORDER BY id LIMIT ?", owner, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.CompactionMetadata{}
	bytes := 0
	for rows.Next() {
		value, err := scanCompaction(rows, false)
		if err != nil {
			return nil, err
		}
		raw, err := encode(value.CompactionMetadata)
		if err != nil {
			return nil, err
		}
		bytes += len(raw)
		if bytes > MaxPageBytes {
			break
		}
		result = append(result, value.CompactionMetadata)
	}
	return result, rows.Err()
}

func sameCompaction(value session.Compaction, draft session.CompactionDraft) bool {
	return value.ID == draft.ID && value.ExpectedRevision == draft.ExpectedRevision && reflect.DeepEqual(value.BaseID, draft.BaseID) &&
		value.ThroughSequence == draft.ThroughSequence && slices.Equal(value.PinnedMessageIDs, draft.PinnedMessageIDs) && value.Text == draft.Text
}

// SettleCompaction commits attempt accounting and valid summary evidence in one
// transaction. It appends no transcript message and never reselects on replay.
func (s *Store) SettleCompaction(ctx context.Context, id session.ModelAttemptID, outcome session.ModelAttemptResult, draft *session.CompactionDraft) (result session.CompactionSettlement, err error) {
	if err := outcome.Validate(); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		attempt, err := readAttempt(ctx, tx, id)
		if err != nil {
			return err
		}
		if attempt.Request.Purpose != "compaction" {
			return fmt.Errorf("%w: model attempt is not a compaction", session.ErrInvalid)
		}
		turn, err := readTurn(ctx, tx, attempt.TurnID)
		if err != nil {
			return err
		}
		result.Head, err = readContextHead(ctx, tx, turn.SessionID)
		if err != nil {
			return err
		}
		result.Attempt, err = settleAttempt(ctx, tx, attempt, outcome, nil)
		if err != nil {
			return err
		}
		if attempt.FinishedAt != nil {
			value, err := scanCompaction(tx.QueryRowContext(ctx, "SELECT "+compactionColumns+",text FROM compactions WHERE attempt_id=?", id), true)
			if errors.Is(err, ErrNotFound) {
				if draft != nil {
					result.Rejection = new("attempt already settled without a compaction")
				}
				return nil
			}
			if err != nil {
				return err
			}
			if draft == nil || !sameCompaction(value, *draft) {
				return ErrConflict
			}
			result.Compaction = &value
			result.Selected = result.Head.CompactionID != nil && *result.Head.CompactionID == value.ID
			return nil
		}
		if draft == nil {
			return nil
		}
		if outcome.State != session.AttemptSucceeded {
			result.Rejection = new("unsuccessful attempt cannot create a compaction")
			return nil
		}
		if err := validateCompaction(ctx, tx, turn, *draft); err != nil {
			if errors.Is(err, session.ErrInvalid) || errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrLimit) {
				result.Rejection = new(err.Error())
				return nil
			}
			return err
		}
		pins, err := encode(append([]session.MessageID{}, draft.PinnedMessageIDs...))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO compactions (id,session_id,turn_id,attempt_id,base_id,expected_revision,through_sequence,pinned_message_ids,text,created_at,history_revision)
 VALUES (?,?,?,?,?,?,?,?,?,?,?)`, draft.ID, turn.SessionID, turn.ID, id, draft.BaseID, draft.ExpectedRevision, draft.ThroughSequence, pins, draft.Text, now(), turn.HistoryRevision); err != nil {
			return err
		}
		value, err := readCompaction(ctx, tx, turn.SessionID, draft.ID)
		if err != nil {
			return err
		}
		result.Compaction = &value
		if turn.State != session.Running {
			result.Rejection = new("turn is cancelling; compaction remains unselected")
		} else if result.Head.Revision != draft.ExpectedRevision || !reflect.DeepEqual(result.Head.CompactionID, draft.BaseID) {
			result.Rejection = new("context selection changed; compaction remains unselected")
		} else if result.Head.Revision == math.MaxInt64 {
			result.Rejection = new("context revision exhausted; compaction remains unselected")
		} else {
			result.Head, err = selectCompaction(ctx, tx, result.Head, &value.ID)
			result.Selected = err == nil
		}
		return err
	})
	return
}

func validateCompaction(ctx context.Context, tx *sql.Tx, turn session.Turn, draft session.CompactionDraft) error {
	if err := draft.Validate(); err != nil {
		return err
	}
	if turn.State != session.Running && turn.State != session.Cancelling {
		return fmt.Errorf("%w: compaction requires an active turn", ErrConflict)
	}
	var revision session.Revision
	if err := tx.QueryRowContext(ctx, "SELECT history_revision FROM sessions WHERE id=?", turn.SessionID).Scan(&revision); err != nil {
		return found(err)
	}
	if revision != turn.HistoryRevision {
		return fmt.Errorf("%w: compaction source history changed", ErrConflict)
	}
	if err := activeCompactionCoverage(ctx, tx, turn.SessionID, turn.HistoryRevision, draft.ThroughSequence); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM compactions WHERE id=?)", draft.ID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: compaction identity already used", ErrConflict)
	}
	if draft.BaseID != nil {
		base, err := readCompaction(ctx, tx, turn.SessionID, *draft.BaseID)
		if err != nil {
			return err
		}
		if err := activeCompaction(ctx, tx, base); err != nil {
			return err
		}
		if draft.ThroughSequence <= base.ThroughSequence {
			return fmt.Errorf("%w: compaction must advance raw coverage", session.ErrInvalid)
		}
	}
	boundaryTurn, requiredPin, err := contextBoundaryPin(ctx, tx, turn.SessionID, draft.ThroughSequence)
	if err != nil {
		return err
	}
	pins, err := readContextPins(ctx, tx, turn.SessionID, draft.PinnedMessageIDs)
	if err != nil {
		return err
	}
	for _, pin := range pins {
		if pin.Sequence > draft.ThroughSequence {
			return fmt.Errorf("%w: pin must identify an opening input message within coverage", session.ErrInvalid)
		}
	}
	if requiredPin != nil && !slices.Contains(draft.PinnedMessageIDs, *requiredPin) {
		return fmt.Errorf("%w: partial prompt turn requires its exact opening input pin", session.ErrInvalid)
	}
	return completeCompactionBoundary(ctx, tx, boundaryTurn, draft.ThroughSequence)
}

func completeCompactionBoundary(ctx context.Context, tx *sql.Tx, group session.HistoryGroupID, through int64) error {
	message, err := scanMessage(tx.QueryRowContext(ctx, messageSelect+" WHERE m.group_id=? AND m.retired_revision IS NULL AND m.role='assistant' AND m.sequence<=? ORDER BY m.sequence DESC LIMIT 1", group, through))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	pending := map[string]bool{}
	for _, part := range message.Parts {
		if part.Call != nil {
			pending[part.Call.ID] = true
		}
	}
	if len(pending) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, messageSelect+" WHERE m.group_id=? AND m.retired_revision IS NULL AND m.role='tool' AND m.sequence>? AND m.sequence<=? ORDER BY m.sequence", group, message.Sequence, through)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return err
		}
		for _, part := range message.Parts {
			if part.Result != nil {
				delete(pending, part.Result.CallID)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(pending) != 0 {
		return fmt.Errorf("%w: compaction boundary splits tool calls from their results", session.ErrInvalid)
	}
	return nil
}

func activeCompactionCoverage(ctx context.Context, q querier, owner session.SessionID, revision session.Revision, through int64) error {
	var retired bool
	if err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM messages WHERE session_id=? AND sequence<=? AND retired_revision>?)", owner, through, revision).Scan(&retired); err != nil {
		return err
	}
	if retired {
		return fmt.Errorf("%w: compaction covers retired history", ErrConflict)
	}
	return nil
}

func activeCompaction(ctx context.Context, q querier, value session.Compaction) error {
	if err := activeCompactionCoverage(ctx, q, value.SessionID, value.HistoryRevision, value.ThroughSequence); err != nil {
		return err
	}
	// Pin checks need only immutable identity, not potentially large input bodies.
	for _, id := range value.PinnedMessageIDs {
		var valid bool
		if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE id=? AND session_id=?
 AND opening_input=1 AND retired_revision IS NULL AND sequence<=?)`, id, value.SessionID, value.ThroughSequence).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("%w: compaction pin is outside active history", ErrConflict)
		}
	}
	return nil
}

func selectCompaction(ctx context.Context, tx *sql.Tx, head session.ContextHead, id *session.CompactionID) (session.ContextHead, error) {
	if _, err := tx.ExecContext(ctx, `INSERT INTO context_heads (session_id,revision,compaction_id) VALUES (?,?,?)
 ON CONFLICT(session_id) DO UPDATE SET revision=excluded.revision,compaction_id=excluded.compaction_id`, head.SessionID, head.Revision+1, id); err != nil {
		return session.ContextHead{}, err
	}
	return readContextHead(ctx, tx, head.SessionID)
}

// SelectCompaction undoes selection to an ancestor (or no summary) while idle.
// It changes no messages, checkpoints or files, and old summaries remain readable.
func (s *Store) SelectCompaction(ctx context.Context, owner session.SessionID, expectedRevision int64, id *session.CompactionID) (result session.ContextHead, err error) {
	if expectedRevision < 0 {
		return result, fmt.Errorf("%w: negative context revision", session.ErrInvalid)
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, err = readContextHead(ctx, tx, owner)
		if err != nil {
			return err
		}
		if result.Revision != expectedRevision {
			return ErrConflict
		}
		var active bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM turns WHERE session_id=? AND state IN ('running','cancelling'))", owner).Scan(&active); err != nil {
			return err
		}
		if active {
			return ErrBusy
		}
		if id != nil {
			value, err := readCompaction(ctx, tx, owner, *id)
			if err != nil {
				return err
			}
			if err := activeCompaction(ctx, tx, value); err != nil {
				return err
			}
		}
		if reflect.DeepEqual(result.CompactionID, id) {
			return nil
		}
		if id != nil {
			var ancestor bool
			if err := tx.QueryRowContext(ctx, `WITH RECURSIVE chain(id,base_id) AS (
 SELECT id,base_id FROM compactions WHERE session_id=? AND id=?
 UNION ALL SELECT c.id,c.base_id FROM compactions c JOIN chain p ON c.id=p.base_id WHERE c.session_id=?
 ) SELECT EXISTS(SELECT 1 FROM chain WHERE id=?)`, owner, result.CompactionID, owner, *id).Scan(&ancestor); err != nil {
				return err
			}
			if !ancestor {
				return ErrConflict
			}
		}
		if result.Revision == math.MaxInt64 {
			return ErrLimit
		}
		result, err = selectCompaction(ctx, tx, result, id)
		return err
	})
	return
}
