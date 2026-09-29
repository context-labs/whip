// Package runner implements the model/code/tool loop through injected
// boundaries. It cannot own SQL, scheduler lifetimes or transport connections.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	TurnGoal(context.Context, session.TurnID) (*session.GoalContext, error)
	History(context.Context, session.SessionID, int64, int) ([]session.Message, error)
	Continuations(context.Context, session.SessionID, []session.MessageID) (map[session.MessageID]session.ModelContinuation, error)
}

// Mail presents authored input and mail steering only at safe model boundaries.
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
	// Instructions resolves the complete base instructions once from this turn's
	// captured policy. Later model requests reuse the returned text.
	Instructions(context.Context, session.Turn, session.Instructions) (string, error)
	// Execute returns the exact committed tool-message parts, including trusted images.
	Execute(context.Context, session.Turn, session.MessageID, session.ToolCall) ([]session.Part, error)
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
	maintenance Maintenance
}

func New(provider Provider, transcript Transcript, attempts Attempts, content ContentReader, executor Executor, progress Progress, mail Mail, compactions Compactions, maintenance Maintenance) (*Runner, error) {
	if provider == nil || transcript == nil || attempts == nil {
		return nil, errors.New("runner requires provider, transcript and attempt ledger")
	}
	return &Runner{provider: provider, transcript: transcript, attempts: attempts, content: content, executor: executor, progress: progress, mail: mail, compactions: compactions, maintenance: maintenance}, nil
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
	if turn.Kind == session.HostOperationInputKind {
		return Failure(errors.New("direct host work cannot enter the model runner")), nil
	}
	if turn.Kind == session.AutomaticTitleInputKind {
		return r.automaticTitle(ctx, turn)
	}
	if turn.Kind == session.GoalFormulationInputKind {
		return r.formulateGoal(ctx, turn, configuration)
	}
	if turn.Kind == session.CompactInput {
		return r.compactTurn(ctx, turn, configuration)
	}
	request := model.Request{SessionID: turn.SessionID, TurnID: turn.ID, Selection: configuration.Model, Instructions: configuration.Instructions.Text}
	if configuration.Run != nil {
		request.CacheKey = configuration.Run.CacheKey
		if configuration.Run.System != "" {
			request.Instructions = configuration.Run.System
		}
	}
	if r.executor != nil {
		instructions, err := r.executor.Instructions(ctx, turn, configuration.Instructions)
		if err != nil {
			return Failure(err), nil
		}
		request.Instructions = instructions
		request.Tools = []model.Tool{{Name: "execute", Description: "Execute a code cell in this session’s persistent, isolated REPL. Host operations require separate authority.", InputSchema: json.RawMessage(`{"type":"object","properties":{"code":{"type":"string"}},"required":["code"],"additionalProperties":false}`)}}
	}
	request.Instructions += outputInstructions(configuration.OutputSchema)
	if err := r.addGoalContext(ctx, turn, &request); err != nil {
		return Failure(err), nil
	}
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
	baseInstructions := request.Instructions
	for round := 1; configuration.Run != nil || round <= 32; round++ {
		if err := ctx.Err(); err != nil {
			return Outcome{}, err
		}
		final := configuration.Run != nil && configuration.Run.MaxTurns > 0 && round > configuration.Run.MaxTurns
		request.Instructions = baseInstructions
		request.Notices = ""
		if notices, ok := r.executor.(interface{ TurnNotices(session.Turn) string }); ok {
			if text := notices.TurnNotices(turn); text != "" {
				request.Notices += "\n\n--- Hook notices ---\n" + text
			}
		}
		if final {
			request.Tools = nil
			request.Purpose = "final"
			request.Notices += "\n\nYou have reached the tool-call limit. Do not request any more tools. Give your final answer using only what you have already gathered."
		}
		request.Instructions += request.Notices
		if r.mail != nil {
			messages, err := r.mail.ObserveSteers(ctx, turn.ID)
			if err != nil {
				return Outcome{}, fmt.Errorf("present steering: %w", err)
			}
			for _, message := range messages {
				if err := r.appendTurnContext(&request, message.Role, message.Parts, &size); err != nil {
					return Failure(err), nil
				}
			}
			if len(messages) > 0 {
				if err := r.hydrate(ctx, &request); err != nil {
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
		if final && len(calls) != 0 {
			return Failure(errors.New("final response requested a tool after the configured limit")), nil
		}
		if correctedOutput && len(calls) != 0 {
			return Failure(errors.New("output_invalid: the corrective response must be a final JSON value without tool calls")), nil
		}
		if len(calls) == 0 {
			// A text response also closes a safe batch. Preserve it, then follow
			// accepted steering in this same turn if another model call is allowed.
			// At a terminal ceiling, untaken inputs retain normal queued delivery.
			if !final && (configuration.Run != nil || round < 32) && r.mail != nil {
				messages, err := r.mail.ObserveSteers(ctx, turn.ID)
				if err != nil {
					return Outcome{}, fmt.Errorf("present steering: %w", err)
				}
				if len(messages) > 0 {
					if err := r.appendCompletedContext(&request, completed, &size); err != nil {
						return Failure(err), nil
					}
					for _, message := range messages {
						if err := r.appendTurnContext(&request, message.Role, message.Parts, &size); err != nil {
							return Failure(err), nil
						}
					}
					if err := r.hydrate(ctx, &request); err != nil {
						return Failure(err), nil
					}
					continue
				}
			}
			_, validationErr := session.ValidateOutput(configuration.OutputSchema, completed.parts)
			if validationErr == nil {
				if folds == 0 && r.compactions != nil && prepared.ContextWindowTokens != nil && *prepared.ContextWindowTokens > 0 {
					if err := r.appendCompletedContext(&request, completed, &size); err != nil {
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
			if err := r.appendCompletedContext(&request, completed, &size); err != nil {
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
		if err := r.appendCompletedContext(&request, completed, &size); err != nil {
			return Failure(err), nil
		}
		for _, call := range calls {
			if call.Name != "execute" {
				return Failure(errors.New("unsupported tool call")), nil
			}
			parts, err := r.executor.Execute(ctx, turn, completed.messageID, call)
			if err != nil {
				return Outcome{}, err
			}
			if err := session.ValidateMessage(session.Tool, parts); err != nil {
				return Outcome{}, err
			}
			if parts[0].Result.CallID != call.ID {
				return Outcome{}, errors.New("committed tool result does not match executed call")
			}
			if err := r.appendTurnContext(&request, session.Tool, parts, &size); err != nil {
				return Failure(err), nil
			}
		}
		if err := r.hydrate(ctx, &request); err != nil {
			return Failure(err), nil
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

func (r *Runner) settleResult(parent context.Context, id session.ModelAttemptID, result session.ModelAttemptResult, message *session.MessageDraft, target *helperTarget, draft helperDraft) error {
	// A completed response survives observer cancellation. Retry only the write,
	// using its stable identity; never send a second provider request.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var err error
		if target != nil && target.compaction != nil {
			target.compaction.settled, err = r.compactions.SettleCompaction(ctx, id, result, draft.compaction)
		} else if target != nil && target.formulation != nil {
			*target.formulation, err = r.maintenance.SettleGoalFormulation(ctx, id, result, draft.formulation)
		} else if target != nil && target.title != nil {
			*target.title, err = r.maintenance.SettleAutomaticTitle(ctx, id, result, draft.title)
		} else {
			_, err = r.attempts.SettleModelAttempt(ctx, id, result, message)
		}
		if err == nil && target != nil {
			target.attemptID = id
		}
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

func (r *Runner) complete(ctx context.Context, turn session.Turn, request model.Request, logicalID string, target *helperTarget) (attemptOutcome, error) {
	prepared, err := r.prepare(ctx, request)
	if err != nil {
		return attemptOutcome{failure: err}, nil //nolint:nilerr // Preparation failure is a turn outcome; no dispatched evidence needs settlement.
	}
	return r.completePrepared(ctx, turn, prepared, logicalID, target)
}

func (r *Runner) completePrepared(ctx context.Context, turn session.Turn, prepared model.Prepared, logicalID string, target *helperTarget) (attemptOutcome, error) {
	if err := validatePrepared(prepared); err != nil {
		return attemptOutcome{failure: err}, nil //nolint:nilerr // Invalid preparation is a turn outcome; nothing was dispatched.
	}
	limit := max(prepared.MaxAttempts, 1)
	var inputTokens *int64
	refreshed := false
	for number := 1; number <= limit; number++ {
		if err := ctx.Err(); err != nil {
			return attemptOutcome{}, err
		}
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
		if number == limit || !outcome.dispatched || !retry || failure.Uncertain || failure.ContextLimit {
			return outcome, nil
		}
		if failure.AuthRejected {
			if refreshed || failure.StatusCode != http.StatusUnauthorized || prepared.RefreshCredentials == nil {
				return outcome, nil
			}
			refreshed = true
			next, err := prepared.RefreshCredentials(ctx)
			if err != nil {
				return attemptOutcome{failure: err}, nil //nolint:nilerr // Refresh failure is a turn outcome after the rejected attempt settled.
			}
			if (next.ContextWindowTokens == nil) != (prepared.ContextWindowTokens == nil) || next.ContextWindowTokens != nil && *next.ContextWindowTokens != *prepared.ContextWindowTokens {
				return attemptOutcome{}, errors.New("credential refresh changed the prepared context capacity")
			}
			// Credential refresh retains the identical prepared request and its
			// captured context evidence, never a fresh context observation.
			if prepared.Snapshot.Context != nil {
				evidence := prepared.Snapshot.Context.Clone()
				next.Snapshot.Context = &evidence
			}
			if err := validatePrepared(next); err != nil {
				return attemptOutcome{failure: err}, nil //nolint:nilerr // Invalid refreshed preparation cannot dispatch.
			}
			if next.Snapshot.RequestDigest != prepared.Snapshot.RequestDigest {
				return attemptOutcome{}, errors.New("credential refresh changed the prepared request")
			}
			next.Capture = prepared.Capture
			prepared = next
			continue
		}
		if !failure.Retryable {
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
	dispatched   bool
	contextLimit bool
	inputTokens  *int64
	failure      error
	parts        []session.Part
	messageID    session.MessageID
	continuation *session.ModelContinuation
}

func (r *Runner) attempt(ctx context.Context, turn session.Turn, prepared model.Prepared, logicalID string, number int, target *helperTarget) (attemptOutcome, error) {
	id := session.ModelAttemptID(fmt.Sprintf("%s_try_%d", logicalID, number))
	spec := session.ModelAttemptSpec{ID: id, TurnID: turn.ID, LogicalID: logicalID, Number: number, Request: prepared.Snapshot}
	if target.stateless() {
		spec.OperationID, spec.BatchIndex = &target.operationID, &target.index
	}
	if prepared.Capture != nil {
		if publisher, ok := r.content.(interface {
			PublishModelCapture(context.Context, session.SessionID, session.ModelCapture) (session.ModelCapture, error)
		}); ok {
			capture, err := publisher.PublishModelCapture(ctx, turn.SessionID, *prepared.Capture)
			if err != nil {
				return attemptOutcome{}, err
			}
			spec.Capture = &capture
		}
	}
	attempt, err := r.attempts.ReserveModelAttempt(ctx, spec)
	if err != nil {
		if target.refused(err) {
			return attemptOutcome{failure: err}, nil
		}
		return attemptOutcome{}, accountingError(target, fmt.Errorf("reserve model attempt: %w", err))
	}
	if attempt.State != session.AttemptReserved {
		return attemptOutcome{}, accountingError(target, errors.New("model attempt already dispatched; automatic replay prohibited"))
	}
	if prepared.BeforeDispatch != nil {
		if err := prepared.BeforeDispatch(ctx); err != nil {
			cancelled := session.ModelAttemptResult{State: session.AttemptCancelled, Failure: Failure(err).Failure}
			if settleErr := r.settleResult(ctx, id, cancelled, nil, target, helperDraft{}); settleErr != nil {
				return attemptOutcome{}, accountingError(target, errors.Join(err, settleErr))
			}
			return attemptOutcome{failure: err}, nil
		}
	}
	allowed, err := r.attempts.DispatchModelAttempt(ctx, id)
	if err != nil || !allowed {
		if err == nil {
			err = errors.New("model attempt dispatch already claimed")
		}
		// Only a confirmed reserved attempt can become a no-dispatch cancellation.
		// Ambiguous dispatch failures stay pending and fault settlement/recovery.
		cancelled := session.ModelAttemptResult{State: session.AttemptCancelled}
		if settleErr := r.settleResult(ctx, id, cancelled, nil, target, helperDraft{}); settleErr != nil {
			return attemptOutcome{}, accountingError(target, errors.Join(err, settleErr))
		}
		if target.refused(err) {
			return attemptOutcome{failure: err}, nil
		}
		return attemptOutcome{}, accountingError(target, err)
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
	var draft helperDraft
	if callErr == nil {
		callErr = session.ValidateMessage(session.Assistant, response.Parts)
	}
	if callErr == nil && (target == nil || target.compaction != nil) && response.Continuation != nil {
		if response.Continuation.Validate() != nil {
			callErr = &model.CallError{Uncertain: true, Message: "provider returned invalid or oversized continuation"}
		}
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
		message = &session.MessageDraft{ID: messageID, Role: session.Assistant, Parts: response.Parts, Continuation: response.Continuation}
	}
	if err := r.settleResult(ctx, id, result, message, target, draft); err != nil {
		return attemptOutcome{}, accountingError(target, fmt.Errorf("settle model attempt: %w", err))
	}
	outcome := attemptOutcome{failure: callErr, dispatched: true}
	if target == nil && result.Usage.Input != nil {
		outcome.inputTokens = new(*result.Usage.Input)
	}
	if failure, ok := errors.AsType[*model.CallError](callErr); ok && target == nil && result.State == session.AttemptFailed {
		outcome.contextLimit = failure.ContextLimit && !failure.Uncertain
	}
	if callErr == nil && target != nil && target.compaction != nil && !target.compaction.settled.Selected {
		outcome.failure = errors.New("compaction was not selected")
		if target.compaction.settled.Rejection != nil {
			outcome.failure = errors.New(*target.compaction.settled.Rejection)
		}
	}
	if callErr == nil && target != nil && target.formulation != nil && !target.formulation.Accepted {
		outcome.failure = errors.New("goal formulation was not accepted")
		if target.formulation.Rejection != nil {
			outcome.failure = errors.New(*target.formulation.Rejection)
		}
	}
	if message != nil {
		outcome.parts = message.Parts
		outcome.messageID = message.ID
		outcome.continuation = message.Continuation
	}
	return outcome, nil
}
