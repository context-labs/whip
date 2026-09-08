package session

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
)

func modelAttempt(id string, input int64, output int) llm.ModelAttempt {
	return llm.ModelAttempt{LogicalID: id, Number: 1, Model: "model", Provider: "provider", Purpose: "turn",
		InputTokens: input, MaxTokens: output, Timeout: time.Second,
		Pricing: llm.Pricing{Prompt: "0.000001", Completion: "0.000002", InputCacheRead: "0"}}
}

func TestCheckModelWorkUsesEveryAncestorBudget(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	if err := store.SetBudgetLimit(t.Context(), root, "", BudgetCost, 100); err != nil {
		t.Fatal(err)
	}
	for child, limit := range map[string]int64{"left": 10, "right": 0} {
		if _, err := store.AdmitAgent(t.Context(), AgentAdmission{RootID: root, ParentAgentID: agent, ChildAgentID: child, Budgets: []BudgetLimit{{BudgetCost, limit}}}); err != nil {
			t.Fatal(err)
		}
	}
	left, err := store.AdmitModelCall(t.Context(), root, "left", modelAttempt("child-charge", 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	childCost := 0.000010
	if _, err := store.SettleModelCall(t.Context(), root, left.ID, llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Reported: true, PromptTokens: 1, Cost: &childCost}}); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckModelWork(t.Context(), root, "left"); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("child cap did not stop child: %v", err)
	}
	for _, target := range []string{agent, "right"} {
		if err := store.CheckModelWork(t.Context(), root, target); err != nil {
			t.Fatalf("child cap stopped %s: %v", target, err)
		}
	}
	rootCall, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("root-charge", 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	rootCost := 0.000090
	if _, err := store.SettleModelCall(t.Context(), root, rootCall.ID, llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Reported: true, PromptTokens: 1, Cost: &rootCost}}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{agent, "left", "right"} {
		if err := store.CheckModelWork(t.Context(), root, target); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("root cap did not stop %s: %v", target, err)
		}
	}
}

func TestCheckModelWorkAllowsLiveReservations(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	admitTestChild(t, store, root, agent, "child")
	for kind, limit := range map[BudgetKind]int64{BudgetTokens: 10, BudgetCost: 15, BudgetElapsed: 1000, BudgetActiveOperations: 1} {
		if err := store.SetBudgetLimit(t.Context(), root, "", kind, limit); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("live", 5, 5)); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{agent, "child"} {
		if err := store.CheckModelWork(t.Context(), root, target); err != nil {
			t.Fatalf("reservation-only exhaustion stopped admitted work for %s: %v", target, err)
		}
	}
}

func TestCheckModelWorkRejectsInvalidCounters(t *testing.T) {
	for _, field := range []string{"limit_value", "used_value", "reserved_value"} {
		t.Run(field, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			if _, err := store.db.ExecContext(t.Context(), `UPDATE budgets SET `+field+`=-1 WHERE root_id=? AND kind=?`, root, BudgetElapsed); err != nil {
				t.Fatal(err)
			}
			if err := store.CheckModelWork(t.Context(), root, agent); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("invalid counter passed barrier: %v", err)
			}
		})
	}
}

func TestCheckModelWorkRejectsSpentTokensAndTime(t *testing.T) {
	for _, kind := range []BudgetKind{BudgetTokens, BudgetElapsed} {
		t.Run(string(kind), func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			if err := store.SetBudgetLimit(t.Context(), root, "", kind, 2); err != nil {
				t.Fatal(err)
			}
			reserved := []capability.Reservation{{Kind: string(kind), Amount: 2, Consume: true}}
			if err := store.ReserveBudget(t.Context(), root, agent, reserved); err != nil {
				t.Fatal(err)
			}
			if err := store.ReconcileBudget(t.Context(), root, agent, reserved, []capability.Usage{{Kind: string(kind), Amount: 2}}); err != nil {
				t.Fatal(err)
			}
			if err := store.CheckModelWork(t.Context(), root, agent); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("spent budget passed barrier: %v", err)
			}
		})
	}
}

