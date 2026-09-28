package store

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func attemptRequest(turn session.TurnID, id string) session.ModelAttemptSpec {
	return session.ModelAttemptSpec{ID: session.ModelAttemptID(id), TurnID: turn, LogicalID: id, Number: 1, Request: session.ModelRequestSnapshot{
		Purpose: "turn", Model: session.ModelSelection{Provider: "fixture", Name: "model"}, Route: "scripted://fixture", Adapter: "scripted", RequestDigest: strings.Repeat("a", 64), MaxOutputTokens: 4096, TimeoutMillis: 30000,
	}}
}

func reserveTest(t *testing.T, s *Store, p session.ModelAttemptSpec) session.ModelAttempt {
	t.Helper()
	value, err := s.ReserveModelAttempt(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func dispatchTest(t *testing.T, s *Store, id session.ModelAttemptID) {
	t.Helper()
	allowed, err := s.DispatchModelAttempt(t.Context(), id)
	if err != nil || !allowed {
		t.Fatalf("dispatch=%v err=%v", allowed, err)
	}
}

func TestAttemptAdmissionAndSingleDispatchAcrossConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s := openTest(t, path)
	other := openTest(t, path)
	_, root := create(t, s, session.DefaultTreePolicy())
	submit(t, s, root.ID, "input")
	claim := claim(t, s, root.ID)
	p := attemptRequest(claim.Turn.ID, "attempt")
	p.Request.Prices.Input = new(int64(1000000))
	first := reserveTest(t, s, p)
	same := reserveTest(t, s, p)
	if first.ID != same.ID || !first.CreatedAt.Equal(same.CreatedAt) {
		t.Fatal("reserve retry created another attempt")
	}
	*p.Request.Prices.Input = 123
	if _, err := s.ReserveModelAttempt(t.Context(), p); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed request: %v", err)
	}
	p.ID = "different-id"
	if _, err := s.ReserveModelAttempt(t.Context(), p); !errors.Is(err, ErrConflict) {
		t.Fatalf("reused logical identity: %v", err)
	}
	var wins atomic.Int32
	var workers sync.WaitGroup
	for i := range 16 {
		workers.Go(func() {
			db := s
			if i%2 != 0 {
				db = other
			}
			allowed, err := db.DispatchModelAttempt(t.Context(), first.ID)
			if err != nil {
				t.Error(err)
			}
			if allowed {
				wins.Add(1)
			}
		})
	}
	workers.Wait()
	if wins.Load() != 1 {
		t.Fatalf("provider dispatch grants=%d", wins.Load())
	}
	stored, err := s.ModelAttempt(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != session.AttemptDispatched || stored.DispatchedAt == nil || *stored.Request.Prices.Input != 1000000 {
		t.Fatalf("unstable request snapshot: %+v", stored)
	}
	if _, err := s.Finish(t.Context(), claim.Turn.ID, session.Succeeded, nil, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("finished unsettled attempt: %v", err)
	}
}

func TestAttemptSettlementAndTranscriptCommitAtomicallyAndRetryExactly(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	submit(t, s, root.ID, "input")
	turn := claim(t, s, root.ID).Turn
	attempt := reserveTest(t, s, attemptRequest(turn.ID, "attempt"))
	dispatchTest(t, s, attempt.ID)
	outcome := session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: new(int64(10)), Output: new(int64(2))}, ReportedCostNanoUSD: new(int64(1200))}
	draft := session.MessageDraft{ID: "answer", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "done"}}}
	execTest(t, s, `CREATE TRIGGER fail_settlement BEFORE UPDATE ON model_attempts WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'injected settlement failure'); END`)
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, &draft); err == nil {
		t.Fatal("injected settlement succeeded")
	}
	pending, err := s.ModelAttempt(t.Context(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != session.AttemptDispatched || pending.Result != nil || count(t, s, "messages") != 1 {
		t.Fatal("partial attempt/transcript commit")
	}
	execTest(t, s, "DROP TRIGGER fail_settlement")
	settled, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, &draft)
	if err != nil {
		t.Fatal(err)
	}
	if settled.CostNanoUSD == nil || *settled.CostNanoUSD != 1200 || settled.CostSource != "provider" || settled.MessageID == nil || *settled.MessageID != draft.ID {
		t.Fatalf("wrong settlement: %+v", settled)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	retry, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, &draft)
	if err != nil || !retry.FinishedAt.Equal(*settled.FinishedAt) || count(t, s, "messages") != 2 {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	changed := draft
	changed.ID = "duplicate-answer"
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, &changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("new answer on same settlement: %v", err)
	}
	changed = draft
	changed.Parts = []session.Part{{Type: "text", Text: "changed"}}
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, &changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed answer: %v", err)
	}
	outcome.ReportedCostNanoUSD = new(int64(1201))
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, &draft); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed outcome: %v", err)
	}
}

