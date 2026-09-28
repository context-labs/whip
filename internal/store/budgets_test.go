package store

import (
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func budgetState(t *testing.T, s *Store, owner session.SessionID, kind session.BudgetKind) session.Budget {
	t.Helper()
	budgets, err := s.Budgets(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(budgets) != 4 {
		t.Fatalf("budget projection: %+v", budgets)
	}
	for _, budget := range budgets {
		if budget.Kind == kind {
			return budget
		}
	}
	t.Fatalf("missing budget %s", kind)
	return session.Budget{}
}

func budgetLimit(t *testing.T, s *Store, owner session.SessionID, kind session.BudgetKind, limit int64) session.Budget {
	t.Helper()
	current := budgetState(t, s, owner, kind)
	result, err := s.SetBudget(t.Context(), owner, current.Revision, session.BudgetLimit{Kind: kind, Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func budgetRequest(turn session.TurnID, id string) session.ModelAttemptSpec {
	request := attemptRequest(turn, id)
	request.Request.InputTokenBound = new(int64(10))
	request.Request.MaxOutputTokens = 20
	request.Request.TimeoutMillis = 100
	request.Request.Prices = session.ModelPrices{
		Input: new(int64(1000000)), CachedInput: new(int64(3000000)),
		Output: new(int64(2000000)), Reasoning: new(int64(5000000)), CachedOutput: new(int64(4000000)),
	}
	return request
}

func budgetTurn(t *testing.T, s *Store, owner session.SessionID) session.TurnID {
	t.Helper()
	submit(t, s, owner, string(owner))
	return claim(t, s, owner).Turn.ID
}

func TestBudgetsCompetingSiblingsReserveAllAncestorsAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	_, root := create(t, s, session.DefaultTreePolicy())
	for kind, limit := range map[session.BudgetKind]int64{
		session.BudgetModelCalls: 1, session.BudgetModelTokens: 30, session.BudgetModelCostNanoUSD: 130, session.BudgetModelElapsedMillis: 100,
	} {
		budgetLimit(t, s, root.ID, kind, limit)
	}
	left := spawnChildTest(t, s, "left", childRequest(root.ID))
	right := spawnChildTest(t, s, "right", childRequest(root.ID))
	leftTurn, rightTurn := claim(t, s, left.Session.ID).Turn.ID, claim(t, s, right.Session.ID).Turn.ID
	requests := []session.ModelAttemptSpec{budgetRequest(leftTurn, "left"), budgetRequest(rightTurn, "right")}
	results := make([]session.ModelAttempt, 2)
	errorsFound := make([]error, 2)
	var workers sync.WaitGroup
	start := make(chan struct{})
	for i := range 2 {
		workers.Go(func() {
			<-start
			db := s
			if i == 1 {
				db = other
			}
			results[i], errorsFound[i] = db.ReserveModelAttempt(t.Context(), requests[i])
		})
	}
	close(start)
	workers.Wait()
	winner, loser := 0, 1
	if errorsFound[0] != nil {
		winner, loser = 1, 0
	}
	if errorsFound[winner] != nil || !errors.Is(errorsFound[loser], ErrLimit) || count(t, s, "model_attempts") != 1 || count(t, s, "attempt_budget_ancestors") != 2 {
		t.Fatalf("sibling reservations were not exclusive: %v", errorsFound)
	}
	for kind, want := range map[session.BudgetKind]int64{session.BudgetModelCalls: 1, session.BudgetModelTokens: 30, session.BudgetModelCostNanoUSD: 130, session.BudgetModelElapsedMillis: 100} {
		state := budgetState(t, s, root.ID, kind)
		if state.Reserved != want || state.Used != 0 || state.Uncertain != 0 || state.Incomplete {
			t.Fatalf("reservation %s: %+v", kind, state)
		}
	}
	if _, err := other.SetBudget(t.Context(), root.ID, 1, session.BudgetLimit{Kind: session.BudgetModelCalls, Limit: new(int64(0))}); !errors.Is(err, ErrLimit) {
		t.Fatalf("tightened below existing reservation: %v", err)
	}
	if _, err := s.SettleModelAttempt(t.Context(), results[winner].ID, session.ModelAttemptResult{State: session.AttemptCancelled}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := other.ReserveModelAttempt(t.Context(), requests[loser]); err != nil {
		t.Fatalf("confirmed undispatched cancellation did not release allowance: %v", err)
	}
}

func TestBudgetSettlementRetainsPartialEvidenceAndOverages(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	turn := budgetTurn(t, s, root.ID)
	for kind, limit := range map[session.BudgetKind]int64{session.BudgetModelTokens: 30, session.BudgetModelCostNanoUSD: 130, session.BudgetModelElapsedMillis: 100} {
		budgetLimit(t, s, root.ID, kind, limit)
	}
	attempt := reserveTest(t, s, budgetRequest(turn, "partial"))
	dispatchTest(t, s, attempt.ID)
	outcome := session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: new(int64(40))}, ReportedCostNanoUSD: new(int64(150)), ElapsedMillis: new(int64(120))}
	draft := session.MessageDraft{ID: "budget-answer", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "answer"}}}
	execTest(t, s, `CREATE TRIGGER fail_budget_settlement BEFORE UPDATE ON model_attempts WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'settlement fault'); END`)
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, &draft); err == nil {
		t.Fatal("fault did not roll back settlement")
	}
	if state := budgetState(t, s, root.ID, session.BudgetModelTokens); state.Reserved != 30 || state.Used != 0 || count(t, s, "messages") != 1 {
		t.Fatalf("message and budget split: %+v", state)
	}
	execTest(t, s, "DROP TRIGGER fail_budget_settlement")
	for range 2 {
		if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, &draft); err != nil {
			t.Fatal(err)
		}
	}
	if state := budgetState(t, s, root.ID, session.BudgetModelTokens); state.Used != 40 || state.Uncertain != 20 || state.Reserved != 0 || state.Incomplete {
		t.Fatalf("partial token components or overage lost: %+v", state)
	}
	if state := budgetState(t, s, root.ID, session.BudgetModelCostNanoUSD); state.Used != 150 || state.Uncertain != 0 {
		t.Fatalf("independent provider cost lost: %+v", state)
	}
	if state := budgetState(t, s, root.ID, session.BudgetModelElapsedMillis); state.Used != 120 || state.Uncertain != 0 {
		t.Fatalf("elapsed overage lost: %+v", state)
	}
	if _, err := s.ReserveModelAttempt(t.Context(), budgetRequest(turn, "blocked")); !errors.Is(err, ErrLimit) {
		t.Fatalf("overage permitted another reservation: %v", err)
	}
	if count(t, s, "model_attempts") != 1 || count(t, s, "messages") != 2 {
		t.Fatal("retry duplicated attempt or message")
	}
}

