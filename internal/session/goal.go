package session

import (
	"fmt"
	"time"
	"unicode/utf8"
)

type GoalID string

type GoalRef struct {
	ID       GoalID `json:"id"`
	Revision int64  `json:"revision,string"`
}

func (r GoalRef) Validate() error {
	if err := ValidateID(string(r.ID)); err != nil {
		return err
	}
	if r.Revision < 1 {
		return fmt.Errorf("%w: goal revision must be positive", ErrInvalid)
	}
	return nil
}

// GoalRequest distinguishes an omitted allowance (100) from an explicit zero.
// The allowance counts additional automatic continuations, excluding the initial
// input. Every nonnegative signed 64-bit allowance is representable.
type GoalRequest struct {
	Text             string `json:"text"`
	MaxContinuations *int64 `json:"max_continuations,string"`
}

type GoalSpec struct {
	Text             string `json:"text"`
	MaxContinuations int64  `json:"max_continuations,string"`
}

func (r GoalRequest) Resolve() (GoalSpec, error) {
	value := GoalSpec{Text: r.Text, MaxContinuations: 100}
	if r.MaxContinuations != nil {
		value.MaxContinuations = *r.MaxContinuations
	}
	if !utf8.ValidString(value.Text) {
		return GoalSpec{}, fmt.Errorf("%w: goal text must be UTF-8", ErrInvalid)
	}
	if value.MaxContinuations < 0 {
		return GoalSpec{}, fmt.Errorf("%w: negative continuation allowance", ErrInvalid)
	}
	if err := ValidateText(value.Text, MaxDocumentBytes); err != nil {
		return GoalSpec{}, err
	}
	return value, nil
}

type GoalState string

const (
	GoalArmed      GoalState = "armed"
	GoalPaused     GoalState = "paused"
	GoalCompleted  GoalState = "completed"
	GoalCancelled  GoalState = "cancelled"
	GoalSuperseded GoalState = "superseded"
)

func (s GoalState) Open() bool { return s == GoalArmed || s == GoalPaused }

// Goal owns an immutable objective and allowance. Current selection is derived
// from creation order, including terminal goals; revision never selects a goal.
type Goal struct {
	GoalRef
	SessionID         SessionID `json:"session_id"`
	Spec              GoalSpec  `json:"spec"`
	State             GoalState `json:"state"`
	ContinuationsUsed int64     `json:"continuations_used,string"`
	StopReason        *string   `json:"stop_reason"`
	CreatedAt         time.Time `json:"created_at"`
}
