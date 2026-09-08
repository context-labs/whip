package daemon

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestBudgetCommandsRouteThroughRealSessionActor(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := root.AdmitAgent(ctx, session.AgentAdmission{ParentAgentID: root.authority.AgentID, ChildAgentID: "parent", Name: "parent", Budgets: []session.BudgetLimit{{Kind: session.BudgetTokens, Limit: 10}}}); err != nil {
		t.Fatal(err)
	}
	if err := root.AdmitAgent(ctx, session.AgentAdmission{ParentAgentID: root.authority.AgentID, ChildAgentID: "unrelated", Name: "unrelated"}); err != nil {
		t.Fatal(err)
	}
	if err := root.AdmitAgent(ctx, session.AgentAdmission{ParentAgentID: "parent", ChildAgentID: "target", Name: "target", Budgets: []session.BudgetLimit{{Kind: session.BudgetTokens, Limit: 8}}}); err != nil {
		t.Fatal(err)
	}
	state, err := root.CapBudget(ctx, "parent", "target", session.BudgetTokens, 4)
	if err != nil || (state.Limit == nil || *state.Limit != 4) {
		t.Fatalf("actor cap=%+v err=%v", state, err)
	}
	states, err := root.InspectBudgets(ctx, "parent", "target")
	if err != nil {
		t.Fatal(err)
	}
	if len(states) == 0 {
		t.Fatal("actor inspection returned no budgets")
	}
	if _, err := root.CapBudget(ctx, "unrelated", "target", session.BudgetTokens, 3); !errors.Is(err, session.ErrAgentAccess) {
		t.Fatalf("unrelated actor cap error=%v", err)
	}
	if _, err := root.InspectBudgets(ctx, "unrelated", "target"); !errors.Is(err, session.ErrAgentAccess) {
		t.Fatalf("unrelated actor inspection error=%v", err)
	}
}