func TestBudgetUnknownBoundsPricesAndFreeRequests(t *testing.T) {
	for _, kind := range []session.BudgetKind{session.BudgetModelTokens, session.BudgetModelCostNanoUSD} {
		t.Run(string(kind), func(t *testing.T) {
			s := fresh(t)
			_, root := create(t, s, session.DefaultTreePolicy())
			turn := budgetTurn(t, s, root.ID)
			budgetLimit(t, s, root.ID, kind, 1000000)
			request := budgetRequest(turn, "unknown")
			request.Request.InputTokenBound = nil
			if _, err := s.ReserveModelAttempt(t.Context(), request); !errors.Is(err, ErrLimit) {
				t.Fatalf("unknown input bound accepted under finite %s: %v", kind, err)
			}
			if kind == session.BudgetModelCostNanoUSD {
				request.Request.InputTokenBound = new(int64(10))
				request.Request.Prices.Reasoning = nil
				if _, err := s.ReserveModelAttempt(t.Context(), request); !errors.Is(err, ErrLimit) {
					t.Fatalf("unknown category price accepted: %v", err)
				}
				zero := new(int64(0))
				request.Request.InputTokenBound = nil
				request.Request.Prices = session.ModelPrices{Input: zero, Output: zero, Reasoning: zero, CachedInput: zero, CachedOutput: zero}
				reserveTest(t, s, request)
				if state := budgetState(t, s, root.ID, kind); state.Reserved != 0 || state.Incomplete {
					t.Fatalf("known free request not bounded at zero: %+v", state)
				}
			}
		})
	}
}

