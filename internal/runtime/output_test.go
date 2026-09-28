package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func configureOutput(t *testing.T, r *Runtime, owner session.Session, schema string) session.Session {
	t.Helper()
	policy := &session.OutputPolicy{}
	if schema != "" {
		policy.Schema = json.RawMessage(schema)
	}
	updated, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Output: policy})
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func TestStructuredOutputExecution(t *testing.T) {
	for _, test := range []struct {
		name, schema, want string
		responses          []string
	}{
		{"exact integer", `{"type":"integer"}`, "9007199254740993", []string{"9007199254740993"}},
		{"fenced object", `{"type":"object","required":["ok"],"properties":{"ok":{"const":true}}}`, `{"ok":true}`, []string{"```json\n{ \"ok\": true }\n```"}},
		{"null", `{"type":"null"}`, "null", []string{"null"}},
		{"corrected", `{"type":"integer"}`, "42", []string{"not JSON", "42"}},
		{"twice invalid", `{"type":"integer"}`, "", []string{"not JSON", `"still not an integer"`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			requests := make(chan model.Request, 3)
			provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				n := int(calls.Add(1)) - 1
				if n >= len(test.responses) {
					return model.Response{}, fmt.Errorf("unexpected provider call %d", n+1)
				}
				requests <- request
				return model.Response{Parts: []session.Part{{Type: "text", Text: test.responses[n]}}}, nil
			})
			r := openTest(t, t.TempDir(), provider)
			owner := configureOutput(t, r, createTest(t, r), test.schema)
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, owner.ID, "output")
			finished := waitTest(t, r, "output", terminal)
			value, err := r.TurnOutput(t.Context(), finished.Turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if finished.Turn.State != session.Failed || finished.Turn.Failure == nil || !strings.HasPrefix(*finished.Turn.Failure, "output_invalid") || value != nil {
					t.Fatalf("invalid output outcome=%+v output=%+v", finished.Turn, value)
				}
			} else if finished.Turn.State != session.Succeeded || value == nil || string(value.Value) != test.want {
				t.Fatalf("turn=%+v output=%+v want=%s", finished.Turn, value, test.want)
			}
			attempts, err := r.ModelAttempts(t.Context(), finished.Turn.ID, "", 100)
			if err != nil || len(attempts) != len(test.responses) || int(calls.Load()) != len(test.responses) {
				t.Fatalf("attempts=%+v calls=%d err=%v", attempts, calls.Load(), err)
			}
			history, err := r.History(t.Context(), owner.ID, 0, 100)
			if err != nil || len(history) != 1+len(test.responses) {
				t.Fatalf("history=%+v err=%v", history, err)
			}
			for i, response := range test.responses {
				if history[i+1].Role != session.Assistant || history[i+1].Parts[0].Text != response {
					t.Fatalf("raw candidate changed: %+v", history[i+1])
				}
			}
			if value != nil && (value.MessageID != history[len(history)-1].ID || value.TurnID != finished.Turn.ID) {
				t.Fatalf("projection lost source identity: %+v", value)
			}
			if len(test.responses) == 2 {
				<-requests
				corrected := <-requests
				if len(corrected.Messages) != 3 || corrected.Messages[1].Role != session.Assistant || corrected.Messages[1].Parts[0].Text != test.responses[0] || corrected.Messages[2].Role != session.System {
					t.Fatalf("correction context=%+v", corrected.Messages)
				}
			}
		})
	}
}

func TestStructuredOutputUsesTurnSnapshotAndReadsAfterRestart(t *testing.T) {
	provider := &observationProvider{started: make(chan observationCall)}
	path := t.TempDir()
	r := openTest(t, path, provider)
	owner := configureOutput(t, r, createTest(t, r), `{"type":"integer"}`)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, owner.ID, "captured")
	first := nextObservationCall(t, provider)
	configureOutput(t, r, owner, "")
	first.complete <- observationResult{response: model.Response{Parts: []session.Part{{Type: "text", Text: "not JSON"}}}}
	second := nextObservationCall(t, provider)
	second.complete <- observationResult{response: model.Response{Parts: []session.Part{{Type: "text", Text: "9007199254740993"}}}}
	finished := waitTest(t, r, "captured", terminal)
	if finished.Turn.State != session.Succeeded || provider.calls.Load() != 2 {
		t.Fatalf("captured schema was not enforced: %+v calls=%d", finished.Turn, provider.calls.Load())
	}
	submitTest(t, r, owner.ID, "cleared")
	third := nextObservationCall(t, provider)
	third.complete <- observationResult{response: model.Response{Parts: []session.Part{{Type: "text", Text: "free text after clearing"}}}}
	cleared := waitTest(t, r, "cleared", terminal)
	if cleared.Turn.State != session.Succeeded {
		t.Fatalf("cleared schema still enforced: %+v", cleared.Turn)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path, provider)
	for range 3 {
		value, err := r.TurnOutput(t.Context(), finished.Turn.ID)
		if err != nil || value == nil || string(value.Value) != "9007199254740993" {
			t.Fatalf("reopened output=%+v err=%v", value, err)
		}
		if value, err := r.TurnOutput(t.Context(), cleared.Turn.ID); err != nil || value != nil {
			t.Fatalf("cleared output=%+v err=%v", value, err)
		}
	}
	if provider.calls.Load() != 3 {
		t.Fatalf("reading output executed provider: calls=%d", provider.calls.Load())
	}
}

