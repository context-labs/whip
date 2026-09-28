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
type Attempts interface {
	ReserveModelAttempt(context.Context, session.ModelAttemptSpec) (session.ModelAttempt, error)
	DispatchModelAttempt(context.Context, session.ModelAttemptID) (bool, error)
	SettleModelAttempt(context.Context, session.ModelAttemptID, session.ModelAttemptResult, *session.MessageDraft) (session.ModelAttempt, error)
}
type Runner struct {
	provider   Provider
	transcript Transcript
	attempts   Attempts
}

func New(provider Provider, transcript Transcript, attempts Attempts) (*Runner, error) {
	if provider == nil || transcript == nil || attempts == nil {
		return nil, errors.New("runner requires provider, transcript and attempt ledger")
	}
	return &Runner{provider: provider, transcript: transcript, attempts: attempts}, nil
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
	return r.complete(ctx, turn, request)
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

func (r *Runner) complete(ctx context.Context, turn session.Turn, request model.Request) (Outcome, error) {
	prepared, err := r.provider.Prepare(ctx, request)
	if err != nil {
		return Failure(err), nil
	}
	if prepared.Execute == nil {
		return Failure(errors.New("provider returned no executable request")), nil
	}
	logicalID := string(turn.ID) + "_model_1"
	id := session.ModelAttemptID(logicalID + "_try_1")
	attempt, err := r.attempts.ReserveModelAttempt(ctx, session.ModelAttemptSpec{ID: id, TurnID: turn.ID, LogicalID: logicalID, Number: 1, Request: prepared.Snapshot})
	if err != nil {
		return Outcome{}, fmt.Errorf("reserve model attempt: %w", err)
	}
	if attempt.State != session.AttemptReserved {
		return Outcome{}, errors.New("model attempt already dispatched; automatic replay prohibited")
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
			return Outcome{}, errors.Join(err, settleErr)
		}
		return Outcome{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(prepared.Snapshot.TimeoutMillis)*time.Millisecond)
	response, callErr := prepared.Execute(callCtx)
	cancel()
	result := session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: response.Usage, ReportedCostNanoUSD: response.ReportedCostNanoUSD}
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
		callErr = session.ValidateParts(response.Parts)
	}
	if callErr != nil {
		result.State = session.AttemptFailed
		if ctx.Err() != nil || errors.Is(callErr, context.DeadlineExceeded) || errors.Is(callErr, context.Canceled) {
			result.State = session.AttemptUncertain
		}
		result.Failure = Failure(callErr).Failure
	} else {
		message = &session.MessageDraft{ID: session.MessageID(string(turn.ID) + "_answer"), Role: session.Assistant, Parts: response.Parts}
	}
	if err := r.settleAttempt(ctx, id, result, message); err != nil {
		return Outcome{}, fmt.Errorf("settle model attempt: %w", err)
	}
	if callErr != nil {
		if ctx.Err() != nil {
			return Outcome{}, ctx.Err()
		}
		return Failure(callErr), nil
	}
	return Outcome{State: session.Succeeded}, nil
}
