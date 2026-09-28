package runner

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func (r *Runner) loadContinuations(ctx context.Context, request *model.Request, size *int) error {
	var ids []session.MessageID
	for _, message := range request.Messages {
		if message.Role == session.Assistant && message.ID != "" {
			ids = append(ids, message.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	values, err := r.transcript.Continuations(ctx, request.SessionID, ids)
	if errors.Is(err, session.ErrContinuationLimit) {
		return errContextLimit
	}
	if err != nil {
		return err
	}
	for i := range request.Messages {
		message := &request.Messages[i]
		if value, ok := values[message.ID]; ok {
			if err := value.Validate(); err != nil {
				return err
			}
			message.Continuation = &value
			raw, _ := json.Marshal(value)
			*size += len(raw)
		}
	}
	if *size > maxContextBytes {
		return errContextLimit
	}
	return nil
}

func (r *Runner) appendCompletedContext(request *model.Request, completed attemptOutcome, size *int) error {
	if err := r.appendTurnContext(request, session.Assistant, completed.parts, size); err != nil {
		return err
	}
	message := &request.Messages[len(request.Messages)-1]
	message.ID, message.Continuation = completed.messageID, completed.continuation
	if completed.continuation != nil {
		raw, _ := json.Marshal(completed.continuation)
		*size += len(raw)
	}
	if *size > maxContextBytes && r.compactions == nil {
		return errContextLimit
	}
	return nil
}
