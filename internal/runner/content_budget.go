package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

// Content references stay immutable. Metadata lets selection bound a request
// before reading bodies, including a helper's incremental source prefix.
type contentBudget struct {
	bytes int
	sizes map[string]int64
}

func (r *Runner) addContent(ctx context.Context, owner session.SessionID, budget *contentBudget, parts []session.Part) error {
	for _, part := range parts {
		if part.Type != "content" {
			continue
		}
		if r.content == nil {
			return errors.New("content reader is unavailable")
		}
		size, exists := budget.sizes[part.ReferenceID]
		if !exists {
			ref, err := r.content.ContentReference(ctx, owner, part.ReferenceID)
			if err != nil {
				return fmt.Errorf("inspect model content: %w", err)
			}
			if ref.ID != part.ReferenceID || ref.SessionID != owner || ref.Size < 0 || ref.Size > session.MaxContentBytes {
				return errors.New("invalid model content metadata")
			}
			size = ref.Size
			if budget.sizes == nil {
				budget.sizes = map[string]int64{}
			}
			budget.sizes[part.ReferenceID] = size
		}
		if size > int64(session.MaxContentBytes-budget.bytes) {
			return fmt.Errorf("hydrated content exceeds context limit: %w", errContextLimit)
		}
		// Providers encode every occurrence even when hydration shares a body.
		budget.bytes += int(size)
	}
	return nil
}

func (r *Runner) contentBudget(ctx context.Context, request *model.Request) (contentBudget, error) {
	var budget contentBudget
	size := len(request.Instructions)
	for _, tool := range request.Tools {
		raw, err := json.Marshal(tool)
		if err != nil {
			return budget, err
		}
		size += len(raw)
	}
	for _, message := range request.Messages {
		raw, err := json.Marshal(message.Parts)
		if err != nil {
			return budget, err
		}
		size += len(raw)
		if err := r.addContent(ctx, request.SessionID, &budget, message.Parts); err != nil {
			return budget, err
		}
	}
	if size+budget.bytes > maxContextBytes {
		return budget, errContextLimit
	}
	return budget, nil
}

// Rebuild only at an already settled boundary. Accepted messages/effects remain
// durable even when no complete input or tool batch fits the request limits.
func (r *Runner) hydrateContext(ctx context.Context, turn session.Turn, configuration session.Configuration, request *model.Request, size, folds *int, correction []session.Part) error {
	_, err := r.contentBudget(ctx, request)
	if err != nil && !errors.Is(err, errContextLimit) {
		return err
	}
	if err != nil || len(request.Messages) > maxContextMessages || *size > maxContextBytes {
		if r.compactions == nil {
			return errContextLimit
		}
		*size, err = r.prepareContext(ctx, turn, configuration, request, folds, correction)
		if err != nil {
			return err
		}
	}
	return r.hydrate(ctx, request)
}
