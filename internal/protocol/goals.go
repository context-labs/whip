package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

type GoalRequest struct {
	Text             string   `json:"text"`
	MaxContinuations *Counter `json:"max_continuations,omitempty"`
}
type GoalSpec struct {
	Text             string  `json:"text"`
	MaxContinuations Counter `json:"max_continuations"`
}
type Goal struct {
	GoalRef
	SessionID             ID       `json:"session_id"`
	Spec                  GoalSpec `json:"spec"`
	State                 string   `json:"state" enum:"armed,paused,completed,cancelled,superseded"`
	ContinuationsUsed     Counter  `json:"continuations_used"`
	StopReason            *string  `json:"stop_reason"`
	CompletionTurnID      *ID      `json:"completion_turn_id"`
	CompletionOperationID *ID      `json:"completion_operation_id"`
	CreatedAt             string   `json:"created_at" format:"date-time"`
}
type CreateGoalParams struct {
	SessionID       ID          `json:"session_id"`
	GoalID          ID          `json:"goal_id"`
	ExpectedCurrent *GoalRef    `json:"expected_current"`
	Spec            GoalRequest `json:"spec"`
	Start           bool        `json:"start"`
}
type GoalParams struct {
	SessionID ID `json:"session_id"`
	GoalID    ID `json:"goal_id"`
}
type ResumeGoalParams struct {
	Identity  RequestIdentity `json:"identity"`
	SessionID ID              `json:"session_id"`
	Goal      GoalRef         `json:"goal"`
}
type CurrentGoalResult struct {
	Goal *Goal `json:"goal"`
}
type GoalAdmission struct {
	ID        ID         `json:"id"`
	Goal      *Goal      `json:"goal"`
	Current   bool       `json:"current"`
	Initial   *Admission `json:"initial"`
	DeletedAt *string    `json:"deleted_at"`
}
type GoalChange struct {
	Goal         Goal `json:"goal"`
	CancelTurnID *ID  `json:"cancel_turn_id"`
}

func (r GoalRef) Domain() session.GoalRef {
	return session.GoalRef{ID: session.GoalID(r.ID), Revision: int64(r.Revision)}
}

func GoalFromDomain(value session.Goal) Goal {
	result := Goal{ID: ID(value.ID), Revision: Counter(value.Revision), SessionID: ID(value.SessionID), Spec: GoalSpec{Text: value.Spec.Text, MaxContinuations: Counter(value.Spec.MaxContinuations)}, State: string(value.State), ContinuationsUsed: Counter(value.ContinuationsUsed), StopReason: value.StopReason, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano)}
	if value.CompletionTurnID != nil {
		result.CompletionTurnID = new(ID(*value.CompletionTurnID))
	}
	if value.CompletionOperationID != nil {
		result.CompletionOperationID = new(ID(*value.CompletionOperationID))
	}
	return result
}
