package store

import (
	"context"
	"database/sql"
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

func (s *Store) SetLifecycle(ctx context.Context, id session.SessionID, lifecycle session.Lifecycle) (result session.Session, err error) {
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
		}
		result, err = readSession(ctx, tx, id)
		return err
	})
	return
}

// Recover must be called only after the runtime obtains exclusive execution
// ownership. It records interruption, never requeues inputs or repeats effects.
func (s *Store) Recover(ctx context.Context) (count int64, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		if err := recoverAttempts(ctx, tx); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "UPDATE turns SET state='interrupted',failure='runtime restarted',finished_at=? WHERE state IN ('running','cancelling')", now())
		if err != nil {
			return err
		}
		count, err = result.RowsAffected()
		return err
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
	return s.write(ctx, func(tx *sql.Tx) error {
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
		if _, err := tx.ExecContext(ctx, subtree+` UPDATE receipts SET input_id=NULL,deleted_at=?
   WHERE input_id IN (SELECT id FROM inputs WHERE session_id IN (SELECT id FROM subtree))`, id, now()); err != nil {
			return err
		}
		if target.ParentID == nil {
			_, err = tx.ExecContext(ctx, "DELETE FROM session_trees WHERE id=?", target.TreeID)
		} else {
			_, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE id=?", id)
		}
		return err
	})
}
