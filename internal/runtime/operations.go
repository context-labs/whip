package runtime

import (
	"context"
	"strings"

	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) CreateGrant(ctx context.Context, grant session.Grant) (session.Grant, error) {
	return r.store.CreateGrant(ctx, grant)
}

func (r *Runtime) RevokeGrant(ctx context.Context, id session.GrantID) (session.Grant, error) {
	grant, err := r.store.RevokeGrant(ctx, id)
	if err == nil {
		r.languageServers.RetireAll()
		if grant.Capability == "browser.control" {
			if owner, readErr := r.store.Session(ctx, grant.SessionID); readErr == nil {
				if identity, identityErr := r.browserIdentity(ctx, owner); identityErr == nil {
					r.browser.RevokeLineage(identity.RootID, grant.Resource)
				}
			}
		}
		if strings.HasPrefix(grant.Capability, "computer.") {
			r.computerMu.Lock()
			r.computer.Disconnect()
			r.computerMu.Unlock()
		}
	}
	return grant, err
}

func (r *Runtime) Grants(ctx context.Context, id session.SessionID, after session.GrantID, limit int) ([]session.Grant, error) {
	return r.store.Grants(ctx, id, after, limit)
}

func (r *Runtime) Operation(ctx context.Context, id session.OperationID) (session.Operation, error) {
	return r.store.Operation(ctx, id)
}

func (r *Runtime) Operations(ctx context.Context, id session.TurnID, after session.OperationID, limit int) ([]session.Operation, error) {
	return r.store.Operations(ctx, id, after, limit)
}

func (r *Runtime) Permissions(ctx context.Context, id session.SessionID, after session.OperationID, limit int) ([]session.Permission, error) {
	return r.store.Permissions(ctx, id, after, limit)
}

func (r *Runtime) PermissionsFiltered(ctx context.Context, id session.SessionID, after session.OperationID, limit int, pendingOnly bool) ([]session.Permission, error) {
	return r.store.PermissionsFiltered(ctx, id, after, limit, pendingOnly)
}

func (r *Runtime) ResolvePermission(ctx context.Context, id session.OperationID, approved bool) (session.Permission, error) {
	return r.store.ResolvePermission(ctx, id, approved)
}

func (r *Runtime) Cell(ctx context.Context, id session.CellID) (session.Cell, error) {
	return r.store.Cell(ctx, id)
}

func (r *Runtime) Cells(ctx context.Context, id session.TurnID, after session.CellID, limit int) ([]session.Cell, error) {
	return r.store.Cells(ctx, id, after, limit)
}
