package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchQuestion(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "questions.get":
		return decode(raw, func(p protocol.QuestionParams) (any, error) {
			value, err := r.Question(ctx, session.SessionID(p.SessionID), session.OperationID(p.OperationID))
			return protocol.QuestionFromDomain(value), err
		})
	case "questions.list":
		return decode(raw, func(p protocol.QuestionsParams) (any, error) {
			var after session.OperationID
			if p.After != nil {
				after = session.OperationID(*p.After)
			}
			values, err := r.Questions(ctx, session.SessionID(p.SessionID), p.PendingOnly, after, p.Limit)
			if err != nil {
				return nil, err
			}
			result := protocol.QuestionsResult{Items: []protocol.Question{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.QuestionFromDomain(value))
			}
			return result, nil
		})
	case "questions.answer":
		return decode(raw, func(p protocol.AnswerQuestionParams) (any, error) {
			value, err := r.AnswerQuestion(ctx, session.SessionID(p.SessionID), session.OperationID(p.OperationID), p.DomainAnswers())
			return protocol.QuestionFromDomain(value), err
		})
	default:
		return nil, ErrMethod
	}
}
