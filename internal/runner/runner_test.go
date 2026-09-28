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

func (f providerFunc) Complete(ctx context.Context, r model.Request) (model.Response, error) {
	return f(ctx, r)
}

type flakyTranscript struct {
	calls int
	ids   []session.MessageID
}

func (*flakyTranscript) History(context.Context, session.SessionID, int64, int) ([]session.Message, error) {
	return nil, nil
}

func (s *flakyTranscript) AppendMessage(ctx context.Context, _ session.TurnID, m session.MessageDraft) (session.Message, error) {
	if err := ctx.Err(); err != nil {
		return session.Message{}, err
	}
	s.calls++
	s.ids = append(s.ids, m.ID)
	if s.calls == 1 {
		return session.Message{}, errors.New("injected ambiguous SQL acknowledgement")
	}
	return session.Message{ID: m.ID}, nil
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
	}), transcript)
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

func TestExternalFailureTextAlwaysFitsDurableContract(t *testing.T) {
	for _, text := range []string{"", " \x00 ", "\xff", strings.Repeat("💥", 6000)} {
		result := Failure(errors.New(text))
		if result.State != session.Failed || result.Failure == nil || *result.Failure == "" || len(*result.Failure) > 16384 || !utf8.ValidString(*result.Failure) || strings.ContainsRune(*result.Failure, 0) {
			t.Fatalf("invalid failure: %+v", result)
		}
	}
}
