package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/context-labs/whip/internal/session"
)

const attemptSelect = `SELECT id,turn_id,logical_id,number,request,state,result,cost_nano_usd,cost_source,cost_note,message_id,created_at,dispatched_at,finished_at FROM model_attempts`

func scanAttempt(row interface{ Scan(...any) error }) (a session.ModelAttempt, err error) {
	var request string
	var result sql.NullString
	var created int64
	var dispatched, finished sql.NullInt64
	err = row.Scan(&a.ID, &a.TurnID, &a.LogicalID, &a.Number, &request, &a.State, &result, &a.CostNanoUSD, &a.CostSource, &a.CostNote, &a.MessageID, &created, &dispatched, &finished)
	if err != nil {
		return a, found(err)
	}
	a.CreatedAt = timestamp(created)
	a.DispatchedAt = optionalTime(dispatched)
	a.FinishedAt = optionalTime(finished)
	if err := json.Unmarshal([]byte(request), &a.Request); err != nil {
		return a, err
	}
	if result.Valid {
		a.Result = &session.ModelAttemptResult{}
		err = json.Unmarshal([]byte(result.String), a.Result)
	}
	return a, err
}

func readAttempt(ctx context.Context, q querier, id session.ModelAttemptID) (session.ModelAttempt, error) {
	return scanAttempt(q.QueryRowContext(ctx, attemptSelect+" WHERE id=?", id))
}

// ReserveModelAttempt commits immutable dispatch provenance before a provider can
// be called. A matching retry reads existing state and grants no dispatch right.
func (s *Store) ReserveModelAttempt(ctx context.Context, p session.ModelAttemptSpec) (result session.ModelAttempt, err error) {
	for _, id := range []string{string(p.ID), string(p.TurnID), p.LogicalID} {
		if err := session.ValidateID(id); err != nil {
			return result, err
		}
	}
	if p.Number < 1 || p.Number > 100 {
		return result, fmt.Errorf("%w: invalid attempt number", session.ErrInvalid)
	}
	if err := p.Request.Validate(); err != nil {
		return result, err
	}
	raw, err := encode(p.Request)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		existing, err := readAttempt(ctx, tx, p.ID)
		if err == nil {
			if existing.TurnID != p.TurnID || existing.LogicalID != p.LogicalID || existing.Number != p.Number || !reflect.DeepEqual(existing.Request, p.Request) {
				return ErrConflict
			}
			result = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		turn, err := readTurn(ctx, tx, p.TurnID)
		if err != nil {
			return err
		}
		if turn.State != session.Running {
			return ErrStopped
		}
		if err := requireTurnPermit(ctx, tx, turn.ID); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM model_attempts WHERE turn_id=?", p.TurnID).Scan(&count); err != nil {
			return err
		}
		if count >= 1024 {
			return ErrLimit
		}
		var used int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM model_attempts WHERE turn_id=? AND logical_id=? AND number=?", p.TurnID, p.LogicalID, p.Number).Scan(&used); err != nil {
			return err
		}
		if used != 0 {
			return ErrConflict
		}
		ancestors, err := reserveBudgets(ctx, tx, turn.SessionID, p.Request)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO model_attempts (id,turn_id,logical_id,number,request,state,cost_source,created_at) VALUES (?,?,?,?,?,'reserved','unknown',?)`, p.ID, p.TurnID, p.LogicalID, p.Number, raw, now()); err != nil {
			return err
		}
		for _, ancestor := range ancestors {
			if _, err := tx.ExecContext(ctx, "INSERT INTO attempt_budget_ancestors VALUES (?,?)", p.ID, ancestor); err != nil {
				return err
			}
		}
		result, err = readAttempt(ctx, tx, p.ID)
		return err
	})
	return
}

// DispatchModelAttempt grants the one dispatch right. A false result means the
// attempt was already dispatched or settled; it must never be sent again.
func (s *Store) DispatchModelAttempt(ctx context.Context, id session.ModelAttemptID) (dispatch bool, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		attempt, err := readAttempt(ctx, tx, id)
		if err != nil {
			return err
		}
		if attempt.State != session.AttemptReserved {
			return nil
		}
		turn, err := readTurn(ctx, tx, attempt.TurnID)
		if err != nil {
			return err
		}
		if turn.State != session.Running {
			return ErrStopped
		}
		if err := requireTurnPermit(ctx, tx, turn.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE model_attempts SET state='dispatched',dispatched_at=? WHERE id=?", now(), id)
		if err == nil {
			dispatch = true
		}
		return err
	})
	if err != nil {
		dispatch = false
	}
	return
}

func (s *Store) ModelAttempt(ctx context.Context, id session.ModelAttemptID) (session.ModelAttempt, error) {
	return readAttempt(ctx, s.db, id)
}

func (s *Store) SettleModelAttempt(ctx context.Context, id session.ModelAttemptID, outcome session.ModelAttemptResult, message *session.MessageDraft) (result session.ModelAttempt, err error) {
	if err := outcome.Validate(); err != nil {
		return result, err
	}
	if message != nil {
		if message.Role != session.Assistant {
			return result, fmt.Errorf("%w: model result must be an assistant message", session.ErrInvalid)
		}
		if err := validDraft(*message); err != nil {
			return result, err
		}
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		attempt, err := readAttempt(ctx, tx, id)
		if err != nil {
			return err
		}
		result, err = settleAttempt(ctx, tx, attempt, outcome, message)
		return err
	})
	return
}

func settleAttempt(ctx context.Context, tx *sql.Tx, attempt session.ModelAttempt, outcome session.ModelAttemptResult, message *session.MessageDraft) (session.ModelAttempt, error) {
	var messageID *session.MessageID
	if message != nil {
		messageID = &message.ID
	}
	if attempt.FinishedAt != nil {
		if !reflect.DeepEqual(attempt.Result, &outcome) || !reflect.DeepEqual(attempt.MessageID, messageID) {
			return session.ModelAttempt{}, ErrConflict
		}
		if message != nil {
			turn, err := readTurn(ctx, tx, attempt.TurnID)
			if err != nil {
				return session.ModelAttempt{}, err
			}
			if _, err := appendMessage(ctx, tx, turn, *message); err != nil {
				return session.ModelAttempt{}, err
			}
		}
		return attempt, nil
	}
	var cost *int64
	var note *string
	source := "unknown"
	if attempt.DispatchedAt != nil && outcome.State == session.AttemptCancelled {
		return session.ModelAttempt{}, ErrConflict
	}
	if attempt.DispatchedAt == nil {
		if outcome.State != session.AttemptCancelled || message != nil || !reflect.DeepEqual(outcome.Usage, session.ModelUsage{}) || outcome.ReportedCostNanoUSD != nil || (outcome.ElapsedMillis != nil && *outcome.ElapsedMillis != 0) {
			return session.ModelAttempt{}, ErrConflict
		}
		cost = new(int64(0))
		source = "not_dispatched"
	} else if outcome.ReportedCostNanoUSD != nil {
		cost = outcome.ReportedCostNanoUSD
		source = "provider"
	} else {
		var err error
		cost, err = attempt.Request.Prices.Cost(outcome.Usage)
		if errors.Is(err, session.ErrCostOverflow) {
			note = new(session.ErrCostOverflow.Error())
		} else if err != nil {
			return session.ModelAttempt{}, err
		}
		if cost != nil {
			source = "prices"
		}
	}
	if message != nil {
		turn, err := readTurn(ctx, tx, attempt.TurnID)
		if err != nil {
			return session.ModelAttempt{}, err
		}
		if _, err := appendMessage(ctx, tx, turn, *message); err != nil {
			return session.ModelAttempt{}, err
		}
	}
	raw, err := encode(outcome)
	if err != nil {
		return session.ModelAttempt{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_attempts SET state=?,result=?,cost_nano_usd=?,cost_source=?,cost_note=?,message_id=?,finished_at=? WHERE id=?`, outcome.State, raw, cost, source, note, messageID, now(), attempt.ID); err != nil {
		return session.ModelAttempt{}, err
	}
	return readAttempt(ctx, tx, attempt.ID)
}

