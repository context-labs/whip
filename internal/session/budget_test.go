package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

func budgetState(t *testing.T, store *Store, rootID, agentID string, kind BudgetKind) BudgetState {
	t.Helper()
	states, err := store.InspectBudgets(context.Background(), rootID, agentID)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		if state.Kind == kind {
			return state
		}
	}
	t.Fatalf("budget %q not found in %+v", kind, states)
	return BudgetState{}
}

func TestDefaultRootBudgetLimits(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	want := map[BudgetKind]int64{
		BudgetTokens:                 DefaultRootTokens,
		BudgetCost:                   DefaultRootCostMicros,
		BudgetElapsed:                DefaultRootElapsedMillis,
		BudgetDurableBytes:           DefaultRootDurableBytes,
		BudgetRecordCount:            DefaultRootRecords,
		BudgetSchedulesSubscriptions: DefaultRootSchedulesSubscriptions,
		BudgetActiveOperations:       DefaultRootActiveOperations,
		BudgetActiveChildren:         DefaultTreeActiveChildren,
		BudgetConcurrentChildTurns:   DefaultTreeConcurrentChildTurns,
		BudgetDepth:                  DefaultTreeDepth,
	}
	states, err := store.InspectBudgets(context.Background(), rootID, rootAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != len(want) {
		t.Fatalf("default budgets=%+v", states)
	}
	for _, state := range states {
		if state.Limit != want[state.Kind] || state.Used != 0 || state.Reserved != 0 || state.Remaining != state.Limit {
			t.Errorf("default %q=%+v want limit=%d", state.Kind, state, want[state.Kind])
		}
	}
}

func TestBudgetKindsExhaustIndependently(t *testing.T) {
	for _, kind := range []BudgetKind{
		BudgetTokens, BudgetCost, BudgetElapsed, BudgetDurableBytes, BudgetRecordCount,
		BudgetSchedulesSubscriptions, BudgetActiveOperations,
	} {
		t.Run(string(kind), func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			if err := store.SetBudgetLimit(context.Background(), rootID, "", kind, 2); err != nil {
				t.Fatal(err)
			}
			reservation := capability.Reservation{Kind: string(kind), Amount: 2, Consume: kind != BudgetActiveOperations}
			if err := store.ReserveBudget(context.Background(), rootID, rootAgentID, []capability.Reservation{reservation}); err != nil {
				t.Fatal(err)
			}
			if err := store.ReserveBudget(context.Background(), rootID, rootAgentID, []capability.Reservation{{Kind: string(kind), Amount: 1}}); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("exhaustion error=%v", err)
			}
			actual := []capability.Usage{{Kind: string(kind), Amount: 1}}
			if kind == BudgetActiveOperations {
				actual = nil
			}
			if err := store.ReconcileBudget(context.Background(), rootID, rootAgentID, []capability.Reservation{reservation}, actual); err != nil {
				t.Fatal(err)
			}
			state := budgetState(t, store, rootID, rootAgentID, kind)
			wantUsed := int64(1)
			if kind == BudgetActiveOperations {
				wantUsed = 0
			}
			if state.Used != wantUsed || state.Reserved != 0 {
				t.Fatalf("reconciled state=%+v", state)
			}
		})
	}
}

