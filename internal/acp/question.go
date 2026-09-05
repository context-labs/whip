package acp

import (
	"encoding/json"
	"strconv"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/session"
)

const optDismiss = "dismiss"

type pendingQuestion struct {
	QuestionID string                   `json:"question_id"`
	Question   string                   `json:"question"`
	Options    []session.QuestionOption `json:"options"`
}

// questionAnswer is the daemon's question.answer payload.
type questionAnswer struct {
	ID        string   `json:"id"`
	Answer    []string `json:"answer,omitempty"`
	Dismissed bool     `json:"dismissed,omitempty"`
}

// handleQuestion surfaces user.ask through ACP's permission prompt, the only
// choice UI the protocol has: one AllowOnce option per label plus Dismiss.
// That prompt is single-select, so a multiple=True question collapses to one
// answer under ACP; the daemon accepts one label for those too.
func (b *Bridge) handleQuestion(s *acpSession, payload []byte) {
	var pending pendingQuestion
	if json.Unmarshal(payload, &pending) != nil || pending.QuestionID == "" {
		return
	}
	answer := questionAnswer{ID: pending.QuestionID, Dismissed: true}
	if b.conn != nil {
		options := make([]acp.PermissionOption, 0, len(pending.Options)+1)
		for index, option := range pending.Options {
			name := option.Label
			if option.Description != "" {
				name += " - " + option.Description
			}
			options = append(options, acp.PermissionOption{OptionId: acp.PermissionOptionId(strconv.Itoa(index)), Name: name, Kind: acp.PermissionOptionKindAllowOnce})
		}
		options = append(options, acp.PermissionOption{OptionId: optDismiss, Name: "Dismiss", Kind: acp.PermissionOptionKindRejectOnce})
		response, err := b.conn.RequestPermission(s.lifecycle, acp.RequestPermissionRequest{
			SessionId: s.id,
			ToolCall: acp.ToolCallUpdate{
				ToolCallId: acp.ToolCallId("question-" + pending.QuestionID),
				Title:      new(pending.Question), Kind: new(acp.ToolKindOther),
			},
			Options: options,
		})
		if err == nil && response.Outcome.Selected != nil {
			if index, convErr := strconv.Atoi(string(response.Outcome.Selected.OptionId)); convErr == nil && index >= 0 && index < len(pending.Options) {
				answer = questionAnswer{ID: pending.QuestionID, Answer: []string{pending.Options[index].Label}}
			}
		}
	}
	action, err := s.root.NewAction("question.answer", answer)
	if err != nil {
		return
	}
	_, _ = s.root.Command(s.lifecycle, action)
}
