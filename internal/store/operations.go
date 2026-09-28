package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/context-labs/whip/internal/session"
)

const operationSelect = `SELECT o.id,o.cell_id,o.request_id,o.capability,o.resource,o.arguments,
 t.session_id,c.turn_id,o.state,o.grant_id,o.result,o.created_at,o.dispatched_at,o.finished_at
 FROM operations o JOIN cells c ON c.id=o.cell_id JOIN turns t ON t.id=c.turn_id`

const (
	grantSelect      = `SELECT id,session_id,capability,resource,operation_id,created_at,revoked_at FROM grants`
	permissionSelect = `SELECT p.operation_id,p.state,p.created_at,p.resolved_at FROM permissions p`
)

func scanOperation(row scanner) (value session.Operation, err error) {
	var arguments string
	var result sql.NullString
	var created int64
	var dispatched, finished sql.NullInt64
	err = row.Scan(&value.ID, &value.CellID, &value.RequestID, &value.Capability, &value.Resource, &arguments,
		&value.SessionID, &value.TurnID, &value.State, &value.GrantID, &result, &created, &dispatched, &finished)
	if err != nil {
		return value, found(err)
	}
	value.Arguments = json.RawMessage(arguments)
	value.CreatedAt, value.DispatchedAt, value.FinishedAt = timestamp(created), optionalTime(dispatched), optionalTime(finished)
	if result.Valid {
		err = json.Unmarshal([]byte(result.String), &value.Result)
	}
	return
}

func scanGrant(row scanner) (value session.Grant, err error) {
	var created int64
	var revoked sql.NullInt64
	err = row.Scan(&value.ID, &value.SessionID, &value.Capability, &value.Resource, &value.OperationID, &created, &revoked)
	value.CreatedAt, value.RevokedAt = timestamp(created), optionalTime(revoked)
	return value, found(err)
}

func scanPermission(row scanner) (value session.Permission, err error) {
	var created int64
	var resolved sql.NullInt64
	err = row.Scan(&value.OperationID, &value.State, &created, &resolved)
	value.CreatedAt, value.ResolvedAt = timestamp(created), optionalTime(resolved)
	return value, found(err)
}

func readOperation(ctx context.Context, q querier, id session.OperationID) (session.Operation, error) {
	return scanOperation(q.QueryRowContext(ctx, operationSelect+" WHERE o.id=?", id))
}

func readGrant(ctx context.Context, q querier, id session.GrantID) (session.Grant, error) {
	return scanGrant(q.QueryRowContext(ctx, grantSelect+" WHERE id=?", id))
}

func readPermission(ctx context.Context, q querier, id session.OperationID) (session.Permission, error) {
	return scanPermission(q.QueryRowContext(ctx, permissionSelect+" WHERE p.operation_id=?", id))
}

func (s *Store) Operation(ctx context.Context, id session.OperationID) (session.Operation, error) {
	return readOperation(ctx, s.db, id)
}

func operationLive(ctx context.Context, q querier, cellID session.CellID) error {
	var cell session.CellState
	var turn session.TurnState
	var lifecycle session.Lifecycle
	err := q.QueryRowContext(ctx, `SELECT c.state,t.state,s.lifecycle FROM cells c
 JOIN turns t ON t.id=c.turn_id JOIN sessions s ON s.id=t.session_id WHERE c.id=?`, cellID).Scan(&cell, &turn, &lifecycle)
	if err != nil {
		return found(err)
	}
	if cell != session.CellRunning || turn != session.Running || lifecycle != session.Active {
		return ErrStopped
	}
	return nil
}