func TestAttemptRecoveryDistinguishesUndispatchedFromUncertainAndRollsBack(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	submit(t, s, root.ID, "input")
	turn := claim(t, s, root.ID).Turn
	reserved := reserveTest(t, s, attemptRequest(turn.ID, "reserved"))
	dispatched := reserveTest(t, s, attemptRequest(turn.ID, "dispatched"))
	dispatchTest(t, s, dispatched.ID)
	execTest(t, s, `CREATE TRIGGER fail_recover BEFORE UPDATE ON turns WHEN NEW.state='interrupted' BEGIN SELECT RAISE(ABORT,'injected recovery failure'); END`)
	if _, err := s.Recover(t.Context()); err == nil {
		t.Fatal("recovery fault was ignored")
	}
	current, err := s.ModelAttempt(t.Context(), reserved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != session.AttemptReserved {
		t.Fatal("attempt recovery escaped rollback")
	}
	execTest(t, s, "DROP TRIGGER fail_recover")
	if _, err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err = s.ModelAttempt(t.Context(), reserved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != session.AttemptCancelled || current.CostNanoUSD == nil || *current.CostNanoUSD != 0 || current.CostSource != "not_dispatched" {
		t.Fatalf("reserved recovery=%+v", current)
	}
	current, err = s.ModelAttempt(t.Context(), dispatched.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != session.AttemptUncertain || current.CostNanoUSD != nil || current.CostSource != "unknown" || current.Result.Usage.Input != nil {
		t.Fatalf("dispatched recovery=%+v", current)
	}
	if allowed, err := s.DispatchModelAttempt(t.Context(), dispatched.ID); err != nil || allowed {
		t.Fatalf("recovery permitted replay: %v %v", allowed, err)
	}
	if changed, err := s.Recover(t.Context()); err != nil || changed != 0 {
		t.Fatalf("recovery not idempotent: %d %v", changed, err)
	}
}

func TestCancellationPreventsNewModelDispatch(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	submit(t, s, root.ID, "input")
	turn := claim(t, s, root.ID).Turn
	pending := reserveTest(t, s, attemptRequest(turn.ID, "pending"))
	if _, err := s.CancelTurn(t.Context(), turn.ID); err != nil {
		t.Fatal(err)
	}
	if allowed, err := s.DispatchModelAttempt(t.Context(), pending.ID); allowed || !errors.Is(err, ErrStopped) {
		t.Fatalf("cancelled turn dispatched: %v %v", allowed, err)
	}
	if _, err := s.ReserveModelAttempt(t.Context(), attemptRequest(turn.ID, "new")); !errors.Is(err, ErrStopped) {
		t.Fatalf("cancelled turn reserved: %v", err)
	}
	if _, err := s.SettleModelAttempt(t.Context(), pending.ID, session.ModelAttemptResult{State: session.AttemptCancelled}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Cancelled, nil, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.ModelAttempt(ctx, pending.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("query ignored context: %v", err)
	}
}

func TestUnrepresentableCostRetainsUsageAndCompletedResponse(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	submit(t, s, root.ID, "input")
	turn := claim(t, s, root.ID).Turn
	p := attemptRequest(turn.ID, "large-cost")
	maximum := new(int64(math.MaxInt64))
	p.Request.Prices = session.ModelPrices{Input: maximum, Output: maximum, Reasoning: maximum, CachedInput: maximum, CachedOutput: maximum}
	attempt := reserveTest(t, s, p)
	dispatchTest(t, s, attempt.ID)
	outcome := session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: maximum, Output: maximum}}
	draft := session.MessageDraft{ID: "complete", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "completed output"}}}
	settled, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, &draft)
	if err != nil {
		t.Fatal(err)
	}
	if settled.CostNanoUSD != nil || settled.CostSource != "unknown" || settled.CostNote == nil || *settled.Result.Usage.Input != math.MaxInt64 || count(t, s, "messages") != 2 {
		t.Fatalf("lost outcome on cost overflow: %+v", settled)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchedAttemptCannotClaimUndispatchedCancellationOrMissingOutcome(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	submit(t, s, root.ID, "input")
	turn := claim(t, s, root.ID).Turn
	attempt := reserveTest(t, s, attemptRequest(turn.ID, "dispatched"))
	dispatchTest(t, s, attempt.ID)
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, session.ModelAttemptResult{State: session.AttemptCancelled}, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("dispatched cancellation erased uncertainty: %v", err)
	}
	for _, result := range []string{`{}`, `{"state":null}`} {
		if _, err := s.db.ExecContext(t.Context(), "UPDATE model_attempts SET state='succeeded',result=?,finished_at=? WHERE id=?", result, now(), attempt.ID); err == nil {
			t.Fatal("schema accepted result without its outcome state")
		}
	}
}
