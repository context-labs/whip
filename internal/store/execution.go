package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/context-labs/whip/internal/session"
)

type Submission struct {
	SessionID session.SessionID
	Source    session.InputSource
	Parts     []session.Part
}

// Admission is a projection, not an independently persisted status.
type Admission struct {
	Receipt session.Receipt
	Input   *session.Input
	Turn    *session.Turn
}

func readReceipt(ctx context.Context, q querier, identity session.RequestIdentity) (result session.Receipt, err error) {
	var created int64
	var deleted sql.NullInt64
	result.RequestIdentity = identity
	err = q.QueryRowContext(ctx, "SELECT digest,input_id,deleted_at,created_at FROM receipts WHERE client_id=? AND request_id=?", identity.ClientID, identity.RequestID).
		Scan(&result.Digest, &result.InputID, &deleted, &created)
	if err != nil {
		return result, found(err)
	}
	result.CreatedAt = timestamp(created)
	result.DeletedAt = optionalTime(deleted)
	return
}

func readInput(ctx context.Context, q querier, id session.InputID) (result session.Input, err error) {
	var raw string
	var created int64
	var cancelled sql.NullInt64
	err = q.QueryRowContext(ctx, "SELECT id,session_id,source,parts,turn_id,cancelled_at,created_at FROM inputs WHERE id=?", id).
		Scan(&result.ID, &result.SessionID, &result.Source, &raw, &result.TurnID, &cancelled, &created)
	if err != nil {
		return result, found(err)
	}
	result.CreatedAt = timestamp(created)
	result.State = session.Queued
	if result.TurnID != nil {
		result.State = session.Claimed
	} else if cancelled.Valid {
		result.State = session.InputCancelled
	}
	err = json.Unmarshal([]byte(raw), &result.Parts)
	return
}

func readTurn(ctx context.Context, q querier, id session.TurnID) (result session.Turn, err error) {
	var started int64
	var finished sql.NullInt64
	err = q.QueryRowContext(ctx, "SELECT id,session_id,config_revision,state,failure,started_at,finished_at FROM turns WHERE id=?", id).
		Scan(&result.ID, &result.SessionID, &result.ConfigRevision, &result.State, &result.Failure, &started, &finished)
	if err != nil {
		return result, found(err)
	}
	result.StartedAt = timestamp(started)
	result.FinishedAt = optionalTime(finished)
	return
}

func readAdmission(ctx context.Context, q querier, identity session.RequestIdentity) (result Admission, err error) {
	result.Receipt, err = readReceipt(ctx, q, identity)
	if err != nil || result.Receipt.InputID == nil {
		return
	}
	input, err := readInput(ctx, q, *result.Receipt.InputID)
	if err != nil {
		return result, err
	}
	result.Input = &input
	if input.TurnID != nil {
		turn, err := readTurn(ctx, q, *input.TurnID)
		if err != nil {
			return result, err
		}
		result.Turn = &turn
	}
	return result, nil
}

func (s *Store) Admit(ctx context.Context, identity session.RequestIdentity, request Submission) (result Admission, err error) {
	for _, id := range []string{identity.ClientID, identity.RequestID, string(request.SessionID)} {
		if err := session.ValidateID(id); err != nil {
			return result, err
		}
	}
	if request.Source != session.UserInput && request.Source != session.AgentInput && request.Source != session.ScheduledInput {
		return result, fmt.Errorf("%w: invalid input source", session.ErrInvalid)
	}
	if err := session.ValidateParts(request.Parts); err != nil {
		return result, err
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	err = s.write(ctx, func(tx *sql.Tx) error {
		receipt, err := readReceipt(ctx, tx, identity)
		if err == nil {
			if receipt.Digest != digest {
				return ErrConflict
			}
			result, err = readAdmission(ctx, tx, identity)
			return err
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		current, err := readSession(ctx, tx, request.SessionID)
		if err != nil {
			return err
		}
		if current.Lifecycle != session.Active {
			return ErrStopped
		}
		if err := validateContentReferences(ctx, tx, current.ID, request.Parts); err != nil {
			return err
		}
		tree, err := readTree(ctx, tx, current.TreeID)
		if err != nil {
			return err
		}
		var queued int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM inputs WHERE session_id=? AND turn_id IS NULL AND cancelled_at IS NULL", current.ID).Scan(&queued); err != nil {
			return err
		}
		if queued >= tree.Policy.MaxQueuedInputsPerSession {
			return ErrLimit
		}
		parts, err := encode(request.Parts)
		if err != nil {
			return err
		}
		inputID := session.InputID(newID("input"))
		created := now()
		if _, err := tx.ExecContext(ctx, "INSERT INTO inputs (id,session_id,source,parts,created_at) VALUES (?,?,?,?,?)", inputID, current.ID, request.Source, parts, created); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO receipts VALUES (?,?,?,?,NULL,?)", identity.ClientID, identity.RequestID, digest, inputID, created); err != nil {
			return err
		}
		result, err = readAdmission(ctx, tx, identity)
		return err
	})
	return
}

