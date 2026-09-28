// Package runner implements the model/code/tool loop through injected
// boundaries. It cannot own SQL, scheduler lifetimes or transport connections.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

type Provider interface {
	Prepare(context.Context, model.Request) (model.Prepared, error)
}
type Transcript interface {
	History(context.Context, session.SessionID, int64, int) ([]session.Message, error)
}

// Mail presents durable steer revisions only at model-request boundaries.
// Reading client views cannot cause presentation or delivery.
type Mail interface {
	ObserveSteers(context.Context, session.TurnID) ([]session.Message, error)
}
type Attempts interface {
	ReserveModelAttempt(context.Context, session.ModelAttemptSpec) (session.ModelAttempt, error)
	DispatchModelAttempt(context.Context, session.ModelAttemptID) (bool, error)
	SettleModelAttempt(context.Context, session.ModelAttemptID, session.ModelAttemptResult, *session.MessageDraft) (session.ModelAttempt, error)
}
type ContentReader interface {
	ReadContent(context.Context, session.SessionID, string, int64) (session.ContentReference, []byte, error)
}
type Executor interface {
	Instructions(context.Context, session.SessionID) (string, error)
	Execute(context.Context, session.Turn, session.MessageID, session.ToolCall) (session.ToolResult, error)
}

// Progress is ephemeral presentation, never transcript or execution authority.
// The observer runs synchronously and cannot veto or own provider execution.
type Progress interface {
	BeginPreview(session.Turn, session.ModelAttemptID, session.MessageID) (func(model.Chunk), func())
}

type Runner struct {
	provider   Provider
	transcript Transcript
	attempts   Attempts
	content    ContentReader
	executor   Executor
	progress   Progress
	mail       Mail
}

func New(provider Provider, transcript Transcript, attempts Attempts, content ContentReader, executor Executor, progress Progress, mail Mail) (*Runner, error) {
	if provider == nil || transcript == nil || attempts == nil {
		return nil, errors.New("runner requires provider, transcript and attempt ledger")
	}
	return &Runner{provider: provider, transcript: transcript, attempts: attempts, content: content, executor: executor, progress: progress, mail: mail}, nil
}

type Outcome struct {
	State   session.TurnState
	Failure *string
}

// Failure normalizes external errors into a valid, bounded durable outcome.
func Failure(err error) Outcome {
	message := strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(err.Error(), "�"), "\x00", "�"))
	if message == "" {
		message = "execution failed"
	}
	if len(message) > 16384 {
		message = message[:16384]
		for !utf8.ValidString(message) {
			message = message[:len(message)-1]
		}
	}
	return Outcome{State: session.Failed, Failure: &message}
}