func TestModelSettlementOverageAndIdempotence(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	if err := store.SetBudgetLimit(t.Context(), root, "", BudgetTokens, 100); err != nil {
		t.Fatal(err)
	}
	first, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("first", 10, 10))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("second", 10, 10))
	if err != nil {
		t.Fatal(err)
	}
	actual := llm.ModelAttemptResult{Usage: llm.Usage{Reported: true, PromptTokens: 90, CompletionTokens: 20}, Dispatched: true, Elapsed: 2 * time.Millisecond}
	settled, err := store.SettleModelCall(t.Context(), root, first.ID, actual)
	if err != nil || !settled.Exhausted {
		t.Fatalf("overage settlement=%+v err=%v", settled, err)
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Used != 110 || got.Reserved != 20 || (got.Remaining == nil || *got.Remaining != 0) {
		t.Fatalf("overage consumed another reservation: %+v", got)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, err := store.SettleModelCall(context.Background(), root, first.ID, actual)
			if err != nil || got != settled {
				t.Errorf("repeated settlement=%+v err=%v", got, err)
			}
		})
	}
	wg.Wait()
	actual.Usage.PromptTokens++
	if _, err := store.SettleModelCall(t.Context(), root, first.ID, actual); !errors.Is(err, ErrModelCallConflict) {
		t.Fatalf("conflicting settlement=%v", err)
	}
	if _, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("blocked", 1, 1)); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("overdrawn admission=%v", err)
	}
	if _, err := store.SettleModelCall(t.Context(), root, second.ID, llm.ModelAttemptResult{Dispatched: true}); err != nil {
		t.Fatal(err)
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Used != 110 || got.Uncertain != 20 || !got.Incomplete || got.Reserved != 0 {
		t.Fatalf("final budget=%+v", got)
	}
	if got := budgetState(t, store, root, agent, BudgetActiveOperations); got.Used != 0 || got.Reserved != 0 {
		t.Fatalf("leaked slot=%+v", got)
	}
	var eventCount int
	if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM events WHERE root_id=? AND kind='model.call.settled'`, root).Scan(&eventCount); err != nil || eventCount != 2 {
		t.Fatalf("settlement events=%d err=%v", eventCount, err)
	}
}

func TestModelAdmissionClampsOutputAndElapsed(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	for kind, limit := range map[BudgetKind]int64{BudgetTokens: 20, BudgetCost: 18, BudgetElapsed: 5} {
		if err := store.SetBudgetLimit(t.Context(), root, "", kind, limit); err != nil {
			t.Fatal(err)
		}
	}
	permit, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("small", 10, 100))
	if err != nil {
		t.Fatal(err)
	}
	if permit.MaxTokens != 4 || permit.Timeout != 5*time.Millisecond {
		t.Fatalf("clamped permit=%+v", permit)
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Reserved != 14 {
		t.Fatalf("token reserve=%+v", got)
	}
	if _, err := store.SettleModelCall(t.Context(), root, permit.ID, llm.ModelAttemptResult{}); err != nil {
		t.Fatal(err)
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Used != 0 || got.Reserved != 0 {
		t.Fatalf("predispatch charge=%+v", got)
	}
	if _, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("input-too-large", 20, 1)); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("input-only exhaustion=%v", err)
	}
}

func TestModelCostPresenceAndProvenance(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	zero := 0.0
	for _, test := range []struct {
		id      string
		pricing llm.Pricing
		usage   llm.Usage
	}{
		{"reported-free", llm.Pricing{}, llm.Usage{Reported: true, PromptTokens: 5, Cost: &zero}},
		{"estimated-free", llm.Pricing{Prompt: "0", Completion: "0"}, llm.Usage{Reported: true, PromptTokens: 5}},
		{"estimated-priced", modelAttempt("", 0, 1).Pricing, llm.Usage{Reported: true, PromptTokens: 5}},
		{"unknown", llm.Pricing{}, llm.Usage{Reported: true, PromptTokens: 5}},
		{"malformed-price", llm.Pricing{Prompt: "invalid", Completion: "0"}, llm.Usage{Reported: true, PromptTokens: 5}},
		{"missing-usage", modelAttempt("", 0, 1).Pricing, llm.Usage{}},
	} {
		attempt := modelAttempt(test.id, 5, 5)
		attempt.Pricing = test.pricing
		permit, err := store.AdmitModelCall(t.Context(), root, agent, attempt)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.SettleModelCall(t.Context(), root, permit.ID, llm.ModelAttemptResult{Dispatched: true, Usage: test.usage}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.ModelAccounting(t.Context(), root, agent, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReportedCostMicros != 0 || got.EstimatedCostMicros != 5 || got.ReportedCostCalls != 1 || got.EstimatedCostCalls != 2 || got.UnknownCostCalls != 3 || got.ReportedCalls != 5 || got.EstimatedCalls != 1 || got.PendingCalls != 0 {
		t.Fatalf("accounting=%+v", got)
	}
}

func TestModelSettlementUsesExactAncestorReservations(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	if _, err := store.AdmitAgent(t.Context(), AgentAdmission{RootID: root, ParentAgentID: agent, ChildAgentID: "parent", Budgets: []BudgetLimit{{BudgetTokens, 100}}}); err != nil {
		t.Fatal(err)
	}
	admitTestChild(t, store, root, "parent", "child")
	permit, err := store.AdmitModelCall(t.Context(), root, "child", modelAttempt("before-cap", 10, 10))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CapBudget(t.Context(), root, agent, "child", BudgetTokens, 50); err != nil {
		t.Fatal(err)
	}
	second, err := store.AdmitModelCall(t.Context(), root, "child", modelAttempt("after-cap", 10, 10))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SettleModelCall(t.Context(), root, permit.ID, llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Reported: true, PromptTokens: 25}}); err != nil {
		t.Fatal(err)
	}
	var used, reserved int64
	if err := store.db.QueryRowContext(t.Context(), `SELECT used_value,reserved_value FROM budgets WHERE root_id=? AND agent_id='child' AND kind=?`, root, BudgetTokens).Scan(&used, &reserved); err != nil {
		t.Fatal(err)
	}
	if used != 0 || reserved != 20 {
		t.Fatalf("new cap lost another call's reservation: used=%d reserved=%d", used, reserved)
	}
	if got := budgetState(t, store, root, "parent", BudgetTokens); got.Used != 25 || got.Reserved != 20 {
		t.Fatalf("ancestor accounting=%+v", got)
	}
	if _, err := store.SettleModelCall(t.Context(), root, second.ID, llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Reported: true, PromptTokens: 5}}); err != nil {
		t.Fatal(err)
	}
	for target, want := range map[string]int64{agent: 30, "parent": 30, "child": 5} {
		if got := budgetState(t, store, root, target, BudgetTokens); got.Used != want || got.Reserved != 0 {
			t.Fatalf("%s budget=%+v", target, got)
		}
	}
	own, err := store.ModelAccounting(t.Context(), root, "parent", false)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := store.ModelAccounting(t.Context(), root, "parent", true)
	if err != nil {
		t.Fatal(err)
	}
	if own.ReportedCalls != 0 || own.Scope != "agent" || tree.ReportedCalls != 2 || tree.Scope != "subtree" {
		t.Fatalf("scoped accounting own=%+v tree=%+v", own, tree)
	}
	snapshot, err := store.SnapshotRoot(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Accounting.ReportedCalls != 2 || snapshot.Accounting.EstimatedCostMicros != 30 || snapshot.Accounting.Revision != snapshot.Cursor {
		t.Fatalf("snapshot accounting=%+v", snapshot.Accounting)
	}
}

func TestModelFreeUnknownAndOverdrawnCost(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	if err := store.SetBudgetLimit(t.Context(), root, "", BudgetCost, 0); err != nil {
		t.Fatal(err)
	}
	for _, pricing := range []llm.Pricing{{Prompt: "0", Completion: "0", InputCacheRead: "0"}} {
		attempt := modelAttempt("zero-"+pricing.Prompt, 5, 5)
		attempt.Pricing = pricing
		permit, err := store.AdmitModelCall(t.Context(), root, agent, attempt)
		if err != nil {
			t.Fatal(err)
		}
		settled, err := store.SettleModelCall(t.Context(), root, permit.ID, llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Reported: true}})
		if err != nil || settled.Exhausted {
			t.Fatalf("zero-cost settlement=%+v err=%v", settled, err)
		}
	}
	accounting, err := store.ModelAccounting(t.Context(), root, agent, false)
	if err != nil {
		t.Fatal(err)
	}
	if accounting.EstimatedCostMicros != 0 || accounting.EstimatedCostCalls != 1 || accounting.ReportedCostCalls != 0 || accounting.UnknownCostCalls != 0 {
		t.Fatalf("free estimated cost lost its provenance: %+v", accounting)
	}
	if _, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("priced", 5, 5)); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("priced zero-budget call=%v", err)
	}
	unknown := modelAttempt("unexpected-charge", 5, 5)
	unknown.Pricing = llm.Pricing{Prompt: "0", Completion: "0"}
	permit, err := store.AdmitModelCall(t.Context(), root, agent, unknown)
	if err != nil {
		t.Fatal(err)
	}
	cost := 0.000003
	settled, err := store.SettleModelCall(t.Context(), root, permit.ID, llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Reported: true, Cost: &cost}})
	if err != nil || !settled.Exhausted {
		t.Fatalf("unreserved provider charge=%+v err=%v", settled, err)
	}
	unknown.LogicalID = "overdrawn"
	if _, err := store.AdmitModelCall(t.Context(), root, agent, unknown); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("overdrawn unpriced call=%v", err)
	}
	if got := budgetState(t, store, root, agent, BudgetCost); got.Used != 3 || got.Reserved != 0 || (got.Remaining == nil || *got.Remaining != 0) {
		t.Fatalf("actual unpriced cost=%+v", got)
	}
	if err := store.SetBudgetLimit(t.Context(), root, "", BudgetCost, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitModelCall(t.Context(), root, agent, unknown); err != nil {
		t.Fatalf("raising limit did not restore admission: %v", err)
	}
}

func TestModelSettlementRejectsInvalidOrOverflowWithoutPartialWrites(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	if err := store.SetBudgetLimit(t.Context(), root, "", BudgetTokens, math.MaxInt64); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `UPDATE budgets SET used_value=? WHERE root_id=? AND kind=?`, int64(math.MaxInt64-20), root, BudgetTokens); err != nil {
		t.Fatal(err)
	}
	attempt := modelAttempt("large", 5, 5)
	attempt.Pricing = llm.Pricing{}
	permit, err := store.AdmitModelCall(t.Context(), root, agent, attempt)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []llm.ModelAttemptResult{
		{Dispatched: true, Usage: llm.Usage{Reported: true, PromptTokens: 30}},
		{Dispatched: true, Elapsed: -time.Millisecond},
		{Usage: llm.Usage{Reported: true}},
	} {
		if _, err := store.SettleModelCall(t.Context(), root, permit.ID, result); err == nil {
			t.Fatalf("invalid settlement succeeded: %+v", result)
		}
		if got := budgetState(t, store, root, agent, BudgetTokens); got.Used != math.MaxInt64-20 || got.Reserved != 10 {
			t.Fatalf("failed settlement partially wrote: %+v", got)
		}
		if got := budgetState(t, store, root, agent, BudgetActiveOperations); got.Reserved != 1 {
			t.Fatalf("failed settlement released slot: %+v", got)
		}
	}
	if _, err := store.SettleModelCall(t.Context(), root, permit.ID, llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Reported: true, PromptTokens: 10}}); err != nil {
		t.Fatal(err)
	}
	var storageType string
	if err := store.db.QueryRowContext(t.Context(), `SELECT typeof(used_value) FROM budgets WHERE root_id=? AND agent_id='' AND kind=?`, root, BudgetTokens).Scan(&storageType); err != nil {
		t.Fatal(err)
	}
	if storageType != "integer" {
		t.Fatalf("budget promoted to %s", storageType)
	}
}

func TestModelMalformedUsageSettlesConservatively(t *testing.T) {
	validCost, invalidCost, nanCost := 0.000009, -1.0, math.NaN()
	for _, test := range []struct {
		name                                                 string
		usage                                                llm.Usage
		tokens, uncertainTokens, reportedCost, estimatedCost int64
		estimatedCalls                                       int64
	}{
		{"negative tokens", llm.Usage{Reported: true, PromptTokens: -1}, 0, 10, 0, 0, 1},
		{"bad tokens with valid cost", llm.Usage{Reported: true, PromptTokens: -1, Cost: &validCost}, 0, 10, 9, 0, 1},
		{"bad cost with valid tokens", llm.Usage{Reported: true, PromptTokens: 3, Cost: &invalidCost}, 3, 0, 0, 3, 0},
		{"nonfinite cost", llm.Usage{Reported: true, PromptTokens: 3, Cost: &nanCost}, 3, 0, 0, 3, 0},
		{"overflowed total tokens", llm.Usage{Reported: true, PromptTokens: math.MaxInt, CompletionTokens: 1}, 0, 10, 0, 0, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			permit, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("malformed", 5, 5))
			if err != nil {
				t.Fatal(err)
			}
			result := llm.ModelAttemptResult{Dispatched: true, Usage: test.usage}
			for range 2 {
				if _, err := store.SettleModelCall(t.Context(), root, permit.ID, result); err != nil {
					t.Fatalf("malformed result stranded reservation: %v", err)
				}
			}
			if got := budgetState(t, store, root, agent, BudgetTokens); got.Used != test.tokens || got.Uncertain != test.uncertainTokens || got.Reserved != 0 {
				t.Fatalf("tokens=%+v", got)
			}
			if got := budgetState(t, store, root, agent, BudgetActiveOperations); got.Reserved != 0 {
				t.Fatalf("slot leaked=%+v", got)
			}
			accounting, err := store.ModelAccounting(t.Context(), root, agent, false)
			if err != nil {
				t.Fatal(err)
			}
			if accounting.ReportedCostMicros != test.reportedCost || accounting.EstimatedCostMicros != test.estimatedCost || accounting.EstimatedCalls != test.estimatedCalls || accounting.PendingCalls != 0 {
				t.Fatalf("malformed accounting=%+v", accounting)
			}
		})
	}
}

func TestModelSubtreeInterruptionIsScopedAndIdempotent(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	admitTestChild(t, store, root, agent, "child")
	admitTestChild(t, store, root, "child", "grandchild")
	admitTestChild(t, store, root, agent, "retained")
	otherRoot, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.EnsureAuthority(t.Context(), otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct{ root, agent string }{{root, agent}, {root, "child"}, {root, "grandchild"}, {otherRoot, other.AgentID}} {
		if _, err := store.AdmitModelCall(t.Context(), target.root, target.agent, modelAttempt("pending", 10, 10)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.TerminalizeSubtree(t.Context(), root, agent, "child", "stopped"); err != nil {
		t.Fatal(err)
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Used != 0 || got.Uncertain != 40 || !got.Incomplete || got.Reserved != 20 {
		t.Fatalf("subtree accounting=%+v", got)
	}
	if got := budgetState(t, store, otherRoot, other.AgentID, BudgetTokens); got.Used != 0 || got.Reserved != 20 {
		t.Fatalf("other root affected=%+v", got)
	}
	if _, err := store.TerminalizeSubtree(t.Context(), root, agent, "child", "stopped"); !errors.Is(err, ErrAgentTerminal) {
		t.Fatalf("repeated stop=%v", err)
	}
	if _, err := store.StopRoot(t.Context(), root, "test stop"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitModelCall(t.Context(), root, "retained", modelAttempt("stopped-root", 1, 1)); !errors.Is(err, ErrRootTerminal) {
		t.Fatalf("idle child admitted through stopped root: %v", err)
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Used != 0 || got.Uncertain != 60 || !got.Incomplete || got.Reserved != 0 {
		t.Fatalf("root stop=%+v", got)
	}
	if got := budgetState(t, store, root, agent, BudgetActiveOperations); got.Reserved != 0 {
		t.Fatalf("stopped root leaked slots=%+v", got)
	}
	if got := budgetState(t, store, otherRoot, other.AgentID, BudgetActiveOperations); got.Reserved != 1 {
		t.Fatalf("other root slot released=%+v", got)
	}
	if err := store.DeleteSession(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	var calls int
	if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM model_calls`).Scan(&calls); err != nil || calls != 1 {
		t.Fatalf("delete call count=%d err=%v", calls, err)
	}
}

