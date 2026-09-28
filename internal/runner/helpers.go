package runner

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

// ModelHelperRequest supplies only captured execution identity and stateless
// prompts. The caller must already have committed the operation's dispatch.
type ModelHelperRequest struct {
	Turn        session.Turn
	Model       session.ModelSelection
	OperationID session.OperationID
	Prompts     []string
	MaxTokens   *int64
}

// ModelResult is ephemeral output in the same position as its input prompt.
// Attempts retain billing and outcome; only the caller settles aggregate output.
type ModelResult struct {
	AttemptID session.ModelAttemptID `json:"attempt_id"`
	Text      string                 `json:"text"`
	Failure   *string                `json:"failure"`
}

// AccountingError means execution cannot safely continue: durable attempt
// admission, dispatch, or settlement could not be confirmed. It remains fatal
// even when joined with cancellation from another batch member.
type AccountingError struct{ Err error }

func (e *AccountingError) Error() string { return e.Err.Error() }
func (e *AccountingError) Unwrap() error { return e.Err }

// helperTarget keeps non-transcript responses on the ordinary attempt path.
// A compaction commits its draft; a stateless item commits accounting only.
type helperTarget struct {
	compaction       *compactionTarget
	operationID      session.OperationID
	index            int
	admissionRefused func(error) bool
	text             string
	attemptID        session.ModelAttemptID
}

func (t *helperTarget) response(id session.ModelAttemptID, parts []session.Part) (*session.CompactionDraft, error) {
	if t.compaction != nil {
		return t.compaction.response(id, parts)
	}
	var text strings.Builder
	for _, part := range parts {
		if part.Type != "text" || len(part.Text) > session.MaxDocumentBytes-text.Len() {
			return nil, errors.New("stateless model response requires bounded text only")
		}
		text.WriteString(part.Text)
	}
	t.text = text.String()
	return nil, nil //nolint:nilnil // Stateless responses intentionally have no compaction draft.
}

func (t *helperTarget) refused(err error) bool {
	return t != nil && t.compaction == nil && t.admissionRefused != nil && t.admissionRefused(err)
}

func accountingError(target *helperTarget, err error) error {
	if target != nil && target.compaction == nil {
		return &AccountingError{Err: err}
	}
	return err
}

// CallModels executes up to 32 stateless prompts with at most four requests in
// flight, using the ordinary provider retry and SQL-only settlement paths. It
// joins every started worker before returning, preserving all accounting errors.
//
// admissionRefused classifies only confirmed semantic refusal at reservation or
// after exact undispatched cancellation. It must reject unknown errors and mixed
// error joins containing any unknown leaf; errors.Is alone is insufficient.
// Invalid request shape returns ErrInvalid before execution. Otherwise failures
// belong to their position; a separate error means cancellation or unresolved
// accounting. Results for unstarted or interrupted items must not be published
// as successful output when that separate error is present.
func (r *Runner) CallModels(ctx context.Context, request ModelHelperRequest, admissionRefused func(error) bool) ([]ModelResult, error) {
	if err := validateModelHelper(request); err != nil {
		return nil, err
	}
	request.Prompts = slices.Clone(request.Prompts)
	request.Model = request.Model.Clone()
	if request.MaxTokens != nil {
		request.MaxTokens = new(*request.MaxTokens)
	}
	results := make([]ModelResult, len(request.Prompts))
	for i := range results {
		results[i].Failure = new("model helper was not started")
	}
	batchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	var mu sync.Mutex
	next := 0
	var failures []error
	for range min(4, len(results)) {
		workers.Go(func() {
			for {
				mu.Lock()
				if batchCtx.Err() != nil || next == len(results) {
					mu.Unlock()
					return
				}
				index := next
				next++
				mu.Unlock()
				result, err := r.callModel(batchCtx, request, index, admissionRefused)
				results[index] = result
				if err != nil {
					mu.Lock()
					failures = append(failures, fmt.Errorf("model helper item %d: %w", index, err))
					cancel()
					mu.Unlock()
					return
				}
			}
		})
	}
	workers.Wait()
	return results, errors.Join(append(failures, ctx.Err())...)
}

func validateModelHelper(request ModelHelperRequest) error {
	if session.ValidateID(string(request.OperationID)) != nil || session.ValidateID(string(request.Turn.ID)) != nil || session.ValidateID(string(request.Turn.SessionID)) != nil {
		return fmt.Errorf("%w: invalid model helper execution identity", session.ErrInvalid)
	}
	if len(request.Prompts) == 0 || len(request.Prompts) > session.MaxModelBatchItems {
		return fmt.Errorf("%w: model helper requires 1–32 prompts", session.ErrInvalid)
	}
	for _, prompt := range request.Prompts {
		if !utf8.ValidString(prompt) || session.ValidateText(prompt, session.MaxDocumentBytes) != nil {
			return fmt.Errorf("%w: invalid model helper prompt", session.ErrInvalid)
		}
	}
	if request.MaxTokens != nil && (*request.MaxTokens < 1 || *request.MaxTokens > 1_000_000) {
		return fmt.Errorf("%w: model helper output cap must be 1–1000000", session.ErrInvalid)
	}
	return nil
}

func (r *Runner) callModel(ctx context.Context, request ModelHelperRequest, index int, admissionRefused func(error) bool) (ModelResult, error) {
	logical, err := session.ModelHelperLogicalID(request.OperationID, index)
	if err != nil {
		return ModelResult{Failure: Failure(err).Failure}, &AccountingError{Err: err}
	}
	target := &helperTarget{operationID: request.OperationID, index: index, admissionRefused: admissionRefused}
	modelRequest := model.Request{
		Purpose: session.ModelHelperPurpose, SessionID: request.Turn.SessionID, TurnID: request.Turn.ID,
		Selection: request.Model, OutputTokenLimit: request.MaxTokens,
		Messages: []model.Message{{Role: session.User, Parts: []session.Part{{Type: "text", Text: request.Prompts[index]}}}},
	}
	outcome, err := r.complete(ctx, request.Turn, modelRequest, logical, target)
	if err != nil {
		return ModelResult{Failure: Failure(err).Failure}, err
	}
	if outcome.failure != nil {
		return ModelResult{AttemptID: target.attemptID, Failure: Failure(outcome.failure).Failure}, nil //nolint:nilerr // A settled item failure belongs in the aggregate, not the fatal accounting channel.
	}
	return ModelResult{AttemptID: target.attemptID, Text: target.text}, nil
}
