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
		BudgetTokens:                 100_000_000,
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
		if _, err := store.AdmitAgent(context.Background(), AgentAdmission{
			RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: child, Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 8}},
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
	if err := store.db.QueryRowContext(context.Background(), `SELECT used_value,reserved_value FROM budgets WHERE root_id=? AND agent_id='left' AND kind=?`, rootID, BudgetTokens).
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
	if _, err := store.AdmitAgent(context.Background(), AgentAdmission{
		RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "parent", Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 9}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitAgent(context.Background(), AgentAdmission{
		RootID: rootID, ParentAgentID: "parent", ChildAgentID: "child", Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 8}},
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
		if err := store.db.QueryRowContext(context.Background(), `SELECT used_value,reserved_value FROM budgets WHERE root_id=? AND agent_id=? AND kind=?`, rootID, rowAgentID, BudgetTokens).
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
		if _, err := store.AdmitAgent(context.Background(), AgentAdmission{
			RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "child", Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 9}},
		}); err != nil {
			t.Fatal(err)
		}
		var limit int64
		if err := store.db.QueryRowContext(context.Background(), `SELECT limit_value FROM budgets WHERE root_id=? AND agent_id='child' AND kind=?`, rootID, BudgetTokens).Scan(&limit); err != nil || limit != 6 {
			t.Fatalf("child limit=%d err=%v", limit, err)
		}
	})

	t.Run("active children", func(t *testing.T) {
		store, rootID, rootAgentID := newSwarmFixture(t)
		if err := store.SetBudgetLimit(context.Background(), rootID, "", BudgetActiveChildren, 1); err != nil {
			t.Fatal(err)
		}
		admitTestChild(t, store, rootID, rootAgentID, "first")
		if _, err := store.AdmitAgent(context.Background(), AgentAdmission{RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "second"}); !errors.Is(err, capability.ErrDenied) {
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
		if _, err := store.AdmitAgent(context.Background(), AgentAdmission{RootID: rootID, ParentAgentID: "grandchild", ChildAgentID: "too-deep"}); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("depth error=%v", err)
		}
	})
}