// Run persists completed output under a stable message ID. The caller settles
// the turn separately; retrying that settlement must never call Run again.
func (r *Runner) Run(ctx context.Context, turn session.Turn, configuration session.Configuration) (Outcome, error) {
	request := model.Request{SessionID: turn.SessionID, TurnID: turn.ID, Selection: configuration.Model, Instructions: configuration.Instructions.Text}
	if r.executor != nil {
		instructions, err := r.executor.Instructions(ctx, turn.SessionID)
		if err != nil {
			return Failure(err), nil
		}
		request.Instructions += "\n" + instructions
		request.Tools = []model.Tool{{Name: "execute", Description: "Execute a code cell in this session’s persistent, isolated REPL. Host operations require separate authority.", InputSchema: json.RawMessage(`{"type":"object","properties":{"code":{"type":"string"}},"required":["code"],"additionalProperties":false}`)}}
	}
	request.Instructions += outputInstructions(configuration.OutputSchema)
	var after int64
	size := len(request.Instructions)
	for {
		messages, err := r.transcript.History(ctx, turn.SessionID, after, 100)
		if err != nil {
			return Outcome{}, fmt.Errorf("load transcript: %w", err)
		}
		if len(messages) == 0 {
			break
		}
		for _, message := range messages {
			raw, err := json.Marshal(message.Parts)
			if err != nil {
				return Outcome{}, err
			}
			size += len(raw)
			// Compaction will supply a summary boundary in Phase 5. Until then, reject
			// overlarge context explicitly instead of silently dropping conversation.
			if len(request.Messages) >= 100 || size > 4<<20 {
				return Failure(errors.New("model context exceeds limit; compaction is required")), nil
			}
			request.Messages = append(request.Messages, model.Message{Role: message.Role, Parts: message.Parts})
			after = message.Sequence
		}
	}
	if err := r.hydrate(ctx, &request); err != nil {
		return Failure(err), nil
	}
	correctedOutput := false
	for round := 1; round <= 32; round++ {
		if r.mail != nil {
			messages, err := r.mail.ObserveSteers(ctx, turn.ID)
			if err != nil {
				return Outcome{}, fmt.Errorf("present steer mail: %w", err)
			}
			for _, message := range messages {
				if err := appendContext(&request, message.Role, message.Parts, &size); err != nil {
					return Failure(err), nil
				}
			}
		}
		completed, err := r.complete(ctx, turn, request, fmt.Sprintf("%s_model_%d", turn.ID, round))
		if err != nil {
			return Outcome{}, err
		}
		if completed.failure != nil {
			return Failure(completed.failure), nil //nolint:nilerr // Settled execution failure is a turn outcome, not an infrastructure error.
		}
		calls := []session.ToolCall{}
		for _, part := range completed.parts {
			if part.Call != nil {
				calls = append(calls, *part.Call)
			}
		}
		if correctedOutput && len(calls) != 0 {
			return Failure(errors.New("output_invalid: the corrective response must be a final JSON value without tool calls")), nil
		}
		if len(calls) == 0 {
			_, validationErr := session.ValidateOutput(configuration.OutputSchema, completed.parts)
			if validationErr == nil {
				return Outcome{State: session.Succeeded}, nil
			}
			if err := ctx.Err(); err != nil {
				return Outcome{}, err
			}
			if correctedOutput {
				return Failure(fmt.Errorf("output_invalid: final response does not match the captured output schema: %w", validationErr)), nil
			}
			correctedOutput = true
			// The attempt and invalid authored message are already durable. Only
			// the corrective notice is provisional context for the next recorded
			// model call; no provider request or completed effect is replayed.
			if err := appendContext(&request, session.Assistant, completed.parts, &size); err != nil {
				return Failure(err), nil
			}
			if err := appendContext(&request, session.System, []session.Part{{Type: "text", Text: outputCorrection(validationErr)}}, &size); err != nil {
				return Failure(err), nil
			}
			if err := r.hydrate(ctx, &request); err != nil {
				return Failure(err), nil
			}
			continue
		}
		if r.executor == nil {
			return Failure(errors.New("code executor is unavailable")), nil
		}
		if err := appendContext(&request, session.Assistant, completed.parts, &size); err != nil {
			return Failure(err), nil
		}
		for _, call := range calls {
			if call.Name != "execute" {
				return Failure(errors.New("unsupported tool call")), nil
			}
			result, err := r.executor.Execute(ctx, turn, completed.messageID, call)
			if err != nil {
				return Outcome{}, err
			}
			if err := appendContext(&request, session.Tool, []session.Part{{Type: "tool_result", Result: &result}}, &size); err != nil {
				return Failure(err), nil
			}
		}
	}
	return Failure(errors.New("turn exceeded the 32 model-call limit")), nil
}

func appendContext(request *model.Request, role session.Role, parts []session.Part, size *int) error {
	raw, err := json.Marshal(parts)
	if err != nil {
		return err
	}
	*size += len(raw)
	if len(request.Messages) >= 100 || *size > 4<<20 {
		return errors.New("model context exceeds limit; compaction is required")
	}
	request.Messages = append(request.Messages, model.Message{Role: role, Parts: parts})
	return nil
}

func (r *Runner) hydrate(ctx context.Context, request *model.Request) error {
	request.Contents = map[string]model.Content{}
	remaining := int64(session.MaxContentBytes)
	for _, message := range request.Messages {
		for _, part := range message.Parts {
			if part.Type != "content" {
				continue
			}
			if _, exists := request.Contents[part.ReferenceID]; exists {
				continue
			}
			if r.content == nil {
				return errors.New("content reader is unavailable")
			}
			reference, data, err := r.content.ReadContent(ctx, request.SessionID, part.ReferenceID, remaining)
			if err != nil {
				return fmt.Errorf("hydrate model content: %w", err)
			}
			if int64(len(data)) > remaining {
				return errors.New("hydrated content exceeds context limit")
			}
			remaining -= int64(len(data))
			request.Contents[part.ReferenceID] = model.Content{MediaType: reference.MediaType, Data: data}
		}
	}
	return nil
}

func (r *Runner) settleAttempt(parent context.Context, id session.ModelAttemptID, result session.ModelAttemptResult, message *session.MessageDraft) error {
	// A completed response survives observer cancellation. Retry only the write,
	// using its stable identity; never send a second provider request.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, err := r.attempts.SettleModelAttempt(ctx, id, result, message)
		if err == nil || errors.Is(err, session.ErrInvalid) {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), err)
		case <-ticker.C:
		}
	}
}

