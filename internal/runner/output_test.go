package runner

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

type outputLedger struct {
	specs     []session.ModelAttemptSpec
	messages  map[session.MessageID]session.MessageDraft
	results   map[session.ModelAttemptID]session.ModelAttemptResult
	writes    map[session.ModelAttemptID]int
	ambiguous bool
}

func newOutputLedger() *outputLedger {
	return &outputLedger{messages: map[session.MessageID]session.MessageDraft{}, results: map[session.ModelAttemptID]session.ModelAttemptResult{}, writes: map[session.ModelAttemptID]int{}}
}

func (*outputLedger) History(context.Context, session.SessionID, int64, int) ([]session.Message, error) {
	return nil, nil
}

func (s *outputLedger) Continuations(_ context.Context, _ session.SessionID, ids []session.MessageID) (map[session.MessageID]session.ModelContinuation, error) {
	result := map[session.MessageID]session.ModelContinuation{}
	for _, id := range ids {
		if message := s.messages[id]; message.Continuation != nil {
			result[id] = *message.Continuation
		}
	}
	return result, nil
}

func (s *outputLedger) ReserveModelAttempt(_ context.Context, spec session.ModelAttemptSpec) (session.ModelAttempt, error) {
	s.specs = append(s.specs, spec)
	return session.ModelAttempt{ID: spec.ID, State: session.AttemptReserved}, nil
}

func (*outputLedger) DispatchModelAttempt(context.Context, session.ModelAttemptID) (bool, error) {
	return true, nil
}

func (s *outputLedger) SettleModelAttempt(_ context.Context, id session.ModelAttemptID, result session.ModelAttemptResult, message *session.MessageDraft) (session.ModelAttempt, error) {
	s.writes[id]++
	s.results[id] = result
	if message != nil {
		s.messages[message.ID] = *message
	}
	if s.ambiguous && s.writes[id] == 1 {
		return session.ModelAttempt{}, errors.New("injected lost SQL acknowledgement")
	}
	return session.ModelAttempt{ID: id, State: result.State, Result: &result}, nil
}

func TestOutputCorrectionFollowsCommittedResponseAndDoesNotRedispatch(t *testing.T) {
	ledger := newOutputLedger()
	ledger.ambiguous = true
	schema := json.RawMessage(`{"type":"object","properties":{"answer":{"type":"integer"}},"required":["answer"],"additionalProperties":false}`)
	invalid := []session.Part{{Type: "text", Text: `{"answer":"wrong"}`}}
	valid := []session.Part{{Type: "text", Text: `{"answer":9007199254740993}`}}
	calls := 0
	provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		calls++
		if !strings.Contains(request.Instructions, string(schema)) {
			return model.Response{}, errors.New("captured schema missing from instructions")
		}
		if calls == 1 {
			return model.Response{Parts: invalid}, nil
		}
		if calls != 2 {
			return model.Response{}, errors.New("unexpected provider redispatch")
		}
		if len(ledger.messages) != 1 || len(ledger.results) != 1 {
			return model.Response{}, errors.New("correction began before invalid evidence committed")
		}
		if len(request.Messages) != 2 || request.Messages[0].Role != session.Assistant || !reflect.DeepEqual(request.Messages[0].Parts, invalid) || request.Messages[1].Role != session.System || !strings.Contains(request.Messages[1].Parts[0].Text, "Correct it once") {
			return model.Response{}, errors.New("correction context did not follow raw invalid response")
		}
		return model.Response{Parts: valid}, nil
	})
	r, err := New(provider, ledger, ledger, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "turn"}, session.Configuration{OutputSchema: schema})
	if err != nil || outcome.State != session.Succeeded || calls != 2 || len(ledger.specs) != 2 || len(ledger.messages) != 2 {
		t.Fatalf("outcome=%+v err=%v calls=%d specs=%d messages=%d", outcome, err, calls, len(ledger.specs), len(ledger.messages))
	}
	for i, spec := range ledger.specs {
		if spec.Number != 1 || ledger.writes[spec.ID] != 2 || ledger.results[spec.ID].State != session.AttemptSucceeded {
			t.Fatalf("correction bypassed ordinary accounting: %+v writes=%d result=%+v", spec, ledger.writes[spec.ID], ledger.results[spec.ID])
		}
		if i > 0 && spec.LogicalID == ledger.specs[i-1].LogicalID {
			t.Fatal("correction reused provider-attempt identity")
		}
	}
	if !reflect.DeepEqual(ledger.messages["turn_model_1_answer"].Parts, invalid) || !reflect.DeepEqual(ledger.messages["turn_model_2_answer"].Parts, valid) {
		t.Fatal("correction overwrote committed raw output")
	}
}

