package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

func cancelTurn(ctx context.Context, tx *sql.Tx, id session.TurnID) (session.Turn, error) {
	turn, err := readTurn(ctx, tx, id)
	if err != nil {
		return turn, err
	}
	if turn.State == session.Running {
		if _, err := tx.ExecContext(ctx, "UPDATE turns SET state='cancelling' WHERE id=?", id); err != nil {
			return turn, err
		}
	}
	return readTurn(ctx, tx, id)
}

func (s *Store) CancelTurn(ctx context.Context, id session.TurnID) (result session.Turn, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error { var err error; result, err = cancelTurn(ctx, tx, id); return err })
	return
}

func (s *Store) CancelInput(ctx context.Context, id session.InputID) (result session.Input, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		input, err := readInput(ctx, tx, id)
		if err != nil {
			return err
		}
		if input.TurnID != nil {
			if _, err := cancelTurn(ctx, tx, *input.TurnID); err != nil {
				return err
			}
		} else if input.State == session.Queued {
			if _, err := tx.ExecContext(ctx, "UPDATE inputs SET cancelled_at=? WHERE id=?", now(), id); err != nil {
				return err
			}
		}
		result, err = readInput(ctx, tx, id)
		return err
	})
	return
}

// LifecycleChange carries the committed session snapshot and the exact execution
// whose cancellation was requested. It is a transaction result, not stored state.
type LifecycleChange struct {
	Session      session.Session
	CancelTurnID *session.TurnID
}

func (s *Store) SetLifecycle(ctx context.Context, id session.SessionID, lifecycle session.Lifecycle) (result LifecycleChange, err error) {
	if lifecycle != session.Active && lifecycle != session.Stopped {
		return result, fmt.Errorf("%w: invalid lifecycle", session.ErrInvalid)
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		if _, err := readSession(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE sessions SET lifecycle=? WHERE id=?", lifecycle, id); err != nil {
			return err
		}
		if lifecycle == session.Stopped {
			if _, err := tx.ExecContext(ctx, "UPDATE turns SET state='cancelling' WHERE session_id=? AND state='running'", id); err != nil {
				return err
			}
			var turn session.TurnID
			err := tx.QueryRowContext(ctx, "SELECT id FROM turns WHERE session_id=? AND state='cancelling'", id).Scan(&turn)
			if err == nil {
				result.CancelTurnID = &turn
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		result.Session, err = readSession(ctx, tx, id)
		return err
	})
	return
}

// Recover must be called only after the runtime obtains exclusive execution
// ownership. It records interruption, never requeues inputs or repeats effects.
func (s *Store) Recover(ctx context.Context) (count int64, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		if err := recoverWorkspace(ctx, tx); err != nil {
			return err
		}
		if err := recoverAttempts(ctx, tx); err != nil {
			return err
		}
		if err := recoverQuestions(ctx, tx); err != nil {
			return err
		}
		if err := recoverOperations(ctx, tx); err != nil {
			return err
		}
		if err := recoverCells(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM turn_permits"); err != nil {
			return err
		}
		// Only newly interrupted turns may pause goals or create reports. Close this
		// cursor before nested reads/writes on the transaction connection.
		rows, err := tx.QueryContext(ctx, "SELECT t.id FROM turns t WHERE t.state IN ('running','cancelling')")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		var interrupted []session.TurnID
		for rows.Next() {
			var id session.TurnID
			if err := rows.Scan(&id); err != nil {
				return errors.Join(err, rows.Close())
			}
			interrupted = append(interrupted, id)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "UPDATE turns SET state='interrupted',failure='runtime restarted',finished_at=? WHERE state IN ('running','cancelling')", now())
		if err != nil {
			return err
		}
		count, err = result.RowsAffected()
		if err != nil {
			return err
		}
		for _, id := range interrupted {
			turn, err := readTurn(ctx, tx, id)
			if err != nil {
				return err
			}
			if err := finishGoal(ctx, tx, turn); err != nil {
				return err
			}
			if err := captureCompletion(ctx, tx, turn); err != nil {
				return err
			}
		}
		return nil
	})
	return
}

const subtree = `WITH RECURSIVE subtree(id) AS (
 SELECT id FROM sessions WHERE id=? UNION ALL
 SELECT s.id FROM sessions s JOIN subtree p ON s.parent_id=p.id
)`

// DeleteSubtree rejects active turns and discards queued work. Receipts retain
// identity and digest so a late retry observes deletion without recreating work.
func (s *Store) DeleteSubtree(ctx context.Context, id session.SessionID) error {
	return s.write(ctx, func(tx *sql.Tx) error { return deleteSubtree(ctx, tx, id) })
}

func deleteSubtree(ctx context.Context, tx *sql.Tx, id session.SessionID) error {
	target, err := readSession(ctx, tx, id)
	if err != nil {
		return err
	}
	var active int
	if err := tx.QueryRowContext(ctx, subtree+" SELECT count(*) FROM turns WHERE session_id IN (SELECT id FROM subtree) AND state IN ('running','cancelling')", id).Scan(&active); err != nil {
		return err
	}
	if active != 0 {
		return ErrBusy
	}
	if err := workspaceDeletionCheck(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, subtree+` UPDATE receipts SET input_id=NULL,deleted_at=?
   WHERE input_id IN (SELECT id FROM inputs WHERE session_id IN (SELECT id FROM subtree))`, id, now()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, subtree+` UPDATE schedules SET first_due=NULL,every_ns=NULL,next_due=NULL,parts=NULL,failure=NULL,cancelled_at=NULL,deleted_at=? WHERE session_id IN (SELECT id FROM subtree) AND deleted_at IS NULL`, id, now()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, subtree+` UPDATE goals SET text=NULL,stop_reason=NULL,completion_turn_id=NULL,completion_operation_id=NULL,deleted_at=? WHERE session_id IN (SELECT id FROM subtree) AND deleted_at IS NULL`, id, now()); err != nil {
		return err
	}
	if err := deleteRecipientMail(ctx, tx, id); err != nil {
		return err
	}
	if target.ParentID == nil {
		if _, err := tx.ExecContext(ctx, "DELETE FROM model_attempts WHERE id IN (SELECT attempt_id FROM attempt_budget_ancestors WHERE session_id=?)", target.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM session_trees WHERE id=?", target.TreeID); err != nil {
			return err
		}
		err = bumpTreeCatalog(ctx, tx)
	} else {
		_, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE id=?", id)
	}
	if err != nil {
		return err
	}
	return releaseDeletedCompletionSlot(ctx, tx, id)
}
