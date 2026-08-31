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
	if err := root.AdmitChild(ctx, root.authority.AgentID, "parent", "exec-parent", session.BudgetLimit{Kind: session.BudgetTokens, Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if err := root.AdmitChild(ctx, root.authority.AgentID, "unrelated", "exec-unrelated"); err != nil {
		t.Fatal(err)
	}
	if err := root.AdmitChild(ctx, "parent", "target", "exec-target", session.BudgetLimit{Kind: session.BudgetTokens, Limit: 8}); err != nil {
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
