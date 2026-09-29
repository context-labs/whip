package runner

import (
	"context"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestInputSteeringFinalCeilingLeavesNewInputUntaken(t *testing.T) {
	calls, observations := 0, 0
	var events []string
	transcript := &flakyTranscript{calls: 1}
	provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		calls++
		if calls == 1 {
			return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: []byte(`{"code":"print(1)"}`)}}}}, nil
		}
		if request.Purpose != "final" || len(request.Tools) != 0 {
			t.Fatal("lost final-no-tools ceiling", request)
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "final"}}}, nil
	})
	boundary := mailFunc(func(context.Context, session.TurnID) ([]session.Message, error) {
		observations++
		if calls == 2 {
			t.Fatal("consumed input that could not receive a model response")
		}
		return nil, nil
	})
	r, err := New(provider, transcript, transcript, nil, mailExecutor{events: &events}, nil, boundary, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "turn"}, session.Configuration{Run: &session.RunConfiguration{MaxTurns: 1}})
	if err != nil || outcome.State != session.Succeeded || calls != 2 || observations != 2 {
		t.Fatal(outcome, err, calls, observations)
	}
}
