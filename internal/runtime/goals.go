package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func (r *Runtime) CreateGoal(ctx context.Context, owner session.SessionID, id session.GoalID, expected *session.GoalRef, spec session.GoalRequest, start bool) (store.GoalAdmission, error) {
	if err := r.Err(); err != nil {
		return store.GoalAdmission{}, err
	}
	result, err := r.store.CreateGoal(ctx, owner, id, expected, spec, start)
	if err == nil {
		r.Wake()
	}
	return result, err
}

func (r *Runtime) CurrentGoal(ctx context.Context, owner session.SessionID) (*session.Goal, error) {
	return r.store.CurrentGoal(ctx, owner)
}

func (r *Runtime) Goal(ctx context.Context, owner session.SessionID, id session.GoalID) (session.Goal, error) {
	return r.store.Goal(ctx, owner, id)
}

func (r *Runtime) ResumeGoal(ctx context.Context, identity session.RequestIdentity, owner session.SessionID, ref session.GoalRef) (store.Admission, error) {
	if err := r.Err(); err != nil {
		return store.Admission{}, err
	}
	result, err := r.store.ResumeGoal(ctx, identity, owner, ref)
	if err == nil {
		r.Wake()
	}
	return result, err
}

func (r *Runtime) CancelGoal(ctx context.Context, owner session.SessionID, id session.GoalID) (store.GoalChange, error) {
	result, err := r.store.CancelGoal(ctx, owner, id)
	if err == nil {
		if result.CancelTurnID != nil {
			r.cancelTurn(*result.CancelTurnID)
		}
		r.Wake()
	}
	return result, err
}

func (r *Runtime) prepareGoal(current session.Session, call tool.Invocation) (tool.Prepared, error) {
	if call.Name != "complete" {
		return tool.Prepared{}, fmt.Errorf("%w: unsupported goal operation", session.ErrInvalid)
	}
	var request session.GoalCompletion
	if err := decodeArguments(call.Arguments, &request); err != nil {
		return tool.Prepared{}, err
	}
	if err := request.Validate(); err != nil {
		return tool.Prepared{}, err
	}
	arguments, err := json.Marshal(request)
	if err != nil {
		return tool.Prepared{}, err
	}
	return tool.Prepared{Capability: "goals.complete", Resource: string(current.TreeID), Arguments: arguments, Apply: func(ctx context.Context, id session.OperationID) (any, error) {
		return r.store.ApplyGoalCompletion(ctx, id)
	}}, nil
}
