package runner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

type goalLedger struct {
	*compactionLedger
	goal  session.GoalContext
	reads int
	err   error
}

func (s *goalLedger) TurnGoal(context.Context, session.TurnID) (*session.GoalContext, error) {
	s.reads++
	return &s.goal, s.err
}

func TestGoalContextFrozenAcrossCorrectionCompactionAndSQLRetry(t *testing.T) {
	ledger := &goalLedger{compactionLedger: newCompactionLedger(compactionMessages(54, 2)), goal: session.GoalContext{GoalRef: session.GoalRef{ID: "goal", Revision: 9007199254740993}, Spec: session.GoalSpec{Text: "SECRET-OBJECTIVE", MaxContinuations: 2}}}
	ledger.ambiguous = true
	ref := ledger.goal.GoalRef
	ordinary, helpers := 0, 0
	captured := ""
	r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		if request.Purpose == "compaction" {
			helpers++
			if strings.Contains(request.Instructions, "SECRET-OBJECTIVE") || strings.Contains(request.Instructions, "goals.complete") {
				t.Fatal("helper inherited goal context")
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
		}
		ordinary++
		if !strings.Contains(request.Instructions, "SECRET-OBJECTIVE") || !strings.Contains(request.Instructions, `"revision":"9007199254740993"`) || !strings.Contains(request.Instructions, "grants no additional authority") {
			t.Fatal("captured goal missing", request.Instructions)
		}
		if captured == "" {
			captured = request.Instructions
		} else if request.Instructions != captured {
			t.Fatal("goal instructions changed across rounds")
		}
		ledger.goal.Spec.Text = "mutated after dispatch"
		ledger.goal.Revision++
		if ordinary == 1 {
			return model.Response{Parts: []session.Part{{Type: "text", Text: `"invalid"`}}}, nil
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: `42`}}}, nil
	}), ledger, ledger, nil, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "turn", SessionID: "owner", Goal: &ref}, session.Configuration{GoalsEnabled: true, OutputSchema: json.RawMessage(`{"type":"integer"}`)})
	if err != nil || outcome.State != session.Succeeded || ordinary != 2 || helpers == 0 || ledger.reads != 1 {
		t.Fatalf("outcome %+v err %v calls %d/%d reads%d", outcome, err, ordinary, helpers, ledger.reads)
	}
	// Context and its guide never become authored transcript content.
	for _, draft := range ledger.messages {
		for _, part := range draft.Parts {
			if strings.Contains(part.Text, "SECRET-OBJECTIVE") {
				t.Fatal("goal duplicated into history")
			}
		}
	}
}

func TestGoalContextFailsBeforeProviderAndCompactNeverLoadsIt(t *testing.T) {
	for _, mode := range []string{"mismatch", "read-error", "bounded", "compact"} {
		t.Run(mode, func(t *testing.T) {
			ledger := &goalLedger{compactionLedger: newCompactionLedger(compactionMessages(6, 2)), goal: session.GoalContext{GoalRef: session.GoalRef{ID: "goal", Revision: 1}, Spec: session.GoalSpec{Text: "objective", MaxContinuations: 2}}}
			turn := session.Turn{ID: "turn", SessionID: "owner", Goal: new(ledger.goal.GoalRef)}
			configuration := session.Configuration{GoalsEnabled: true}
			switch mode {
			case "mismatch":
				ledger.goal.Revision++
			case "read-error":
				ledger.err = errors.New("database failed")
			case "bounded":
				configuration.Instructions.Text = strings.Repeat("x", 8*session.MaxDocumentBytes)
			case "compact":
				turn.Kind = session.CompactInput
			}
			calls := 0
			r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				calls++
				if strings.Contains(request.Instructions, "goals.complete") {
					t.Fatal("compact inherited goal")
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
			}), ledger, ledger, nil, nil, nil, nil, ledger, nil)
			if err != nil {
				t.Fatal(err)
			}
			result, err := r.Run(t.Context(), turn, configuration)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "compact" {
				if ledger.reads != 0 {
					t.Fatal("compact loaded goal")
				}
			} else if result.State != session.Failed || calls != 0 {
				t.Fatal(result, calls)
			}
		})
	}
}