func TestBudgetRecoveryReleasesOnlyUndispatchedAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, root := create(t, s, session.DefaultTreePolicy())
	turn := budgetTurn(t, s, root.ID)
	reserved := reserveTest(t, s, budgetRequest(turn, "reserved"))
	dispatched := reserveTest(t, s, budgetRequest(turn, "dispatched"))
	dispatchTest(t, s, dispatched.ID)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	for range 2 {
		if _, err := reopened.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		for kind, want := range map[session.BudgetKind]int64{session.BudgetModelTokens: 30, session.BudgetModelCostNanoUSD: 130, session.BudgetModelElapsedMillis: 100} {
			state := budgetState(t, reopened, root.ID, kind)
			if state.Reserved != 0 || state.Used != 0 || state.Uncertain != want || state.Incomplete {
				t.Fatalf("recovery %s: %+v", kind, state)
			}
		}
		if calls := budgetState(t, reopened, root.ID, session.BudgetModelCalls); calls.Used != 1 || calls.Reserved != 0 || calls.Uncertain != 0 {
			t.Fatalf("recovery call charge: %+v", calls)
		}
	}
	cancelled, err := reopened.ModelAttempt(t.Context(), reserved.ID)
	if err != nil || cancelled.State != session.AttemptCancelled {
		t.Fatalf("reserved recovery: %+v %v", cancelled, err)
	}
	budgetLimit(t, reopened, root.ID, session.BudgetModelTokens, 30)
	if _, err := reopened.SetBudget(t.Context(), root.ID, 1, session.BudgetLimit{Kind: session.BudgetModelTokens, Limit: new(int64(29))}); !errors.Is(err, ErrLimit) {
		t.Fatalf("limit ignored uncertain exposure: %v", err)
	}
}

func TestUnknownSettlementBlocksInstallingFiniteLimit(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	turn := budgetTurn(t, s, root.ID)
	request := budgetRequest(turn, "unknown-input")
	request.Request.InputTokenBound = nil
	attempt := reserveTest(t, s, request)
	dispatchTest(t, s, attempt.ID)
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, session.ModelAttemptResult{State: session.AttemptFailed, Usage: session.ModelUsage{Output: new(int64(5))}}, nil); err != nil {
		t.Fatal(err)
	}
	state := budgetState(t, s, root.ID, session.BudgetModelTokens)
	if state.Used != 5 || state.Uncertain != 0 || !state.Incomplete {
		t.Fatalf("unknown input erased known output: %+v", state)
	}
	for _, kind := range []session.BudgetKind{session.BudgetModelTokens, session.BudgetModelCostNanoUSD} {
		if _, err := s.SetBudget(t.Context(), root.ID, 0, session.BudgetLimit{Kind: kind, Limit: new(int64(math.MaxInt64))}); !errors.Is(err, ErrLimit) {
			t.Fatalf("installed limit over unknown %s exposure: %v", kind, err)
		}
	}
	if state := budgetState(t, s, root.ID, session.BudgetModelElapsedMillis); state.Uncertain != 100 || state.Incomplete {
		t.Fatalf("missing elapsed did not retain timeout: %+v", state)
	}
}