func (r *Runner) complete(ctx context.Context, turn session.Turn, request model.Request, logicalID string) (attemptOutcome, error) {
	prepared, err := r.provider.Prepare(ctx, request)
	if err != nil {
		return attemptOutcome{failure: err}, nil //nolint:nilerr // Preparation failure is a turn outcome; no dispatched evidence needs settlement.
	}
	if prepared.Execute == nil {
		return attemptOutcome{failure: errors.New("provider returned no executable request")}, nil
	}
	limit := max(prepared.MaxAttempts, 1)
	if limit > 5 {
		return attemptOutcome{failure: errors.New("provider attempt limit exceeds five")}, nil
	}
	for number := 1; number <= limit; number++ {
		outcome, err := r.attempt(ctx, turn, prepared, logicalID, number)
		if err != nil {
			return attemptOutcome{}, err
		}
		callErr := outcome.failure
		if callErr == nil {
			return outcome, nil
		}
		if ctx.Err() != nil {
			return attemptOutcome{}, ctx.Err()
		}
		failure, retry := errors.AsType[*model.CallError](callErr)
		if number == limit || !retry || !failure.Retryable || failure.Uncertain {
			return outcome, nil
		}
		// Only an explicit retryable provider response permits another dispatch.
		// Network uncertainty and settlement failures never automatically replay.
		delay := min(max(time.Duration(number)*time.Second, failure.RetryAfter), time.Minute)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return attemptOutcome{}, ctx.Err()
		case <-timer.C:
		}
	}
	return attemptOutcome{}, errors.New("model attempts exhausted without an outcome")
}

// Attempt execution can fail normally. A separate returned error means its
// durable evidence could not be settled and execution must stop without retry.
type attemptOutcome struct {
	failure   error
	parts     []session.Part
	messageID session.MessageID
}

func (r *Runner) attempt(ctx context.Context, turn session.Turn, prepared model.Prepared, logicalID string, number int) (attemptOutcome, error) {
	id := session.ModelAttemptID(fmt.Sprintf("%s_try_%d", logicalID, number))
	attempt, err := r.attempts.ReserveModelAttempt(ctx, session.ModelAttemptSpec{ID: id, TurnID: turn.ID, LogicalID: logicalID, Number: number, Request: prepared.Snapshot})
	if err != nil {
		return attemptOutcome{}, fmt.Errorf("reserve model attempt: %w", err)
	}
	if attempt.State != session.AttemptReserved {
		return attemptOutcome{}, errors.New("model attempt already dispatched; automatic replay prohibited")
	}
	allowed, err := r.attempts.DispatchModelAttempt(ctx, id)
	if err != nil || !allowed {
		if err == nil {
			err = errors.New("model attempt dispatch already claimed")
		}
		// Only a confirmed reserved attempt can become a no-dispatch cancellation.
		// Ambiguous dispatch failures stay pending and fault settlement/recovery.
		cancelled := session.ModelAttemptResult{State: session.AttemptCancelled}
		if settleErr := r.settleAttempt(ctx, id, cancelled, nil); settleErr != nil {
			return attemptOutcome{}, errors.Join(err, settleErr)
		}
		return attemptOutcome{}, err
	}
	messageID := session.MessageID(logicalID + "_answer")
	var emit func(model.Chunk)
	if r.progress != nil {
		var end func()
		emit, end = r.progress.BeginPreview(turn, id, messageID)
		defer end() // Keep the preview until settlement, including SQL-only retries.
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(prepared.Snapshot.TimeoutMillis)*time.Millisecond)
	started := time.Now()
	response, callErr := prepared.Execute(callCtx, emit)
	elapsed := time.Since(started)
	elapsedMillis := elapsed.Milliseconds()
	if elapsed%time.Millisecond != 0 {
		elapsedMillis++
	}
	cancel()
	result := session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: response.Usage, ReportedCostNanoUSD: response.ReportedCostNanoUSD, UsageNote: response.UsageNote, ElapsedMillis: &elapsedMillis}
	if result.Usage.Validate() != nil {
		result.Usage = session.ModelUsage{}
		result.UsageNote = new("provider returned invalid usage; counts unavailable")
	}
	if result.ReportedCostNanoUSD != nil && *result.ReportedCostNanoUSD < 0 {
		result.ReportedCostNanoUSD = nil
		result.UsageNote = new("provider returned invalid cost; reported cost unavailable")
	}
	var message *session.MessageDraft
	if callErr == nil {
		callErr = session.ValidateMessage(session.Assistant, response.Parts)
	}
	if callErr != nil {
		result.State = session.AttemptFailed
		failure, typed := errors.AsType[*model.CallError](callErr)
		uncertain := typed && failure.Uncertain
		if uncertain || ctx.Err() != nil || errors.Is(callErr, context.DeadlineExceeded) || errors.Is(callErr, context.Canceled) {
			result.State = session.AttemptUncertain
		}
		result.Failure = Failure(callErr).Failure
	} else {
		message = &session.MessageDraft{ID: messageID, Role: session.Assistant, Parts: response.Parts}
	}
	if err := r.settleAttempt(ctx, id, result, message); err != nil {
		return attemptOutcome{}, fmt.Errorf("settle model attempt: %w", err)
	}
	outcome := attemptOutcome{failure: callErr}
	if message != nil {
		outcome.parts = message.Parts
		outcome.messageID = message.ID
	}
	return outcome, nil
}
