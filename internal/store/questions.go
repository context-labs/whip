package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/session"
)

const questionSelect = `SELECT q.operation_id,t.session_id,c.turn_id,o.cell_id,o.arguments,o.state,o.result,
 q.created_at,q.deadline,q.close_reason,o.finished_at FROM questions q
 JOIN operations o ON o.id=q.operation_id JOIN cells c ON c.id=o.cell_id JOIN turns t ON t.id=c.turn_id`

func validateQuestionIntent(owner session.SessionID, spec session.OperationSpec) error {
	if spec.Capability != session.QuestionCapability || spec.Resource != string(owner) {
		return ErrConflict
	}
	_, err := session.DecodeQuestionRequest(spec.Arguments)
	return err
}

func scanQuestion(row scanner) (value session.Question, err error) {
	var arguments string
	var state session.OperationState
	var result sql.NullString
	var created, deadline int64
	var closed sql.NullInt64
	err = row.Scan(&value.OperationID, &value.SessionID, &value.TurnID, &value.CellID, &arguments, &state, &result,
		&created, &deadline, &value.CloseReason, &closed)
	if err != nil {
		return value, found(err)
	}
	value.Request, err = session.DecodeQuestionRequest([]byte(arguments))
	if err != nil {
		return value, err
	}
	value.CreatedAt, value.Deadline, value.ClosedAt = timestamp(created), timestamp(deadline), optionalTime(closed)
	value.State = session.QuestionPending
	if value.CloseReason != nil {
		value.State = session.QuestionClosed
	} else if state == session.OperationSucceeded {
		var outcome session.OperationResult
		if err := json.Unmarshal([]byte(result.String), &outcome); err != nil {
			return value, err
		}
		value.Answers, err = value.Request.DecodeAnswers(outcome.Value)
		value.State = session.QuestionDismissed
		for _, answer := range value.Answers {
			if !answer.Dismissed {
				value.State = session.QuestionAnswered
			}
		}
	} else if state != session.OperationDispatched {
		return value, ErrConflict
	}
	return value, err
}

func readQuestion(ctx context.Context, q querier, owner session.SessionID, id session.OperationID) (session.Question, error) {
	return scanQuestion(q.QueryRowContext(ctx, questionSelect+" WHERE t.session_id=? AND q.operation_id=?", owner, id))
}

func (s *Store) Question(ctx context.Context, owner session.SessionID, id session.OperationID) (session.Question, error) {
	return readQuestion(ctx, s.db, owner, id)
}

// Questions reads a bounded, atomically joined view of intent and outcome. A
// pending-only read is for live presentation; history never recreates a waiter.
func (s *Store) Questions(ctx context.Context, owner session.SessionID, pendingOnly bool, after session.OperationID, limit int) ([]session.Question, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, questionSelect+` WHERE t.session_id=? AND q.operation_id>?
 AND (NOT ? OR o.state='dispatched') ORDER BY q.operation_id LIMIT ?`, owner, after, pendingOnly, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.Question{}
	size := 0
	for rows.Next() {
		question, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		raw, err := encode(question)
		if err != nil {
			return nil, err
		}
		size += len(raw)
		if size > MaxPageBytes {
			break
		}
		result = append(result, question)
	}
	return result, rows.Err()
}

