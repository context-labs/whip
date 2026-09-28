package runner

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type formulationStore struct {
	*store.Store
	afterSettle func(session.GoalFormulationSettlement) error
}

func (s *formulationStore) SettleGoalFormulation(ctx context.Context, id session.ModelAttemptID, result session.ModelAttemptResult, draft *session.GoalFormulationDraft) (session.GoalFormulationSettlement, error) {
	settled, err := s.Store.SettleGoalFormulation(ctx, id, result, draft)
	if err == nil && s.afterSettle != nil {
		err = s.afterSettle(settled)
	}
	return settled, err
}

func formulationFixture(t *testing.T, expected *session.GoalRef) (*formulationStore, store.Claim) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "formulation.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	_, _, definition, err := session.CanonicalDefinition(session.Builtins()[0])
	if err != nil {
		t.Fatal(err)
	}
	selection := session.ModelSelection{Provider: "scripted", Name: "scripted", Effort: "high", Temperature: new(0.2), TopP: new(0.8)}
	_, owner, err := db.CreateTree(t.Context(), store.CreateTree{
		Engine: session.Starlark, Definition: definition, WorkingDirectory: t.TempDir(),
		Defaults: session.Configuration{Model: selection},
		Overrides: session.ConfigPatch{
			Instructions: &session.Instructions{Text: "private session instructions"},
			Compaction:   &session.CompactionPolicy{Model: &session.ModelSelection{Provider: "different", Name: "helper"}},
			Output:       &session.OutputPolicy{Schema: json.RawMessage(`{"type":"number"}`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "history"}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "Build a résumé exporter; preserve Unicode and CSV quoting."}}}); err != nil {
		t.Fatal(err)
	}
	historyTurn, err := db.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppendMessage(t.Context(), historyTurn.Turn.ID, session.MessageDraft{ID: "answer", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "I will preserve both requirements."}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdmitGoalFormulation(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "formulate"}, owner.ID, session.GoalFormulationRequest{GoalID: "formulated", Expected: expected, Start: true, MaxContinuations: new(int64(0)), TailMessages: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppendMessage(t.Context(), historyTurn.Turn.ID, session.MessageDraft{ID: "later", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "after the frozen source window"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Finish(t.Context(), historyTurn.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	claim, err := db.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &formulationStore{Store: db}, claim
}

func TestGoalFormulationFrozenSourceSamplingAndAtomicLostAck(t *testing.T) {
	ledger, claim := formulationFixture(t, nil)
	before, err := ledger.History(t.Context(), claim.Turn.SessionID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ledger.Session(t.Context(), claim.Turn.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "changed", Name: "later"}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writes := 0
	ledger.afterSettle = func(result session.GoalFormulationSettlement) error {
		writes++
		if result.Candidate == nil || !result.Accepted {
			return errors.New("candidate missing from atomic settlement")
		}
		if writes == 1 {
			cancel() // The provider has completed and candidate+initial input committed.
			return errors.New("committed candidate acknowledgement lost")
		}
		return nil
	}
	capturedModel := claim.Configuration.Model.Clone()
	calls := 0
	provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
		calls++
		if request.Purpose != session.GoalFormulationPurpose || !request.Selection.Equal(capturedModel) || len(request.Tools) != 0 || len(request.Contents) != 0 || request.OutputTokenLimit != nil || strings.Contains(request.Instructions, "private session instructions") || strings.Contains(request.Instructions, "Output contract") {
			return model.Prepared{}, errors.New("formulation inherited ordinary configuration")
		}
		var data strings.Builder
		for _, message := range request.Messages {
			if message.Role != session.User || message.ID != "" || message.Continuation != nil || len(message.Parts) != 1 || message.Parts[0].Type != "text" {
				return model.Prepared{}, errors.New("formulation interpreted historical execution")
			}
			data.WriteString(message.Parts[0].Text)
		}
		var sources []session.Message
		if err := json.Unmarshal([]byte(data.String()), &sources); err != nil || !reflect.DeepEqual(sources, before[:2]) {
			return model.Prepared{}, errors.New("formulation did not use the exact frozen raw tail")
		}
		*claim.Configuration.Model.Temperature = 0.9
		if !request.Selection.Equal(capturedModel) {
			return model.Prepared{}, errors.New("captured sampling was aliased")
		}
		prepared, err := (model.Scripted{}).Prepare(ctx, request)
		prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
			return model.Response{Parts: []session.Part{{Type: "text", Text: "Deliver a résumé CSV exporter preserving Unicode and quoting."}}, Continuation: &session.ModelContinuation{}, Usage: session.ModelUsage{Input: new(int64(20)), Output: new(int64(10))}}, nil
		}
		return prepared, err
	})
	preview := &helperPreview{}
	r := &Runner{provider: provider, attempts: ledger, maintenance: ledger, progress: preview, executor: forbiddenExecutor{}}
	outcome, err := r.Run(ctx, claim.Turn, claim.Configuration)
	if err != nil || outcome.State != session.Succeeded || calls != 1 || writes != 2 || preview.calls.Load() != 0 {
		t.Fatalf("outcome=%+v calls=%d writes=%d previews=%d err=%v", outcome, calls, writes, preview.calls.Load(), err)
	}
	attempts, err := ledger.ModelAttempts(t.Context(), claim.Turn.ID, "", 100)
	if err != nil || len(attempts) != 1 || attempts[0].State != session.AttemptSucceeded || attempts[0].MessageID != nil || attempts[0].OperationID != nil || !attempts[0].Request.Model.Equal(capturedModel) {
		t.Fatalf("attempt=%+v err=%v", attempts, err)
	}
	candidate, err := ledger.GoalFormulation(t.Context(), claim.Turn.SessionID, attempts[0].ID)
	if err != nil || candidate.Rejection != nil || candidate.ThroughSequence != 2 {
		t.Fatalf("candidate=%+v err=%v", candidate, err)
	}
	history, err := ledger.History(t.Context(), claim.Turn.SessionID, 0, 100)
	if err != nil || !reflect.DeepEqual(before, history) {
		t.Fatalf("formulation authored history: %+v %v", history, err)
	}
	if _, err := ledger.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	turn, err := ledger.Turn(t.Context(), claim.Turn.ID)
	if err != nil || turn.State != session.Interrupted {
		t.Fatalf("helper outcome=%+v %v", turn, err)
	}
	goal, err := ledger.CurrentGoal(t.Context(), claim.Turn.SessionID)
	if err != nil || goal == nil || goal.ID != "formulated" || goal.State != session.GoalArmed || goal.OriginFormulationAttemptID == nil || *goal.OriginFormulationAttemptID != attempts[0].ID {
		t.Fatalf("accepted goal was lost to interrupted helper: %+v %v", goal, err)
	}
	next, err := ledger.Claim(t.Context(), claim.Turn.SessionID)
	if err != nil || next.Input.Goal == nil || next.Input.Goal.ID != goal.ID || next.Turn.Kind != session.PromptInput {
		t.Fatalf("accepted initial input was lost: %+v %v", next, err)
	}
	if output, err := ledger.TurnOutput(t.Context(), claim.Turn.ID); err != nil || output != nil {
		t.Fatalf("maintenance produced ordinary output: %+v %v", output, err)
	}
}

func TestGoalFormulationSemanticRejectionRetainsCandidate(t *testing.T) {
	ledger, claim := formulationFixture(t, &session.GoalRef{ID: "unselected", Revision: 1})
	var calls atomic.Int64
	provider := providerFunc(func(context.Context, model.Request) (model.Response, error) {
		calls.Add(1)
		return model.Response{Parts: []session.Part{{Type: "text", Text: "Build the requested exporter."}}}, nil
	})
	r := &Runner{provider: provider, attempts: ledger, maintenance: ledger}
	outcome, err := r.Run(t.Context(), claim.Turn, claim.Configuration)
	if err != nil || outcome.State != session.Failed || outcome.Failure == nil || calls.Load() != 1 {
		t.Fatalf("activation rejection=%+v calls=%d err=%v", outcome, calls.Load(), err)
	}
	attempts, err := ledger.ModelAttempts(t.Context(), claim.Turn.ID, "", 100)
	if err != nil || len(attempts) != 1 || attempts[0].State != session.AttemptSucceeded {
		t.Fatalf("rejection lost provider evidence: %+v %v", attempts, err)
	}
	candidate, err := ledger.GoalFormulation(t.Context(), claim.Turn.SessionID, attempts[0].ID)
	if err != nil || candidate.Text == "" || candidate.Rejection == nil {
		t.Fatalf("rejection lost candidate: %+v %v", candidate, err)
	}
	if goal, err := ledger.CurrentGoal(t.Context(), claim.Turn.SessionID); err != nil || goal != nil {
		t.Fatalf("rejected candidate activated: %+v %v", goal, err)
	}
	if _, err := ledger.Finish(t.Context(), claim.Turn.ID, outcome.State, outcome.Failure, nil); err != nil {
		t.Fatal(err)
	}
}

func TestGoalFormulationProviderOutcomesAndExplicitRetry(t *testing.T) {
	for _, mode := range []string{"retry", "uncertain", "cancel", "blank", "tool", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			ledger, claim := formulationFixture(t, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
				prepared, err := (model.Scripted{}).Prepare(ctx, request)
				prepared.MaxAttempts = 2
				prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
					calls++
					switch mode {
					case "retry":
						if calls == 1 {
							return model.Response{}, &model.CallError{Retryable: true, Message: "known rejection"}
						}
					case "uncertain":
						return model.Response{}, &model.CallError{Retryable: true, Uncertain: true, Message: "connection lost"}
					case "cancel":
						cancel()
						return model.Response{}, ctx.Err()
					case "blank":
						return model.Response{Parts: []session.Part{{Type: "text", Text: " \n "}}}, nil
					case "tool":
						return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{}`)}}}}, nil
					case "oversized":
						return model.Response{Parts: []session.Part{{Type: "text", Text: strings.Repeat("x", session.MaxDocumentBytes+1)}}}, nil
					}
					return model.Response{Parts: []session.Part{{Type: "text", Text: "Build the requested exporter."}}}, nil
				}
				return prepared, err
			})
			r := &Runner{provider: provider, attempts: ledger, maintenance: ledger}
			outcome, err := r.Run(ctx, claim.Turn, claim.Configuration)
			wantCalls, wantState := 1, session.AttemptFailed
			if mode == "retry" {
				wantCalls, wantState = 2, session.AttemptSucceeded
				if err != nil || outcome.State != session.Succeeded {
					t.Fatalf("retry failed: %+v %v", outcome, err)
				}
			} else if mode == "cancel" {
				wantState = session.AttemptUncertain
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel lost: %+v %v", outcome, err)
				}
			} else if err != nil || outcome.State != session.Failed {
				t.Fatalf("invalid formulation succeeded: %+v %v", outcome, err)
			}
			if mode == "uncertain" {
				wantState = session.AttemptUncertain
			}
			attempts, err := ledger.ModelAttempts(t.Context(), claim.Turn.ID, "", 100)
			if err != nil || len(attempts) != wantCalls || calls != wantCalls || attempts[wantCalls-1].State != wantState {
				t.Fatalf("attempts=%+v calls=%d err=%v", attempts, calls, err)
			}
			if mode != "retry" {
				if _, err := ledger.GoalFormulation(t.Context(), claim.Turn.SessionID, attempts[0].ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("failed provider invented candidate: %v", err)
				}
			}
		})
	}
}

func TestGoalFormulationQuotesPartialToolBatchesAndBoundedUnicodeChunks(t *testing.T) {
	messages := []session.Message{
		{ID: "tool", SessionID: "owner", TurnID: "old", Sequence: 10, Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "old-call", Output: strings.Repeat("界", 40000)}}}},
		{ID: "call", SessionID: "owner", TurnID: "old", Sequence: 11, Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "later-call", Name: "execute", Arguments: json.RawMessage(`{"code":"side effect"}`)}}}},
	}
	request, err := formulationRequest(session.Turn{ID: "turn", SessionID: "owner"}, session.ModelSelection{}, messages)
	if err != nil || len(request.Messages) < 2 {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	var source strings.Builder
	for _, message := range request.Messages {
		if message.Role != session.User || message.Continuation != nil || session.ValidateMessage(message.Role, message.Parts) != nil {
			t.Fatal("quoted source escaped ordinary message bounds")
		}
		source.WriteString(message.Parts[0].Text)
	}
	raw, err := json.Marshal(messages)
	if err != nil || source.String() != string(raw) {
		t.Fatal("chunking omitted or altered raw source")
	}
	messages[0].Parts[0].Result.Output = strings.Repeat("x", maxContextBytes)
	if _, err := formulationRequest(session.Turn{}, session.ModelSelection{}, messages); !errors.Is(err, errContextLimit) {
		t.Fatalf("oversized source did not fail before provider: %v", err)
	}
}