func (s *Store) Admission(ctx context.Context, identity session.RequestIdentity) (result Admission, err error) {
	// A transaction gives the multi-row projection one consistent snapshot.
	err = s.write(ctx, func(tx *sql.Tx) error { var err error; result, err = readAdmission(ctx, tx, identity); return err })
	return
}

func (s *Store) Input(ctx context.Context, id session.InputID) (session.Input, error) {
	return readInput(ctx, s.db, id)
}

func (s *Store) Turn(ctx context.Context, id session.TurnID) (session.Turn, error) {
	return readTurn(ctx, s.db, id)
}

type Claim struct {
	Turn          session.Turn
	Input         session.Input
	Configuration session.Configuration
}

func (s *Store) Claim(ctx context.Context, id session.SessionID) (result Claim, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		current, err := readSession(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Lifecycle != session.Active {
			return ErrStopped
		}
		var active int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM turns WHERE session_id=? AND state IN ('running','cancelling')", id).Scan(&active); err != nil {
			return err
		}
		if active != 0 {
			return ErrBusy
		}
		var inputID session.InputID
		err = tx.QueryRowContext(ctx, "SELECT id FROM inputs WHERE session_id=? AND turn_id IS NULL AND cancelled_at IS NULL ORDER BY ordinal LIMIT 1", id).Scan(&inputID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoWork
		}
		if err != nil {
			return err
		}
		turnID := session.TurnID(newID("turn"))
		started := now()
		if _, err := tx.ExecContext(ctx, "INSERT INTO turns VALUES (?,?,?,'running',NULL,?,NULL)", turnID, id, current.ConfigRevision, started); err != nil {
			return err
		}
		update, err := tx.ExecContext(ctx, "UPDATE inputs SET turn_id=? WHERE id=? AND turn_id IS NULL AND cancelled_at IS NULL", turnID, inputID)
		if err != nil {
			return err
		}
		affected, err := update.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id,session_id,turn_id,sequence,role,input_id,created_at)
   SELECT ?,?,?,COALESCE(MAX(sequence),0)+1,'user',?,? FROM messages WHERE session_id=?`,
			"message_"+string(inputID), id, turnID, inputID, started, id); err != nil {
			return err
		}
		result.Input, err = readInput(ctx, tx, inputID)
		if err != nil {
			return err
		}
		result.Turn, err = readTurn(ctx, tx, turnID)
		result.Configuration = current.Config
		return err
	})
	return
}

const messageSelect = `SELECT m.id,m.session_id,m.turn_id,m.sequence,m.role,m.input_id,
 COALESCE(m.parts,i.parts),m.created_at FROM messages m LEFT JOIN inputs i ON i.id=m.input_id`

func scanMessage(row scanner) (result session.Message, err error) {
	var raw string
	var created int64
	err = row.Scan(&result.ID, &result.SessionID, &result.TurnID, &result.Sequence, &result.Role, &result.InputID, &raw, &created)
	if err != nil {
		return result, found(err)
	}
	result.CreatedAt = timestamp(created)
	err = json.Unmarshal([]byte(raw), &result.Parts)
	return
}

func validDraft(draft session.MessageDraft) error {
	if err := session.ValidateID(string(draft.ID)); err != nil {
		return err
	}
	if draft.Role != session.Assistant && draft.Role != session.Tool && draft.Role != session.System {
		return fmt.Errorf("%w: user messages are created only by claiming input", session.ErrInvalid)
	}
	return session.ValidateParts(draft.Parts)
}

func appendMessage(ctx context.Context, tx *sql.Tx, turn session.Turn, draft session.MessageDraft) (session.Message, error) {
	if err := validDraft(draft); err != nil {
		return session.Message{}, err
	}
	existing, err := scanMessage(tx.QueryRowContext(ctx, messageSelect+" WHERE m.id=?", draft.ID))
	if err == nil {
		if existing.TurnID != turn.ID || existing.Role != draft.Role || !reflect.DeepEqual(existing.Parts, draft.Parts) {
			return session.Message{}, ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return session.Message{}, err
	}
	if turn.State.Terminal() {
		return session.Message{}, ErrConflict
	}
	if err := validateContentReferences(ctx, tx, turn.SessionID, draft.Parts); err != nil {
		return session.Message{}, err
	}
	raw, err := encode(draft.Parts)
	if err != nil {
		return session.Message{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id,session_id,turn_id,sequence,role,parts,created_at)
  SELECT ?,?,?,COALESCE(MAX(sequence),0)+1,?,?,? FROM messages WHERE session_id=?`,
		draft.ID, turn.SessionID, turn.ID, draft.Role, raw, now(), turn.SessionID); err != nil {
		return session.Message{}, err
	}
	return scanMessage(tx.QueryRowContext(ctx, messageSelect+" WHERE m.id=?", draft.ID))
}