func TestBudgetRetryEachAttemptChargesAndChildDeletionPreservesAllowance(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	budgetLimit(t, s, root.ID, session.BudgetModelCalls, 2)
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	turn := claim(t, s, child.Session.ID).Turn.ID
	var last session.ModelAttempt
	for i, id := range []string{"first", "retry"} {
		request := budgetRequest(turn, id)
		request.LogicalID = "logical-call"
		request.Number = i + 1
		last = reserveTest(t, s, request)
		reserveTest(t, s, request)
		dispatchTest(t, s, last.ID)
		outcome := session.ModelAttemptResult{State: session.AttemptFailed, Usage: session.ModelUsage{Input: new(int64(1)), Output: new(int64(2))}, ReportedCostNanoUSD: new(int64(3)), ElapsedMillis: new(int64(4))}
		if _, err := s.SettleModelAttempt(t.Context(), last.ID, outcome, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Finish(t.Context(), turn, session.Failed, new("provider exhausted"), nil); err != nil {
		t.Fatal(err)
	}
	before, err := s.Budgets(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), child.Session.ID); err != nil {
		t.Fatal(err)
	}
	after, err := s.Budgets(t.Context(), root.ID)
	if err != nil || !reflect.DeepEqual(before, after) || count(t, s, "model_attempts") != 2 {
		t.Fatalf("child deletion replenished budget: %+v %+v %v", before, after, err)
	}
	retained, err := s.ModelAttempt(t.Context(), last.ID)
	if err != nil || retained.TurnID != turn || retained.Result == nil {
		t.Fatalf("original attempt evidence not retained: %+v %v", retained, err)
	}
	sibling := spawnChildTest(t, s, "sibling", childRequest(root.ID))
	siblingTurn := claim(t, s, sibling.Session.ID).Turn.ID
	if _, err := s.ReserveModelAttempt(t.Context(), budgetRequest(siblingTurn, "blocked")); !errors.Is(err, ErrLimit) {
		t.Fatalf("sibling reused deleted spend: %v", err)
	}
	if _, err := s.Finish(t.Context(), siblingTurn, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "model_attempts") != 0 || count(t, s, "attempt_budget_ancestors") != 0 || count(t, s, "budget_limits") != 0 {
		t.Fatal("root deletion leaked tree accounting")
	}
}

func TestBudgetCASAndExplicitChildNarrowing(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	initial := budgetState(t, s, root.ID, session.BudgetModelTokens)
	if initial.Revision != 0 || initial.Limit != nil {
		t.Fatalf("absent budget: %+v", initial)
	}
	budgetLimit(t, s, root.ID, session.BudgetModelTokens, 100)
	request := childRequest(root.ID)
	request.Budgets = []session.BudgetLimit{{Kind: session.BudgetModelTokens, Limit: new(int64(90))}}
	child := spawnChildTest(t, s, "narrow", request)
	if state := budgetState(t, s, child.Session.ID, session.BudgetModelTokens); state.Revision != 1 || state.Limit == nil || *state.Limit != 90 {
		t.Fatalf("explicit child cap: %+v", state)
	}
	budgetLimit(t, s, root.ID, session.BudgetModelTokens, 80)
	for _, limit := range []*int64{nil, new(int64(81))} {
		if _, err := s.SetBudget(t.Context(), child.Session.ID, 1, session.BudgetLimit{Kind: session.BudgetModelTokens, Limit: limit}); !errors.Is(err, ErrLimit) {
			t.Fatalf("widened child cap beyond live ancestor: %v", err)
		}
	}
	if _, err := s.SetBudget(t.Context(), child.Session.ID, 0, session.BudgetLimit{Kind: session.BudgetModelTokens, Limit: new(int64(70))}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision succeeded: %v", err)
	}
	budgetLimit(t, s, child.Session.ID, session.BudgetModelTokens, 70)
	request = childRequest(root.ID)
	request.Budgets = []session.BudgetLimit{{Kind: session.BudgetModelCalls, Limit: new(int64(1))}, {Kind: session.BudgetModelTokens, Limit: new(int64(81))}}
	before := count(t, s, "budget_limits")
	if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "client", RequestID: "wide"}, request); !errors.Is(err, ErrLimit) {
		t.Fatalf("wide child cap accepted: %v", err)
	}
	if count(t, s, "sessions") != 2 || count(t, s, "budget_limits") != before || count(t, s, "inputs") != 1 {
		t.Fatal("failed narrowing left partial child or budget")
	}
	implicit := spawnChildTest(t, s, "implicit", childRequest(root.ID))
	if state := budgetState(t, s, implicit.Session.ID, session.BudgetModelTokens); state.Revision != 0 || state.Limit != nil {
		t.Fatalf("ancestor limit copied into child: %+v", state)
	}
}

