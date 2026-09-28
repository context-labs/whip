package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

// FormulateGoal admits maintenance under an ordinary recoverable input identity.
// Admission does not mean the proposed goal was formulated or accepted.
func (r *Runtime) FormulateGoal(ctx context.Context, identity session.RequestIdentity, owner session.SessionID, request session.GoalFormulationRequest) (store.Admission, error) {
	if err := r.Err(); err != nil {
		return store.Admission{}, err
	}
	result, err := r.store.AdmitGoalFormulation(ctx, identity, owner, request)
	if err == nil {
		r.Wake()
	}
	return result, err
}

// GoalFormulation reads immutable candidate evidence without executing work.
// Accepted work can survive a subsequently interrupted maintenance turn.
func (r *Runtime) GoalFormulation(ctx context.Context, owner session.SessionID, attempt session.ModelAttemptID) (session.GoalFormulation, error) {
	return r.store.GoalFormulation(ctx, owner, attempt)
}