// AdmitOperation records immutable intent and either an exact standing grant or
// a pending permission. Retrying admission never grants a dispatch right.
func (s *Store) AdmitOperation(ctx context.Context, spec session.OperationSpec) (result session.Operation, err error) {
	if err := spec.Validate(); err != nil {
		return result, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, spec.Arguments); err != nil {
		return result, err
	}
	spec.Arguments = compact.Bytes()
	err = s.write(ctx, func(tx *sql.Tx) error {
		existing, err := readOperation(ctx, tx, spec.ID)
		if err == nil {
			if !reflect.DeepEqual(existing.OperationSpec, spec) {
				return ErrConflict
			}
			result = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		cell, err := scanCell(tx.QueryRowContext(ctx, cellSelect+" WHERE c.id=?", spec.CellID))
		if err != nil {
			return err
		}
		if err := operationLive(ctx, tx, cell.ID); err != nil {
			return err
		}
		var count, duplicate int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM operations o JOIN cells c ON c.id=o.cell_id WHERE c.turn_id=?", cell.TurnID).Scan(&count); err != nil {
			return err
		}
		if count >= session.MaxOperationsPerTurn {
			return ErrLimit
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM operations WHERE cell_id=? AND request_id=?", spec.CellID, spec.RequestID).Scan(&duplicate); err != nil {
			return err
		}
		if duplicate != 0 {
			return ErrConflict
		}
		grant, err := scanGrant(tx.QueryRowContext(ctx, grantSelect+" WHERE session_id=? AND capability=? AND resource=? AND operation_id IS NULL AND revoked_at IS NULL ORDER BY id LIMIT 1", cell.SessionID, spec.Capability, spec.Resource))
		var grantID *session.GrantID
		state := session.OperationWaiting
		if err == nil {
			grantID, state = &grant.ID, session.OperationReady
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		created := now()
		if _, err := tx.ExecContext(ctx, `INSERT INTO operations
 (id,cell_id,request_id,capability,resource,arguments,state,grant_id,created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			spec.ID, spec.CellID, spec.RequestID, spec.Capability, spec.Resource, string(spec.Arguments), state, grantID, created); err != nil {
			return err
		}
		if state == session.OperationWaiting {
			if _, err := tx.ExecContext(ctx, "INSERT INTO permissions (operation_id,state,created_at) VALUES (?,'pending',?)", spec.ID, created); err != nil {
				return err
			}
		}
		result, err = readOperation(ctx, tx, spec.ID)
		return err
	})
	return
}

// DispatchOperation is the authorization linearization point. Only a successful
// commit returns true; cancellation or revocation that committed first wins.
func (s *Store) DispatchOperation(ctx context.Context, id session.OperationID) (dispatch bool, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		operation, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if operation.State != session.OperationReady {
			return nil
		}
		if err := operationLive(ctx, tx, operation.CellID); err != nil {
			return err
		}
		if operation.GrantID == nil {
			return ErrConflict
		}
		grant, err := readGrant(ctx, tx, *operation.GrantID)
		if err != nil {
			return err
		}
		if grant.RevokedAt != nil || grant.SessionID != operation.SessionID || grant.Capability != operation.Capability || grant.Resource != operation.Resource || (grant.OperationID != nil && *grant.OperationID != operation.ID) {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, "UPDATE operations SET state='dispatched',dispatched_at=? WHERE id=?", now(), id)
		dispatch = err == nil
		return err
	})
	return dispatch && err == nil, err
}

func (s *Store) SettleOperation(ctx context.Context, id session.OperationID, outcome session.OperationResult) (result session.Operation, err error) {
	if err := outcome.Validate(); err != nil {
		return result, err
	}
	// Round-trip the durable JSON form so equivalent escaping and whitespace do
	// not turn a retry into a different settlement.
	raw, err := encode(outcome)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal([]byte(raw), &outcome); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		operation, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		result, err = settleOperation(ctx, tx, operation, outcome)
		return err
	})
	return
}

func settleOperation(ctx context.Context, tx *sql.Tx, operation session.Operation, outcome session.OperationResult) (session.Operation, error) {
	if operation.State.Terminal() {
		if !reflect.DeepEqual(operation.Result, &outcome) {
			return session.Operation{}, ErrConflict
		}
		return operation, nil
	}
	if operation.State == session.OperationDispatched {
		if outcome.State != session.OperationSucceeded && outcome.State != session.OperationFailed && outcome.State != session.OperationUncertain {
			return session.Operation{}, ErrConflict
		}
	} else if outcome.State != session.OperationCancelled && outcome.State != session.OperationDenied {
		return session.Operation{}, ErrConflict
	}
	raw, err := encode(outcome)
	if err != nil {
		return session.Operation{}, err
	}
	finished := now()
	if _, err := tx.ExecContext(ctx, "UPDATE operations SET state=?,result=?,finished_at=? WHERE id=?", outcome.State, raw, finished, operation.ID); err != nil {
		return session.Operation{}, err
	}
	permissionState := session.PermissionCancelled
	if outcome.State == session.OperationDenied {
		permissionState = session.PermissionDenied
	}
	if _, err := tx.ExecContext(ctx, "UPDATE permissions SET state=?,resolved_at=? WHERE operation_id=? AND state='pending'", permissionState, finished, operation.ID); err != nil {
		return session.Operation{}, err
	}
	return readOperation(ctx, tx, operation.ID)
}

func insertGrant(ctx context.Context, tx *sql.Tx, grant session.Grant) error {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM grants WHERE session_id=?", grant.SessionID).Scan(&count); err != nil {
		return err
	}
	if count >= session.MaxGrantsPerSession {
		return ErrLimit
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO grants (id,session_id,capability,resource,operation_id,created_at) VALUES (?,?,?,?,?,?)",
		grant.ID, grant.SessionID, grant.Capability, grant.Resource, grant.OperationID, now())
	return err
}

// CreateGrant explicitly creates standing authority. One-use grants may only be
// created by resolving the permission for their admitted operation.
func (s *Store) CreateGrant(ctx context.Context, grant session.Grant) (result session.Grant, err error) {
	if err := grant.Validate(); err != nil {
		return result, err
	}
	if grant.OperationID != nil || grant.RevokedAt != nil {
		return result, fmt.Errorf("%w: create requires an unrevoked standing grant", session.ErrInvalid)
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		existing, err := readGrant(ctx, tx, grant.ID)
		if err == nil {
			if existing.SessionID != grant.SessionID || existing.Capability != grant.Capability || existing.Resource != grant.Resource || existing.OperationID != nil {
				return ErrConflict
			}
			result = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if _, err := readSession(ctx, tx, grant.SessionID); err != nil {
			return err
		}
		if err := insertGrant(ctx, tx, grant); err != nil {
			return err
		}
		result, err = readGrant(ctx, tx, grant.ID)
		return err
	})
	return
}

func (s *Store) ResolvePermission(ctx context.Context, id session.OperationID, approved bool) (result session.Permission, err error) {
	decision := session.PermissionDenied
	if approved {
		decision = session.PermissionApproved
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		permission, err := readPermission(ctx, tx, id)
		if err != nil {
			return err
		}
		if permission.State != session.PermissionPending {
			if permission.State != decision {
				return ErrConflict
			}
			result = permission
			return nil
		}
		operation, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if operation.State != session.OperationWaiting {
			return ErrConflict
		}
		if err := operationLive(ctx, tx, operation.CellID); err != nil {
			return err
		}
		if approved {
			grant := session.Grant{
				ID: session.GrantID(newID("grant")), SessionID: operation.SessionID,
				Capability: operation.Capability, Resource: operation.Resource, OperationID: &operation.ID,
			}
			if err := insertGrant(ctx, tx, grant); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='ready',grant_id=? WHERE id=?", grant.ID, id); err != nil {
				return err
			}
		} else {
			if _, err := settleOperation(ctx, tx, operation, session.OperationResult{State: session.OperationDenied, Failure: new("permission denied")}); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE permissions SET state=?,resolved_at=? WHERE operation_id=? AND state='pending'", decision, now(), id); err != nil {
			return err
		}
		result, err = readPermission(ctx, tx, id)
		return err
	})
	return
}

func (s *Store) RevokeGrant(ctx context.Context, id session.GrantID) (result session.Grant, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		grant, err := readGrant(ctx, tx, id)
		if err != nil {
			return err
		}
		if grant.RevokedAt != nil {
			result = grant
			return nil
		}
		if _, err := tx.ExecContext(ctx, "UPDATE grants SET revoked_at=? WHERE id=?", now(), id); err != nil {
			return err
		}
		// Each statement is bounded to the owner's operations and grants. No
		// external cancellation occurs inside this transaction.
		for {
			operation, err := scanOperation(tx.QueryRowContext(ctx, operationSelect+" WHERE o.grant_id=? AND o.state='ready' ORDER BY o.id LIMIT 1", id))
			if errors.Is(err, ErrNotFound) {
				break
			}
			if err != nil {
				return err
			}
			if _, err := settleOperation(ctx, tx, operation, session.OperationResult{State: session.OperationDenied, Failure: new("grant revoked before dispatch")}); err != nil {
				return err
			}
		}
		result, err = readGrant(ctx, tx, id)
		return err
	})
	return
}

func recoverOperations(ctx context.Context, tx *sql.Tx) error {
	for {
		operation, err := scanOperation(tx.QueryRowContext(ctx, operationSelect+" WHERE o.finished_at IS NULL ORDER BY o.id LIMIT 1"))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		state := session.OperationCancelled
		if operation.State == session.OperationDispatched {
			state = session.OperationUncertain
		}
		if _, err := settleOperation(ctx, tx, operation, session.OperationResult{State: state, Failure: new("runtime restarted before operation settlement")}); err != nil {
			return err
		}
	}
}

func (s *Store) Operations(ctx context.Context, turn session.TurnID, after session.OperationID, limit int) ([]session.Operation, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, operationSelect+" WHERE c.turn_id=? AND o.id>? ORDER BY o.id LIMIT ?", turn, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.Operation{}
	size := 0
	for rows.Next() {
		value, err := scanOperation(rows)
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

func (s *Store) Grants(ctx context.Context, owner session.SessionID, after session.GrantID, limit int) ([]session.Grant, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, grantSelect+" WHERE session_id=? AND id>? ORDER BY id LIMIT ?", owner, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.Grant{}
	size := 0
	for rows.Next() {
		value, err := scanGrant(rows)
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

func (s *Store) Permissions(ctx context.Context, owner session.SessionID, after session.OperationID, limit int) ([]session.Permission, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, permissionSelect+` JOIN operations o ON o.id=p.operation_id
 JOIN cells c ON c.id=o.cell_id JOIN turns t ON t.id=c.turn_id
 WHERE t.session_id=? AND p.operation_id>? ORDER BY p.operation_id LIMIT ?`, owner, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.Permission{}
	size := 0
	for rows.Next() {
		value, err := scanPermission(rows)
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