func TestModelRecoveryAfterReopenSettlesOnlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := store.AdmitModelCall(t.Context(), root, authority.AgentID, modelAttempt("crashed", 10, 10))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveBudget(t.Context(), root, authority.AgentID, []capability.Reservation{{Kind: string(BudgetTokens), Amount: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := budgetState(t, store, root, authority.AgentID, BudgetTokens); got.Used != 0 || got.Reserved != 23 {
		t.Fatalf("open settled live work=%+v", got)
	}
	for range 2 {
		if err := store.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := budgetState(t, store, root, authority.AgentID, BudgetTokens); got.Used != 0 || got.Uncertain != 23 || !got.Incomplete || got.Reserved != 0 {
			t.Fatalf("recovery charged twice=%+v", got)
		}
		if got := budgetState(t, store, root, authority.AgentID, BudgetActiveOperations); got.Used != 0 || got.Reserved != 0 {
			t.Fatalf("recovery leaked slot=%+v", got)
		}
	}
	accounting, err := store.ModelAccounting(t.Context(), root, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if accounting.EstimatedCalls != 1 || accounting.EstimatedCostMicros != 0 || accounting.UnknownCostCalls != 1 || accounting.PendingCalls != 0 {
		t.Fatalf("recovered accounting=%+v", accounting)
	}
	var notices int
	if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM events WHERE root_id=? AND kind='model.call.interrupted'`, root).Scan(&notices); err != nil || notices != 1 {
		t.Fatalf("uncertainty notices=%d err=%v", notices, err)
	}
	if _, err := store.SettleModelCall(t.Context(), root, permit.ID, llm.ModelAttemptResult{Dispatched: true, Failed: true, Elapsed: time.Second}); err != nil {
		t.Fatalf("identical recovered settlement failed: %v", err)
	}
	if _, err := store.SettleModelCall(t.Context(), root, permit.ID, llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Reported: true, PromptTokens: 2}}); !errors.Is(err, ErrModelCallConflict) {
		t.Fatalf("conflicting result after one correction=%v", err)
	}
}

func TestModelLateOutcomeCorrectsInterruptedEstimateWithoutReleasingOtherCalls(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	admitTestChild(t, store, root, agent, "child")
	permit, err := store.AdmitModelCall(t.Context(), root, "child", modelAttempt("cancelled", 5, 5))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TerminalizeSubtree(t.Context(), root, agent, "child", "stopped"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("other-live-call", 5, 5)); err != nil {
		t.Fatal(err)
	}
	cost := 0.000020
	actual := llm.ModelAttemptResult{Dispatched: true, Failed: true, Usage: llm.Usage{Reported: true, PromptTokens: 3, Cost: &cost}, Elapsed: time.Millisecond}
	for range 2 {
		if _, err := store.SettleModelCall(t.Context(), root, permit.ID, actual); err != nil {
			t.Fatal(err)
		}
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Used != 3 || got.Reserved != 10 {
		t.Fatalf("late token correction=%+v", got)
	}
	if got := budgetState(t, store, root, agent, BudgetCost); got.Used != 20 || got.Reserved != 15 {
		t.Fatalf("late cost correction=%+v", got)
	}
	if got := budgetState(t, store, root, agent, BudgetElapsed); got.Used != 1 || got.Reserved != 1000 {
		t.Fatalf("late elapsed correction=%+v", got)
	}
	if got := budgetState(t, store, root, agent, BudgetActiveOperations); got.Used != 0 || got.Reserved != 1 {
		t.Fatalf("late correction released another live slot=%+v", got)
	}
	accounting, err := store.ModelAccounting(t.Context(), root, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if accounting.ReportedCalls != 1 || accounting.EstimatedCalls != 0 || accounting.ReportedCostMicros != 20 || accounting.EstimatedCostMicros != 0 || accounting.PendingCalls != 1 {
		t.Fatalf("corrected accounting=%+v", accounting)
	}
	var corrected int
	if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM events WHERE root_id=? AND kind='model.call.corrected'`, root).Scan(&corrected); err != nil || corrected != 1 {
		t.Fatalf("correction events=%d err=%v", corrected, err)
	}
	actual.Usage.PromptTokens = 4
	if _, err := store.SettleModelCall(t.Context(), root, permit.ID, actual); !errors.Is(err, ErrModelCallConflict) {
		t.Fatalf("second conflicting correction=%v", err)
	}
}

func TestModelDatabaseFailureRollsBackAdmissionAndSettlement(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	attempt := modelAttempt("durable", 5, 5)
	if _, err := store.db.ExecContext(t.Context(), `CREATE TRIGGER fail_model_admission BEFORE INSERT ON model_calls BEGIN SELECT RAISE(ABORT,'injected admission failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitModelCall(t.Context(), root, agent, attempt); err == nil {
		t.Fatal("injected admission failure succeeded")
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Reserved != 0 {
		t.Fatalf("failed admission leaked reservation=%+v", got)
	}
	if _, err := store.db.ExecContext(t.Context(), `DROP TRIGGER fail_model_admission`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `CREATE TRIGGER fail_model_started BEFORE INSERT ON events WHEN NEW.kind='model.call.started' BEGIN SELECT RAISE(ABORT,'injected admission event failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitModelCall(t.Context(), root, agent, attempt); err == nil {
		t.Fatal("admission event failure ignored")
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Reserved != 0 {
		t.Fatalf("admission event failure leaked reserve: %+v", got)
	}
	var running int
	if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM model_calls`).Scan(&running); err != nil || running != 0 {
		t.Fatalf("admission event failure left call rows=%d error=%v", running, err)
	}
	if _, err := store.db.ExecContext(t.Context(), `DROP TRIGGER fail_model_started`); err != nil {
		t.Fatal(err)
	}
	permit, err := store.AdmitModelCall(t.Context(), root, agent, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := store.AdmitModelCall(t.Context(), root, agent, attempt); err != nil || repeated != permit {
		t.Fatalf("repeated admission=%+v err=%v", repeated, err)
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Reserved != 10 {
		t.Fatalf("repeated admission leaked reservation=%+v", got)
	}
	if _, err := store.db.ExecContext(t.Context(), `CREATE TRIGGER fail_model_settlement BEFORE INSERT ON events WHEN NEW.kind='model.call.settled' BEGIN SELECT RAISE(ABORT,'injected settlement event failure'); END`); err != nil {
		t.Fatal(err)
	}
	result := llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Reported: true, PromptTokens: 30}, Elapsed: time.Millisecond}
	if _, err := store.SettleModelCall(t.Context(), root, permit.ID, result); err == nil {
		t.Fatal("injected settlement failure succeeded")
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Used != 0 || got.Reserved != 10 {
		t.Fatalf("failed settlement partially wrote=%+v", got)
	}
	if got := budgetState(t, store, root, agent, BudgetActiveOperations); got.Reserved != 1 {
		t.Fatalf("failed settlement lost live slot=%+v", got)
	}
	if _, err := store.db.ExecContext(t.Context(), `DROP TRIGGER fail_model_settlement`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := store.SettleModelCall(t.Context(), root, permit.ID, result); err != nil {
			t.Fatal(err)
		}
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Used != 30 || got.Reserved != 0 {
		t.Fatalf("retried settlement=%+v", got)
	}
	accounting, err := store.ModelAccounting(t.Context(), root, agent, false)
	if err != nil {
		t.Fatal(err)
	}
	if accounting.ReportedCalls != 1 || accounting.EstimatedCostMicros != 30 || accounting.PendingCalls != 0 {
		t.Fatalf("retried accounting=%+v", accounting)
	}
	var payload []byte
	if err := store.db.QueryRowContext(t.Context(), `SELECT payload_inline FROM events WHERE root_id=? AND kind='model.call.settled'`, root).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var event LifecycleEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.RootID != root || event.AgentID != agent || event.Status != "succeeded" || event.ModelCall == nil || event.ModelCall.ID != permit.ID || event.ModelCall.Tokens != 30 || event.ModelCall.CostSource != "estimated" {
		t.Fatalf("durable accounting event=%+v", event)
	}
}

func TestModelLateCorrectionKeepsOtherUnknownAndGenericContributions(t *testing.T) {
	for _, generic := range []bool{false, true} {
		t.Run(strconv.FormatBool(generic), func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			var permits []ModelCallReservation
			for _, id := range []string{"first", "second"} {
				attempt := modelAttempt(id, 5, 5)
				attempt.Pricing = llm.Pricing{}
				permit, err := store.AdmitModelCall(t.Context(), root, agent, attempt)
				if err != nil {
					t.Fatal(err)
				}
				permits = append(permits, permit)
			}
			if generic {
				if err := store.ReserveBudget(t.Context(), root, agent, []capability.Reservation{{Kind: string(BudgetCost), Amount: 7}}); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			wantUncertainCost := int64(0)
			if generic {
				wantUncertainCost = 7
			}
			cost := budgetState(t, store, root, agent, BudgetCost)
			if cost.Used != 0 || cost.Uncertain != wantUncertainCost || !cost.Incomplete || cost.Reserved != 0 {
				t.Fatalf("unknown zero cost: %+v", cost)
			}
			for i, permit := range permits {
				zero := 0.0
				outcome := llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Reported: true, Cost: &zero}, Elapsed: time.Millisecond}
				if _, err := store.SettleModelCall(t.Context(), root, permit.ID, outcome); err != nil {
					t.Fatal(err)
				}
				cost = budgetState(t, store, root, agent, BudgetCost)
				wantIncomplete := generic || i == 0
				if cost.Used != 0 || cost.Uncertain != wantUncertainCost || cost.Incomplete != wantIncomplete || cost.Reserved != 0 {
					t.Fatalf("correction %d: %+v", i, cost)
				}
				tokens := budgetState(t, store, root, agent, BudgetTokens)
				if tokens.Used != 0 || tokens.Uncertain != int64(1-i)*10 || tokens.Incomplete != (i == 0) {
					t.Fatalf("token correction %d: %+v", i, tokens)
				}
				elapsed := budgetState(t, store, root, agent, BudgetElapsed)
				if elapsed.Used != int64(i+1) || elapsed.Uncertain != int64(1-i)*1000 || elapsed.Incomplete != (i == 0) {
					t.Fatalf("elapsed correction %d: %+v", i, elapsed)
				}
				snapshot, err := store.SnapshotRoot(t.Context(), root)
				if err != nil {
					t.Fatal(err)
				}
				for _, budget := range snapshot.Budgets {
					if budget.AgentID == "" && budget.State.Kind == BudgetCost && budget.State.Incomplete != wantIncomplete {
						t.Fatalf("snapshot incomplete: %+v", budget)
					}
				}
				page, err := store.RootCollectionPage(t.Context(), root, "budgets", CollectionPageOptions{Limit: 128, MaxBytes: 512 << 10})
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range page.Items {
					if entry.Budget != nil && entry.Budget.AgentID == "" && entry.Budget.State.Kind == BudgetCost && entry.Budget.State.Incomplete != wantIncomplete {
						t.Fatalf("page incomplete: %+v", entry.Budget)
					}
				}
			}
		})
	}
}

func TestModelFiniteAncestorRejectsUnknownPrice(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	admitTestChild(t, store, root, agent, "parent")
	admitTestChild(t, store, root, "parent", "child")
	if _, err := store.CapBudget(t.Context(), root, agent, "parent", BudgetCost, 5); err != nil {
		t.Fatal(err)
	}
	attempt := modelAttempt("unknown", 1, 1)
	attempt.Pricing = llm.Pricing{}
	if _, err := store.AdmitModelCall(t.Context(), root, "child", attempt); !errors.Is(err, capability.ErrDenied) || !strings.Contains(err.Error(), "pricing is unavailable") {
		t.Fatalf("finite ancestor: %v", err)
	}
	if got := budgetState(t, store, root, agent, BudgetTokens); got.Used != 0 || got.Reserved != 0 {
		t.Fatalf("denied call changed root: %+v", got)
	}
	if _, err := store.AdmitModelCall(t.Context(), root, agent, attempt); err != nil {
		t.Fatalf("descendant cap stopped parent: %v", err)
	}
}

func TestModelConcurrentAdmissionRespectsSharedAncestor(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	for _, id := range []string{"a", "b"} {
		admitTestChild(t, store, root, agent, id)
	}
	if err := store.SetBudgetLimit(t.Context(), root, "", BudgetTokens, 20); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var admitted atomic.Int64
	for i := range 12 {
		wg.Go(func() {
			target := "a"
			if i%2 != 0 {
				target = "b"
			}
			_, err := store.AdmitModelCall(t.Context(), root, target, modelAttempt(strconv.Itoa(i), 5, 5))
			if err == nil {
				admitted.Add(1)
			} else if !errors.Is(err, capability.ErrDenied) {
				t.Errorf("admission: %v", err)
			}
		})
	}
	wg.Wait()
	if admitted.Load() != 2 {
		t.Fatalf("admitted %d calls, want 2", admitted.Load())
	}
	if tokens := budgetState(t, store, root, agent, BudgetTokens); tokens.Reserved != 20 {
		t.Fatalf("shared reserve: %+v", tokens)
	}
	if err := store.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if tokens := budgetState(t, store, root, agent, BudgetTokens); tokens.Used != 0 || tokens.Uncertain != 20 || tokens.Reserved != 0 {
		t.Fatalf("shared recovery: %+v", tokens)
	}
}

func TestModelAccountingDoesNotConsumeAgentStorageAllowances(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	before := map[BudgetKind]BudgetState{}
	for _, kind := range []BudgetKind{BudgetRecordCount, BudgetDurableBytes} {
		state := budgetState(t, store, root, agent, kind)
		before[kind] = state
		if err := store.SetBudgetLimit(t.Context(), root, "", kind, state.Used+state.Reserved); err != nil {
			t.Fatal(err)
		}
	}
	permit, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("internal-bookkeeping", 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SettleModelCall(t.Context(), root, permit.ID, llm.ModelAttemptResult{Dispatched: true}); err != nil {
		t.Fatal(err)
	}
	for kind, previous := range before {
		state := budgetState(t, store, root, agent, kind)
		if state.Used != previous.Used || state.Reserved != previous.Reserved {
			t.Fatalf("internal accounting charged %s: before=%+v after=%+v", kind, previous, state)
		}
	}
}

func TestModelAdmissionAdvancesAccountingRevisionOnce(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	admitTestChild(t, store, root, agent, "child")
	before, err := store.ModelAccounting(t.Context(), root, "", true)
	if err != nil {
		t.Fatal(err)
	}
	attempt := modelAttempt("revision", 1, 1)
	permit, err := store.AdmitModelCall(t.Context(), root, "child", attempt)
	if err != nil {
		t.Fatal(err)
	}
	started, err := store.ModelAccounting(t.Context(), root, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if started.Revision <= before.Revision || started.PendingCalls != 1 {
		t.Fatalf("pending revision before=%+v after=%+v", before, started)
	}
	if repeated, err := store.AdmitModelCall(t.Context(), root, "child", attempt); err != nil || repeated != permit {
		t.Fatalf("duplicate admission=%+v error=%v", repeated, err)
	}
	duplicate, err := store.ModelAccounting(t.Context(), root, "", true)
	if err != nil || duplicate != started {
		t.Fatalf("duplicate changed accounting=%+v error=%v", duplicate, err)
	}
	own, err := store.ModelAccounting(t.Context(), root, agent, false)
	if err != nil || own.PendingCalls != 0 {
		t.Fatalf("parent own view leaked child=%+v error=%v", own, err)
	}
	if _, err := store.SettleModelCall(t.Context(), root, permit.ID, llm.ModelAttemptResult{}); err != nil {
		t.Fatal(err)
	}
	settled, err := store.ModelAccounting(t.Context(), root, "", true)
	if err != nil || settled.Revision <= started.Revision || settled.PendingCalls != 0 {
		t.Fatalf("settled revision=%+v error=%v", settled, err)
	}
}

func TestModelUnknownHistoricalCostCannotAcquireFiniteCap(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	attempt := modelAttempt("unknown-history", 1, 1)
	attempt.Pricing = llm.Pricing{}
	permit, err := store.AdmitModelCall(t.Context(), root, agent, attempt)
	if err != nil {
		t.Fatal(err)
	}
	assertBlocked := func() {
		t.Helper()
		for _, amount := range []int64{0, 100} {
			if err := store.SetBudgetLimit(t.Context(), root, "", BudgetCost, amount); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("set cap over unknown history: %v", err)
			}
			if _, err := store.CapBudget(t.Context(), root, agent, agent, BudgetCost, amount); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("cap over unknown history: %v", err)
			}
		}
		if cost := budgetState(t, store, root, agent, BudgetCost); cost.Limit != nil {
			t.Fatalf("rejected cap changed limit: %+v", cost)
		}
	}
	assertBlocked()
	if err := store.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertBlocked()
	zero := 0.0
	if _, err := store.SettleModelCall(t.Context(), root, permit.ID, llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Cost: &zero}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetBudgetLimit(t.Context(), root, "", BudgetCost, 0); err != nil {
		t.Fatalf("known charge did not restore cap: %v", err)
	}
	if cost := budgetState(t, store, root, agent, BudgetCost); cost.Limit == nil || *cost.Limit != 0 || cost.Incomplete {
		t.Fatalf("resolved cost: %+v", cost)
	}
}

func TestModelKnownFreeCostRemainsCompleteWithoutUsage(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	if err := store.SetBudgetLimit(t.Context(), root, "", BudgetCost, 0); err != nil {
		t.Fatal(err)
	}
	attempt := modelAttempt("free-missing-usage", 1, 1)
	attempt.Pricing = llm.Pricing{Prompt: "0", Completion: "0", InputCacheRead: "0"}
	permit, err := store.AdmitModelCall(t.Context(), root, agent, attempt)
	if err != nil {
		t.Fatal(err)
	}
	settlement, err := store.SettleModelCall(t.Context(), root, permit.ID, llm.ModelAttemptResult{Dispatched: true})
	if err != nil || settlement.Exhausted {
		t.Fatalf("free settlement: %+v error=%v", settlement, err)
	}
	cost := budgetState(t, store, root, agent, BudgetCost)
	if cost.Used != 0 || cost.Uncertain != 0 || cost.Incomplete {
		t.Fatalf("free cost uncertainty: %+v", cost)
	}
	tokens := budgetState(t, store, root, agent, BudgetTokens)
	if tokens.Used != 0 || tokens.Uncertain != 2 || !tokens.Incomplete {
		t.Fatalf("missing tokens: %+v", tokens)
	}
	accounting, err := store.ModelAccounting(t.Context(), root, agent, false)
	if err != nil || accounting.EstimatedCostCalls != 1 || accounting.UnknownCostCalls != 0 || accounting.EstimatedCalls != 1 {
		t.Fatalf("free provenance: %+v error=%v", accounting, err)
	}
}
