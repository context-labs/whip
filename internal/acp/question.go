package acp

import (
	"encoding/json"
	"strconv"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

const optDismiss = "dismiss"

type pendingQuestion struct {
	QuestionID string                   `json:"question_id"`
	Question   string                   `json:"question"`
	Options    []session.QuestionOption `json:"options"`
	Questions  []session.QuestionSet    `json:"questions"`
}

// sets returns the batch's questions, or the legacy single ask.
func (q pendingQuestion) sets() []session.QuestionSet {
	if len(q.Questions) > 0 {
		return q.Questions
	}
	if q.Question == "" {
		return nil
	}
	return []session.QuestionSet{{Question: q.Question, Options: q.Options}}
}

// questionAnswer is the daemon's question.answer payload.
type questionAnswer struct {
	ID        string                         `json:"id"`
	Answer    []string                       `json:"answer,omitempty"`
	Dismissed bool                           `json:"dismissed,omitempty"`
	Answers   []protocol.QuestionAnswerEntry `json:"answers,omitempty"`
}

// handleQuestion surfaces user.ask through ACP's permission prompt, the only
// choice UI the protocol has: one AllowOnce option per label plus Dismiss.
// That prompt is single-select, so a multiple=True question collapses to one
// answer under ACP; the daemon accepts one label for those too. A batched
// ask pages through each question as its own prompt, then submits all the
// answers at once.
func (b *Bridge) handleQuestion(s *acpSession, payload []byte) {
	var pending pendingQuestion
	if json.Unmarshal(payload, &pending) != nil || pending.QuestionID == "" {
		return
	}
	sets := pending.sets()
	if len(sets) == 0 {
		return
	}
	answer := questionAnswer{ID: pending.QuestionID, Dismissed: true}
	if b.conn != nil {
		answers := make([]protocol.QuestionAnswerEntry, len(sets))
		for i, set := range sets {
			answers[i] = protocol.QuestionAnswerEntry{Dismissed: true}
			options := make([]acp.PermissionOption, 0, len(set.Options)+1)
			for index, option := range set.Options {
				name := option.Label
				if option.Recommended {
					name += " (recommended)"
				}
				if option.Description != "" {
					name += " - " + option.Description
				}
				options = append(options, acp.PermissionOption{OptionId: acp.PermissionOptionId(strconv.Itoa(index)), Name: name, Kind: acp.PermissionOptionKindAllowOnce})
			}
			options = append(options, acp.PermissionOption{OptionId: optDismiss, Name: "Dismiss", Kind: acp.PermissionOptionKindRejectOnce})
			response, err := b.conn.RequestPermission(s.lifecycle, acp.RequestPermissionRequest{
				SessionId: s.id,
				ToolCall: acp.ToolCallUpdate{
					ToolCallId: acp.ToolCallId("question-" + pending.QuestionID + "-" + strconv.Itoa(i)),
					Title:      new(set.Question), Kind: new(acp.ToolKindOther),
				},
				Options: options,
			})
			if err == nil && response.Outcome.Selected != nil {
				if index, convErr := strconv.Atoi(string(response.Outcome.Selected.OptionId)); convErr == nil && index >= 0 && index < len(set.Options) {
					answers[i] = protocol.QuestionAnswerEntry{Answer: []string{set.Options[index].Label}}
				}
			}
		}
		if len(sets) > 1 {
			answer = questionAnswer{ID: pending.QuestionID, Answers: answers}
		} else {
			answer = questionAnswer{ID: pending.QuestionID, Answer: answers[0].Answer, Dismissed: answers[0].Dismissed}
		}
	}
	action, err := s.root.NewAction("question.answer", answer)
	if err != nil {
		return
	}
	_, _ = s.root.Command(s.lifecycle, action)
}