func (s *Store) ModelAttempts(ctx context.Context, turn session.TurnID, after session.ModelAttemptID, limit int) ([]session.ModelAttempt, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, attemptSelect+" WHERE turn_id=? AND id>? ORDER BY id LIMIT ?", turn, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.ModelAttempt{}
	size := 0
	for rows.Next() {
		value, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		raw, err := encode(value)
		if err != nil {
			return nil, err
		}
		size += len(raw)
		if size > MaxPageBytes {
			break
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func pendingAttempts(ctx context.Context, tx *sql.Tx) ([]session.ModelAttempt, error) {
	rows, err := tx.QueryContext(ctx, attemptSelect+" WHERE finished_at IS NULL ORDER BY id LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var pending []session.ModelAttempt
	for rows.Next() {
		attempt, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		pending = append(pending, attempt)
	}
	return pending, errors.Join(rows.Err(), rows.Close())
}

func recoverAttempts(ctx context.Context, tx *sql.Tx) error {
	for {
		pending, err := pendingAttempts(ctx, tx)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			return nil
		}
		for _, attempt := range pending {
			state := session.AttemptUncertain
			if attempt.DispatchedAt == nil {
				state = session.AttemptCancelled
			}
			if _, err := settleAttempt(ctx, tx, attempt, session.ModelAttemptResult{State: state, Failure: new("runtime restarted before attempt settlement")}, nil); err != nil {
				return err
			}
		}
	}
}
