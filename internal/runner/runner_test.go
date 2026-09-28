package runner

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"
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
	}), transcript, transcript, nil, nil, nil, nil, nil)
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
	}), transcript, transcript, nil, nil, nil, nil, nil)
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

func TestAttemptElapsedEvidenceExcludesSQLRetriesAndSurvivesFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		state session.ModelAttemptState
	}{
		{name: "success", state: session.AttemptSucceeded},
		{name: "failure", err: errors.New("provider rejected request"), state: session.AttemptFailed},
		{name: "cancelled", err: context.Canceled, state: session.AttemptUncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transcript := &flakyTranscript{}
				calls := 0
				r, err := New(providerFunc(func(context.Context, model.Request) (model.Response, error) {
					calls++
					time.Sleep(1500 * time.Microsecond)
					return model.Response{Parts: []session.Part{{Type: "text", Text: "completed"}}, Usage: session.ModelUsage{Input: new(int64(99))}}, tc.err
				}), transcript, transcript, nil, nil, nil, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				started := time.Now()
				if _, err := r.Run(t.Context(), session.Turn{ID: "turn"}, session.Configuration{}); err != nil {
					t.Fatal(err)
				}
				if calls != 1 || transcript.calls != 2 || time.Since(started) < 20*time.Millisecond {
					t.Fatal("fixture did not exercise a SQL-only settlement retry")
				}
				result := transcript.result
				if result.State != tc.state || result.ElapsedMillis == nil || *result.ElapsedMillis != 2 || result.Usage.Input == nil || *result.Usage.Input != 99 {
					t.Fatalf("execution evidence was clipped, lost or included settlement time: %+v", result)
				}
			})
		})
	}
}

type instructionExecutor struct {
	*compactionExecutor
	resolve func(context.Context, session.Turn, session.Instructions) (string, error)
}

func (e instructionExecutor) Instructions(ctx context.Context, turn session.Turn, policy session.Instructions) (string, error) {
	return e.resolve(ctx, turn, policy)
}

func TestResolvedInstructionsStayFrozenThroughEffectsCorrectionAndReplan(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(6, 2))
	turn := session.Turn{ID: "current", SessionID: "owner", Kind: session.PromptInput, ConfigRevision: 7}
	policy := session.Instructions{Text: "configured instruction", ProjectFiles: []string{"AGENTS.md"}, DiscoverSkills: true}
	schema := json.RawMessage(`{"type":"string"}`)
	base := policy.Text + "\nproject rules v1\nskill catalog v1\nengine instructions"
	want := base + outputInstructions(schema)
	instructions, ordinary, helpers := 0, 0, 0
	effects := &compactionExecutor{ledger: ledger}
	executor := instructionExecutor{
		compactionExecutor: effects,
		resolve: func(_ context.Context, actual session.Turn, captured session.Instructions) (string, error) {
			instructions++
			if !reflect.DeepEqual(actual, turn) || !reflect.DeepEqual(captured, policy) {
				return "", errors.New("instruction boundary lost captured turn or policy")
			}
			return base, nil
		},
	}
	provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		if request.Purpose == "compaction" {
			helpers++
			if request.Instructions != compactionInstructions || instructions != 1 {
				return model.Response{}, errors.New("context helper refreshed or inherited ordinary instructions")
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
		}
		ordinary++
		if request.Instructions != want || instructions != 1 || len(request.Tools) != 1 {
			return model.Response{}, errors.New("resolved base was duplicated, refreshed or lost")
		}
		// A source changing after the first dispatch cannot affect this turn.
		base = "project rules v2 and changed skill catalog"
		switch ordinary {
		case 1:
			return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "cell", Name: "execute", Arguments: json.RawMessage(`{}`)}}}}, nil
		case 2:
			if effects.calls != 1 {
				return model.Response{}, errors.New("tool effect did not precede the next request")
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: "42"}}}, nil
		case 3, 4:
			last := request.Messages[len(request.Messages)-1]
			if last.Role != session.System || !strings.Contains(last.Parts[0].Text, "Correct it once") {
				return model.Response{}, errors.New("corrective notice was lost")
			}
			if ordinary == 3 {
				return model.Response{}, &model.CallError{ContextLimit: true, Message: "context rejected"}
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: `"done"`}}}, nil
		default:
			return model.Response{}, errors.New("unexpected ordinary replay")
		}
	})
	r, err := New(provider, ledger, ledger, nil, executor, nil, nil, ledger)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), turn, session.Configuration{Instructions: policy, OutputSchema: schema})
	if err != nil || outcome.State != session.Succeeded || instructions != 1 || ordinary != 4 || helpers != 1 || effects.calls != 1 || len(ledger.messages) != 3 {
		t.Fatalf("outcome=%+v err=%v instructions=%d ordinary=%d helpers=%d effects=%d", outcome, err, instructions, ordinary, helpers, effects.calls)
	}
}

func TestInstructionFailurePreventsProviderDispatch(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(6, 2))
	instructions, calls := 0, 0
	executor := instructionExecutor{compactionExecutor: &compactionExecutor{ledger: ledger}, resolve: func(context.Context, session.Turn, session.Instructions) (string, error) {
		instructions++
		return "", errors.New("project instructions unavailable")
	}}
	provider := providerFunc(func(context.Context, model.Request) (model.Response, error) {
		calls++
		return model.Response{Parts: []session.Part{{Type: "text", Text: "must not run"}}}, nil
	})
	r, err := New(provider, ledger, ledger, nil, executor, nil, nil, ledger)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Failed || instructions != 1 || calls != 0 || len(ledger.specs) != 0 {
		t.Fatalf("outcome=%+v err=%v instructions=%d calls=%d attempts=%d", outcome, err, instructions, calls, len(ledger.specs))
	}
}
