package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) SetPermissionDenial(ctx context.Context, request session.PermissionDenialRequest) (session.PermissionDenialEdit, error) {
	result, changed, err := r.store.ApplyPermissionDenial(ctx, request)
	if err == nil && changed {
		r.languageServers.RetireAll()
	}
	return result, err
}

func (r *Runtime) PermissionDenialEdit(ctx context.Context, owner session.SessionID, id string) (session.PermissionDenialEdit, error) {
	return r.store.PermissionDenialEdit(ctx, owner, id)
}
