package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

const maxWaitInputs = 128

type ChildWait struct {
	InputIDs []session.InputID `json:"input_ids"`
}

type ChildWaitRegistration struct {
	InputIDs []session.InputID `json:"input_ids"`
	Boundary string            `json:"boundary"`
}

// RegisterChildWait records intent to wait at the completed cell boundary. No
// live guest continuation or kernel permit is held by this database operation.
func (s *Store) RegisterChildWait(ctx context.Context, id session.OperationID) (result ChildWaitRegistration, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		operation, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if operation.Capability != "agents.wait_after_cell" {
			return ErrConflict
		}
		owner, err := readSession(ctx, tx, operation.SessionID)
		if err != nil {
			return err
		}
		if operation.Resource != string(owner.TreeID) {
			return ErrConflict
		}
		if operation.State == session.OperationSucceeded && operation.Result != nil {
			return json.Unmarshal(operation.Result.Value, &result)
		}
		var request ChildWait
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return err
		}
		if len(request.InputIDs) < 1 || len(request.InputIDs) > maxWaitInputs {
			return fmt.Errorf("%w: expected 1-128 child inputs", session.ErrInvalid)
		}
		seen := map[session.InputID]bool{}
		for _, inputID := range request.InputIDs {
			if session.ValidateID(string(inputID)) != nil || seen[inputID] {
				return fmt.Errorf("%w: invalid or duplicate child input", session.ErrInvalid)
			}
			seen[inputID] = true
			if _, err := descendantInput(ctx, tx, owner.ID, inputID); err != nil {
				return err
			}
		}
		existing, err := cellWaitInputs(ctx, tx, operation.CellID)
		if err != nil {
			return err
		}
		for _, inputID := range existing {
			seen[inputID] = true
		}
		if len(seen) > maxWaitInputs {
			return ErrLimit
		}
		dispatch, err := dispatchOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if !dispatch {
			return ErrConflict
		}
		operation.State = session.OperationDispatched
		result = ChildWaitRegistration{InputIDs: request.InputIDs, Boundary: "after_cell"}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		_, err = settleOperation(ctx, tx, operation, session.OperationResult{State: session.OperationSucceeded, Value: raw})
		return err
	})
	return
}

func descendantInput(ctx context.Context, q querier, owner session.SessionID, id session.InputID) (session.Input, error) {
	input, err := readInput(ctx, q, id)
	if err != nil {
		return input, err
	}
	var descendant bool
	err = q.QueryRowContext(ctx, `WITH RECURSIVE ancestors(id,parent_id) AS (
  SELECT id,parent_id FROM sessions WHERE id=? UNION ALL
  SELECT s.id,s.parent_id FROM sessions s JOIN ancestors a ON a.parent_id=s.id
 ) SELECT EXISTS(SELECT 1 FROM ancestors WHERE parent_id=?)`, input.SessionID, owner).Scan(&descendant)
	if err != nil {
		return input, err
	}
	if !descendant {
		return input, fmt.Errorf("%w: wait target must be a descendant input", session.ErrInvalid)
	}
	return input, nil
}

func cellWaitInputs(ctx context.Context, q querier, cell session.CellID) ([]session.InputID, error) {
	rows, err := q.QueryContext(ctx, `SELECT result FROM operations WHERE cell_id=? AND capability='agents.wait_after_cell' AND state='succeeded' ORDER BY id`, cell)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.InputID{}
	seen := map[session.InputID]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var outcome session.OperationResult
		if err := json.Unmarshal([]byte(raw), &outcome); err != nil {
			return nil, err
		}
		var registration ChildWaitRegistration
		if err := json.Unmarshal(outcome.Value, &registration); err != nil {
			return nil, err
		}
		for _, id := range registration.InputIDs {
			if !seen[id] {
				seen[id] = true
				result = append(result, id)
			}
			if len(result) > maxWaitInputs {
				return nil, ErrLimit
			}
		}
	}
	return result, rows.Err()
}

func (s *Store) CellWaitInputs(ctx context.Context, cell session.CellID) ([]session.InputID, error) {
	return cellWaitInputs(ctx, s.db, cell)
}

// ChildInputsComplete is a bounded snapshot. Cancellation is completion; a
// failed or interrupted child remains an observable outcome, not a parent fault.
func (s *Store) ChildInputsComplete(ctx context.Context, owner session.SessionID, inputs []session.InputID) (complete bool, err error) {
	if len(inputs) < 1 || len(inputs) > maxWaitInputs {
		return false, session.ErrInvalid
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		complete = true
		for _, id := range inputs {
			input, err := descendantInput(ctx, tx, owner, id)
			if err != nil {
				return err
			}
			if input.State == session.InputCancelled {
				continue
			}
			if input.TurnID == nil {
				complete = false
				continue
			}
			turn, err := readTurn(ctx, tx, *input.TurnID)
			if err != nil {
				return err
			}
			if !turn.State.Terminal() {
				complete = false
			}
		}
		return nil
	})
	return
}
