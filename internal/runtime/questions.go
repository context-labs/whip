package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func (r *Runtime) Question(ctx context.Context, owner session.SessionID, id session.OperationID) (session.Question, error) {
	return r.store.Question(ctx, owner, id)
}

func (r *Runtime) Questions(ctx context.Context, owner session.SessionID, pendingOnly bool, after session.OperationID, limit int) ([]session.Question, error) {
	return r.store.Questions(ctx, owner, pendingOnly, after, limit)
}

func (r *Runtime) AnswerQuestion(ctx context.Context, owner session.SessionID, id session.OperationID, answers []session.QuestionAnswer) (session.Question, error) {
	return r.store.AnswerQuestion(ctx, owner, id, answers)
}

func (r *Runtime) prepareQuestion(current session.Session, call tool.Invocation) (tool.Prepared, error) {
	if call.Name != "ask" {
		return tool.Prepared{}, fmt.Errorf("%w: unsupported user operation", session.ErrInvalid)
	}
	raw, err := json.Marshal(call.Arguments)
	if err != nil {
		return tool.Prepared{}, session.ErrInvalid
	}
	request, err := session.ParseQuestionRequest(raw)
	if err != nil {
		return tool.Prepared{}, err
	}
	arguments, err := json.Marshal(request)
	if err != nil {
		return tool.Prepared{}, err
	}
	return tool.Prepared{
		Capability: session.QuestionCapability, Resource: string(current.ID), Arguments: arguments,
		Apply: func(ctx context.Context, id session.OperationID) (any, error) {
			question, err := r.store.BeginQuestion(ctx, id)
			if err != nil {
				return nil, err
			}
			return r.waitQuestion(ctx, question)
		},
	}, nil
}

func (r *Runtime) waitQuestion(parent context.Context, question session.Question) (json.RawMessage, error) {
	ctx, cancel := context.WithDeadline(parent, question.Deadline)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if question.State != session.QuestionPending {
			return questionValue(question)
		}
		select {
		case <-ctx.Done():
			reason := session.QuestionCancelled
			if !time.Now().Before(question.Deadline) {
				reason = session.QuestionExpired
			}
			return r.closeQuestion(parent, question, reason, ctx.Err())
		case <-ticker.C:
		}
		current, err := r.store.Question(ctx, question.SessionID, question.OperationID)
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			return r.closeQuestion(parent, question, session.QuestionCancelled, err)
		}
		question = current
	}
}

func questionValue(question session.Question) (json.RawMessage, error) {
	if question.State == session.QuestionAnswered || question.State == session.QuestionDismissed {
		return question.Request.AnswerValue(question.Answers)
	}
	if question.CloseReason != nil {
		return nil, fmt.Errorf("human question %s", *question.CloseReason)
	}
	return nil, errors.New("human question has no terminal outcome")
}

// Join the SQL closure even after guest cancellation. No waiter survives this
// call, and an answer that committed first remains the sole durable outcome.
func (r *Runtime) closeQuestion(parent context.Context, question session.Question, reason session.QuestionCloseReason, cause error) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		closed, err := r.store.CloseQuestion(ctx, question.SessionID, question.OperationID, reason)
		if err == nil {
			return questionValue(closed)
		}
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) || errors.Is(err, session.ErrInvalid) {
			return nil, tool.Fatal(errors.Join(cause, err))
		}
		select {
		case <-ctx.Done():
			return nil, tool.Fatal(errors.Join(cause, err, ctx.Err()))
		case <-ticker.C:
		}
	}
}
