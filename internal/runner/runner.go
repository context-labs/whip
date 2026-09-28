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
	provider    Provider
	transcript  Transcript
	attempts    Attempts
	content     ContentReader
	executor    Executor
	progress    Progress
	mail        Mail
	compactions Compactions
}

func New(provider Provider, transcript Transcript, attempts Attempts, content ContentReader, executor Executor, progress Progress, mail Mail, compactions Compactions) (*Runner, error) {
	if provider == nil || transcript == nil || attempts == nil {
		return nil, errors.New("runner requires provider, transcript and attempt ledger")
	}
	return &Runner{provider: provider, transcript: transcript, attempts: attempts, content: content, executor: executor, progress: progress, mail: mail, compactions: compactions}, nil
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
	if turn.Kind == session.CompactInput {
		return r.compactTurn(ctx, turn, configuration)
	}
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
	folds := 0
	size, err := r.prepareContext(ctx, turn, configuration, &request, &folds, nil)
	if err != nil {
		return Failure(err), nil
	}
	if err := r.hydrate(ctx, &request); err != nil {
		return Failure(err), nil
	}
	pressure := contextPressure{}
	correctedOutput := false
	replannedContext := false
	var correction []session.Part
	for round := 1; round <= 32; round++ {
		if r.mail != nil {
			messages, err := r.mail.ObserveSteers(ctx, turn.ID)
			if err != nil {
				return Outcome{}, fmt.Errorf("present steer mail: %w", err)
			}
			for _, message := range messages {
				if err := r.appendTurnContext(&request, message.Role, message.Parts, &size); err != nil {
					return Failure(err), nil
				}
			}
		}
		if len(request.Messages) > maxContextMessages || size > maxContextBytes {
			size, err = r.prepareContext(ctx, turn, configuration, &request, &folds, correction)
			if err != nil {
				return Failure(err), nil
			}
			if err := r.hydrate(ctx, &request); err != nil {
				return Failure(err), nil
			}
		}
		prepared, err := r.prepareOrdinary(ctx, turn, configuration, &request, &size, &folds, correction, &pressure)
		if err != nil {
			return Failure(err), nil
		}
		completed, err := r.completePrepared(ctx, turn, prepared, fmt.Sprintf("%s_model_%d", turn.ID, round), nil)
		if err != nil {
			return Outcome{}, err
		}
		if completed.failure != nil {
			if completed.contextLimit && !replannedContext && r.compactions != nil && ctx.Err() == nil {
				replannedContext = true
				size, err = r.replanContext(ctx, turn, configuration, &request, &folds, correction)
				if err != nil {
					return Failure(fmt.Errorf("context rejection cannot be replanned: %w", err)), nil
				}
				continue
			}
			return Failure(completed.failure), nil
		}
		pressure.observe(completed.inputTokens, model.EstimateInputTokens(request))
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
				if folds == 0 && r.compactions != nil && prepared.ContextWindowTokens != nil && *prepared.ContextWindowTokens > 0 {
					if err := r.appendTurnContext(&request, session.Assistant, completed.parts, &size); err != nil {
						return Failure(err), nil
					}
					if err := r.hydrate(ctx, &request); err != nil {
						return Failure(err), nil
					}
					if _, err := r.proactiveContext(ctx, turn, configuration, &request, &size, &folds, correction, &pressure, prepared); err != nil {
						return Failure(err), nil
					}
				}
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
			if err := r.appendTurnContext(&request, session.Assistant, completed.parts, &size); err != nil {
				return Failure(err), nil
			}
			correction = []session.Part{{Type: "text", Text: outputCorrection(validationErr)}}
			if err := r.appendTurnContext(&request, session.System, correction, &size); err != nil {
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
		if err := r.appendTurnContext(&request, session.Assistant, completed.parts, &size); err != nil {
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
			if err := r.appendTurnContext(&request, session.Tool, []session.Part{{Type: "tool_result", Result: &result}}, &size); err != nil {
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
	request.Messages = append(request.Messages, model.Message{Role: role, Parts: parts})
	if len(request.Messages) > maxContextMessages || *size > maxContextBytes {
		return errContextLimit
	}
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

func (r *Runner) complete(ctx context.Context, turn session.Turn, request model.Request, logicalID string, target *compactionTarget) (attemptOutcome, error) {
	prepared, err := r.provider.Prepare(ctx, request)
	if err != nil {
		return attemptOutcome{failure: err}, nil //nolint:nilerr // Preparation failure is a turn outcome; no dispatched evidence needs settlement.
	}
	return r.completePrepared(ctx, turn, prepared, logicalID, target)
}

func (r *Runner) completePrepared(ctx context.Context, turn session.Turn, prepared model.Prepared, logicalID string, target *compactionTarget) (attemptOutcome, error) {
	if err := validatePrepared(prepared); err != nil {
		return attemptOutcome{failure: err}, nil //nolint:nilerr // Invalid preparation is a turn outcome; nothing was dispatched.
	}
	limit := max(prepared.MaxAttempts, 1)
	var inputTokens *int64
	for number := 1; number <= limit; number++ {
		outcome, err := r.attempt(ctx, turn, prepared, logicalID, number, target)
		if err != nil {
			return attemptOutcome{}, err
		}
		if outcome.inputTokens != nil {
			inputTokens = outcome.inputTokens
		} else {
			// A confirmed retry uses the same frozen request. Missing later
			// usage does not invalidate an earlier settled input observation.
			outcome.inputTokens = inputTokens
		}
		callErr := outcome.failure
		if callErr == nil {
			return outcome, nil
		}
		if ctx.Err() != nil {
			return attemptOutcome{}, ctx.Err()
		}
		failure, retry := errors.AsType[*model.CallError](callErr)
		if number == limit || !retry || !failure.Retryable || failure.Uncertain || failure.ContextLimit {
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

func validatePrepared(prepared model.Prepared) error {
	if prepared.Execute == nil {
		return errors.New("provider returned no executable request")
	}
	if prepared.MaxAttempts > 5 {
		return errors.New("provider attempt limit exceeds five")
	}
	return nil
}

// Attempt execution can fail normally. A separate returned error means its
// durable evidence could not be settled and execution must stop without retry.
type attemptOutcome struct {
	contextLimit bool
	inputTokens  *int64
	failure      error
	parts        []session.Part
	messageID    session.MessageID
}

func (r *Runner) attempt(ctx context.Context, turn session.Turn, prepared model.Prepared, logicalID string, number int, target *compactionTarget) (attemptOutcome, error) {
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
		if settleErr := r.settleResult(ctx, id, cancelled, nil, target, nil); settleErr != nil {
			return attemptOutcome{}, errors.Join(err, settleErr)
		}
		return attemptOutcome{}, err
	}
	messageID := session.MessageID(logicalID + "_answer")
	var emit func(model.Chunk)
	if r.progress != nil && target == nil {
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
	var draft *session.CompactionDraft
	if callErr == nil {
		callErr = session.ValidateMessage(session.Assistant, response.Parts)
	}
	if callErr == nil && target != nil {
		draft, callErr = target.response(id, response.Parts)
	}
	if callErr != nil {
		result.State = session.AttemptFailed
		failure, typed := errors.AsType[*model.CallError](callErr)
		uncertain := typed && failure.Uncertain
		if uncertain || ctx.Err() != nil || errors.Is(callErr, context.DeadlineExceeded) || errors.Is(callErr, context.Canceled) {
			result.State = session.AttemptUncertain
		}
		result.Failure = Failure(callErr).Failure
	} else if target == nil {
		message = &session.MessageDraft{ID: messageID, Role: session.Assistant, Parts: response.Parts}
	}
	if err := r.settleResult(ctx, id, result, message, target, draft); err != nil {
		return attemptOutcome{}, fmt.Errorf("settle model attempt: %w", err)
	}
	outcome := attemptOutcome{failure: callErr}
	if target == nil && result.Usage.Input != nil {
		outcome.inputTokens = new(*result.Usage.Input)
	}
	if failure, ok := errors.AsType[*model.CallError](callErr); ok && target == nil && result.State == session.AttemptFailed {
		outcome.contextLimit = failure.ContextLimit && !failure.Uncertain
	}
	if callErr == nil && target != nil && !target.settled.Selected {
		outcome.failure = errors.New("compaction was not selected")
		if target.settled.Rejection != nil {
			outcome.failure = errors.New(*target.settled.Rejection)
		}
	}
	if message != nil {
		outcome.parts = message.Parts
		outcome.messageID = message.ID
	}
	return outcome, nil
}
