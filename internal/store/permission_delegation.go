package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/context-labs/whip/internal/session"
)

// inheritedPermissionPolicy validates the ongoing parent-policy relationship.
// The recorded revision is admission evidence, not an expiry: each operation
// captures and independently validates the tree's current policy revision.
func inheritedPermissionPolicy(ctx context.Context, q querier, owner session.Session) (*session.PermissionPolicy, error) {
	policy, err := readPermissionPolicy(ctx, q, owner.ID)
	if err != nil {
		return nil, err
	}
	for range session.MaxSessionDepth + 1 {
		if owner.TreeID != policy.TreeID {
			return nil, nil
		}
		if owner.ParentID == nil {
			return &policy, nil
		}
		var revision session.Revision
		if err := q.QueryRowContext(ctx, "SELECT policy_revision FROM child_permission_policies WHERE session_id=?", owner.ID).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		} else if err != nil {
			return nil, err
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

func automaticPermissionRevision(ctx context.Context, q querier, owner session.Session) (*session.Revision, error) {
	policy, err := inheritedPermissionPolicy(ctx, q, owner)
	if err != nil || policy == nil {
		return nil, err
	}
	if policy.Mode != session.PermissionAutomatic || policy.DenyInteractive {
		return nil, nil
	}
	return &policy.Revision, nil
}

// Explicit grant selection narrows delegation and never adds policy authority.
func childPermissionRevision(ctx context.Context, q querier, parent session.Session, cwd string, grants []session.GrantID) (*session.Revision, error) {
	if grants != nil || cwd != parent.WorkingDirectory {
		return nil, nil
	}
	policy, err := inheritedPermissionPolicy(ctx, q, parent)
	if err != nil || policy == nil {
		return nil, err
	}
	return &policy.Revision, nil
}