func TestBudgetReservationAssociationRollback(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	turn := budgetTurn(t, s, root.ID)
	budgetLimit(t, s, root.ID, session.BudgetModelCalls, 1)
	execTest(t, s, `CREATE TRIGGER fail_budget_capture BEFORE INSERT ON attempt_budget_ancestors BEGIN SELECT RAISE(ABORT,'capture fault'); END`)
	if _, err := s.ReserveModelAttempt(t.Context(), budgetRequest(turn, "fault")); err == nil {
		t.Fatal("fault did not fail reservation")
	}
	if state := budgetState(t, s, root.ID, session.BudgetModelCalls); state.Reserved != 0 || count(t, s, "model_attempts") != 0 {
		t.Fatalf("attempt and ancestry split: %+v", state)
	}
}

func TestBudgetOverflowNeverWrapsOrDropsActualEvidence(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	turn := budgetTurn(t, s, root.ID)
	for _, id := range []string{"huge", "second"} {
		attempt := reserveTest(t, s, budgetRequest(turn, id))
		dispatchTest(t, s, attempt.ID)
		outcome := session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: new(int64(math.MaxInt64)), Output: new(int64(1))}, ReportedCostNanoUSD: new(int64(math.MaxInt64)), ElapsedMillis: new(int64(math.MaxInt64))}
		settled, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, nil)
		if err != nil || *settled.Result.Usage.Input != math.MaxInt64 || *settled.CostNanoUSD != math.MaxInt64 {
			t.Fatalf("actual evidence clamped: %+v %v", settled, err)
		}
	}
	for _, kind := range []session.BudgetKind{session.BudgetModelTokens, session.BudgetModelCostNanoUSD, session.BudgetModelElapsedMillis} {
		state := budgetState(t, s, root.ID, kind)
		if state.Used != math.MaxInt64 || !state.Incomplete {
			t.Fatalf("overflow projection %s: %+v", kind, state)
		}
		if _, err := s.SetBudget(t.Context(), root.ID, 0, session.BudgetLimit{Kind: kind, Limit: new(int64(math.MaxInt64))}); !errors.Is(err, ErrLimit) {
			t.Fatalf("finite limit accepted overflow: %v", err)
		}
	}
	_, bounded := create(t, s, session.DefaultTreePolicy())
	boundedTurn := budgetTurn(t, s, bounded.ID)
	budgetLimit(t, s, bounded.ID, session.BudgetModelCostNanoUSD, math.MaxInt64)
	request := budgetRequest(boundedTurn, "cost-overflow")
	maximum := new(int64(math.MaxInt64))
	request.Request.InputTokenBound = new(int64(1000000))
	request.Request.Prices = session.ModelPrices{Input: maximum, Output: maximum, Reasoning: maximum, CachedInput: maximum, CachedOutput: maximum}
	if _, err := s.ReserveModelAttempt(t.Context(), request); !errors.Is(err, ErrLimit) {
		t.Fatalf("overflow reservation passed finite cap: %v", err)
	}
}

func TestBudgetUnknownComplementPreservesReportedOverage(t *testing.T) {
	for _, test := range []struct {
		name                              string
		usage                             session.ModelUsage
		wantUsed, wantUncertain, wantCost int64
		incomplete                        bool
	}{
		{"total-overage", session.ModelUsage{Input: new(int64(1000))}, 1000, 20, 3100, false},
		{"input-detail-overage", session.ModelUsage{CachedInput: new(int64(1000))}, 1000, 20, 3100, true},
		{"output-detail-overage", session.ModelUsage{Reasoning: new(int64(25)), CachedOutput: new(int64(10))}, 35, 10, 205, true},
		{"bounded-details", session.ModelUsage{CachedInput: new(int64(4)), Reasoning: new(int64(2)), CachedOutput: new(int64(3))}, 9, 21, 130, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := fresh(t)
			_, root := create(t, s, session.DefaultTreePolicy())
			turn := budgetTurn(t, s, root.ID)
			attempt := reserveTest(t, s, budgetRequest(turn, test.name))
			dispatchTest(t, s, attempt.ID)
			if calls := budgetState(t, s, root.ID, session.BudgetModelCalls); calls.Used != 1 || calls.Reserved != 0 {
				t.Fatalf("dispatch not charged immediately: %+v", calls)
			}
			outcome := session.ModelAttemptResult{State: session.AttemptFailed, Usage: test.usage}
			settled, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, nil)
			if err != nil || !reflect.DeepEqual(settled.Result, &outcome) {
				t.Fatalf("canonical partial evidence changed: %+v %v", settled, err)
			}
			tokens := budgetState(t, s, root.ID, session.BudgetModelTokens)
			if tokens.Used != test.wantUsed || tokens.Uncertain != test.wantUncertain || tokens.Incomplete != test.incomplete {
				t.Fatalf("token overage lost: %+v", tokens)
			}
			cost := budgetState(t, s, root.ID, session.BudgetModelCostNanoUSD)
			if cost.Used != 0 || cost.Uncertain != test.wantCost || cost.Incomplete != test.incomplete {
				t.Fatalf("cost overage lost: %+v", cost)
			}
		})
	}
}

