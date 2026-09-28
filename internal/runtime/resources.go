package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

// Resources returns reusable capacity at each applicable ancestor scope,
// starting with the requested session. Reads neither reserve nor consume capacity.
func (r *Runtime) Resources(ctx context.Context, id session.SessionID) ([]session.ResourceUsage, error) {
	return r.store.Resources(ctx, id)
}

func (r *Runtime) SetResource(ctx context.Context, id session.SessionID, revision int64, limit session.ResourceLimit) (session.ResourceUsage, error) {
	result, err := r.store.SetResource(ctx, id, revision, limit)
	if err == nil {
		r.Wake()
	}
	return result, err
}
