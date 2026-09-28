package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestContinuationBytesTriggerCompactionWithoutEnteringHelperContext(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(6, 2))
	for _, message := range ledger.raw {
		if message.Role == session.Assistant {
			ledger.messages[message.ID] = session.MessageDraft{
				ID: message.ID, Role: message.Role, Parts: message.Parts,
				Continuation: &session.ModelContinuation{Scope: strings.Repeat("a", 64), Data: `[{"opaque":"` + strings.Repeat("x", 900000) + `"}]`},
			}
		}
	}
	helpers, ordinary := 0, 0
	provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		if request.Purpose == "compaction" {
			helpers++
			for _, message := range request.Messages {
				if message.Continuation != nil {
					t.Fatal("private state entered a compaction helper")
				}
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: "Short summary."}}, Continuation: &session.ModelContinuation{Scope: strings.Repeat("b", 64), Data: `[{"opaque":"helper-only"}]`}}, nil
		}
		ordinary++
		continuations := 0
		for _, message := range request.Messages {
			if message.Continuation != nil {
				continuations++
			}
		}
		if continuations != 4 || model.EstimateInputTokens(request) < 900000 {
			t.Fatalf("selected continuations or pressure estimate lost: %d", continuations)
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
	})
	runner, err := New(provider, ledger, ledger, nil, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := runner.Run(t.Context(), session.Turn{ID: "turn", SessionID: "owner", State: session.Running}, session.Configuration{Model: session.ModelSelection{Provider: "fixture", Name: "model"}})
	if err != nil || outcome.State != session.Succeeded || helpers != 1 || ordinary != 1 {
		t.Fatalf("outcome=%+v err=%v helpers=%d ordinary=%d", outcome, err, helpers, ordinary)
	}
	if len(ledger.values) != 1 || len(ledger.raw) != 13 {
		t.Fatal("compaction changed raw history or authored a helper message")
	}
	for _, value := range ledger.values {
		raw, _ := json.Marshal(value)
		if strings.Contains(string(raw), "helper-only") {
			t.Fatal("discarded helper continuation was persisted in the summary")
		}
	}
}

func TestInvalidContinuationStillSettlesKnownAccountingWithoutMessage(t *testing.T) {
	for _, data := range []string{"not-json", `[{"opaque":"` + strings.Repeat("x", session.MaxContinuationBytes) + `"}]`} {
		ledger := newOutputLedger()
		provider := providerFunc(func(context.Context, model.Request) (model.Response, error) {
			return model.Response{
				Parts: []session.Part{{Type: "text", Text: "completed"}}, Continuation: &session.ModelContinuation{Scope: strings.Repeat("a", 64), Data: data},
				Usage: session.ModelUsage{Input: new(int64(17)), Output: new(int64(3))}, ReportedCostNanoUSD: new(int64(23)),
			}, nil
		})
		runner, err := New(provider, ledger, ledger, nil, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := runner.Run(t.Context(), session.Turn{ID: "turn", SessionID: "owner"}, session.Configuration{Model: session.ModelSelection{Provider: "fixture", Name: "model"}})
		if err != nil || outcome.State != session.Failed || len(ledger.messages) != 0 || len(ledger.results) != 1 {
			t.Fatalf("invalid private state was executable: %+v %v", outcome, err)
		}
		for _, result := range ledger.results {
			if result.State != session.AttemptUncertain || result.Usage.Input == nil || *result.Usage.Input != 17 || result.ReportedCostNanoUSD == nil || *result.ReportedCostNanoUSD != 23 {
				t.Fatal("invalid continuation lost truthful usage", result)
			}
		}
	}
}
