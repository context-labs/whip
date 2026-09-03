package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

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
	if err != nil || state.Limit != 4 {
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
	if err := root.ConfigureModelPricing(0.000002, 0.000005, 0.0000005); err != nil {
		t.Fatal(err)
	}
	settle, err := root.ReserveModelCall(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := settle(llm.Usage{PromptTokens: 10, CompletionTokens: 4, PromptTokensDetails: &struct {
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
	usage := llm.Usage{PromptTokens: 10, CompletionTokens: 4, PromptTokensDetails: &struct {
		CachedTokens int `json:"cached_tokens"`
	}{CachedTokens: 2}}
	if got := byKind[session.BudgetCost]; got.Used != actualCostMicros(usage, modelPricing{input: 0.000002, output: 0.000005, cacheRead: 0.0000005}) || got.Reserved != 0 {
		t.Fatalf("cost budget = %+v", got)
	}
	if got := byKind[session.BudgetElapsed]; got.Reserved != 0 {
		t.Fatalf("elapsed budget = %+v", got)
	}
	if got := byKind[session.BudgetActiveOperations]; got.Used != 0 || got.Reserved != 0 {
		t.Fatalf("active operation budget = %+v", got)
	}
	if err := root.ConfigureModelPricing(-1, 0, 0); err == nil {
		t.Fatal("negative model price was accepted")
	}
}
