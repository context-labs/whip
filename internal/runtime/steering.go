package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) SteerInput(ctx context.Context, request session.SteerInputRequest) (session.InputSteeringResult, error) {
	if err := r.Err(); err != nil {
		return session.InputSteeringResult{}, err
	}
	result, err := r.store.SteerInput(ctx, request)
	if err == nil {
		r.Wake()
	}
	return result, err
}

func (r *Runtime) InputSteering(ctx context.Context, owner session.SessionID, id session.InputSteeringID) (session.InputSteeringResult, error) {
	return r.store.InputSteering(ctx, owner, id)
}