func TestOutputSecondMismatchFailsAndCorrectionDoesNotLeakBetweenTurns(t *testing.T) {
	ledger := newOutputLedger()
	calls := 0
	r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		calls++
		if calls == 3 && len(request.Messages) != 0 {
			return model.Response{}, errors.New("previous turn correction leaked")
		}
		text := `42`
		if calls == 3 {
			text = `"valid"`
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: text}}}, nil
	}), ledger, ledger, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	config := session.Configuration{OutputSchema: json.RawMessage(`{"type":"string"}`)}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "invalid"}, config)
	if err != nil || outcome.State != session.Failed || outcome.Failure == nil || !strings.HasPrefix(*outcome.Failure, "output_invalid:") || calls != 2 || len(ledger.messages) != 2 {
		t.Fatalf("outcome=%+v err=%v calls=%d", outcome, err, calls)
	}
	outcome, err = r.Run(t.Context(), session.Turn{ID: "next"}, config)
	if err != nil || outcome.State != session.Succeeded || calls != 3 {
		t.Fatalf("next turn=%+v %v calls=%d", outcome, err, calls)
	}
}

func TestOutputCancellationDoesNotDispatchCorrection(t *testing.T) {
	ledger := newOutputLedger()
	ledger.ambiguous = true
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	r, err := New(providerFunc(func(context.Context, model.Request) (model.Response, error) {
		calls++
		cancel()
		return model.Response{Parts: []session.Part{{Type: "text", Text: `false`}}}, nil
	}), ledger, ledger, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Run(ctx, session.Turn{ID: "cancel"}, session.Configuration{OutputSchema: json.RawMessage(`{"type":"string"}`)})
	if !errors.Is(err, context.Canceled) || calls != 1 || len(ledger.specs) != 1 || len(ledger.messages) != 1 || ledger.writes[ledger.specs[0].ID] != 2 {
		t.Fatalf("cancelled invalid response=%v calls=%d specs=%d messages=%d", err, calls, len(ledger.specs), len(ledger.messages))
	}
}

func TestOutputContractsAllowToolsBeforeFinalButNeverDuringCorrection(t *testing.T) {
	for _, correctionCallsTool := range []bool{false, true} {
		t.Run(strconv.FormatBool(correctionCallsTool), func(t *testing.T) {
			ledger := newOutputLedger()
			calls := 0
			var tools []string
			provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				calls++
				if calls == 1 || (calls == 3 && correctionCallsTool) {
					return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: string(rune('a' + calls)), Name: "execute", Arguments: json.RawMessage(`{"code":"print(1)"}`)}}}}, nil
				}
				if calls == 2 {
					return model.Response{Parts: []session.Part{{Type: "text", Text: "not JSON"}}}, nil
				}
				if calls != 3 {
					return model.Response{}, errors.New("unexpected round")
				}
				notices := 0
				for _, message := range request.Messages {
					if message.Role == session.System {
						notices++
					}
				}
				if notices != 1 {
					return model.Response{}, errors.New("missing single correction")
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "null"}}}, nil
			})
			r, err := New(provider, ledger, ledger, nil, mailExecutor{events: &tools}, nil, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := r.Run(t.Context(), session.Turn{ID: "tools"}, session.Configuration{OutputSchema: json.RawMessage(`{"type":"null"}`)})
			want := session.Succeeded
			if correctionCallsTool {
				want = session.Failed
				if outcome.Failure == nil || !strings.HasPrefix(*outcome.Failure, "output_invalid:") || ledger.messages["tools_model_3_answer"].Parts[0].Call == nil {
					t.Fatalf("corrective call failure/evidence missing: %+v", outcome)
				}
			}
			if err != nil || outcome.State != want || calls != 3 || len(tools) != 1 || len(ledger.specs) != 3 {
				t.Fatalf("outcome=%+v err=%v calls=%d tools=%v", outcome, err, calls, tools)
			}
		})
	}
}

func TestOutputUnconfiguredOrClearedDoesNotAddPolicy(t *testing.T) {
	for _, schema := range []json.RawMessage{nil, json.RawMessage(`null`)} {
		ledger := newOutputLedger()
		calls := 0
		r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
			calls++
			if request.Instructions != "original" {
				return model.Response{}, errors.New("cleared schema left instructions")
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: "ordinary prose"}}}, nil
		}), ledger, ledger, nil, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := r.Run(t.Context(), session.Turn{ID: "unconfigured"}, session.Configuration{Instructions: session.Instructions{Text: "original"}, OutputSchema: schema})
		if err != nil || outcome.State != session.Succeeded || calls != 1 {
			t.Fatalf("outcome=%+v err=%v calls=%d", outcome, err, calls)
		}
	}
	notice := outputCorrection(errors.New(strings.Repeat("💥", 10000)))
	if len(notice) > 2048 || !utf8.ValidString(notice) || !strings.HasSuffix(notice, "any other content.") {
		t.Fatal("correction notice is not bounded UTF-8")
	}
}

func (*outputLedger) TurnGoal(context.Context, session.TurnID) (*session.GoalContext, error) {
	return nil, errors.New("goal context not configured")
}