type outputSettlementBarrier struct {
	*store.Store
	committed chan session.TurnID
	release   chan struct{}
}

func (s *outputSettlementBarrier) SettleModelAttempt(ctx context.Context, id session.ModelAttemptID, result session.ModelAttemptResult, message *session.MessageDraft) (session.ModelAttempt, error) {
	value, err := s.Store.SettleModelAttempt(ctx, id, result, message)
	if err != nil {
		return value, err
	}
	select {
	case s.committed <- value.TurnID:
	case <-ctx.Done():
		return value, ctx.Err()
	}
	select {
	case <-s.release:
		return value, nil
	case <-ctx.Done():
		return value, ctx.Err()
	}
}

func TestStructuredOutputCancellationBeforeCorrectionDispatch(t *testing.T) {
	var calls atomic.Int32
	provider := providerFunc(func(context.Context, model.Request) (model.Response, error) {
		calls.Add(1)
		return model.Response{Parts: []session.Part{{Type: "text", Text: "invalid"}}}, nil
	})
	r := openTest(t, t.TempDir(), provider)
	owner := configureOutput(t, r, createTest(t, r), `{"type":"integer"}`)
	barrier := &outputSettlementBarrier{Store: r.store, committed: make(chan session.TurnID, 1), release: make(chan struct{}, 1)}
	var err error
	r.runner, err = runner.New(provider, r.store, barrier, r, r, r, r, r.store)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, owner.ID, "cancel-correction")
	select {
	case turn := <-barrier.committed:
		if _, err := r.CancelTurn(t.Context(), turn); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first candidate did not commit")
	}
	barrier.release <- struct{}{}
	finished := waitTest(t, r, "cancel-correction", terminal)
	if finished.Turn.State != session.Cancelled || calls.Load() != 1 {
		t.Fatalf("cancellation dispatched correction: turn=%+v calls=%d", finished.Turn, calls.Load())
	}
	attempts, err := r.ModelAttempts(t.Context(), finished.Turn.ID, "", 100)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts=%+v err=%v", attempts, err)
	}
	history, err := r.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 2 || history[1].Parts[0].Text != "invalid" {
		t.Fatalf("cancelled candidate disappeared: history=%+v err=%v", history, err)
	}
	if value, err := r.TurnOutput(t.Context(), finished.Turn.ID); err != nil || value != nil {
		t.Fatalf("cancelled turn exposed output=%+v err=%v", value, err)
	}
}

func TestStructuredOutputSQLSettlementRetryDoesNotReplayProvider(t *testing.T) {
	provider := &observationProvider{started: make(chan observationCall)}
	directory := t.TempDir()
	r := openTest(t, directory, provider)
	owner := configureOutput(t, r, createTest(t, r), `{"type":"integer"}`)
	settlements := &observationSettlements{Store: r.store, failed: make(chan struct{}, 1)}
	var err error
	r.runner, err = runner.New(provider, r.store, settlements, r, r, r, r, r.store)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(directory, "state.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER fail_output_settlement BEFORE INSERT ON messages WHEN NEW.role='assistant' BEGIN SELECT RAISE(ABORT,'injected settlement failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, owner.ID, "settlement")
	call := nextObservationCall(t, provider)
	call.complete <- observationResult{response: model.Response{Parts: []session.Part{{Type: "text", Text: "9007199254740993"}}}}
	select {
	case <-settlements.failed:
	case <-time.After(5 * time.Second):
		t.Fatal("settlement did not exercise SQL failure")
	}
	if _, err := db.ExecContext(t.Context(), "DROP TRIGGER fail_output_settlement"); err != nil {
		t.Fatal(err)
	}
	finished := waitTest(t, r, "settlement", terminal)
	value, err := r.TurnOutput(t.Context(), finished.Turn.ID)
	if err != nil || value == nil || string(value.Value) != "9007199254740993" || provider.calls.Load() != 1 || settlements.calls.Load() < 2 {
		t.Fatalf("output=%+v provider=%d settlement=%d err=%v", value, provider.calls.Load(), settlements.calls.Load(), err)
	}
	attempts, err := r.ModelAttempts(t.Context(), finished.Turn.ID, "", 100)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("settlement duplicated attempt: %+v err=%v", attempts, err)
	}
}

