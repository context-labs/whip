package store

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func usageTest(t *testing.T, s *Store, owner session.SessionID) session.Usage {
	t.Helper()
	value, err := s.Usage(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func settleUsage(t *testing.T, s *Store, spec session.ModelAttemptSpec, result session.ModelAttemptResult) session.ModelAttempt {
	t.Helper()
	value := reserveTest(t, s, spec)
	dispatchTest(t, s, value.ID)
	settled, err := s.SettleModelAttempt(t.Context(), value.ID, result, nil)
	if err != nil {
		t.Fatal(err)
	}
	return settled
}

func TestUsageCapturesNativeCostProvenancePresenceAndInactiveAttempts(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	turn := budgetTurn(t, s, owner.ID)
	zero := session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: new(int64(0)), Output: new(int64(0)), Reasoning: new(int64(0)), CachedInput: new(int64(0)), CachedOutput: new(int64(0))}, ReportedCostNanoUSD: new(int64(0)), ElapsedMillis: new(int64(0))}
	settleUsage(t, s, attemptRequest(turn, "zero"), zero)
	priced := budgetRequest(turn, "priced")
	settled := settleUsage(t, s, priced, session.ModelAttemptResult{State: session.AttemptFailed, Usage: session.ModelUsage{Input: new(int64(3)), Output: new(int64(4)), Reasoning: new(int64(1)), CachedInput: new(int64(1)), CachedOutput: new(int64(1))}, ElapsedMillis: new(int64(50))})
	if settled.CostSource != "prices" || settled.CostNanoUSD == nil {
		t.Fatal(settled)
	}
	partial := session.ModelAttemptResult{State: session.AttemptUncertain, Usage: session.ModelUsage{Input: new(int64(8))}}
	settleUsage(t, s, attemptRequest(turn, "unknown"), partial)
	reserved := reserveTest(t, s, attemptRequest(turn, "reserved"))
	inFlight := reserveTest(t, s, attemptRequest(turn, "in_flight"))
	dispatchTest(t, s, inFlight.ID)
	cancelled := reserveTest(t, s, attemptRequest(turn, "cancelled"))
	if _, err := s.SettleModelAttempt(t.Context(), cancelled.ID, session.ModelAttemptResult{State: session.AttemptCancelled}, nil); err != nil {
		t.Fatal(err)
	}
	before := count(t, s, "logical_writes")
	got := usageTest(t, s, owner.ID)
	if got.Attempts != (session.UsageAttempts{Reserved: 1, InFlight: 1, Settled: 3, NotDispatched: 1, Uncertain: 1}) || got.ReportedCost != (session.UsageCost{Attempts: 1}) || got.EstimatedCost != (session.UsageCost{Value: *settled.CostNanoUSD, Attempts: 1}) || got.UnknownCost != 1 {
		t.Fatalf("cost or attempt distinction lost: %+v", got)
	}
	if got.InputTokens != (session.UsageQuantity{Value: 11, KnownAttempts: 3}) || got.OutputTokens != (session.UsageQuantity{Value: 4, KnownAttempts: 2, MissingAttempts: 1}) || got.ReasoningTokens != (session.UsageQuantity{Value: 1, KnownAttempts: 2, MissingAttempts: 1}) || got.ElapsedMillis != (session.UsageQuantity{Value: 50, KnownAttempts: 2, MissingAttempts: 1}) {
		t.Fatalf("zero or missing usage changed: %+v", got)
	}
	if !reflect.DeepEqual(got, usageTest(t, s, owner.ID)) || count(t, s, "logical_writes") != before {
		t.Fatal("usage read mutated canonical work")
	}
	if current, err := s.ModelAttempt(t.Context(), reserved.ID); err != nil || current.DispatchedAt != nil {
		t.Fatal("usage read dispatched reserved work", current, err)
	}
}

