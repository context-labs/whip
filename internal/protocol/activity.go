package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

type SessionActivity struct {
	SessionID               ID      `json:"session_id"`
	Lifecycle               string  `json:"lifecycle" enum:"active,stopped"`
	ActiveTurn              *Turn   `json:"active_turn"`
	ActiveInputID           *ID     `json:"active_input_id"`
	QueuedInputCount        Counter `json:"queued_input_count"`
	PendingPermissionCount  Counter `json:"pending_permission_count"`
	PendingQuestionCount    Counter `json:"pending_question_count"`
	ExecutionPermit         bool    `json:"execution_permit"`
	ActiveWorkspaceActionID *ID     `json:"active_workspace_action_id"`
}

type InputPageParams struct {
	SessionID ID       `json:"session_id"`
	State     string   `json:"state" enum:"queued,all"`
	After     *Counter `json:"after,omitempty"`
	Limit     int      `json:"limit" min:"1" max:"100"`
}

type SessionInputParams struct {
	SessionID ID `json:"session_id"`
	InputID   ID `json:"input_id"`
}

type InputSummary struct {
	Steering         *InputSteeringRef `json:"steering,omitempty"`
	ID               ID                `json:"id"`
	SessionID        ID                `json:"session_id"`
	Ordinal          Counter           `json:"ordinal"`
	Source           string            `json:"source" enum:"user,agent,schedule,goal"`
	Kind             string            `json:"kind" enum:"prompt,compact,goal_formulation,automatic_title,host_operation"`
	State            string            `json:"state" enum:"queued,claimed,cancelled"`
	TurnID           *ID               `json:"turn_id"`
	CreatedAt        string            `json:"created_at"`
	TextPreview      string            `json:"text_preview"`
	PreviewTruncated bool              `json:"preview_truncated"`
	AttachmentCount  Counter           `json:"attachment_count"`
}

type InputPageResult struct {
	Items      []InputSummary `json:"items"`
	NextCursor *Counter       `json:"next_cursor"`
}

func ActivityFromDomain(value session.Activity) SessionActivity {
	result := SessionActivity{SessionID: ID(value.SessionID), Lifecycle: string(value.Lifecycle), QueuedInputCount: Counter(value.QueuedInputCount), PendingPermissionCount: Counter(value.PendingPermissionCount), PendingQuestionCount: Counter(value.PendingQuestionCount), ExecutionPermit: value.ExecutionPermit}
	if value.ActiveTurn != nil {
		result.ActiveTurn = new(TurnFromDomain(*value.ActiveTurn))
	}
	if value.ActiveInputID != nil {
		result.ActiveInputID = new(ID(*value.ActiveInputID))
	}
	if value.ActiveWorkspaceActionID != nil {
		result.ActiveWorkspaceActionID = new(ID(*value.ActiveWorkspaceActionID))
	}
	return result
}

func InputPageFromDomain(value session.InputPage) InputPageResult {
	result := InputPageResult{Items: []InputSummary{}, NextCursor: counter(value.NextCursor)}
	for _, item := range value.Items {
		summary := InputSummary{ID: ID(item.ID), SessionID: ID(item.SessionID), Ordinal: Counter(item.Ordinal), Source: string(item.Source), Kind: string(item.Kind), State: string(item.State), CreatedAt: item.CreatedAt.Format(time.RFC3339Nano), TextPreview: item.TextPreview, PreviewTruncated: item.PreviewTruncated, AttachmentCount: Counter(item.AttachmentCount)}
		if item.TurnID != nil {
			summary.TurnID = new(ID(*item.TurnID))
		}
		summary.Steering = steeringRef(item.Steering)
		result.Items = append(result.Items, summary)
	}
	return result
}
