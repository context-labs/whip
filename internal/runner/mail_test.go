package runner

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

type mailFunc func(context.Context, session.TurnID) ([]session.Message, error)

func (f mailFunc) ObserveSteers(ctx context.Context, turn session.TurnID) ([]session.Message, error) {
	return f(ctx, turn)
}

type mailExecutor struct{ events *[]string }

func (mailExecutor) Instructions(_ context.Context, _ session.Turn, policy session.Instructions) (string, error) {
	return policy.Text + "\nexecution instructions", nil
}

func (e mailExecutor) Execute(_ context.Context, _ session.Turn, _ session.MessageID, call session.ToolCall) ([]session.Part, error) {
	*e.events = append(*e.events, call.ID)
	return []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: call.ID, Output: "settled"}}}, nil
}

func TestMailBoundaryFollowsAllToolResultsBeforeNextModelRequest(t *testing.T) {
	var events []string
	calls := 0
	transcript := &flakyTranscript{calls: 1}
	mail := mailFunc(func(context.Context, session.TurnID) ([]session.Message, error) {
		events = append(events, "mail")
		if calls == 0 {
			return nil, nil
		}
		return []session.Message{{Role: session.User, Parts: []session.Part{{Type: "text", Text: "new steer"}}}}, nil
	})
	provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		calls++
		events = append(events, "model")
		if calls == 1 {
			return model.Response{Parts: []session.Part{
				{Type: "tool_call", Call: &session.ToolCall{ID: "first", Name: "execute", Arguments: json.RawMessage(`{"code":"print(1)"}`)}},
				{Type: "tool_call", Call: &session.ToolCall{ID: "second", Name: "execute", Arguments: json.RawMessage(`{"code":"print(2)"}`)}},
			}}, nil
		}
		if len(request.Messages) != 4 || request.Messages[1].Role != session.Tool || request.Messages[2].Role != session.Tool || request.Messages[3].Parts[0].Text != "new steer" {
			return model.Response{}, errors.New("mail interrupted an unsettled tool batch")
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
	})
	r, err := New(provider, transcript, transcript, nil, mailExecutor{events: &events}, nil, mail, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "turn"}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	want := []string{"mail", "model", "first", "second", "mail", "model"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("execution order=%v want=%v", events, want)
	}
}

func TestMailObservationFailureDoesNotMasqueradeAsNoMail(t *testing.T) {
	failure := errors.New("mail storage unavailable")
	transcript := &flakyTranscript{}
	calls := 0
	provider := providerFunc(func(context.Context, model.Request) (model.Response, error) {
		calls++
		return model.Response{}, nil
	})
	r, err := New(provider, transcript, transcript, nil, nil, nil, mailFunc(func(context.Context, session.TurnID) ([]session.Message, error) {
		return nil, failure
	}), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(t.Context(), session.Turn{ID: "turn"}, session.Configuration{}); !errors.Is(err, failure) || calls != 0 {
		t.Fatalf("observation failure=%v provider calls=%d", err, calls)
	}
}
