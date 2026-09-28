package rpc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func goalAdmission(value store.GoalAdmission) protocol.GoalAdmission {
	result := protocol.GoalAdmission{ID: protocol.ID(value.ID), Current: value.Current}
	if value.Goal != nil {
		result.Goal = new(protocol.GoalFromDomain(*value.Goal))
	}
	if value.Initial != nil {
		result.Initial = new(admission(*value.Initial))
	}
	if value.DeletedAt != nil {
		result.DeletedAt = new(value.DeletedAt.Format(time.RFC3339Nano))
	}
	return result
}

func dispatchGoal(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "goals.formulate":
		return decode(raw, func(p protocol.FormulateGoalParams) (any, error) {
			value, err := r.FormulateGoal(ctx, session.RequestIdentity{ClientID: string(p.Identity.ClientID), RequestID: string(p.Identity.RequestID)}, session.SessionID(p.SessionID), p.Request.Domain())
			return admission(value), err
		})
	case "goals.formulation":
		return decode(raw, func(p protocol.GoalFormulationParams) (any, error) {
			value, err := r.GoalFormulation(ctx, session.SessionID(p.SessionID), session.ModelAttemptID(p.AttemptID))
			return protocol.GoalFormulationFromDomain(value), err
		})
	case "goals.create":
		return decode(raw, func(p protocol.CreateGoalParams) (any, error) {
			var expected *session.GoalRef
			if p.ExpectedCurrent != nil {
				expected = new(p.ExpectedCurrent.Domain())
			}
			spec := session.GoalRequest{Text: p.Spec.Text}
			if p.Spec.MaxContinuations != nil {
				spec.MaxContinuations = new(int64(*p.Spec.MaxContinuations))
			}
			value, err := r.CreateGoal(ctx, session.SessionID(p.SessionID), session.GoalID(p.GoalID), expected, spec, p.Start)
			return goalAdmission(value), err
		})
	case "goals.current":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			value, err := r.CurrentGoal(ctx, session.SessionID(p.SessionID))
			result := protocol.CurrentGoalResult{}
			if value != nil {
				result.Goal = new(protocol.GoalFromDomain(*value))
			}
			return result, err
		})
	case "goals.get":
		return decode(raw, func(p protocol.GoalParams) (any, error) {
			value, err := r.Goal(ctx, session.SessionID(p.SessionID), session.GoalID(p.GoalID))
			return protocol.GoalFromDomain(value), err
		})
	case "goals.resume":
		return decode(raw, func(p protocol.ResumeGoalParams) (any, error) {
			value, err := r.ResumeGoal(ctx, session.RequestIdentity{ClientID: string(p.Identity.ClientID), RequestID: string(p.Identity.RequestID)}, session.SessionID(p.SessionID), p.Goal.Domain())
			return admission(value), err
		})
	case "goals.cancel":
		return decode(raw, func(p protocol.GoalParams) (any, error) {
			value, err := r.CancelGoal(ctx, session.SessionID(p.SessionID), session.GoalID(p.GoalID))
			result := protocol.GoalChange{Goal: protocol.GoalFromDomain(value.Goal)}
			if value.CancelTurnID != nil {
				result.CancelTurnID = new(protocol.ID(*value.CancelTurnID))
			}
			return result, err
		})
	default:
		return nil, ErrMethod
	}
}
