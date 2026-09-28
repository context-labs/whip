package runner

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type titleStore struct {
	*store.Store
	afterSettle func(session.AutomaticTitleSettlement) error
}

func (s *titleStore) SettleAutomaticTitle(ctx context.Context, id session.ModelAttemptID, result session.ModelAttemptResult, draft *session.AutomaticTitleDraft) (session.AutomaticTitleSettlement, error) {
	settled, err := s.Store.SettleAutomaticTitle(ctx, id, result, draft)
	if err == nil && s.afterSettle != nil {
		err = s.afterSettle(settled)
	}
	return settled, err
}

func titleFixture(t *testing.T) (*titleStore, store.Claim, session.Session) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "title.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, _, ref, err := session.CanonicalDefinition(session.Builtins()[0])
	if err != nil {
		t.Fatal(err)
	}
	helper := session.ModelSelection{Provider: "captured", Name: "helper", Effort: "high", Temperature: new(0.25), TopP: new(0.75)}
	_, owner, err := db.CreateTree(t.Context(), store.CreateTree{
		Engine: session.Starlark, Definition: ref, WorkingDirectory: t.TempDir(),
		Defaults:  session.Configuration{Model: session.ModelSelection{Provider: "ordinary", Name: "chat"}},
		Overrides: session.ConfigPatch{AutomaticTitle: new(true), Compaction: &session.CompactionPolicy{Model: &helper}, Instructions: &session.Instructions{Text: "private ordinary instructions"}, Output: &session.OutputPolicy{Schema: json.RawMessage(`{"type":"number"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Admit(t.Context(), session.RequestIdentity{ClientID: "fixture", RequestID: "source"}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "  Build a résumé exporter\n with careful quoting.  "}}}); err != nil {
		t.Fatal(err)
	}
	initial, err := db.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Finish(t.Context(), initial.Turn.ID, session.Cancelled, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{AutomaticTitle: new(false), Model: &session.ModelSelection{Provider: "changed", Name: "later"}, Compaction: &session.CompactionPolicy{}}); err != nil {
		t.Fatal(err)
	}
	claim, err := db.Claim(t.Context(), owner.ID)
	if err != nil || claim.Turn.Kind != session.AutomaticTitleInputKind {
		t.Fatal("foreground cancellation lost independent pending title", err)
	}
	return &titleStore{Store: db}, claim, owner
}

func TestAutomaticTitleFrozenRequestDeadlineAndLostSettlementAcknowledgement(t *testing.T) {
	ledger, claim, owner := titleFixture(t)
	before, _ := ledger.History(t.Context(), owner.ID, 0, 100)
	captured := owner.Config.Compaction.Model.Clone()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writes, calls := 0, 0
	ledger.afterSettle = func(value session.AutomaticTitleSettlement) error {
		writes++
		if value.Candidate == nil || !value.Candidate.Applied {
			return errors.New("candidate and title did not commit together")
		}
		if writes == 1 {
			cancel()
			return errors.New("committed acknowledgement lost")
		}
		return nil
	}
	provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 20*time.Second || request.Purpose != session.AutomaticTitlePurpose || !request.Selection.Equal(captured) || request.OutputTokenLimit != nil || len(request.Tools) != 0 || len(request.Contents) != 0 || len(request.Messages) != 1 || request.Messages[0].Continuation != nil || request.Messages[0].Parts[0].Text != "Build a résumé exporter with careful quoting." || strings.Contains(request.Instructions, "private ordinary") || strings.Contains(request.Instructions, "Output contract") {
			return model.Prepared{}, errors.New("title request did not use isolated captured source and policy")
		}
		prepared, err := (model.Scripted{}).Prepare(ctx, request)
		prepared.MaxAttempts = 5
		prepared.Execute = func(ctx context.Context, emit func(model.Chunk)) (model.Response, error) {
			calls++
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 20*time.Second || emit != nil {
				return model.Response{}, errors.New("title escaped deadline or published preview")
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: "Résumé CSV exporter"}}, Continuation: &session.ModelContinuation{}, Usage: session.ModelUsage{Input: new(int64(20)), Output: new(int64(7))}, ReportedCostNanoUSD: new(int64(1200))}, nil
		}
		return prepared, err
	})
	preview := &helperPreview{}
	r := &Runner{provider: provider, attempts: ledger, maintenance: ledger, executor: forbiddenExecutor{}, progress: preview}
	outcome, err := r.Run(ctx, claim.Turn, claim.Configuration)
	if err != nil || outcome.State != session.Succeeded || calls != 1 || writes != 2 || preview.calls.Load() != 0 {
		t.Fatal("title repeated provider work on settlement retry", calls, writes, outcome, err)
	}
	attempts, err := ledger.ModelAttempts(t.Context(), claim.Turn.ID, "", 100)
	if err != nil || len(attempts) != 1 || attempts[0].Request.MaxOutputTokens != 4096 || attempts[0].Request.TimeoutMillis != 20000 || attempts[0].MessageID != nil || attempts[0].OperationID != nil || !attempts[0].Request.Model.Equal(captured) || attempts[0].CostNanoUSD == nil || *attempts[0].CostNanoUSD != 1200 {
		t.Fatal("title lost captured ceiling/selection/billing", attempts, err)
	}
	history, _ := ledger.History(t.Context(), owner.ID, 0, 100)
	if !reflect.DeepEqual(before, history) {
		t.Fatal("title changed conversation history")
	}
	tree, err := ledger.Tree(t.Context(), owner.TreeID)
	if err != nil || tree.Metadata.Title == nil || *tree.Metadata.Title != "Résumé CSV exporter" {
		t.Fatal("title was not selected", err)
	}
}

func TestAutomaticTitleSingleAttemptInvalidAndFailedOutputs(t *testing.T) {
	for _, mode := range []string{"retryable", "uncertain", "deadline", "blank", "multiline", "81-runes", "tool"} {
		t.Run(mode, func(t *testing.T) {
			ledger, claim, owner := titleFixture(t)
			before, _ := ledger.Tree(t.Context(), owner.TreeID)
			calls := 0
			provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
				prepared, err := (model.Scripted{}).Prepare(ctx, request)
				prepared.MaxAttempts = 5
				prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
					calls++
					response := model.Response{Parts: []session.Part{{Type: "text", Text: "valid title"}}, ReportedCostNanoUSD: new(int64(1200))}
					switch mode {
					case "retryable":
						return response, &model.CallError{Retryable: true, StatusCode: 503, Message: "known failure"}
					case "uncertain":
						return response, &model.CallError{Retryable: true, Uncertain: true, Message: "unknown result"}
					case "deadline":
						return response, context.DeadlineExceeded
					case "blank":
						response.Parts[0].Text = " "
					case "multiline":
						response.Parts[0].Text = "first\nsecond"
					case "81-runes":
						response.Parts[0].Text = strings.Repeat("猫", 81)
					case "tool":
						response.Parts = []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{}`)}}}
					}
					return response, nil
				}
				return prepared, err
			})
			r := &Runner{provider: provider, attempts: ledger, maintenance: ledger}
			outcome, err := r.Run(t.Context(), claim.Turn, claim.Configuration)
			if err != nil || outcome.State != session.Failed || calls != 1 {
				t.Fatal("failed naming repeated dispatch or succeeded", calls, outcome, err)
			}
			attempts, err := ledger.ModelAttempts(t.Context(), claim.Turn.ID, "", 100)
			if err != nil || len(attempts) != 1 || attempts[0].State == session.AttemptSucceeded || attempts[0].CostNanoUSD == nil || *attempts[0].CostNanoUSD != 1200 {
				t.Fatal("invalid title lost single attempt billing", attempts, err)
			}
			if _, err := ledger.AutomaticTitleResult(t.Context(), owner.TreeID, attempts[0].ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("failed title created a candidate", err)
			}
			after, _ := ledger.Tree(t.Context(), owner.TreeID)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed title changed selected metadata")
			}
			if _, err := ledger.Finish(t.Context(), claim.Turn.ID, outcome.State, outcome.Failure, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := ledger.Claim(t.Context(), owner.ID); !errors.Is(err, store.ErrNoWork) {
				t.Fatal("failed naming automatically rearmed", err)
			}
		})
	}
}
