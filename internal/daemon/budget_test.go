package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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
	prices := llm.TokenPrices{Input: 0.000002, Output: 0.000005, CacheRead: 0.0000005, Known: true, CacheReadKnown: true}
	settle, err := root.ReserveModelCall(context.Background(), llm.CallEstimate{PromptTokens: 60, OutputTokens: 40, Prices: prices})
	if err != nil {
		t.Fatal(err)
	}
	if err := settle(llm.Usage{Reported: true, Dispatched: true, PromptTokens: 10, CompletionTokens: 4, PromptTokensDetails: &struct {
		CachedTokens int `json:"cached_tokens"`
	}{CachedTokens: 2}}); err != nil {
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
	usage := llm.Usage{Reported: true, Dispatched: true, PromptTokens: 10, CompletionTokens: 4, PromptTokensDetails: &struct {
		CachedTokens int `json:"cached_tokens"`
	}{CachedTokens: 2}}
	if got := byKind[session.BudgetCost]; got.Used != actualCostMicros(usage, prices) || got.Reserved != 0 {
		t.Fatalf("cost budget = %+v", got)
	}
	if got := byKind[session.BudgetElapsed]; got.Reserved != 0 {
		t.Fatalf("elapsed budget = %+v", got)
	}
	if got := byKind[session.BudgetActiveOperations]; got.Used != 0 || got.Reserved != 0 {
		t.Fatalf("active operation budget = %+v", got)
	}
	if _, err := root.ReserveModelCall(context.Background(), llm.CallEstimate{OutputTokens: 1, Prices: llm.TokenPrices{Input: -1}}); err == nil {
		t.Fatal("negative model price was accepted")
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
	estimate := llm.CallEstimate{PromptTokens: 12929, OutputTokens: 1048576, Prices: llm.TokenPrices{Input: 0.0000045, Output: 0.0000225, Known: true}}
	type admission struct {
		settle func(llm.Usage) error
		err    error
	}
	results := make(chan admission, len(ids))
	for _, id := range ids {
		go func() {
			settle, err := root.ReserveAgentModelCall(t.Context(), id, estimate)
			results <- admission{settle, err}
		}()
	}
	var settlements []func(llm.Usage) error
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
		usage := llm.Usage{Reported: true, Dispatched: true, PromptTokens: 100, CompletionTokens: 10}
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
	request := llm.Request{MaxTokens: 1, BeforeAttempt: func(ctx context.Context, _ llm.Request) (func(llm.Usage) error, error) {
		return root.ReserveModelCall(ctx, llm.CallEstimate{OutputTokens: 1, Prices: llm.TokenPrices{Output: 1, Known: true}})
	}}
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
