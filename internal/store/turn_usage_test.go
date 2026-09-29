package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestTurnUsageExcludesOtherTurnsAndDescendants(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := helperOperation(t, s, owner, cell, "helper", "models.call", `{"prompt":"question"}`, true)
	large := int64(9007199254740993)
	settleUsage(t, s, helperRequest(t, op, 0), session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: &large, Usage: session.ModelUsage{Input: &large}})
	child := spawnChildTest(t, s, "child", childRequest(owner.ID))
	childTurn := claim(t, s, child.Session.ID).Turn.ID
	settleUsage(t, s, attemptRequest(childTurn, "child-charge"), session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(7))})
	before := count(t, s, "logical_writes")
	got, err := s.TurnUsage(t.Context(), owner.ID, cell.TurnID)
	if err != nil || got.TurnID != cell.TurnID || got.Usage.ReportedCost.Value != large || got.Usage.InputTokens.Value != large || got.Usage.OutputTokens.MissingAttempts != 1 || got.Usage.Attempts.Settled != 1 || got.Compactions != 0 {
		t.Fatal(got, err)
	}
	if count(t, s, "logical_writes") != before {
		t.Fatal("observation wrote state")
	}
	whole := usageTest(t, s, owner.ID)
	if whole.ReportedCost.Value != large+7 {
		t.Fatal("ancestor accounting no longer includes child", whole)
	}
	for _, ids := range []struct {
		owner session.SessionID
		turn  session.TurnID
	}{{owner.ID, childTurn}, {child.Session.ID, cell.TurnID}, {owner.ID, "absent"}} {
		if _, err = s.TurnUsage(t.Context(), ids.owner, ids.turn); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign or absent turn accepted", ids, err)
		}
	}
	if _, err = s.Finish(t.Context(), childTurn, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	next := budgetTurn(t, s, child.Session.ID)
	empty, err := s.TurnUsage(t.Context(), child.Session.ID, next)
	if err != nil || empty.Usage.Attempts != (session.UsageAttempts{}) {
		t.Fatal("empty next turn inherited prior charges", empty, err)
	}
	if after, err := s.TurnUsage(t.Context(), owner.ID, cell.TurnID); err != nil || !reflect.DeepEqual(got, after) {
		t.Fatal(after, err)
	}
}

func TestTurnUsageCompactionsAreAtomicAndSurviveContextUndo(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	history := compactionHistoryTest(t, s, owner.ID, "source")
	turn := compactionTurnTest(t, s, owner.ID, "compact")
	failed := compactionAttemptTest(t, s, turn.ID, "failed")
	if _, err := s.SettleModelAttempt(t.Context(), failed.ID, session.ModelAttemptResult{State: session.AttemptFailed}, nil); err != nil {
		t.Fatal(err)
	}
	attempt := compactionAttemptTest(t, s, turn.ID, "selected")
	draft := session.CompactionDraft{ID: "summary", ThroughSequence: 2, PinnedMessageIDs: []session.MessageID{history[0].ID}, Text: "retain"}
	execTest(t, s, `CREATE TRIGGER reject_turn_usage BEFORE INSERT ON compactions BEGIN SELECT RAISE(ABORT,'rollback'); END`)
	if _, err := s.SettleCompaction(t.Context(), attempt.ID, compactionOutcomeTest(), &draft); err == nil {
		t.Fatal("fault ignored")
	}
	rolled, err := s.TurnUsage(t.Context(), owner.ID, turn.ID)
	if err != nil || rolled.Compactions != 0 || rolled.CompactionAttempts != (session.UsageAttempts{InFlight: 1, Settled: 1}) || rolled.Usage.ReportedCost.Attempts != 0 {
		t.Fatal(rolled, err)
	}
	execTest(t, s, "DROP TRIGGER reject_turn_usage")
	settleCompactionTest(t, s, attempt.ID, draft)
	settleCompactionTest(t, s, attempt.ID, draft)
	got, err := s.TurnUsage(t.Context(), owner.ID, turn.ID)
	if err != nil || got.Compactions != 1 || got.CompactionAttempts != (session.UsageAttempts{Settled: 2}) || got.Usage.Attempts != got.CompactionAttempts || got.Usage.UnknownCost != 1 || got.Usage.InputTokens.KnownAttempts != 1 || got.Usage.InputTokens.MissingAttempts != 1 {
		t.Fatal(got, err)
	}
	if _, err = s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SelectCompaction(t.Context(), owner.ID, 1, nil); err != nil {
		t.Fatal(err)
	}
	if after, err := s.TurnUsage(t.Context(), owner.ID, turn.ID); err != nil || !reflect.DeepEqual(after, got) {
		t.Fatal("undo erased billed compaction evidence", after, err)
	}
}
