package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

type GoalFormulationRequest struct {
	GoalID           ID       `json:"goal_id"`
	ExpectedCurrent  *GoalRef `json:"expected_current"`
	MaxContinuations *Counter `json:"max_continuations,omitempty"`
	Start            bool     `json:"start"`
	TailMessages     int      `json:"tail_messages,omitempty"`
}

type FormulateGoalParams struct {
	Identity  RequestIdentity        `json:"identity"`
	SessionID ID                     `json:"session_id"`
	Request   GoalFormulationRequest `json:"request"`
}

type GoalFormulationParams struct {
	SessionID ID `json:"session_id"`
	AttemptID ID `json:"attempt_id"`
}

// GoalFormulation is immutable evidence, not the current goal or turn outcome.
type GoalFormulation struct {
	HistoryRevision Counter                `json:"history_revision"`
	InputID         ID                     `json:"input_id"`
	SessionID       ID                     `json:"session_id"`
	Request         GoalFormulationRequest `json:"request"`
	AfterSequence   Counter                `json:"after_sequence"`
	ThroughSequence Counter                `json:"through_sequence"`
	TurnID          ID                     `json:"turn_id"`
	AttemptID       ID                     `json:"attempt_id"`
	Text            string                 `json:"text"`
	Accepted        bool                   `json:"accepted"`
	Rejection       *string                `json:"rejection"`
	CreatedAt       string                 `json:"created_at" format:"date-time"`
}

func (r GoalFormulationRequest) Domain() session.GoalFormulationRequest {
	result := session.GoalFormulationRequest{GoalID: session.GoalID(r.GoalID), Start: r.Start, TailMessages: r.TailMessages}
	if r.ExpectedCurrent != nil {
		result.Expected = new(r.ExpectedCurrent.Domain())
	}
	if r.MaxContinuations != nil {
		result.MaxContinuations = new(int64(*r.MaxContinuations))
	}
	return result
}

func GoalFormulationFromDomain(value session.GoalFormulation) GoalFormulation {
	request := GoalFormulationRequest{GoalID: ID(value.Request.GoalID), Start: value.Request.Start, TailMessages: value.Request.TailMessages}
	if value.Request.Expected != nil {
		request.ExpectedCurrent = &GoalRef{ID: ID(value.Request.Expected.ID), Revision: Counter(value.Request.Expected.Revision)}
	}
	if value.Request.MaxContinuations != nil {
		request.MaxContinuations = new(Counter(*value.Request.MaxContinuations))
	}
	// Every retained candidate is inserted atomically with either successful
	// activation or an explicit rejection. This never consults mutable goal or
	// turn state: acceptance survives cancellation, replacement and deletion.
	return GoalFormulation{
		HistoryRevision: Counter(value.HistoryRevision), InputID: ID(value.InputID), SessionID: ID(value.SessionID), Request: request,
		AfterSequence: Counter(value.AfterSequence), ThroughSequence: Counter(value.ThroughSequence),
		TurnID: ID(value.TurnID), AttemptID: ID(value.AttemptID), Text: value.Text,
		Accepted: value.Rejection == nil, Rejection: value.Rejection, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano),
	}
}
