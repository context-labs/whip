package acp

import (
	"context"
	"strconv"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/protocol"
)

const optDismiss = "dismiss"

// ACP offers single selection. A native multiple-choice question can still
// receive one selected label; batches submit one exact ordered answer array.
func (b *Bridge) handleQuestion(parent context.Context, s *acpSession, question protocol.Question) {
	deadline, err := time.Parse(time.RFC3339Nano, question.Deadline)
	if err != nil {
		return
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	defer cancel()
	answers := make([]protocol.QuestionAnswer, len(question.Request.Questions))
	for i, set := range question.Request.Questions {
		if len(set.Options) < 2 || len(set.Options) > 6 {
			return
		}
		options := make([]acp.PermissionOption, 0, len(set.Options)+1)
		for j, option := range set.Options {
			label := option.Label
			if option.Recommended {
				label += " (recommended)"
			}
			if option.Description != "" {
				label += " - " + option.Description
			}
			options = append(options, acp.PermissionOption{OptionId: acp.PermissionOptionId(strconv.Itoa(j)), Name: label, Kind: acp.PermissionOptionKindAllowOnce})
		}
		options = append(options, acp.PermissionOption{OptionId: optDismiss, Name: "Dismiss", Kind: acp.PermissionOptionKindRejectOnce})
		response, err := b.awaitDecision(ctx, acp.RequestPermissionRequest{SessionId: s.id, ToolCall: acp.ToolCallUpdate{ToolCallId: acp.ToolCallId("question-" + question.OperationID + "-" + protocol.ID(strconv.Itoa(i))), Title: new(set.Question), Kind: new(acp.ToolKindOther)}, Options: options}, func(ctx context.Context) bool {
			var value protocol.Question
			return b.client.Call(ctx, "questions.get", protocol.QuestionParams{SessionID: question.SessionID, OperationID: question.OperationID}, &value) == nil && value.SessionID == question.SessionID && value.State == "pending"
		})
		if ctx.Err() != nil || err != nil {
			return
		}
		answers[i] = protocol.QuestionAnswer{Answer: []string{}, Dismissed: true}
		if err == nil && response.Outcome.Selected != nil {
			selected, err := strconv.Atoi(string(response.Outcome.Selected.OptionId))
			if err == nil && selected >= 0 && selected < len(set.Options) {
				answers[i] = protocol.QuestionAnswer{Answer: []string{set.Options[selected].Label}}
			}
		}
	}
	var value protocol.Question
	if err := b.client.Call(ctx, "questions.answer", protocol.AnswerQuestionParams{SessionID: question.SessionID, OperationID: question.OperationID, Answers: answers}, &value); err != nil {
		b.decisionError(s, "question answer outcome requires inspection", err)
	}
}
