package store

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/context-labs/whip/internal/session"
)

func insertPermissionPolicy(ctx context.Context, tx *sql.Tx, tree session.TreeID, mode session.PermissionMode) error {
	if err := mode.Validate(); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO permission_policies(tree_id,mode,revision,updated_at) VALUES (?,?,1,?)", tree, mode, now())
	return err
}

func readPermissionPolicy(ctx context.Context, q querier, owner session.SessionID) (policy session.PermissionPolicy, err error) {
	var updated int64
	err = q.QueryRowContext(ctx, `SELECT p.tree_id,p.mode,p.revision,p.updated_at
 FROM permission_policies p JOIN sessions s ON s.tree_id=p.tree_id WHERE s.id=?`, owner).
		Scan(&policy.TreeID, &policy.Mode, &policy.Revision, &updated)
	policy.UpdatedAt = timestamp(updated)
	return policy, found(err)
}

func (s *Store) PermissionPolicy(ctx context.Context, owner session.SessionID) (session.PermissionPolicy, error) {
	return readPermissionPolicy(ctx, s.db, owner)
}

const permissionModeEditSelect = `SELECT id,digest,session_id,tree_id,expected_revision,mode,previous_mode,revision,updated_at,created_at
 FROM permission_mode_edits`

func scanPermissionModeEdit(row scanner) (edit session.PermissionModeEdit, digest string, err error) {
	var updated, created int64
	err = row.Scan(&edit.ID, &digest, &edit.SessionID, &edit.Policy.TreeID, &edit.ExpectedRevision, &edit.Mode,
		&edit.PreviousMode, &edit.Policy.Revision, &updated, &created)
	edit.Policy.Mode, edit.Policy.UpdatedAt, edit.CreatedAt = edit.Mode, timestamp(updated), timestamp(created)
	return edit, digest, found(err)
}

// PermissionModeEdit reads original evidence, including after tree deletion.
func (s *Store) PermissionModeEdit(ctx context.Context, owner session.SessionID, id session.PermissionModeEditID) (session.PermissionModeEdit, error) {
	result, _, err := scanPermissionModeEdit(s.db.QueryRowContext(ctx, permissionModeEditSelect+" WHERE id=? AND session_id=?", id, owner))
	return result, err
}

// SetPermissionMode resolves exact retries before live state and compare-and-set.
// A same-value edit records the original revision without retiring any operation.
func (s *Store) SetPermissionMode(ctx context.Context, request session.PermissionModeRequest) (session.PermissionModeEdit, error) {
	result, _, err := s.ApplyPermissionMode(ctx, request)
	return result, err
}

// ApplyPermissionMode reports whether this commit newly changed live policy.
// The flag is ephemeral: retries and same-value edits never retire newly created
// resources merely because their original receipt describes an older change.
func (s *Store) ApplyPermissionMode(ctx context.Context, request session.PermissionModeRequest) (result session.PermissionModeEdit, changed bool, err error) {
	if err := session.ValidateID(string(request.ID)); err != nil {
		return result, false, err
	}
	digest, err := requestDigest("permission_mode", request)
	if err != nil {
		return result, false, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		previous, previousDigest, err := scanPermissionModeEdit(tx.QueryRowContext(ctx, permissionModeEditSelect+" WHERE id=?", request.ID))
		if err == nil {
			if previousDigest != digest {
				return ErrConflict
			}
			result = previous
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := request.Validate(); err != nil {
			return err
		}
		owner, err := readSession(ctx, tx, request.SessionID)
		if err != nil {
			return err
		}
		if owner.ParentID != nil {
			return ErrConflict
		}
		policy, err := readPermissionPolicy(ctx, tx, owner.ID)
		if err != nil {
			return err
		}
		if policy.Revision != request.ExpectedRevision {
			return ErrConflict
		}
		oldMode := policy.Mode
		if policy.Mode != request.Mode {
			if policy.Revision == math.MaxInt64 {
				return ErrConflict
			}
			policy.Mode, policy.Revision, policy.UpdatedAt = request.Mode, policy.Revision+1, timestamp(now())
			if _, err := tx.ExecContext(ctx, "UPDATE permission_policies SET mode=?,revision=?,updated_at=? WHERE tree_id=?",
				policy.Mode, policy.Revision, policy.UpdatedAt.UnixMicro(), policy.TreeID); err != nil {
				return err
			}
			if err := retirePolicyOperations(ctx, tx, owner.ID); err != nil {
				return err
			}
			changed = true
		}
		created := now()
		if _, err := tx.ExecContext(ctx, `INSERT INTO permission_mode_edits
 (id,digest,session_id,tree_id,expected_revision,mode,previous_mode,revision,updated_at,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			request.ID, digest, owner.ID, policy.TreeID, request.ExpectedRevision, request.Mode, oldMode, policy.Revision, policy.UpdatedAt.UnixMicro(), created); err != nil {
			return err
		}
		result = session.PermissionModeEdit{PermissionModeRequest: request, Policy: policy, PreviousMode: oldMode, CreatedAt: timestamp(created)}
		return nil
	})
	if err != nil {
		return session.PermissionModeEdit{}, false, err
	}
	return result, changed, nil
}

// Pending human approvals become obsolete on an actual policy change. Captured
// automatic authority retires only before dispatch; grants and dispatched work
// retain their ordinary validation and settlement paths.
func retirePolicyOperations(ctx context.Context, tx *sql.Tx, owner session.SessionID) error {
	rows, err := tx.QueryContext(ctx, `SELECT o.id FROM operations o LEFT JOIN cells c ON c.id=o.cell_id JOIN turns t ON t.id=COALESCE(c.turn_id,o.direct_turn_id)
 WHERE t.session_id=? AND (o.state='waiting' OR (o.state='ready' AND o.permission_revision IS NOT NULL)) ORDER BY o.id`, owner)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var ids []session.OperationID
	for rows.Next() {
		var id session.OperationID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	failure := "session permission policy changed before dispatch"
	for _, id := range ids {
		operation, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := settleOperation(ctx, tx, operation, session.OperationResult{State: session.OperationDenied, Failure: &failure}); err != nil {
			return err
		}
	}
	return nil
}