func TestBudgetDescendantsRollUpAndReconcileConservatively(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	if err := store.SetBudgetLimit(context.Background(), rootID, "", BudgetTokens, 10); err != nil {
		t.Fatal(err)
	}
	for _, child := range []string{"left", "right"} {
		if _, err := store.AdmitChild(context.Background(), ChildAdmission{
			RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: child, ExecutionID: "exec-" + child,
			Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 8}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	left := []capability.Reservation{{Kind: string(BudgetTokens), Amount: 6, Consume: true}}
	if err := store.ReserveBudget(context.Background(), rootID, "left", left); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileBudget(context.Background(), rootID, "left", left, []capability.Usage{{Kind: string(BudgetTokens), Amount: 4}}); err != nil {
		t.Fatal(err)
	}
	right := []capability.Reservation{{Kind: string(BudgetTokens), Amount: 6}}
	if err := store.ReserveBudget(context.Background(), rootID, "right", right); err != nil {
		t.Fatal(err)
	}
	if got := budgetState(t, store, rootID, rootAgentID, BudgetTokens); got.Used != 4 || got.Reserved != 6 {
		t.Fatalf("root roll-up=%+v", got)
	}
	var leftUsed, leftReserved int64
	if err := store.db.QueryRow(`SELECT used_value,reserved_value FROM budgets WHERE root_id=? AND agent_id='left' AND kind=?`, rootID, BudgetTokens).
		Scan(&leftUsed, &leftReserved); err != nil || leftUsed != 4 || leftReserved != 0 {
		t.Fatalf("completed sibling used=%d reserved=%d err=%v", leftUsed, leftReserved, err)
	}
	if err := store.ReconcileBudget(context.Background(), rootID, "right", right, nil); err != nil {
		t.Fatal(err)
	}
	if got := budgetState(t, store, rootID, rootAgentID, BudgetTokens); got.Used != 10 || got.Reserved != 0 || got.Remaining != 0 {
		t.Fatalf("missing actual did not consume reservation: %+v", got)
	}
}

func TestCapabilityBudgetUsesAgentAncestryAndCompletionUsage(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	if err := store.SetBudgetLimit(context.Background(), rootID, "", BudgetTokens, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitChild(context.Background(), ChildAdmission{
		RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "parent", ExecutionID: "exec-parent",
		Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 9}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitChild(context.Background(), ChildAdmission{
		RootID: rootID, ParentAgentID: "parent", ChildAgentID: "child", ExecutionID: "exec-child",
		Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 8}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.IssueCapability(context.Background(), capability.Grant{ID: "child-read", RootID: rootID, AgentID: "child", Operations: []string{"read"}}); err != nil {
		t.Fatal(err)
	}
	admission := capability.Admission{Request: capability.Request{
		RootID: rootID, AgentID: "child", CapabilityID: "child-read", OperationID: "known", Operation: "read", TraceID: "trace-known",
		Reservations: []capability.Reservation{{Kind: string(BudgetTokens), Amount: 6}},
	}}
	ticket, err := store.Begin(context.Background(), admission)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(context.Background(), capability.Completion{
		Admission: admission, LeaseID: ticket.LeaseID, Status: capability.StatusSucceeded,
		Usage: []capability.Usage{{Kind: string(BudgetTokens), Amount: 4}},
	}); err != nil {
		t.Fatal(err)
	}
	admission.Request.OperationID = "missing"
	admission.Request.TraceID = "trace-missing"
	admission.Request.Reservations[0].Amount = 2
	ticket, err = store.Begin(context.Background(), admission)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(context.Background(), capability.Completion{Admission: admission, LeaseID: ticket.LeaseID, Status: capability.StatusSucceeded}); err != nil {
		t.Fatal(err)
	}
	for agentID, want := range map[string]int64{rootAgentID: 6, "parent": 6, "child": 6} {
		var used, reserved int64
		rowAgentID := agentID
		if agentID == rootAgentID {
			rowAgentID = ""
		}
		if err := store.db.QueryRow(`SELECT used_value,reserved_value FROM budgets WHERE root_id=? AND agent_id=? AND kind=?`, rootID, rowAgentID, BudgetTokens).
			Scan(&used, &reserved); err != nil || used != want || reserved != 0 {
			t.Errorf("agent %q used=%d reserved=%d err=%v", agentID, used, reserved, err)
		}
	}
}

func TestChildBudgetClampingDepthAndActiveChildLimits(t *testing.T) {
	t.Run("requested limit clamps to parent remaining", func(t *testing.T) {
		store, rootID, rootAgentID := newSwarmFixture(t)
		if err := store.SetBudgetLimit(context.Background(), rootID, "", BudgetTokens, 10); err != nil {
			t.Fatal(err)
		}
		held := []capability.Reservation{{Kind: string(BudgetTokens), Amount: 4}}
		if err := store.ReserveBudget(context.Background(), rootID, rootAgentID, held); err != nil {
			t.Fatal(err)
		}
		if _, err := store.AdmitChild(context.Background(), ChildAdmission{
			RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "child", ExecutionID: "exec-child",
			Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 9}},
		}); err != nil {
			t.Fatal(err)
		}
		var limit int64
		if err := store.db.QueryRow(`SELECT limit_value FROM budgets WHERE root_id=? AND agent_id='child' AND kind=?`, rootID, BudgetTokens).Scan(&limit); err != nil || limit != 6 {
			t.Fatalf("child limit=%d err=%v", limit, err)
		}
	})

	t.Run("active children", func(t *testing.T) {
		store, rootID, rootAgentID := newSwarmFixture(t)
		if err := store.SetBudgetLimit(context.Background(), rootID, "", BudgetActiveChildren, 1); err != nil {
			t.Fatal(err)
		}
		admitTestChild(t, store, rootID, rootAgentID, "first")
		if _, err := store.AdmitChild(context.Background(), ChildAdmission{RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "second", ExecutionID: "exec-second"}); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("second child error=%v", err)
		}
	})

	t.Run("depth", func(t *testing.T) {
		store, rootID, rootAgentID := newSwarmFixture(t)
		if err := store.SetBudgetLimit(context.Background(), rootID, "", BudgetDepth, 2); err != nil {
			t.Fatal(err)
		}
		admitTestChild(t, store, rootID, rootAgentID, "child")
		admitTestChild(t, store, rootID, "child", "grandchild")
		if _, err := store.AdmitChild(context.Background(), ChildAdmission{RootID: rootID, ParentAgentID: "grandchild", ChildAgentID: "too-deep", ExecutionID: "exec-too-deep"}); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("depth error=%v", err)
		}
	})
}

func TestChildTurnAndSubtreeTerminalizationReleaseLiveBudgetsOnce(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	if err := store.SetBudgetLimit(context.Background(), rootID, "", BudgetConcurrentChildTurns, 1); err != nil {
		t.Fatal(err)
	}
	admitTestChild(t, store, rootID, rootAgentID, "left")
	admitTestChild(t, store, rootID, rootAgentID, "right")
	if err := store.SetBudgetLimit(context.Background(), rootID, "", BudgetTokens, 10); err != nil {
		t.Fatal(err)
	}
	consumed := []capability.Reservation{{Kind: string(BudgetTokens), Amount: 2}}
	if err := store.ReserveBudget(context.Background(), rootID, "left", consumed); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileBudget(context.Background(), rootID, "left", consumed, []capability.Usage{{Kind: string(BudgetTokens), Amount: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := store.IssueCapability(context.Background(), capability.Grant{ID: "left-read", RootID: rootID, AgentID: "left", Operations: []string{"read"}}); err != nil {
		t.Fatal(err)
	}
	operation := capability.Admission{Request: capability.Request{
		RootID: rootID, AgentID: "left", CapabilityID: "left-read", OperationID: "left-operation", Operation: "read", TraceID: "trace",
		Reservations: []capability.Reservation{{Kind: string(BudgetTokens), Amount: 4}},
	}}
	if _, err := store.Begin(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartChildTurn(context.Background(), rootID, rootAgentID, "exec-left"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartChildTurn(context.Background(), rootID, rootAgentID, "exec-right"); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("concurrent turn error=%v", err)
	}
	if _, err := store.TerminalizeSubtree(context.Background(), rootID, rootAgentID, "left", "stopped"); err != nil {
		t.Fatal(err)
	}
	if got := budgetState(t, store, rootID, rootAgentID, BudgetConcurrentChildTurns); got.Reserved != 0 || got.Used != 0 {
		t.Fatalf("concurrency after stop=%+v", got)
	}
	if got := budgetState(t, store, rootID, rootAgentID, BudgetActiveChildren); got.Reserved != 1 {
		t.Fatalf("active children after stop=%+v", got)
	}
	if got := budgetState(t, store, rootID, rootAgentID, BudgetTokens); got.Used != 6 || got.Reserved != 0 {
		t.Fatalf("terminal operation accounting=%+v", got)
	}
	if _, err := store.TerminalizeSubtree(context.Background(), rootID, rootAgentID, "left", "stopped"); !errors.Is(err, ErrAgentTerminal) {
		t.Fatalf("second terminalization error=%v", err)
	}
	if _, err := store.StartChildTurn(context.Background(), rootID, rootAgentID, "exec-right"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishChildTurn(context.Background(), rootID, rootAgentID, "exec-right", "succeeded"); err != nil {
		t.Fatal(err)
	}
	if got := budgetState(t, store, rootID, rootAgentID, BudgetActiveChildren); got.Reserved != 0 || got.Used != 0 {
		t.Fatalf("active children after finish=%+v", got)
	}
}

func TestCapBudgetEnforcesAncestryAndPreservesAccounting(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	if err := store.SetBudgetLimit(context.Background(), rootID, "", BudgetTokens, 20); err != nil {
		t.Fatal(err)
	}
	for _, child := range []string{"parent", "unrelated"} {
		if _, err := store.AdmitChild(context.Background(), ChildAdmission{
			RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: child, ExecutionID: "exec-" + child,
			Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 10}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.AdmitChild(context.Background(), ChildAdmission{
		RootID: rootID, ParentAgentID: "parent", ChildAgentID: "target", ExecutionID: "exec-target",
		Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 10}},
	}); err != nil {
		t.Fatal(err)
	}
	used := []capability.Reservation{{Kind: string(BudgetTokens), Amount: 3, Consume: true}}
	if err := store.ReserveBudget(context.Background(), rootID, "target", used); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileBudget(context.Background(), rootID, "target", used, []capability.Usage{{Kind: string(BudgetTokens), Amount: 3}}); err != nil {
		t.Fatal(err)
	}
	held := []capability.Reservation{{Kind: string(BudgetTokens), Amount: 2}}
	if err := store.ReserveBudget(context.Background(), rootID, "target", held); err != nil {
		t.Fatal(err)
	}
	state, err := store.CapBudget(context.Background(), rootID, "parent", "target", BudgetTokens, 5)
	if err != nil || state.Limit != 5 || state.Used != 3 || state.Reserved != 2 || state.Remaining != 0 {
		t.Fatalf("cap state=%+v err=%v", state, err)
	}
	if _, err := store.CapBudget(context.Background(), rootID, "parent", "target", BudgetTokens, 4); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("cap below accounting error=%v", err)
	}
	if _, err := store.CapBudget(context.Background(), rootID, "unrelated", "target", BudgetTokens, 5); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("unrelated cap error=%v", err)
	}
	if _, err := store.InspectBudgetsFor(context.Background(), rootID, "unrelated", "target"); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("unrelated inspection error=%v", err)
	}
	otherStore, otherRoot, otherAgent := newSwarmFixture(t)
	_ = otherStore
	if _, err := store.CapBudget(context.Background(), rootID, otherAgent, "target", BudgetTokens, 5); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("cross-root cap from %s error=%v", otherRoot, err)
	}
	if _, err := store.InspectBudgetsFor(context.Background(), rootID, otherAgent, "target"); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("cross-root inspection from %s error=%v", otherRoot, err)
	}
	var eventCount int
	if err := store.db.QueryRow(`SELECT count(*) FROM events WHERE root_id=? AND kind='budget.capped'`, rootID).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("budget cap events=%d err=%v", eventCount, err)
	}
	var payload []byte
	if err := store.db.QueryRow(`SELECT payload_inline FROM events WHERE root_id=? AND kind='budget.capped'`, rootID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var event actorEvent
	if err := json.Unmarshal(payload, &event); err != nil || event.AgentID != "target" || event.BudgetKind != string(BudgetTokens) || event.Limit != 5 {
		t.Fatalf("budget cap event=%+v err=%v", event, err)
	}
}

func TestCapBudgetCreatesDescendantLimitForInheritedKind(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	if err := store.SetBudgetLimit(context.Background(), rootID, "", BudgetTokens, 10); err != nil {
		t.Fatal(err)
	}
	held := []capability.Reservation{{Kind: string(BudgetTokens), Amount: 4}}
	if err := store.ReserveBudget(context.Background(), rootID, rootAgentID, held); err != nil {
		t.Fatal(err)
	}
	admitTestChild(t, store, rootID, rootAgentID, "child")

	state, err := store.CapBudget(context.Background(), rootID, rootAgentID, "child", BudgetTokens, 8)
	if err != nil || state.Limit != 6 || state.Used != 0 || state.Reserved != 0 || state.Remaining != 6 {
		t.Fatalf("inherited cap state=%+v err=%v", state, err)
	}
	var limit, used, reserved int64
	if err := store.db.QueryRow(`SELECT limit_value,used_value,reserved_value FROM budgets WHERE root_id=? AND agent_id='child' AND kind=?`, rootID, BudgetTokens).
		Scan(&limit, &used, &reserved); err != nil || limit != 6 || used != 0 || reserved != 0 {
		t.Fatalf("persisted inherited cap limit=%d used=%d reserved=%d err=%v", limit, used, reserved, err)
	}
	if err := store.ReleaseBudget(context.Background(), rootID, rootAgentID, held); err != nil {
		t.Fatal(err)
	}
	if got := budgetState(t, store, rootID, "child", BudgetTokens); got.Limit != 6 || got.Remaining != 6 {
		t.Fatalf("released ancestor allowance increased child authority: %+v", got)
	}
	if _, err := store.CapBudget(context.Background(), rootID, rootAgentID, "child", BudgetTokens, 8); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("cap increased child authority: %v", err)
	}
	if err := store.ReserveBudget(context.Background(), rootID, "child", []capability.Reservation{{Kind: string(BudgetTokens), Amount: 6}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CapBudget(context.Background(), rootID, rootAgentID, "child", BudgetTokens, 5); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("cap fell below child accounting: %v", err)
	}
	if err := store.ReserveBudget(context.Background(), rootID, "child", []capability.Reservation{{Kind: string(BudgetTokens), Amount: 1}}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("cap did not bind later admission: %v", err)
	}
}

func TestRecoveryReleasesDescendantOperationAndChildReservations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rootID, err := store.Create(t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitChild(context.Background(), ChildAdmission{
		RootID: rootID, ParentAgentID: authority.AgentID, ChildAgentID: "child", ExecutionID: "exec-child",
		Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 10}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartChildTurn(context.Background(), rootID, authority.AgentID, "exec-child"); err != nil {
		t.Fatal(err)
	}
	if err := store.IssueCapability(context.Background(), capability.Grant{ID: "child-read", RootID: rootID, AgentID: "child", Operations: []string{"read"}}); err != nil {
		t.Fatal(err)
	}
	admission := capability.Admission{Request: capability.Request{
		RootID: rootID, AgentID: "child", CapabilityID: "child-read", OperationID: "child-operation", Operation: "read", TraceID: "trace",
		Reservations: []capability.Reservation{{Kind: string(BudgetTokens), Amount: 4, Consume: true}},
	}}
	if _, err := store.Begin(context.Background(), admission); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []BudgetKind{BudgetTokens, BudgetActiveChildren, BudgetConcurrentChildTurns} {
		got := budgetState(t, store, rootID, authority.AgentID, kind)
		if got.Reserved != 0 {
			t.Errorf("recovered %q=%+v", kind, got)
		}
		if kind == BudgetTokens && got.Used != 4 {
			t.Errorf("recovered cumulative usage=%+v", got)
		}
	}
	var operationStatus, executionStatus string
	if err := store.db.QueryRow(`SELECT status FROM operations WHERE id='child-operation'`).Scan(&operationStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT status FROM child_executions WHERE id='exec-child'`).Scan(&executionStatus); err != nil {
		t.Fatal(err)
	}
	if operationStatus != "interrupted" || executionStatus != "interrupted" {
		t.Fatalf("recovery statuses operation=%q execution=%q", operationStatus, executionStatus)
	}
}
