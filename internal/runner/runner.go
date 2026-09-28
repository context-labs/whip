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
	Complete(context.Context, model.Request) (model.Response, error)
}
type Transcript interface {
	History(context.Context, session.SessionID, int64, int) ([]session.Message, error)
	AppendMessage(context.Context, session.TurnID, session.MessageDraft) (session.Message, error)
}
type Runner struct {
	provider   Provider
	transcript Transcript
}

func New(provider Provider, transcript Transcript) (*Runner, error) {
	if provider == nil || transcript == nil {
		return nil, errors.New("runner requires provider and transcript")
	}
	return &Runner{provider: provider, transcript: transcript}, nil
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
	response, err := r.provider.Complete(ctx, request)
	if err != nil {
		if ctx.Err() != nil {
			return Outcome{}, ctx.Err()
		}
		return Failure(err), nil
	}
	if err := session.ValidateParts(response.Parts); err != nil {
		return Failure(fmt.Errorf("invalid model output: %w", err)), nil
	}
	if err := r.persist(ctx, turn.ID, session.MessageDraft{
		ID: session.MessageID(string(turn.ID) + "_answer"), Role: session.Assistant, Parts: response.Parts,
	}); err != nil {
		return Outcome{}, fmt.Errorf("persist completed output: %w", err)
	}
	return Outcome{State: session.Succeeded}, nil
}

func (r *Runner) persist(parent context.Context, turn session.TurnID, message session.MessageDraft) error {
	// A completed response survives observer cancellation. Retry only the write,
	// using its stable identity; never send a second provider request.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, err := r.transcript.AppendMessage(ctx, turn, message)
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
