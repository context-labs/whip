package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) PermissionPolicy(ctx context.Context, owner session.SessionID) (session.PermissionPolicy, error) {
	return r.store.PermissionPolicy(ctx, owner)
}

func (r *Runtime) SetPermissionMode(ctx context.Context, request session.PermissionModeRequest) (session.PermissionModeEdit, error) {
	return r.store.SetPermissionMode(ctx, request)
}

func (r *Runtime) PermissionModeEdit(ctx context.Context, owner session.SessionID, id session.PermissionModeEditID) (session.PermissionModeEdit, error) {
	return r.store.PermissionModeEdit(ctx, owner, id)
}