func TestEveryBudgetKindEnforcesFrozenRequestWithoutClamping(t *testing.T) {
	for kind, required := range map[session.BudgetKind]int64{session.BudgetModelCalls: 1, session.BudgetModelTokens: 30, session.BudgetModelCostNanoUSD: 130, session.BudgetModelElapsedMillis: 100} {
		t.Run(string(kind), func(t *testing.T) {
			s := fresh(t)
			_, root := create(t, s, session.DefaultTreePolicy())
			turn := budgetTurn(t, s, root.ID)
			budgetLimit(t, s, root.ID, kind, required-1)
			request := budgetRequest(turn, "bounded")
			if _, err := s.ReserveModelAttempt(t.Context(), request); !errors.Is(err, ErrLimit) {
				t.Fatalf("finite %s did not reject full prepared request: %v", kind, err)
			}
			if count(t, s, "model_attempts") != 0 {
				t.Fatal("denied reservation persisted an attempt")
			}
			budgetLimit(t, s, root.ID, kind, required)
			attempt := reserveTest(t, s, request)
			if !reflect.DeepEqual(attempt.Request, request.Request) {
				t.Fatal("reservation clamped frozen request")
			}
		})
	}
}

func TestBudgetLimitChangeRacesReservation(t *testing.T) {
	for range 8 {
		path := filepath.Join(t.TempDir(), "runtime.db")
		s, other := openTest(t, path), openTest(t, path)
		_, root := create(t, s, session.DefaultTreePolicy())
		turn := budgetTurn(t, s, root.ID)
		budgetLimit(t, s, root.ID, session.BudgetModelCalls, 1)
		start := make(chan struct{})
		var workers sync.WaitGroup
		var reserveErr, setErr error
		workers.Go(func() { <-start; _, reserveErr = s.ReserveModelAttempt(t.Context(), budgetRequest(turn, "attempt")) })
		workers.Go(func() {
			<-start
			_, setErr = other.SetBudget(t.Context(), root.ID, 1, session.BudgetLimit{Kind: session.BudgetModelCalls, Limit: new(int64(0))})
		})
		close(start)
		workers.Wait()
		if reserveErr == nil {
			if !errors.Is(setErr, ErrLimit) {
				t.Fatalf("limit raced past reservation: %v", setErr)
			}
		} else if setErr != nil || !errors.Is(reserveErr, ErrLimit) {
			t.Fatalf("unexpected race outcomes: %v %v", reserveErr, setErr)
		}
		state := budgetState(t, s, root.ID, session.BudgetModelCalls)
		if state.Reserved > *state.Limit {
			t.Fatalf("committed reservation exceeded live limit: %+v", state)
		}
	}
}

func TestBudgetComputedCostOverflowRemainsKnownSpentLowerBound(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	turn := budgetTurn(t, s, root.ID)
	attempt := reserveTest(t, s, budgetRequest(turn, "cost-overflow"))
	dispatchTest(t, s, attempt.ID)
	outcome := session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: new(int64(math.MaxInt64)), Output: new(int64(0)), CachedInput: new(int64(math.MaxInt64))}}
	settled, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, nil)
	if err != nil || settled.CostNote == nil || *settled.CostNote != session.ErrCostOverflow.Error() {
		t.Fatalf("missing overflow evidence: %+v %v", settled, err)
	}
	state := budgetState(t, s, root.ID, session.BudgetModelCostNanoUSD)
	if state.Used != math.MaxInt64 || !state.Incomplete {
		t.Fatalf("overflow was shown as small reservation: %+v", state)
	}
}
