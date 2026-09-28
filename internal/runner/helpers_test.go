package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type helperStore struct {
	*store.Store
	settle func(session.ModelAttemptID, session.ModelAttemptResult) error
}

func (s *helperStore) SettleModelAttempt(ctx context.Context, id session.ModelAttemptID, result session.ModelAttemptResult, message *session.MessageDraft) (session.ModelAttempt, error) {
	value, err := s.Store.SettleModelAttempt(ctx, id, result, message)
	if err == nil && s.settle != nil {
		err = s.settle(id, result)
	}
	return value, err
}

func helperFixture(t *testing.T, prompts []string, outputCap *int64) (*helperStore, ModelHelperRequest) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "helper.db"))
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
	selection := session.ModelSelection{Provider: "scripted", Name: "scripted", Effort: "high"}
	_, owner, err := db.CreateTree(t.Context(), store.CreateTree{Engine: session.Starlark, Definition: definition, WorkingDirectory: t.TempDir(), Defaults: session.Configuration{Model: selection}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "input"}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "private history"}}}); err != nil {
		t.Fatal(err)
	}
	claim, err := db.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	message, err := db.AppendMessage(t.Context(), claim.Turn.ID, session.MessageDraft{ID: "call", Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "execute", Name: "execute", Arguments: json.RawMessage(`{"code":"models.batch(...)"}`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	cell, dispatched, err := db.BeginCell(t.Context(), session.CellSpec{ID: "cell", TurnID: claim.Turn.ID, CallMessageID: message.ID, CallID: "execute"})
	if err != nil || !dispatched {
		t.Fatalf("begin cell: %v %v", dispatched, err)
	}
	args := map[string]any{"prompts": prompts}
	if outputCap != nil {
		args["max_tokens"] = *outputCap
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	op, err := db.AdmitOperation(t.Context(), session.OperationSpec{ID: "helper", CellID: cell.ID, RequestID: "helper", Capability: "models.batch", Resource: string(owner.TreeID), Arguments: raw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	if allowed, err := db.DispatchOperation(t.Context(), op.ID); err != nil || !allowed {
		t.Fatalf("dispatch: %v %v", allowed, err)
	}
	return &helperStore{Store: db}, ModelHelperRequest{Turn: claim.Turn, Model: selection, OperationID: op.ID, Prompts: prompts, MaxTokens: outputCap}
}

func TestModelHelpersCapturedStatelessAccountingAndLostAcknowledgement(t *testing.T) {
	ledger, request := helperFixture(t, []string{"one", "two"}, new(int64(77)))
	var mu sync.Mutex
	writes := map[session.ModelAttemptID]int{}
	ledger.settle = func(id session.ModelAttemptID, _ session.ModelAttemptResult) error {
		mu.Lock()
		defer mu.Unlock()
		writes[id]++
		if writes[id] == 1 {
			return errors.New("committed SQL acknowledgement lost")
		}
		return nil
	}
	var calls atomic.Int64
	provider := preparedProvider(func(ctx context.Context, value model.Request) (model.Prepared, error) {
		if value.Purpose != session.ModelHelperPurpose || value.Selection != request.Model || value.OutputTokenLimit == nil || *value.OutputTokenLimit != 77 || value.SessionID != request.Turn.SessionID || value.TurnID != request.Turn.ID {
			return model.Prepared{}, errors.New("captured execution identity changed")
		}
		if value.Instructions != "" || len(value.Tools) != 0 || len(value.Contents) != 0 || len(value.Messages) != 1 || value.Messages[0].ID != "" || value.Messages[0].Continuation != nil || value.Messages[0].Role != session.User || len(value.Messages[0].Parts) != 1 {
			return model.Prepared{}, errors.New("stateless request inherited conversation state")
		}
		prepared, err := (model.Scripted{}).Prepare(ctx, value)
		prepared.Execute = func(_ context.Context, emit func(model.Chunk)) (model.Response, error) {
			calls.Add(1)
			if emit != nil {
				return model.Response{}, errors.New("helper acquired a preview")
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: value.Messages[0].Parts[0].Text}}, Usage: session.ModelUsage{Input: new(int64(2)), Output: new(int64(1))}, Continuation: &session.ModelContinuation{}}, nil
		}
		return prepared, err
	})
	preview := &helperPreview{}
	r := &Runner{provider: provider, attempts: ledger, progress: preview} // Nil transcript/executor/mail/compaction makes accidental use observable.
	results, err := r.CallModels(t.Context(), request, nil)
	if err != nil || len(results) != 2 || results[0].Text != "one" || results[1].Text != "two" || results[0].Failure != nil || results[1].Failure != nil || calls.Load() != 2 {
		t.Fatalf("results=%+v calls=%d err=%v", results, calls.Load(), err)
	}
	attempts, err := ledger.ModelAttempts(t.Context(), request.Turn.ID, "", 100)
	if err != nil || len(attempts) != 2 {
		t.Fatalf("attempts=%+v err=%v", attempts, err)
	}
	for _, attempt := range attempts {
		if attempt.State != session.AttemptSucceeded || attempt.MessageID != nil || attempt.OperationID == nil || *attempt.OperationID != request.OperationID || attempt.BatchIndex == nil || attempt.Request.MaxOutputTokens != 77 || attempt.Result.ElapsedMillis == nil || writes[attempt.ID] != 2 {
			t.Fatalf("helper accounting changed: %+v writes=%v", attempt, writes)
		}
		logical, _ := session.ModelHelperLogicalID(request.OperationID, *attempt.BatchIndex)
		if results[*attempt.BatchIndex].AttemptID != attempt.ID {
			t.Fatal("returned output is not linked to its settled attempt")
		}
		if attempt.LogicalID != logical {
			t.Fatal("noncanonical helper identity")
		}
	}
	history, err := ledger.History(t.Context(), request.Turn.SessionID, 0, 100)
	if err != nil || len(history) != 2 || preview.calls.Load() != 0 {
		t.Fatalf("helper authored history: %+v %v", history, err)
	}
	if _, err := ledger.SettleOperation(t.Context(), request.OperationID, session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(`[]`)}); err != nil {
		t.Fatal(err)
	}
	_, err = r.CallModels(t.Context(), request, nil)
	if _, ok := errors.AsType[*AccountingError](err); !ok || calls.Load() != 2 {
		t.Fatalf("already settled helper replayed: calls=%d err=%v", calls.Load(), err)
	}
}

func TestModelHelpersReverseCompletionAndFourWorkerBound(t *testing.T) {
	prompts := make([]string, 8)
	release := make([]chan struct{}, len(prompts))
	for i := range prompts {
		prompts[i] = strconv.Itoa(i)
		release[i] = make(chan struct{})
	}
	ledger, request := helperFixture(t, prompts, nil)
	started := make(chan int, len(prompts))
	finished := make(chan int, len(prompts))
	var active, maximum atomic.Int64
	provider := providerFunc(func(ctx context.Context, value model.Request) (model.Response, error) {
		index, _ := strconv.Atoi(value.Messages[0].Parts[0].Text)
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
		}
		started <- index
		select {
		case <-ctx.Done():
			return model.Response{}, ctx.Err()
		case <-release[index]:
		}
		finished <- index
		return model.Response{Parts: []session.Part{{Type: "text", Text: strconv.Itoa(index)}}}, nil
	})
	r := &Runner{provider: provider, attempts: ledger}
	type answer struct {
		results []ModelResult
		err     error
	}
	done := make(chan answer, 1)
	go func() {
		result, err := r.CallModels(t.Context(), request, nil)
		done <- answer{result, err}
	}()
	for range 4 {
		receiveHelper(t, started)
	}
	// Keep three initial requests blocked while each freed slot starts the next
	// item; a scheduler that launches more than four would exceed maximum.
	for _, index := range []int{3, 4, 5, 6, 7, 2, 1, 0} {
		close(release[index])
		if got := receiveHelper(t, finished); got != index {
			t.Fatalf("completion order: %d != %d", got, index)
		}
		if index >= 3 && index < 7 {
			if got := receiveHelper(t, started); got != index+1 {
				t.Fatalf("next item: %d", got)
			}
		}
	}
	result := receiveHelper(t, done)
	if result.err != nil || maximum.Load() != 4 || active.Load() != 0 {
		t.Fatalf("batch=%+v active=%d maximum=%d", result, active.Load(), maximum.Load())
	}
	for i, item := range result.results {
		if item.Failure != nil || item.Text != prompts[i] {
			t.Fatalf("unstable result order: %+v", result.results)
		}
	}
}

func receiveHelper[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(10 * time.Second):
		t.Fatal("helper barrier timed out")
		var zero T
		return zero
	}
}

