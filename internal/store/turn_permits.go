package store

import (
	"context"
	"database/sql"

	"github.com/context-labs/whip/internal/session"
)

// A permit authorizes execution; a turn may remain running while waiting without
// one. The runtime's exclusive owner carries it to exactly one turn goroutine.
func requireTurnPermit(ctx context.Context, q querier, id session.TurnID) error {
	var present bool
	if err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM turn_permits WHERE turn_id=?)", id).Scan(&present); err != nil {
		return err
	}
	if !present {
		return ErrBusy
	}
	return nil
}

func acquireTurnPermit(ctx context.Context, tx *sql.Tx, turn session.Turn) error {
	if turn.State != session.Running {
		return ErrStopped
	}
	owner, err := readSession(ctx, tx, turn.SessionID)
	if err != nil {
		return err
	}
	if owner.Lifecycle != session.Active {
		return ErrStopped
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO turn_permits (turn_id) VALUES (?) ON CONFLICT DO NOTHING", turn.ID); err != nil {
		return err
	}
	return checkResources(ctx, tx, owner.ID, session.ResourceRunnableDescendants)
}

// ResumeTurn reacquires the same turn's permission without claiming another input
// or replaying work. Only the owning runtime may use or retry this transition.
func (s *Store) ResumeTurn(ctx context.Context, id session.TurnID) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		turn, err := readTurn(ctx, tx, id)
		if err != nil {
			return err
		}
		return acquireTurnPermit(ctx, tx, turn)
	})
}

func unfinishedExecution(ctx context.Context, q querier, id session.TurnID) (bool, error) {
	var pending bool
	err := q.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM model_attempts WHERE turn_id=? AND finished_at IS NULL) OR
 EXISTS(SELECT 1 FROM cells WHERE turn_id=? AND state='running') OR
 EXISTS(SELECT 1 FROM operations o JOIN cells c ON c.id=o.cell_id WHERE c.turn_id=? AND o.finished_at IS NULL)`, id, id, id).Scan(&pending)
	return pending, err
}

// YieldTurn can release capacity only at a settled execution boundary. It does
// not finish the turn, cancel input, discard its checkpoint or release ownership.
func (s *Store) YieldTurn(ctx context.Context, id session.TurnID) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		turn, err := readTurn(ctx, tx, id)
		if err != nil {
			return err
		}
		if turn.State != session.Running {
			return ErrStopped
		}
		pending, err := unfinishedExecution(ctx, tx, id)
		if err != nil {
			return err
		}
		if pending {
			return ErrBusy
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM turn_permits WHERE turn_id=?", id)
		return err
	})
}
