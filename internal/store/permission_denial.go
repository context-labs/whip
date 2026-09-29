package store

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/context-labs/whip/internal/session"
)

const permissionDenialSelect = `SELECT id,digest,session_id,tree_id,expected_revision,deny_interactive,previous_denial,mode,revision,updated_at,created_at FROM permission_denial_edits`

func scanPermissionDenial(row scanner) (edit session.PermissionDenialEdit, digest string, err error) {
	var updated, created int64
	err = row.Scan(&edit.ID, &digest, &edit.SessionID, &edit.Policy.TreeID, &edit.ExpectedRevision, &edit.DenyInteractive, &edit.PreviousDenial, &edit.Policy.Mode, &edit.Policy.Revision, &updated, &created)
	edit.Policy.DenyInteractive = edit.DenyInteractive
	edit.Policy.UpdatedAt, edit.CreatedAt = timestamp(updated), timestamp(created)
	return edit, digest, found(err)
}

func (s *Store) PermissionDenialEdit(ctx context.Context, owner session.SessionID, id string) (session.PermissionDenialEdit, error) {
	edit, _, err := scanPermissionDenial(s.db.QueryRowContext(ctx, permissionDenialSelect+" WHERE id=? AND session_id=?", id, owner))
	return edit, err
}

// ApplyPermissionDenial retains original results before checking live policy.
// The ephemeral changed flag prevents exact retries from retiring later resources.
func (s *Store) ApplyPermissionDenial(ctx context.Context, request session.PermissionDenialRequest) (result session.PermissionDenialEdit, changed bool, err error) {
	if err := session.ValidateID(request.ID); err != nil {
		return result, false, err
	}
	digest, err := requestDigest("permission_denial", request)
	if err != nil {
		return result, false, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		previous, priorDigest, err := scanPermissionDenial(tx.QueryRowContext(ctx, permissionDenialSelect+" WHERE id=?", request.ID))
		if err == nil {
			if priorDigest != digest {
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
		before := policy.DenyInteractive
		if before != request.DenyInteractive {
			if policy.Revision == math.MaxInt64 {
				return ErrConflict
			}
			policy.DenyInteractive, policy.Revision, policy.UpdatedAt = request.DenyInteractive, policy.Revision+1, timestamp(now())
			if _, err := tx.ExecContext(ctx, "UPDATE permission_policies SET deny_interactive=?,revision=?,updated_at=? WHERE tree_id=?", policy.DenyInteractive, policy.Revision, policy.UpdatedAt.UnixMicro(), policy.TreeID); err != nil {
				return err
			}
			// Each session retains ordinary operation settlement and resource accounting.
			owners, err := treeOwners(ctx, tx, owner.TreeID)
			if err != nil {
				return err
			}
			for _, id := range owners {
				if err := retirePolicyOperations(ctx, tx, id); err != nil {
					return err
				}
			}
			changed = true
		}
		created := now()
		if _, err := tx.ExecContext(ctx, `INSERT INTO permission_denial_edits (id,digest,session_id,tree_id,expected_revision,deny_interactive,previous_denial,mode,revision,updated_at,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, request.ID, digest, owner.ID, policy.TreeID, request.ExpectedRevision, request.DenyInteractive, before, policy.Mode, policy.Revision, policy.UpdatedAt.UnixMicro(), created); err != nil {
			return err
		}
		result = session.PermissionDenialEdit{PermissionDenialRequest: request, Policy: policy, PreviousDenial: before, CreatedAt: timestamp(created)}
		return nil
	})
	if err != nil {
		return session.PermissionDenialEdit{}, false, err
	}
	return result, changed, nil
}