func TestModelCallBudgetAccountsPriceElapsedAndActiveOperation(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := root.BeginModelAttempt(context.Background(), llm.ModelAttempt{
		LogicalID: "call", Number: 1, Model: "test", InputTokens: 80, MaxTokens: 20, Timeout: time.Second,
		Pricing: llm.Pricing{Prompt: "0.000002", Completion: "0.000005", InputCacheRead: "0.0000005"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := permit.Settle(llm.ModelAttemptResult{Dispatched: true, Elapsed: time.Millisecond, Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 4, PromptTokensDetails: &struct {
		CachedTokens int `json:"cached_tokens"`
	}{CachedTokens: 2}}}); err != nil {
		t.Fatal(err)
	}
	states, err := root.InspectBudgets(context.Background(), root.AgentID(), root.AgentID())
	if err != nil {
		t.Fatal(err)
	}
	byKind := make(map[session.BudgetKind]session.BudgetState, len(states))
	for _, state := range states {
		byKind[state.Kind] = state
	}
	if got := byKind[session.BudgetTokens]; got.Used != 14 || got.Reserved != 0 {
		t.Fatalf("token budget = %+v", got)
	}
	// Eight uncached prompt tokens (16 micros), two cache tokens (1 micro)
	// and four output tokens (20 micros).
	if got := byKind[session.BudgetCost]; got.Used != 37 || got.Reserved != 0 {
		t.Fatalf("cost budget = %+v", got)
	}
	if got := byKind[session.BudgetElapsed]; got.Reserved != 0 {
		t.Fatalf("elapsed budget = %+v", got)
	}
	if got := byKind[session.BudgetActiveOperations]; got.Used != 0 || got.Reserved != 0 {
		t.Fatalf("active operation budget = %+v", got)
	}
}

func TestModelAccountingFailureRetriesSettlementWithoutProviderReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	attempt := llm.ModelAttempt{LogicalID: "one", Number: 1, Model: "m", InputTokens: 10, MaxTokens: 10, Timeout: time.Second}
	permit, err := root.BeginModelAttempt(t.Context(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := routeControlOwnedValue(root, t.Context(), func(ctx context.Context) (struct{}, error) {
		_, err := db.ExecContext(ctx, `CREATE TRIGGER fail_accounting BEFORE UPDATE ON model_calls BEGIN SELECT RAISE(ABORT,'injected settlement failure'); END`)
		return struct{}{}, err
	}); err != nil {
		t.Fatal(err)
	}
	result := llm.ModelAttemptResult{Dispatched: true, Elapsed: time.Millisecond, Usage: llm.Usage{PromptTokens: 5, CompletionTokens: 3}}
	if err := permit.Settle(result); err == nil {
		t.Fatal("injected settlement failure was ignored")
	}
	conflicting := result
	conflicting.Usage.CompletionTokens = 99
	if err := permit.Settle(conflicting); err == nil {
		t.Fatal("conflicting callback replaced a result while storage was unavailable")
	}
	attempt.LogicalID = "two"
	if _, err := root.BeginModelAttempt(t.Context(), attempt); err == nil {
		t.Fatal("unresolved accounting admitted another call")
	}
	if _, err := routeControlOwnedValue(root, t.Context(), func(ctx context.Context) (struct{}, error) {
		_, err := db.ExecContext(ctx, `DROP TRIGGER fail_accounting`)
		return struct{}{}, err
	}); err != nil {
		t.Fatal(err)
	}
	second, err := root.BeginModelAttempt(t.Context(), attempt)
	if err != nil {
		t.Fatalf("admission: %v; root: %v", err, root.Err())
	}
	if err := second.Settle(llm.ModelAttemptResult{}); err != nil {
		t.Fatal(err)
	}
	// Repeating the first callback can only return the prior settlement.
	if err := permit.Settle(result); err != nil {
		t.Fatal(err)
	}
	if err := permit.Settle(conflicting); !errors.Is(err, session.ErrModelCallConflict) {
		t.Fatalf("conflicting terminal settlement: %v", err)
	}
	root.accountingMu.Lock()
	pending := len(root.pendingAccounting)
	root.accountingMu.Unlock()
	if pending != 0 {
		t.Fatal("conflicting settled callback poisoned future admission")
	}
	var calls, tokens int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*),sum(tokens) FROM model_calls WHERE root_id=?`, rootID).Scan(&calls, &tokens); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || tokens != 8 {
		t.Fatalf("calls=%d tokens=%d; settlement repair duplicated spend", calls, tokens)
	}
	accounting, err := store.ModelAccounting(t.Context(), rootID, "", true)
	if err != nil || accounting.PendingCalls != 0 || accounting.ReportedCalls != 1 {
		t.Fatalf("accounting=%+v error=%v", accounting, err)
	}
}

func TestModelAccountingShutdownFlushesRetainedOutcome(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := root.BeginModelAttempt(t.Context(), llm.ModelAttempt{LogicalID: "shutdown", Number: 1, Model: "m", InputTokens: 100, MaxTokens: 20, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cost := 0.000123
	// This is the live state after a transient persistence failure. Closing
	// must retry this exact known outcome before estimating interrupted calls.
	root.accountingMu.Lock()
	root.pendingAccounting = map[string]llm.ModelAttemptResult{permit.ID: {Dispatched: true, Elapsed: time.Millisecond, Usage: llm.Usage{PromptTokens: 2, CompletionTokens: 3, Cost: &cost}}}
	root.accountingMu.Unlock()
	root.Stop()
	accounting, err := store.ModelAccounting(t.Context(), rootID, "", true)
	if err != nil || accounting.ReportedCostMicros != 123 || accounting.ReportedCalls != 1 || accounting.EstimatedCalls != 0 || accounting.PendingCalls != 0 {
		t.Fatalf("shutdown lost retained outcome: %+v %v", accounting, err)
	}
}

func TestUnlimitedModelCallsShareAccountingWithoutDefaultDenial(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{root.AgentID(), "overview", "go", "frontend"}
	for _, id := range ids[1:] {
		if err := root.AdmitAgent(t.Context(), session.AgentAdmission{ParentAgentID: root.AgentID(), ChildAgentID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	attempt := llm.ModelAttempt{Number: 1, Model: "fixture", InputTokens: 12929, MaxTokens: 1048576, Timeout: time.Minute, Pricing: llm.Pricing{Prompt: "0.0000045", Completion: "0.0000225"}}
	type admission struct {
		settle func(llm.ModelAttemptResult) error
		err    error
	}
	results := make(chan admission, len(ids))
	for _, id := range ids {
		go func() {
			request := attempt
			request.LogicalID = id
			permit, err := root.beginAgentModelAttempt(t.Context(), id, request)
			results <- admission{permit.Settle, err}
		}()
	}
	var settlements []func(llm.ModelAttemptResult) error
	for range ids {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		settlements = append(settlements, result.settle)
	}
	states, err := root.InspectBudgets(t.Context(), root.AgentID(), root.AgentID())
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		if state.Kind == session.BudgetCost && (state.Limit != nil || state.Reserved < 4*23_000_000) {
			t.Fatalf("concurrent reservations: %+v", state)
		}
	}
	for _, settle := range settlements {
		usage := llm.ModelAttemptResult{Dispatched: true, Elapsed: time.Millisecond, Usage: llm.Usage{Reported: true, PromptTokens: 100, CompletionTokens: 10}}
		if err := settle(usage); err != nil {
			t.Fatal(err)
		}
		if err := settle(usage); err != nil {
			t.Fatal(err)
		}
	}
	states, err = root.InspectBudgets(t.Context(), root.AgentID(), root.AgentID())
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		if state.Kind == session.BudgetTokens && (state.Used != 440 || state.Reserved != 0) {
			t.Fatalf("exactly once: %+v", state)
		}
		if state.Kind == session.BudgetActiveOperations && state.Reserved != 0 {
			t.Fatalf("live capacity: %+v", state)
		}
	}
}

func TestFiniteModelCapStopsRetryAfterUnknownUsage(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.CapBudget(t.Context(), root.AgentID(), root.AgentID(), session.BudgetCost, 1_000_000); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "retry", http.StatusBadGateway)
	}))
	defer server.Close()
	client := llm.New(server.URL, "fixture")
	client.MaxRetries = 2
	request := llm.Request{Model: "fixture", MaxTokens: 1, Accounting: &llm.CallAccounting{Budget: root, Purpose: "test", Pricing: llm.Pricing{Prompt: "0", Completion: "1"}}}
	if _, _, err := client.Complete(t.Context(), request); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("retry error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d; retry bypassed cap", requests)
	}
	states, err := root.InspectBudgets(t.Context(), root.AgentID(), root.AgentID())
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		if state.Kind == session.BudgetCost && (state.Used != 0 || state.Reserved != 0 || state.Uncertain != 1_000_000 || !state.Incomplete) {
			t.Fatalf("unknown cost: %+v", state)
		}
		if state.Kind == session.BudgetActiveOperations && state.Reserved != 0 {
			t.Fatalf("active operations: %+v", state)
		}
	}
}