func TestAgentTurnAndSubtreeTerminalizationReleaseLiveBudgetsOnce(t *testing.T) {
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
	left, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: "left", Kind: "submit", Payload: RuntimePayload{Data: []byte("left")}})
	if err != nil {
		t.Fatal(err)
	}
	right, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: "right", Kind: "submit", Payload: RuntimePayload{Data: []byte("right")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAgentTurn(context.Background(), rootID, "left", "turn-left"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAgentTurn(context.Background(), rootID, "right", "turn-right"); !errors.Is(err, capability.ErrDenied) {
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
	if _, err := store.StartAgentTurn(context.Background(), rootID, "right", "turn-right"); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAgentTurn(context.Background(), rootID, "right", AgentTurnCommit{TurnID: "turn-right", Status: "succeeded", AcknowledgedInbox: []int64{right.InboxSeq}}); err != nil {
		t.Fatal(err)
	}
	if got := budgetState(t, store, rootID, rootAgentID, BudgetActiveChildren); got.Reserved != 1 || got.Used != 0 {
		t.Fatalf("retained idle child was not counted after turn: %+v", got)
	}
	if _, err := store.TerminalizeSubtree(context.Background(), rootID, rootAgentID, "right", "stopped"); err != nil {
		t.Fatal(err)
	}
	if got := budgetState(t, store, rootID, rootAgentID, BudgetActiveChildren); got.Reserved != 0 {
		t.Fatalf("active children after stop=%+v", got)
	}
	_ = left
}

func TestCapBudgetEnforcesAncestryAndPreservesAccounting(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	if err := store.SetBudgetLimit(context.Background(), rootID, "", BudgetTokens, 20); err != nil {
		t.Fatal(err)
	}
	for _, child := range []string{"parent", "unrelated"} {
		if _, err := store.AdmitAgent(context.Background(), AgentAdmission{
			RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: child, Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 10}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.AdmitAgent(context.Background(), AgentAdmission{
		RootID: rootID, ParentAgentID: "parent", ChildAgentID: "target", Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 10}},
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
	if err := store.db.QueryRowContext(context.Background(), `SELECT count(*) FROM events WHERE root_id=? AND kind='budget.capped'`, rootID).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("budget cap events=%d err=%v", eventCount, err)
	}
	var payload []byte
	if err := store.db.QueryRowContext(context.Background(), `SELECT payload_inline FROM events WHERE root_id=? AND kind='budget.capped'`, rootID).Scan(&payload); err != nil {
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
	if err := store.db.QueryRowContext(context.Background(), `SELECT limit_value,used_value,reserved_value FROM budgets WHERE root_id=? AND agent_id='child' AND kind=?`, rootID, BudgetTokens).
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

func TestRecoveryReleasesDescendantOperationAndAgentTurnReservations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rootID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitAgent(context.Background(), AgentAdmission{
		RootID: rootID, ParentAgentID: authority.AgentID, ChildAgentID: "child",
		Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 10}}, Prompt: RuntimePayload{Data: []byte("work")},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAgentTurn(context.Background(), rootID, "child", "turn-child"); err != nil {
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
	if err := store.ReserveBudget(context.Background(), rootID, authority.AgentID, []capability.Reservation{
		{Kind: string(BudgetTokens), Amount: 3},
		{Kind: string(BudgetCost), Amount: 2},
		{Kind: string(BudgetActiveOperations), Amount: 1},
	}); err != nil {
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
	for _, kind := range []BudgetKind{BudgetTokens, BudgetCost, BudgetActiveOperations, BudgetActiveChildren, BudgetConcurrentChildTurns} {
		got := budgetState(t, store, rootID, authority.AgentID, kind)
		wantReserved := int64(0)
		if kind == BudgetActiveChildren {
			wantReserved = 1 // the retained agent survives its interrupted turn
		}
		if got.Reserved != wantReserved {
			t.Errorf("recovered %q=%+v", kind, got)
		}
		if kind == BudgetTokens && got.Used != 7 {
			t.Errorf("recovered cumulative usage=%+v", got)
		}
		if kind == BudgetCost && got.Used != 2 {
			t.Errorf("recovered monetary usage=%+v", got)
		}
	}
	var operationStatus, turnStatus string
	if err := store.db.QueryRowContext(context.Background(), `SELECT status FROM operations WHERE id='child-operation'`).Scan(&operationStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(context.Background(), `SELECT status FROM turns WHERE id='turn-child'`).Scan(&turnStatus); err != nil {
		t.Fatal(err)
	}
	if operationStatus != "interrupted" || turnStatus != "interrupted" {
		t.Fatalf("recovery statuses operation=%q turn=%q", operationStatus, turnStatus)
	}
}

func TestBudgetValidationAndAccountingDenials(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	ctx := context.Background()
	if err := store.SetBudgetLimit(ctx, rootID, "", BudgetTokens, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitAgent(ctx, AgentAdmission{
		RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "child", Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 10}},
	}); err != nil {
		t.Fatal(err)
	}

	for name, call := range map[string]func() error{
		"missing root":   func() error { return store.SetBudgetLimit(ctx, "", "", BudgetTokens, 1) },
		"missing kind":   func() error { return store.SetBudgetLimit(ctx, rootID, "", "", 1) },
		"negative limit": func() error { return store.SetBudgetLimit(ctx, rootID, "", BudgetTokens, -1) },
		"unknown root":   func() error { return store.SetBudgetLimit(ctx, "missing", "", BudgetTokens, 1) },
		"unknown agent":  func() error { return store.SetBudgetLimit(ctx, rootID, "missing", BudgetTokens, 1) },
		"missing row":    func() error { return store.SetBudgetLimit(ctx, rootID, "child", BudgetCost, 1) },
	} {
		if err := call(); err == nil {
			t.Fatalf("invalid %s budget was accepted", name)
		}
	}
	if err := store.SetBudgetLimit(ctx, rootID, "child", BudgetTokens, 8); err != nil {
		t.Fatal(err)
	}
	states, err := store.InspectBudgetsFor(ctx, rootID, rootAgentID, "child")
	if err != nil || len(states) == 0 {
		t.Fatalf("descendant budgets=%+v err=%v", states, err)
	}
	if _, err := store.CapBudget(ctx, rootID, "", "child", BudgetTokens, 1); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("invalid cap identity error=%v", err)
	}

	invalidReservations := [][]capability.Reservation{
		{{Kind: "", Amount: 1}},
		{{Kind: string(BudgetTokens), Amount: 0}},
		{{Kind: string(BudgetTokens), Amount: -1}},
		{{Kind: string(BudgetTokens), Amount: 1}, {Kind: string(BudgetTokens), Amount: 1}},
	}
	for _, reservations := range invalidReservations {
		if err := store.ReserveBudget(ctx, rootID, "child", reservations); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("invalid reservations %+v error=%v", reservations, err)
		}
	}
	reservation := []capability.Reservation{{Kind: string(BudgetTokens), Amount: 5, Consume: true}}
	if err := store.ReserveBudget(ctx, rootID, "child", reservation); err != nil {
		t.Fatal(err)
	}
	if err := store.SetBudgetLimit(ctx, rootID, "", BudgetTokens, 4); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("root limit below reservation error=%v", err)
	}
	if err := store.SetBudgetLimit(ctx, rootID, "child", BudgetTokens, 4); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("child limit below reservation error=%v", err)
	}
	invalidActual := [][]capability.Usage{
		{{Kind: string(BudgetTokens), Amount: 6}},
		{{Kind: "", Amount: 1}},
		{{Kind: string(BudgetTokens), Amount: -1}},
		{{Kind: string(BudgetTokens), Amount: 1}, {Kind: string(BudgetTokens), Amount: 1}},
		{{Kind: string(BudgetTokens), Amount: 1}, {Kind: string(BudgetCost), Amount: 1}},
	}
	for _, actual := range invalidActual {
		if err := store.ReconcileBudget(ctx, rootID, "child", reservation, actual); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("invalid actual %+v error=%v", actual, err)
		}
	}
	if err := store.ReleaseBudget(ctx, rootID, "child", []capability.Reservation{{Kind: string(BudgetTokens), Amount: 6}}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("oversized release error=%v", err)
	}
	if err := store.ReconcileBudget(ctx, rootID, "child", reservation, []capability.Usage{{Kind: string(BudgetTokens), Amount: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseBudget(ctx, rootID, "child", reservation); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("settled release error=%v", err)
	}
	if err := store.SetBudgetLimit(ctx, rootID, "", BudgetTokens, 2); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("limit below usage error=%v", err)
	}
	remaining := []capability.Reservation{{Kind: string(BudgetTokens), Amount: 5}}
	if err := store.ReserveBudget(ctx, rootID, "child", remaining); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveBudget(ctx, rootID, "child", []capability.Reservation{{Kind: string(BudgetTokens), Amount: 1}}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("exhausted child budget error=%v", err)
	}
	if err := store.ReleaseBudget(ctx, rootID, "child", remaining); err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE budgets SET used_value=limit_value+1 WHERE root_id=? AND agent_id='' AND kind=?`, rootID, BudgetTokens); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveBudget(ctx, rootID, "child", []capability.Reservation{{Kind: string(BudgetTokens), Amount: 1}}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("corrupt budget reserve error=%v", err)
	}
	if _, err := store.AdmitAgent(ctx, AgentAdmission{
		RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "corrupt-budget", Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 1}},
	}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("corrupt inherited budget error=%v", err)
	}
}

func TestBudgetAPIsReturnClosedStoreErrors(t *testing.T) {
	store, rootID, agentID := newSwarmFixture(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	reservation := []capability.Reservation{{Kind: string(BudgetTokens), Amount: 1}}
	for name, call := range map[string]func() error{
		"set":         func() error { return store.SetBudgetLimit(ctx, rootID, "", BudgetTokens, 1) },
		"inspect":     func() error { _, err := store.InspectBudgets(ctx, rootID, agentID); return err },
		"inspect for": func() error { _, err := store.InspectBudgetsFor(ctx, rootID, agentID, agentID); return err },
		"reserve":     func() error { return store.ReserveBudget(ctx, rootID, agentID, reservation) },
		"reconcile":   func() error { return store.ReconcileBudget(ctx, rootID, agentID, reservation, nil) },
		"release":     func() error { return store.ReleaseBudget(ctx, rootID, agentID, reservation) },
		"cap":         func() error { _, err := store.CapBudget(ctx, rootID, agentID, agentID, BudgetTokens, 1); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("closed store call succeeded")
			}
		})
	}
}

func TestBudgetAdditionalAccessAndCorruptionPaths(t *testing.T) {
	ctx := context.Background()
	t.Run("access and missing kinds", func(t *testing.T) {
		store, rootID, rootAgentID := newSwarmFixture(t)
		admitTestChild(t, store, rootID, rootAgentID, "child")
		for name, call := range map[string]func() error{
			"inspect root":           func() error { _, err := store.InspectBudgets(ctx, "", "child"); return err },
			"inspect agent":          func() error { _, err := store.InspectBudgets(ctx, rootID, ""); return err },
			"inspect missing target": func() error { _, err := store.InspectBudgetsFor(ctx, rootID, rootAgentID, "missing"); return err },
			"cap missing target": func() error {
				_, err := store.CapBudget(ctx, rootID, rootAgentID, "missing", BudgetTokens, 1)
				return err
			},
			"cap missing root kind": func() error {
				_, err := store.CapBudget(ctx, rootID, rootAgentID, rootAgentID, "missing", 1)
				return err
			},
			"cap missing child kind": func() error { _, err := store.CapBudget(ctx, rootID, rootAgentID, "child", "missing", 1); return err },
			"reserve missing kind": func() error {
				return store.ReserveBudget(ctx, rootID, "child", []capability.Reservation{{Kind: "missing", Amount: 1}})
			},
			"reconcile invalid reservation": func() error {
				return store.ReconcileBudget(ctx, rootID, "child", []capability.Reservation{{Amount: 1}}, nil)
			},
			"reconcile missing kind": func() error {
				return store.ReconcileBudget(ctx, rootID, "child", []capability.Reservation{{Kind: "missing", Amount: 1}}, nil)
			},
			"release invalid reservation": func() error {
				return store.ReleaseBudget(ctx, rootID, "child", []capability.Reservation{{Amount: 1}})
			},
			"release missing kind": func() error {
				return store.ReleaseBudget(ctx, rootID, "child", []capability.Reservation{{Kind: "missing", Amount: 1}})
			},
		} {
			t.Run(name, func(t *testing.T) {
				if err := call(); err == nil {
					t.Fatal("invalid budget call succeeded")
				}
			})
		}
		if state, err := store.CapBudget(ctx, rootID, rootAgentID, rootAgentID, BudgetTokens, 100); err != nil || state.Limit != 100 {
			t.Fatalf("root cap=%+v err=%v", state, err)
		}
	})

	t.Run("invalid inherited accounting", func(t *testing.T) {
		store, rootID, rootAgentID := newSwarmFixture(t)
		admitTestChild(t, store, rootID, rootAgentID, "child")
		if _, err := store.db.ExecContext(ctx, `UPDATE budgets SET used_value=limit_value+1 WHERE root_id=? AND agent_id='' AND kind=?`, rootID, BudgetCost); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CapBudget(ctx, rootID, rootAgentID, "child", BudgetCost, 1); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("corrupt inherited cap error=%v", err)
		}
	})

	t.Run("invalid stored type", func(t *testing.T) {
		store, rootID, rootAgentID := newSwarmFixture(t)
		if _, err := store.db.ExecContext(ctx, `UPDATE budgets SET limit_value='bad' WHERE root_id=? AND agent_id='' AND kind=?`, rootID, BudgetTokens); err != nil {
			t.Fatal(err)
		}
		if _, err := store.InspectBudgets(ctx, rootID, rootAgentID); err == nil {
			t.Fatal("invalid stored budget was inspected")
		}
	})

	t.Run("missing rows", func(t *testing.T) {
		store, rootID, rootAgentID := newSwarmFixture(t)
		if _, err := store.db.ExecContext(ctx, `DELETE FROM budgets WHERE root_id=?`, rootID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.InspectBudgets(ctx, rootID, rootAgentID); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("missing budgets error=%v", err)
		}
	})
}