// BeginQuestion commits the real host-operation dispatch and pending evidence
// together. Retrying only reads that evidence; it never extends the deadline.
func (s *Store) BeginQuestion(ctx context.Context, id session.OperationID) (result session.Question, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		operation, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := validateQuestionIntent(operation.SessionID, operation.OperationSpec); err != nil {
			return err
		}
		existing, err := readQuestion(ctx, tx, operation.SessionID, id)
		if err == nil {
			result = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		var pending bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM questions q JOIN operations o ON o.id=q.operation_id
 JOIN cells c ON c.id=o.cell_id JOIN turns t ON t.id=c.turn_id WHERE t.session_id=? AND o.state='dispatched')`, operation.SessionID).Scan(&pending); err != nil {
			return err
		}
		if pending {
			return ErrBusy
		}
		dispatch, err := dispatchOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if !dispatch {
			return ErrConflict
		}
		created := now()
		if _, err := tx.ExecContext(ctx, "INSERT INTO questions (operation_id,created_at,deadline) VALUES (?,?,?)", id, created, created+session.QuestionWait.Microseconds()); err != nil {
			return err
		}
		result, err = readQuestion(ctx, tx, operation.SessionID, id)
		return err
	})
	return
}

// AnswerQuestion settles the ordinary operation with the sole durable answer.
// Exact retries precede deadline, cell, turn and lifecycle checks.
func (s *Store) AnswerQuestion(ctx context.Context, owner session.SessionID, id session.OperationID, answers []session.QuestionAnswer) (result session.Question, err error) {
	var expired bool
	err = s.write(ctx, func(tx *sql.Tx) error {
		question, err := readQuestion(ctx, tx, owner, id)
		if err != nil {
			return err
		}
		value, err := question.Request.AnswerValue(answers)
		if err != nil {
			return err
		}
		operation, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if operation.State == session.OperationSucceeded && operation.Result != nil && bytes.Equal(operation.Result.Value, value) {
			result = question
			return nil
		}
		if question.State != session.QuestionPending {
			return ErrConflict
		}
		if !time.Now().Before(question.Deadline) {
			result, err = closeQuestion(ctx, tx, question, session.QuestionExpired)
			expired = err == nil
			return err
		}
		if err := authorizeOperation(ctx, tx, operation); err != nil {
			return err
		}
		if _, err := settleOperation(ctx, tx, operation, session.OperationResult{State: session.OperationSucceeded, Value: value}); err != nil {
			return err
		}
		result, err = readQuestion(ctx, tx, owner, id)
		return err
	})
	if err == nil && expired {
		err = ErrStopped
	}
	return
}

// CloseQuestion is a SQL-only, idempotent join of cancellation with an answer.
// Whichever terminal outcome committed first is returned unchanged.
func (s *Store) CloseQuestion(ctx context.Context, owner session.SessionID, id session.OperationID, reason session.QuestionCloseReason) (result session.Question, err error) {
	if reason != session.QuestionCancelled && reason != session.QuestionExpired && reason != session.QuestionInterrupted {
		return result, session.ErrInvalid
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		question, err := readQuestion(ctx, tx, owner, id)
		if err != nil {
			return err
		}
		if reason == session.QuestionExpired && question.State == session.QuestionPending && time.Now().Before(question.Deadline) {
			return ErrConflict
		}
		result, err = closeQuestion(ctx, tx, question, reason)
		return err
	})
	return
}

func closeQuestion(ctx context.Context, tx *sql.Tx, question session.Question, reason session.QuestionCloseReason) (session.Question, error) {
	if question.State != session.QuestionPending {
		return question, nil
	}
	operation, err := readOperation(ctx, tx, question.OperationID)
	if err != nil {
		return question, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE questions SET close_reason=? WHERE operation_id=?", reason, question.OperationID); err != nil {
		return question, err
	}
	failure := "human question " + string(reason)
	if _, err := settleOperation(ctx, tx, operation, session.OperationResult{State: session.OperationFailed, Failure: &failure}); err != nil {
		return question, err
	}
	return readQuestion(ctx, tx, question.SessionID, question.OperationID)
}

func recoverQuestions(ctx context.Context, tx *sql.Tx) error {
	for {
		question, err := scanQuestion(tx.QueryRowContext(ctx, questionSelect+" WHERE o.state='dispatched' ORDER BY q.operation_id LIMIT 1"))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := closeQuestion(ctx, tx, question, session.QuestionInterrupted); err != nil {
			return err
		}
	}
}
