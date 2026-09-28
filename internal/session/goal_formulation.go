package session

import (
	"fmt"
	"time"
)

const GoalFormulationPurpose = "goal_formulation"

// GoalFormulationRequest freezes the proposed goal identity and activation CAS.
// Zero TailMessages selects eight; explicit windows contain 2 to 100 messages.
type GoalFormulationRequest struct {
	GoalID           GoalID   `json:"goal_id"`
	Expected         *GoalRef `json:"expected"`
	MaxContinuations *int64   `json:"max_continuations,string"`
	Start            bool     `json:"start"`
	TailMessages     int      `json:"tail_messages"`
}

func (r GoalFormulationRequest) Resolve() (GoalFormulationRequest, error) {
	if err := ValidateID(string(r.GoalID)); err != nil {
		return r, err
	}
	if r.Expected != nil {
		if err := r.Expected.Validate(); err != nil {
			return r, err
		}
		r.Expected = new(*r.Expected)
	}
	if r.MaxContinuations != nil {
		if *r.MaxContinuations < 0 {
			return r, fmt.Errorf("%w: negative continuation allowance", ErrInvalid)
		}
		r.MaxContinuations = new(*r.MaxContinuations)
	}
	if r.TailMessages == 0 {
		r.TailMessages = 8
	}
	if r.TailMessages < 2 || r.TailMessages > 100 {
		return r, fmt.Errorf("%w: formulation requires 2 to 100 tail messages", ErrInvalid)
	}
	return r, nil
}

// GoalFormulationInput captures raw source coordinates at admission. The main
// model is separately captured by the ordinary turn configuration at Claim.
type GoalFormulationInput struct {
	HistoryRevision Revision               `json:"history_revision,string"`
	InputID         InputID                `json:"input_id"`
	SessionID       SessionID              `json:"session_id"`
	Request         GoalFormulationRequest `json:"request"`
	AfterSequence   int64                  `json:"after_sequence,string"`
	ThroughSequence int64                  `json:"through_sequence,string"`
}

type GoalFormulationDraft struct {
	Text string
}

// GoalFormulation is immutable helper evidence, including a semantic activation
// rejection. Its source identity and accounting survive child deletion.
type GoalFormulation struct {
	GoalFormulationInput
	TurnID    TurnID         `json:"turn_id"`
	AttemptID ModelAttemptID `json:"attempt_id"`
	Text      string         `json:"text"`
	Rejection *string        `json:"rejection"`
	CreatedAt time.Time      `json:"created_at"`
}

// Accepted means this attempt created its goal, even if it was later replaced,
// cancelled or deleted. It is independent of the maintenance turn's outcome.
type GoalFormulationSettlement struct {
	Attempt   ModelAttempt
	Candidate *GoalFormulation
	Accepted  bool
	Rejection *string
}
