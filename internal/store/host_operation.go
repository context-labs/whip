package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

// AdmitHostOperation resolves exact retries before current lifecycle, busy or
// configuration checks. A second direct action cannot join an existing queue;
// ordinary prompt admissions can queue behind this accepted input.
func (s *Store) AdmitHostOperation(ctx context.Context, identity session.RequestIdentity, owner session.SessionID, operation session.HostOperation) (result Admission, err error) {
	if err := validatePublicIdentity(identity); err != nil {
		return result, err
	}
	for _, id := range []string{identity.ClientID, identity.RequestID, string(owner)} {
		if err := session.ValidateID(id); err != nil {
			return result, err
		}
	}
	request, digest, err := hostOperationSubmission(owner, operation)
	if err != nil {
		return result, err
	}
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
		var busy bool
		if err := tx.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM turns WHERE session_id=? AND state IN ('running','cancelling')) OR
 EXISTS(SELECT 1 FROM inputs WHERE session_id=? AND turn_id IS NULL AND steered_turn_id IS NULL AND cancelled_at IS NULL) OR
 EXISTS(SELECT 1 FROM workspace_actions WHERE session_id=? AND state='claimed')`, owner, owner, owner).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return ErrBusy
		}
		result, err = admitInput(ctx, tx, identity, digest, request)
		return err
	})
	return
}

func readHostOperation(ctx context.Context, q querier, input session.InputID) (session.HostOperation, error) {
	var value session.HostOperation
	var raw string
	err := q.QueryRowContext(ctx, "SELECT module,name,arguments FROM host_operation_inputs WHERE input_id=?", input).Scan(&value.Module, &value.Name, &raw)
	value.Arguments = []byte(raw)
	return value, found(err)
}

func requireNoDirectWork(ctx context.Context, q querier, owner session.SessionID) error {
	var busy bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inputs i LEFT JOIN turns t ON t.id=i.turn_id
 WHERE i.session_id=? AND i.kind='host_operation' AND ((i.turn_id IS NULL AND i.steered_turn_id IS NULL AND i.cancelled_at IS NULL) OR t.state IN ('running','cancelling')))`, owner).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return ErrBusy
	}
	return nil
}

func directOperationLive(ctx context.Context, q querier, id session.TurnID) (session.Turn, session.HostOperation, error) {
	turn, err := readTurn(ctx, q, id)
	if err != nil {
		return turn, session.HostOperation{}, err
	}
	if turn.Kind != session.HostOperationInputKind {
		return turn, session.HostOperation{}, ErrConflict
	}
	owner, err := readSession(ctx, q, turn.SessionID)
	if err != nil {
		return turn, session.HostOperation{}, err
	}
	if turn.State != session.Running || owner.Lifecycle != session.Active {
		return turn, session.HostOperation{}, ErrStopped
	}
	if err := requireTurnPermit(ctx, q, turn.ID); err != nil {
		return turn, session.HostOperation{}, err
	}
	var input session.InputID
	if err := q.QueryRowContext(ctx, "SELECT id FROM inputs WHERE turn_id=?", id).Scan(&input); err != nil {
		return turn, session.HostOperation{}, found(err)
	}
	operation, err := readHostOperation(ctx, q, input)
	return turn, operation, err
}

func operationOwnerLive(ctx context.Context, q querier, spec session.OperationSpec) (session.SessionID, session.TurnID, error) {
	if spec.DirectTurnID != "" {
		turn, accepted, err := directOperationLive(ctx, q, spec.DirectTurnID)
		if err != nil {
			return "", "", err
		}
		if !accepted.DirectCapability(spec.Capability) {
			return "", "", fmt.Errorf("%w: capability differs from direct input", ErrConflict)
		}
		return turn.SessionID, turn.ID, nil
	}
	if err := operationLive(ctx, q, spec.CellID); err != nil {
		return "", "", err
	}
	cell, err := scanCell(q.QueryRowContext(ctx, cellSelect+" WHERE c.id=?", spec.CellID))
	return cell.SessionID, cell.TurnID, err
}

// HostTurnSession returns the configuration captured by genuine direct work.
func (s *Store) HostTurnSession(ctx context.Context, owner session.SessionID, id session.TurnID) (result session.Session, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		turn, _, err := directOperationLive(ctx, tx, id)
		if err != nil {
			return err
		}
		if turn.SessionID != owner {
			return ErrConflict
		}
		result, err = readSession(ctx, tx, owner)
		if err != nil {
			return err
		}
		result, err = capturedSession(ctx, tx, result, turn.ConfigRevision)
		result.ConfigRevision = turn.ConfigRevision
		return err
	})
	return
}