func (s *Store) AppendMessage(ctx context.Context, id session.TurnID, draft session.MessageDraft) (result session.Message, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		turn, err := readTurn(ctx, tx, id)
		if err != nil {
			return err
		}
		result, err = appendMessage(ctx, tx, turn, draft)
		return err
	})
	return
}

func (s *Store) History(ctx context.Context, id session.SessionID, after int64, limit int) ([]session.Message, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, fmt.Errorf("%w: negative history cursor", session.ErrInvalid)
	}
	rows, err := s.db.QueryContext(ctx, messageSelect+" WHERE m.session_id=? AND m.sequence>? ORDER BY m.sequence LIMIT ?", id, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.Message{}
	size := 0
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		raw, err := encode(message)
		if err != nil {
			return nil, err
		}
		size += len(raw)
		if size > MaxPageBytes {
			break
		}
		result = append(result, message)
	}
	return result, rows.Err()
}

func (s *Store) Finish(ctx context.Context, id session.TurnID, state session.TurnState, failure *string, messages []session.MessageDraft) (result session.Turn, err error) {
	if !state.Terminal() || (state == session.Succeeded && failure != nil) || len(messages) > 128 {
		return result, fmt.Errorf("%w: invalid terminal outcome", session.ErrInvalid)
	}
	if failure != nil {
		if err := session.ValidateText(*failure, 16384); err != nil {
			return result, err
		}
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		current, err := readTurn(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.State.Terminal() {
			if current.State != state || !reflect.DeepEqual(current.Failure, failure) {
				return ErrConflict
			}
		} else if !current.State.CanTransitionTo(state) {
			return ErrConflict
		}
		var pending int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM model_attempts WHERE turn_id=? AND finished_at IS NULL", id).Scan(&pending); err != nil {
			return err
		}
		if pending != 0 {
			return ErrBusy
		}
		for _, draft := range messages {
			if _, err := appendMessage(ctx, tx, current, draft); err != nil {
				return err
			}
		}
		if !current.State.Terminal() {
			if _, err := tx.ExecContext(ctx, "UPDATE turns SET state=?,failure=?,finished_at=? WHERE id=?", state, failure, now(), id); err != nil {
				return err
			}
		}
		result, err = readTurn(ctx, tx, id)
		return err
	})
	return
}
