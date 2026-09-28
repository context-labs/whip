package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

type providerFunc func(context.Context, model.Request) (model.Response, error)

func (f providerFunc) Prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	prepared, err := (model.Scripted{}).Prepare(ctx, request)
	prepared.Execute = func(ctx context.Context, _ func(model.Chunk)) (model.Response, error) { return f(ctx, request) }
	return prepared, err
}

type flakyTranscript struct {
	calls  int
	ids    []session.MessageID
	result session.ModelAttemptResult
}

func (*flakyTranscript) History(context.Context, session.SessionID, int64, int) ([]session.Message, error) {
	return nil, nil
}

func (*flakyTranscript) ReserveModelAttempt(_ context.Context, p session.ModelAttemptSpec) (session.ModelAttempt, error) {
	return session.ModelAttempt{ID: p.ID, State: session.AttemptReserved}, nil
}

func (*flakyTranscript) DispatchModelAttempt(context.Context, session.ModelAttemptID) (bool, error) {
	return true, nil
}

func (s *flakyTranscript) SettleModelAttempt(ctx context.Context, _ session.ModelAttemptID, result session.ModelAttemptResult, m *session.MessageDraft) (session.ModelAttempt, error) {
	if err := ctx.Err(); err != nil {
		return session.ModelAttempt{}, err
	}
	s.calls++
	if m != nil {
		s.ids = append(s.ids, m.ID)
	}
	s.result = result
	if s.calls == 1 {
		return session.ModelAttempt{}, errors.New("injected ambiguous SQL acknowledgement")
	}
	return session.ModelAttempt{Result: &result}, nil
}

func TestCompletedResponseWriteRetryDoesNotRedispatch(t *testing.T) {
	transcript := &flakyTranscript{}
	calls := 0
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r, err := New(providerFunc(func(context.Context, model.Request) (model.Response, error) {
		calls++
		cancel()
		return model.Response{Parts: []session.Part{{Type: "text", Text: "already completed"}}}, nil
	}), transcript, transcript, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Run(ctx, session.Turn{ID: "turn"}, session.Configuration{})
	if err != nil || result.State != session.Succeeded {
		t.Fatalf("outcome=%+v err=%v", result, err)
	}
	if calls != 1 || transcript.calls != 2 || transcript.ids[0] != transcript.ids[1] {
		t.Fatalf("provider calls=%d writes=%+v", calls, transcript.ids)
	}
}

func TestMalformedUsageDoesNotEraseCompletedOutput(t *testing.T) {
	transcript := &flakyTranscript{}
	r, err := New(providerFunc(func(context.Context, model.Request) (model.Response, error) {
		return model.Response{Parts: []session.Part{{Type: "text", Text: "completed"}}, Usage: session.ModelUsage{Input: new(int64(-1))}}, nil
	}), transcript, transcript, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "turn"}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || len(transcript.ids) != 2 || transcript.result.Usage.Input != nil || transcript.result.UsageNote == nil {
		t.Fatalf("outcome=%+v result=%+v err=%v", outcome, transcript.result, err)
	}
}

func TestExternalFailureTextAlwaysFitsDurableContract(t *testing.T) {
	for _, text := range []string{"", " \x00 ", "\xff", strings.Repeat("💥", 6000)} {
		result := Failure(errors.New(text))
		if result.State != session.Failed || result.Failure == nil || *result.Failure == "" || len(*result.Failure) > 16384 || !utf8.ValidString(*result.Failure) || strings.ContainsRune(*result.Failure, 0) {
			t.Fatalf("invalid failure: %+v", result)
		}
	}
}