func TestUsageRetainsDeletedDescendantsRetriesAndHelperCharges(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := helperOperation(t, s, owner, cell, "helper", "models.call", `{"prompt":"question"}`, true)
	settleUsage(t, s, helperRequest(t, op, 0), session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(5))})
	child := spawnChildTest(t, s, "child", childRequest(owner.ID))
	turn := claim(t, s, child.Session.ID).Turn.ID
	for index, id := range []string{"first", "retry"} {
		spec := attemptRequest(turn, id)
		spec.LogicalID, spec.Number = "logical", index+1
		result := session.ModelAttemptResult{State: session.AttemptFailed, ReportedCostNanoUSD: new(int64(7)), Usage: session.ModelUsage{Input: new(int64(9))}}
		settled := settleUsage(t, s, spec, result)
		if _, err := s.SettleModelAttempt(t.Context(), settled.ID, result, nil); err != nil {
			t.Fatal(err)
		}
	}
	got := usageTest(t, s, owner.ID)
	if got.ReportedCost.Value != 19 || got.ReportedCost.Attempts != 3 || got.InputTokens != (session.UsageQuantity{Value: 18, KnownAttempts: 2, MissingAttempts: 1}) {
		t.Fatal(got)
	}
	childUsage := usageTest(t, s, child.Session.ID)
	if childUsage.ReportedCost.Value != 14 || childUsage.Attempts.Settled != 2 {
		t.Fatal(childUsage)
	}
	if _, err := s.Finish(t.Context(), turn, session.Failed, new("done"), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), child.Session.ID); err != nil {
		t.Fatal(err)
	}
	if after := usageTest(t, s, owner.ID); !reflect.DeepEqual(got, after) {
		t.Fatal("deleted child lost historical usage", got, after)
	}
	if _, err := s.Usage(t.Context(), child.Session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted owner remained readable", err)
	}
	_, other := create(t, s, nil)
	if got := usageTest(t, s, other.ID); got.Attempts != (session.UsageAttempts{}) {
		t.Fatal("foreign root inherited usage", got)
	}
}

func TestUsageExactLargeOverflowRollbackAndExplicitScanLimit(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	turn := budgetTurn(t, s, owner.ID)
	large := int64(9007199254740993)
	first := settleUsage(t, s, attemptRequest(turn, "large"), session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: &large, Usage: session.ModelUsage{Input: &large}})
	if got := usageTest(t, s, owner.ID); got.ReportedCost.Value != large || got.InputTokens.Value != large || got.InputTokens.Overflow {
		t.Fatal("large exact integer changed", got)
	}
	spec := attemptRequest(turn, "overflow")
	second := reserveTest(t, s, spec)
	dispatchTest(t, s, second.ID)
	execTest(t, s, `CREATE TRIGGER fail_usage_settlement BEFORE UPDATE ON model_attempts WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'rollback'); END`)
	result := session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(math.MaxInt64)), Usage: session.ModelUsage{Input: new(int64(math.MaxInt64))}}
	if _, err := s.SettleModelAttempt(t.Context(), second.ID, result, nil); err == nil {
		t.Fatal("settlement fault ignored")
	}
	if got := usageTest(t, s, owner.ID); got.ReportedCost.Value != *first.CostNanoUSD || got.Attempts.InFlight != 1 || got.Attempts.Settled != 1 {
		t.Fatal("usage escaped rollback", got)
	}
	execTest(t, s, "DROP TRIGGER fail_usage_settlement")
	if _, err := s.SettleModelAttempt(t.Context(), second.ID, result, nil); err != nil {
		t.Fatal(err)
	}
	got := usageTest(t, s, owner.ID)
	if got.ReportedCost.Value != math.MaxInt64 || !got.ReportedCost.Overflow || got.InputTokens.Value != math.MaxInt64 || !got.InputTokens.Overflow || got.InputTokens.KnownAttempts != 2 {
		t.Fatal("overflow was hidden or wrapped", got)
	}
	partial, err := readUsage(t.Context(), s.db, owner.ID, 1)
	if !errors.Is(err, ErrLimit) || !reflect.DeepEqual(partial, session.Usage{}) {
		t.Fatal("scan cap exposed partial totals", partial, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Usage(ctx, owner.ID); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Usage(t.Context(), "absent"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestUsageForkDoesNotCopySourceCharges(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	history := compactionHistoryTest(t, s, owner.ID, "source")
	turn := budgetTurn(t, s, owner.ID)
	settleUsage(t, s, attemptRequest(turn, "charged"), session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(17))})
	if _, err := s.Finish(t.Context(), turn, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	forked := forkTest(t, s, forkRequestTest(t, s, owner.ID, "fork", history[len(history)-1].Sequence))
	if got := usageTest(t, s, forked.Root.ID); got.Attempts != (session.UsageAttempts{}) || got.ReportedCost != (session.UsageCost{}) {
		t.Fatal("fork imported accounting", got)
	}
}
