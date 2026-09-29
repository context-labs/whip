package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func (r *Runtime) MatchSubmission(ctx context.Context, identity session.RequestIdentity, request store.Submission) (store.Admission, error) {
	return r.store.MatchSubmission(ctx, identity, request)
}

func (r *Runtime) MatchChild(ctx context.Context, identity session.RequestIdentity, request store.ChildRequest) (store.Admission, error) {
	return r.store.MatchChild(ctx, identity, request)
}

func (r *Runtime) MatchGoalFormulation(ctx context.Context, identity session.RequestIdentity, owner session.SessionID, request session.GoalFormulationRequest) (store.Admission, error) {
	return r.store.MatchGoalFormulation(ctx, identity, owner, request)
}

func (r *Runtime) MatchGoalResume(ctx context.Context, identity session.RequestIdentity, owner session.SessionID, ref session.GoalRef) (store.Admission, error) {
	return r.store.MatchGoalResume(ctx, identity, owner, ref)
}

func (r *Runtime) MatchHostOperation(ctx context.Context, identity session.RequestIdentity, owner session.SessionID, operation session.HostOperation) (store.Admission, error) {
	return r.store.MatchHostOperation(ctx, identity, owner, operation)
}