func TestModelHelpersHTTPRetryUncertaintyAndRecovery(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(strconv.FormatBool(uncertain), func(t *testing.T) {
			ledger, request := helperFixture(t, []string{"one"}, new(int64(50)))
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if uncertain {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
					return
				}
				if n == 1 {
					w.WriteHeader(http.StatusTooManyRequests)
					fmt.Fprint(w, `{"error":{"message":"retry"}}`)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`)
			}))
			defer server.Close()
			request.Model = session.ModelSelection{Provider: "openai", Name: "example"}
			provider := model.OpenAI{Resolve: func(context.Context, session.ModelSelection) (model.Route, error) {
				return model.Route{Kind: "openai-chat", URL: server.URL, Credential: "test", MaxOutputTokens: 100, MaxAttempts: 3, TimeoutMillis: 2000}, nil
			}}
			r := &Runner{provider: provider, attempts: ledger}
			results, err := r.CallModels(t.Context(), request, nil)
			if err != nil || len(results) != 1 {
				t.Fatalf("result=%+v err=%v", results, err)
			}
			wantCalls, wantState := int64(2), session.AttemptSucceeded
			if uncertain {
				wantCalls, wantState = 1, session.AttemptUncertain
				if results[0].Failure == nil || results[0].Text != "" {
					t.Fatal("uncertain partial result published")
				}
			} else if results[0].Text != "done" || results[0].Failure != nil {
				t.Fatalf("retry result=%+v", results)
			}
			attempts, err := ledger.ModelAttempts(t.Context(), request.Turn.ID, "", 100)
			if err != nil || int64(len(attempts)) != wantCalls || calls.Load() != wantCalls || attempts[len(attempts)-1].State != wantState {
				t.Fatalf("attempts=%+v calls=%d err=%v", attempts, calls.Load(), err)
			}
			if _, err := ledger.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			op, err := ledger.Operation(t.Context(), request.OperationID)
			if err != nil || op.State != session.OperationUncertain || calls.Load() != wantCalls {
				t.Fatalf("recovery fabricated aggregate/replayed provider: %+v %v", op, err)
			}
		})
	}
}

// A mutex ledger isolates cancellation/settlement scheduling from real time;
// the tests above verify the same paths against the actual SQLite constraints.
type helperMemoryLedger struct {
	mu          sync.Mutex
	specs       []session.ModelAttemptSpec
	results     map[session.ModelAttemptID]session.ModelAttemptResult
	reserveErr  error
	dispatchErr error
	settle      func(session.ModelAttemptID, session.ModelAttemptResult) error
}

func (s *helperMemoryLedger) ReserveModelAttempt(_ context.Context, spec session.ModelAttemptSpec) (session.ModelAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reserveErr != nil {
		return session.ModelAttempt{}, s.reserveErr
	}
	s.specs = append(s.specs, spec)
	return session.ModelAttempt{ID: spec.ID, State: session.AttemptReserved}, nil
}

func (s *helperMemoryLedger) DispatchModelAttempt(context.Context, session.ModelAttemptID) (bool, error) {
	return s.dispatchErr == nil, s.dispatchErr
}

func (s *helperMemoryLedger) SettleModelAttempt(_ context.Context, id session.ModelAttemptID, result session.ModelAttemptResult, message *session.MessageDraft) (session.ModelAttempt, error) {
	if message != nil {
		return session.ModelAttempt{}, fmt.Errorf("%w: helper authored transcript", session.ErrInvalid)
	}
	if s.settle != nil {
		if err := s.settle(id, result); err != nil {
			return session.ModelAttempt{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.results == nil {
		s.results = map[session.ModelAttemptID]session.ModelAttemptResult{}
	}
	s.results[id] = result
	return session.ModelAttempt{ID: id, Result: &result, State: result.State}, nil
}

func helperTestRequest(prompts ...string) ModelHelperRequest {
	return ModelHelperRequest{Turn: session.Turn{ID: "turn", SessionID: "owner"}, Model: session.ModelSelection{Provider: "scripted", Name: "scripted"}, OperationID: "helper", Prompts: prompts}
}

func TestModelHelpersConfirmedRefusalIsOrdinaryButMixedErrorIsFatal(t *testing.T) {
	sqlFailure := errors.New("rollback failed")
	for _, test := range []struct {
		name          string
		reserve, send error
		fatal         bool
	}{
		{name: "reserve budget", reserve: store.ErrLimit},
		{name: "dispatch budget", send: store.ErrLimit},
		{name: "stopped", reserve: store.ErrStopped},
		{name: "mixed", reserve: errors.Join(store.ErrLimit, sqlFailure), fatal: true},
		{name: "conflicting identity", reserve: store.ErrConflict, fatal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ledger := &helperMemoryLedger{reserveErr: test.reserve, dispatchErr: test.send}
			var calls int
			r := &Runner{attempts: ledger, provider: providerFunc(func(context.Context, model.Request) (model.Response, error) {
				calls++
				return model.Response{}, errors.New("must not execute")
			})}
			results, err := r.CallModels(t.Context(), helperTestRequest("one"), helperRefused)
			_, accounting := errors.AsType[*AccountingError](err)
			if accounting != test.fatal || results[0].Failure == nil || calls != 0 || (!test.fatal && err != nil) {
				t.Fatalf("result=%+v calls=%d accounting=%v err=%v", results, calls, accounting, err)
			}
			if test.send != nil {
				logical, _ := session.ModelHelperLogicalID("helper", 0)
				if ledger.results[session.ModelAttemptID(logical+"_try_1")].State != session.AttemptCancelled {
					t.Fatal("dispatch refusal did not settle exact undispatched attempt")
				}
			}
			if test.name == "mixed" && !errors.Is(err, sqlFailure) {
				t.Fatal("SQL failure hidden by refusal")
			}
		})
	}
}

func TestModelHelpersFatalSettlementCancelsAndDrainsEveryStartedItem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		firstFailure, siblingFailure := errors.New("first settlement unavailable"), errors.New("sibling settlement unavailable")
		firstID, _ := session.ModelHelperLogicalID("helper", 0)
		secondID, _ := session.ModelHelperLogicalID("helper", 1)
		ledger := &helperMemoryLedger{settle: func(id session.ModelAttemptID, _ session.ModelAttemptResult) error {
			switch string(id) {
			case firstID + "_try_1":
				return firstFailure // Exercise the bounded SQL-only retry loop.
			case secondID + "_try_1":
				return errors.Join(session.ErrInvalid, siblingFailure)
			default:
				return nil
			}
		}}
		var started, finished atomic.Int64
		allStarted := make(chan struct{})
		r := &Runner{attempts: ledger, provider: providerFunc(func(ctx context.Context, value model.Request) (model.Response, error) {
			if started.Add(1) == 4 {
				close(allStarted)
			}
			defer finished.Add(1)
			<-allStarted
			if value.Messages[0].Parts[0].Text == "0" {
				return model.Response{Parts: []session.Part{{Type: "text", Text: "completed but not settled"}}}, nil
			}
			<-ctx.Done()
			return model.Response{}, ctx.Err()
		})}
		results, err := r.CallModels(t.Context(), helperTestRequest("0", "1", "2", "3", "4", "5"), nil)
		if _, ok := errors.AsType[*AccountingError](err); !ok || !errors.Is(err, firstFailure) || !errors.Is(err, siblingFailure) || !errors.Is(err, context.Canceled) {
			t.Fatalf("drain hid independent errors: %v", err)
		}
		if started.Load() != 4 || finished.Load() != 4 || len(ledger.specs) != 4 || len(ledger.results) != 2 {
			t.Fatalf("started=%d finished=%d specs=%d results=%+v", started.Load(), finished.Load(), len(ledger.specs), ledger.results)
		}
		for _, result := range ledger.results {
			if result.State != session.AttemptUncertain {
				t.Fatal("cancelled dispatched sibling lost uncertainty")
			}
		}
		if *results[4].Failure != "model helper was not started" || *results[5].Failure != "model helper was not started" {
			t.Fatal("fatal batch admitted later prompts")
		}
	})
}

func TestModelHelpersCancellationBeforeAndAfterDispatch(t *testing.T) {
	for _, beforeDispatch := range []bool{true, false} {
		t.Run(strconv.FormatBool(beforeDispatch), func(t *testing.T) {
			ledger := &helperMemoryLedger{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			provider := preparedProvider(func(ctx context.Context, value model.Request) (model.Prepared, error) {
				prepared, err := (model.Scripted{}).Prepare(ctx, value)
				if beforeDispatch {
					prepared.BeforeDispatch = func(context.Context) error { cancel(); return ctx.Err() }
				}
				prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
					calls++
					cancel()
					return model.Response{}, ctx.Err()
				}
				return prepared, err
			})
			r := &Runner{provider: provider, attempts: ledger}
			_, err := r.CallModels(ctx, helperTestRequest("one"), nil)
			_, accounting := errors.AsType[*AccountingError](err)
			if !errors.Is(err, context.Canceled) || accounting || len(ledger.results) != 1 {
				t.Fatalf("cancellation err=%v results=%+v", err, ledger.results)
			}
			wantCalls, wantState := 1, session.AttemptUncertain
			if beforeDispatch {
				wantCalls, wantState = 0, session.AttemptCancelled
			}
			for _, result := range ledger.results {
				if result.State != wantState || calls != wantCalls {
					t.Fatalf("result=%+v calls=%d", result, calls)
				}
			}
		})
	}
}

func TestModelHelpersPrepareAndMalformedResponseFailuresStayPositional(t *testing.T) {
	ledger := &helperMemoryLedger{}
	provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
		prompt := request.Messages[0].Parts[0].Text
		if prompt == "prepare" {
			return model.Prepared{}, errors.New("no route")
		}
		prepared, err := (model.Scripted{}).Prepare(ctx, request)
		prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
			switch prompt {
			case "nontext":
				return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "bad", Name: "execute", Arguments: json.RawMessage(`{}`)}}}}, nil
			case "large":
				return model.Response{Parts: []session.Part{{Type: "text", Text: strings.Repeat("x", session.MaxDocumentBytes)}}}, nil
			case "provider":
				return model.Response{}, &model.CallError{Message: "confirmed failure"}
			default:
				return model.Response{Parts: []session.Part{{Type: "text", Text: "answer"}}}, nil
			}
		}
		return prepared, err
	})
	r := &Runner{provider: provider, attempts: ledger}
	results, err := r.CallModels(t.Context(), helperTestRequest("prepare", "nontext", "large", "provider", "valid"), nil)
	if err != nil || len(results) != 5 || len(ledger.specs) != 4 || len(ledger.results) != 4 {
		t.Fatalf("results=%+v specs=%d err=%v", results, len(ledger.specs), err)
	}
	for _, result := range results[:4] {
		if result.Text != "" || result.Failure == nil {
			t.Fatalf("invalid response escaped: %+v", result)
		}
	}
	if results[0].AttemptID != "" || results[4].Text != "answer" || results[4].AttemptID == "" || results[4].Failure != nil {
		t.Fatalf("attempt output identity: %+v", results)
	}
}

func TestModelHelpersInvalidShapeDoesNotPrepare(t *testing.T) {
	for _, change := range []func(*ModelHelperRequest){
		func(r *ModelHelperRequest) { r.Prompts = nil },
		func(r *ModelHelperRequest) { r.Prompts = make([]string, 33) },
		func(r *ModelHelperRequest) { r.Prompts = []string{" "} },
		func(r *ModelHelperRequest) { r.Prompts = []string{"\xff"} },
		func(r *ModelHelperRequest) { r.OperationID = "" },
		func(r *ModelHelperRequest) { r.MaxTokens = new(int64(0)) },
		func(r *ModelHelperRequest) { r.MaxTokens = new(int64(1000001)) },
	} {
		request := helperTestRequest("one")
		change(&request)
		result, err := (&Runner{}).CallModels(t.Context(), request, nil)
		if !errors.Is(err, session.ErrInvalid) || result != nil {
			t.Fatalf("invalid request accepted: %+v %v", result, err)
		}
	}
}

func TestModelHelpersBudgetRefusalAllowsOperationSettlement(t *testing.T) {
	ledger, request := helperFixture(t, []string{"one", "two"}, nil)
	if _, err := ledger.SetBudget(t.Context(), request.Turn.SessionID, 0, session.BudgetLimit{Kind: session.BudgetModelCalls, Limit: new(int64(0))}); err != nil {
		t.Fatal(err)
	}
	r := &Runner{provider: model.Scripted{}, attempts: ledger}
	results, err := r.CallModels(t.Context(), request, helperRefused)
	if err != nil || len(results) != 2 || results[0].Failure == nil || results[1].Failure == nil {
		t.Fatalf("budget refusal became fatal: %+v %v", results, err)
	}
	attempts, err := ledger.ModelAttempts(t.Context(), request.Turn.ID, "", 100)
	if err != nil || len(attempts) != 0 {
		t.Fatalf("refusal left attempt: %+v %v", attempts, err)
	}
	raw, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.SettleOperation(t.Context(), request.OperationID, session.OperationResult{State: session.OperationSucceeded, Value: raw}); err != nil {
		t.Fatalf("ordinary refused aggregate could not settle: %v", err)
	}
}

// This is a fixture for the caller-owned semantic classifier, not a runner
// dependency on store errors. Mixed rollback failures must stay fatal.
func helperRefused(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if !helperRefused(child) {
				return false
			}
		}
		return len(joined.Unwrap()) > 0
	}
	if wrapped := errors.Unwrap(err); wrapped != nil {
		return helperRefused(wrapped)
	}
	return err == store.ErrLimit || err == store.ErrStopped || err == session.ErrInvalid //nolint:errorlint // Match only known leaves; errors.Is would hide a mixed SQL failure.
}

type helperPreview struct{ calls atomic.Int64 }

func (p *helperPreview) BeginPreview(session.Turn, session.ModelAttemptID, session.MessageID) (func(model.Chunk), func()) {
	p.calls.Add(1)
	return func(model.Chunk) {}, func() {}
}

func TestModelHelpersFreezePromptSelectionAndCapForLaterBatchMembers(t *testing.T) {
	ledger := &helperMemoryLedger{}
	request := helperTestRequest("0", "1", "2", "3", "4")
	request.MaxTokens = new(int64(50))
	request.Model.Temperature, request.Model.TopP = new(0.0), new(0.75)
	wantModel := request.Model.Clone()
	ready, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	provider := preparedProvider(func(ctx context.Context, value model.Request) (model.Prepared, error) {
		once.Do(func() { close(ready) })
		<-release
		if !value.Selection.Equal(wantModel) || value.OutputTokenLimit == nil || *value.OutputTokenLimit != 50 || value.Messages[0].Parts[0].Text == "mutated" {
			return model.Prepared{}, errors.New("captured helper request was aliased")
		}
		return (model.Scripted{}).Prepare(ctx, value)
	})
	r := &Runner{provider: provider, attempts: ledger}
	done := make(chan error, 1)
	captured := request
	go func() {
		results, err := r.CallModels(t.Context(), captured, nil)
		for _, result := range results {
			if result.Failure != nil {
				err = errors.Join(err, errors.New(*result.Failure))
			}
		}
		done <- err
	}()
	receiveHelper(t, ready)
	request.Prompts[4] = "mutated"
	*request.MaxTokens = 99
	request.Model.Name = "changed"
	*request.Model.Temperature, *request.Model.TopP = 2, 1
	close(release)
	if err := receiveHelper(t, done); err != nil {
		t.Fatal(err)
	}
	for _, spec := range ledger.specs {
		if spec.Request.MaxOutputTokens != 50 || !spec.Request.Model.Equal(wantModel) {
			t.Fatalf("captured snapshot changed: %+v", spec)
		}
	}
}
