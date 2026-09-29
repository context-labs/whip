package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/context-labs/whip/internal/session"
)

// automaticPermissionRevision validates the whole captured delegation chain.
// A policy change expires child delegation permanently, including after an
// off/on transition. Missing records (including pre-upgrade children) deny it.
func automaticPermissionRevision(ctx context.Context, q querier, owner session.Session) (*session.Revision, error) {
	policy, err := readPermissionPolicy(ctx, q, owner.ID)
	if err != nil {
		return nil, err
	}
	if policy.Mode != session.PermissionAutomatic || policy.DenyInteractive {
		return nil, nil
	}
	for range session.MaxSessionDepth + 1 {
		if owner.TreeID != policy.TreeID {
			return nil, nil
		}
		if owner.ParentID == nil {
			return &policy.Revision, nil
		}
		var revision session.Revision
		if err := q.QueryRowContext(ctx, "SELECT policy_revision FROM child_permission_policies WHERE session_id=?", owner.ID).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		} else if err != nil {
			return nil, err
		}
		if revision != policy.Revision {
			return nil, nil
		}
		parent, err := readSession(ctx, q, *owner.ParentID)
		if err != nil {
			return nil, err
		}
		if parent.WorkingDirectory != owner.WorkingDirectory {
			return nil, nil
		}
		owner = parent
	}
	return nil, nil
}

// Explicit grant selection narrows delegation and never adds policy authority.
func childPermissionRevision(ctx context.Context, q querier, parent session.Session, cwd string, grants []session.GrantID) (*session.Revision, error) {
	if grants != nil || cwd != parent.WorkingDirectory {
		return nil, nil
	}
	return automaticPermissionRevision(ctx, q, parent)
}