func TestStructuredOutputUncertainCorrectionDoesNotReplay(t *testing.T) {
	var calls atomic.Int32
	provider := providerFunc(func(context.Context, model.Request) (model.Response, error) {
		if calls.Add(1) == 1 {
			return model.Response{Parts: []session.Part{{Type: "text", Text: "not JSON"}}}, nil
		}
		return model.Response{Usage: session.ModelUsage{Input: new(int64(17))}}, &model.CallError{Uncertain: true, Message: "injected incomplete provider response"}
	})
	r := openTest(t, t.TempDir(), provider)
	owner := configureOutput(t, r, createTest(t, r), `{"type":"integer"}`)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, owner.ID, "uncertain-correction")
	finished := waitTest(t, r, "uncertain-correction", terminal)
	if finished.Turn.State != session.Failed || calls.Load() != 2 {
		t.Fatalf("provider failure replayed correction: turn=%+v calls=%d", finished.Turn, calls.Load())
	}
	attempts, err := r.ModelAttempts(t.Context(), finished.Turn.ID, "", 100)
	if err != nil || len(attempts) != 2 {
		t.Fatalf("attempts=%+v err=%v", attempts, err)
	}
	var uncertain *session.ModelAttempt
	for i := range attempts {
		if attempts[i].State == session.AttemptUncertain {
			uncertain = &attempts[i]
		}
	}
	if uncertain == nil || uncertain.Result == nil || uncertain.Result.Usage.Input == nil || *uncertain.Result.Usage.Input != 17 {
		t.Fatalf("uncertain correction lost accounting: %+v", attempts)
	}
	history, err := r.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 2 || history[1].Parts[0].Text != "not JSON" {
		t.Fatalf("failed correction changed transcript: %+v err=%v", history, err)
	}
	if value, err := r.TurnOutput(t.Context(), finished.Turn.ID); err != nil || value != nil {
		t.Fatalf("uncertain correction exposed output=%+v err=%v", value, err)
	}
}

func TestStructuredOutputCorrectionCannotExecuteTools(t *testing.T) {
	var calls atomic.Int32
	provider := providerFunc(func(context.Context, model.Request) (model.Response, error) {
		if calls.Add(1) == 1 {
			return model.Response{Parts: []session.Part{{Type: "text", Text: "not JSON"}}}, nil
		}
		return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "unexpected-tool", Name: "execute", Arguments: json.RawMessage(`{"code":"print(42)"}`)}}}}, nil
	})
	r := openTest(t, t.TempDir(), provider)
	owner := configureOutput(t, r, createTest(t, r), `{"type":"integer"}`)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, owner.ID, "tool-correction")
	finished := waitTest(t, r, "tool-correction", terminal)
	if finished.Turn.State != session.Failed || finished.Turn.Failure == nil || !strings.HasPrefix(*finished.Turn.Failure, "output_invalid") || calls.Load() != 2 {
		t.Fatalf("tool correction outcome=%+v calls=%d", finished.Turn, calls.Load())
	}
	cells, err := r.Cells(t.Context(), finished.Turn.ID, "", 100)
	if err != nil || len(cells) != 0 {
		t.Fatalf("correction executed a cell: %+v err=%v", cells, err)
	}
	history, err := r.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 4 || history[2].Parts[0].Call == nil || history[2].Parts[0].Call.ID != "unexpected-tool" || history[3].Parts[0].Result == nil || !history[3].Parts[0].Result.IsError {
		t.Fatalf("rejected tool call lost raw evidence or terminal reconciliation: %+v err=%v", history, err)
	}
	if value, err := r.TurnOutput(t.Context(), finished.Turn.ID); err != nil || value != nil {
		t.Fatalf("tool correction exposed output=%+v err=%v", value, err)
	}
}
