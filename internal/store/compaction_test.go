package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func compactionHistoryTest(t *testing.T, s *Store, owner session.SessionID, key string) []session.Message {
	t.Helper()
	submit(t, s, owner, key)
	turn := claim(t, s, owner).Turn
	for i := range 3 {
		if _, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{ID: session.MessageID(fmt.Sprintf("%s_answer_%d", key, i)), Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "raw evidence"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	value, err := s.History(t.Context(), owner, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func compactionTurnTest(t *testing.T, s *Store, owner session.SessionID, key string) session.Turn {
	t.Helper()
	if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "compact", RequestID: key}, Submission{SessionID: owner, Source: session.UserInput, Kind: session.CompactInput}); err != nil {
		t.Fatal(err)
	}
	return claim(t, s, owner).Turn
}

func compactionAttemptTest(t *testing.T, s *Store, turn session.TurnID, key string) session.ModelAttempt {
	t.Helper()
	request := attemptRequest(turn, key)
	request.Request.Purpose = "compaction"
	value := reserveTest(t, s, request)
	dispatchTest(t, s, value.ID)
	return value
}

func compactionOutcomeTest() session.ModelAttemptResult {
	return session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: new(int64(10)), Output: new(int64(2))}, ReportedCostNanoUSD: new(int64(1200))}
}

func settleCompactionTest(t *testing.T, s *Store, attempt session.ModelAttemptID, draft session.CompactionDraft) session.CompactionSettlement {
	t.Helper()
	result, err := s.SettleCompaction(t.Context(), attempt, compactionOutcomeTest(), &draft)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCompactionAtomicSettlementUndoRetryAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	history := compactionHistoryTest(t, s, owner.ID, "raw")
	head, err := s.ContextHead(t.Context(), owner.ID)
	if err != nil || head.Revision != 0 || head.CompactionID != nil {
		t.Fatalf("initial head=%+v %v", head, err)
	}
	turn := compactionTurnTest(t, s, owner.ID, "compact")
	attempt := compactionAttemptTest(t, s, turn.ID, "summary_attempt")
	draft := session.CompactionDraft{ID: "summary", ThroughSequence: 2, PinnedMessageIDs: []session.MessageID{history[0].ID}, Text: "retain the task"}
	for _, trigger := range []string{
		`CREATE TRIGGER compaction_failure BEFORE INSERT ON context_heads BEGIN SELECT RAISE(ABORT,'head failed'); END`,
		`CREATE TRIGGER compaction_failure BEFORE UPDATE ON model_attempts WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'billing failed'); END`,
	} {
		execTest(t, s, trigger)
		if _, err := s.SettleCompaction(t.Context(), attempt.ID, compactionOutcomeTest(), &draft); err == nil {
			t.Fatal("SQL failure ignored")
		}
		pending, err := s.ModelAttempt(t.Context(), attempt.ID)
		if err != nil || pending.State != session.AttemptDispatched || count(t, s, "compactions") != 0 || count(t, s, "context_heads") != 0 {
			t.Fatalf("partial accounting/summary/selection: %+v %v", pending, err)
		}
		execTest(t, s, "DROP TRIGGER compaction_failure")
	}
	writes := count(t, s, "logical_writes")
	settled := settleCompactionTest(t, s, attempt.ID, draft)
	if !settled.Selected || settled.Rejection != nil || settled.Compaction == nil || settled.Attempt.MessageID != nil || settled.Head.Revision != 1 || settled.Attempt.CostNanoUSD == nil || *settled.Attempt.CostNanoUSD != 1200 {
		t.Fatalf("wrong settlement: %+v", settled)
	}
	if count(t, s, "logical_writes") != writes {
		t.Fatal("derived summary charged a logical write")
	}
	retry := settleCompactionTest(t, s, attempt.ID, draft)
	if !reflect.DeepEqual(settled, retry) {
		t.Fatalf("ambiguous acknowledgement retry changed settlement: %+v %+v", settled, retry)
	}
	if _, err := s.SelectCompaction(t.Context(), owner.ID, 1, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("undo during execution=%v", err)
	}
	changed := draft
	changed.Text = "different response"
	if _, err := s.SettleCompaction(t.Context(), attempt.ID, compactionOutcomeTest(), &changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed settlement=%v", err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	head, err = s.SelectCompaction(t.Context(), owner.ID, 1, nil)
	if err != nil || head.Revision != 2 || head.CompactionID != nil {
		t.Fatalf("undo=%+v %v", head, err)
	}
	if _, err := s.SelectCompaction(t.Context(), owner.ID, 1, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale undo=%v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	retry = settleCompactionTest(t, reopened, attempt.ID, draft)
	if retry.Selected || retry.Head.Revision != 2 || retry.Head.CompactionID != nil || !reflect.DeepEqual(retry.Compaction, settled.Compaction) || count(t, reopened, "compactions") != 1 {
		t.Fatalf("retry resurrected undone selection: %+v", retry)
	}
	after, err := reopened.History(t.Context(), owner.ID, 0, 100)
	if err != nil || !reflect.DeepEqual(after, history) {
		t.Fatal("compaction altered raw history", err)
	}
	if output, err := reopened.TurnOutput(t.Context(), turn.ID); err != nil || output != nil {
		t.Fatalf("compaction became structured output: %+v %v", output, err)
	}
}

func TestCompactionStaleHeadRetainsEvidenceAndInvalidDraftsSettleBilling(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	history := compactionHistoryTest(t, s, owner.ID, "raw")
	_, foreign := create(t, s, nil)
	foreignHistory := compactionHistoryTest(t, s, foreign.ID, "foreign")
	foreignTurn := compactionTurnTest(t, s, foreign.ID, "foreign_compact")
	foreignAttempt := compactionAttemptTest(t, s, foreignTurn.ID, "foreign_attempt")
	foreignDraft := session.CompactionDraft{ID: "foreign_summary", ThroughSequence: 1, Text: "foreign"}
	settleCompactionTest(t, s, foreignAttempt.ID, foreignDraft)
	turn := compactionTurnTest(t, s, owner.ID, "compact")
	first := session.CompactionDraft{ID: "first", ThroughSequence: 1, Text: "first"}
	settleCompactionTest(t, s, compactionAttemptTest(t, s, turn.ID, "first_attempt").ID, first)
	stale := session.CompactionDraft{ID: "stale", ThroughSequence: 2, Text: "stale selection"}
	value := settleCompactionTest(t, s, compactionAttemptTest(t, s, turn.ID, "stale_attempt").ID, stale)
	if value.Selected || value.Rejection == nil || value.Compaction == nil || value.Head.CompactionID == nil || *value.Head.CompactionID != first.ID || value.Attempt.State != session.AttemptSucceeded {
		t.Fatalf("stale result discarded evidence or selected: %+v", value)
	}
	for i, mutate := range []func(*session.CompactionDraft){
		func(d *session.CompactionDraft) { d.ThroughSequence = 999 },
		func(d *session.CompactionDraft) { d.ThroughSequence = 1 },
		func(d *session.CompactionDraft) { d.BaseID = &foreignDraft.ID },
		func(d *session.CompactionDraft) { d.PinnedMessageIDs = []session.MessageID{foreignHistory[0].ID} },
		func(d *session.CompactionDraft) { d.PinnedMessageIDs = []session.MessageID{history[1].ID} },
		func(d *session.CompactionDraft) {
			d.PinnedMessageIDs = []session.MessageID{history[0].ID, history[0].ID}
		},
		func(d *session.CompactionDraft) { d.ID = first.ID },
		func(d *session.CompactionDraft) { d.Text = "\xff" },
		func(d *session.CompactionDraft) { d.Text = strings.Repeat("x", session.MaxCompactionBytes+1) },
	} {
		draft := session.CompactionDraft{ID: session.CompactionID(fmt.Sprintf("invalid_%d", i)), ExpectedRevision: 1, BaseID: &first.ID, ThroughSequence: 2, Text: "candidate"}
		mutate(&draft)
		attempt := compactionAttemptTest(t, s, turn.ID, fmt.Sprintf("invalid_attempt_%d", i))
		before := count(t, s, "compactions")
		value := settleCompactionTest(t, s, attempt.ID, draft)
		if value.Compaction != nil || value.Rejection == nil || value.Selected || value.Attempt.State != session.AttemptSucceeded || value.Attempt.CostNanoUSD == nil || count(t, s, "compactions") != before {
			t.Fatalf("invalid draft lost billing or entered context: %+v", value)
		}
		// Later SQL retries cannot attach a replacement summary to a settled attempt.
		valid := session.CompactionDraft{ID: session.CompactionID(fmt.Sprintf("replacement_%d", i)), ExpectedRevision: 1, BaseID: &first.ID, ThroughSequence: 2, Text: "replacement"}
		retry := settleCompactionTest(t, s, attempt.ID, valid)
		if retry.Compaction != nil || retry.Rejection == nil || count(t, s, "compactions") != before {
			t.Fatal("retry attached late summary", retry)
		}
	}
	if _, err := s.Compaction(t.Context(), owner.ID, foreignDraft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner summary read=%v", err)
	}
}

func TestCompactionBoundariesKeepEveryToolResult(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	submit(t, s, owner.ID, "input")
	turn := claim(t, s, owner.ID).Turn
	parts := []session.Part{}
	for _, id := range []string{"a", "b"} {
		parts = append(parts, session.Part{Type: "tool_call", Call: &session.ToolCall{ID: id, Name: "execute", Arguments: json.RawMessage(`{"code":"1"}`)}})
	}
	if _, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{ID: "calls", Role: session.Assistant, Parts: parts}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{ID: session.MessageID("result_" + id), Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: id, Output: "done"}}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	history, err := s.History(t.Context(), owner.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	compact := compactionTurnTest(t, s, owner.ID, "compact")
	for through := int64(2); through <= 4; through++ {
		attempt := compactionAttemptTest(t, s, compact.ID, fmt.Sprintf("attempt_%d", through))
		draft := session.CompactionDraft{ID: session.CompactionID(fmt.Sprintf("summary_%d", through)), ThroughSequence: through, PinnedMessageIDs: []session.MessageID{history[0].ID}, Text: "summary"}
		value := settleCompactionTest(t, s, attempt.ID, draft)
		if through < 4 && (value.Compaction != nil || value.Rejection == nil) {
			t.Fatalf("split tool group admitted at %d: %+v", through, value)
		}
		if through == 4 && (!value.Selected || value.Rejection != nil || value.Compaction == nil) {
			t.Fatalf("complete tool group rejected: %+v", value)
		}
	}
}

func TestCompactionAncestorUndoMetadataAndCascade(t *testing.T) {
	for _, child := range []bool{false, true} {
		t.Run(fmt.Sprintf("child_%v", child), func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			if child {
				admission := controlChild(t, s, owner.ID, "child")
				owner = *admission.Session
				initial := claim(t, s, owner.ID).Turn
				if _, err := s.Finish(t.Context(), initial.ID, session.Succeeded, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			compactionHistoryTest(t, s, owner.ID, "raw")
			turn := compactionTurnTest(t, s, owner.ID, "compact")
			var base *session.CompactionID
			var last session.CompactionDraft
			var attempt session.ModelAttempt
			for i := int64(1); i <= 3; i++ {
				attempt = compactionAttemptTest(t, s, turn.ID, fmt.Sprintf("attempt_%d", i))
				last = session.CompactionDraft{ID: session.CompactionID(fmt.Sprintf("summary_%d", i)), BaseID: base, ExpectedRevision: i - 1, ThroughSequence: i, Text: strings.Repeat("s", session.MaxCompactionBytes)}
				value := settleCompactionTest(t, s, attempt.ID, last)
				if !value.Selected || value.Head.Revision != i {
					t.Fatalf("summary chain=%+v", value)
				}
				base = new(last.ID)
			}
			page, err := s.Compactions(t.Context(), owner.ID, "", 2)
			if err != nil || len(page) != 2 || page[0].TextBytes != session.MaxCompactionBytes || page[0].PinnedMessageIDs == nil {
				t.Fatalf("metadata page=%+v %v", page, err)
			}
			next, err := s.Compactions(t.Context(), owner.ID, page[1].ID, 2)
			if err != nil || len(next) != 1 || next[0].ID != last.ID {
				t.Fatalf("metadata next=%+v %v", next, err)
			}
			if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
				t.Fatal(err)
			}
			first := session.CompactionID("summary_1")
			head, err := s.SelectCompaction(t.Context(), owner.ID, 3, &first)
			if err != nil || head.Revision != 4 || head.CompactionID == nil || *head.CompactionID != first {
				t.Fatalf("ancestor undo=%+v %v", head, err)
			}
			if _, err := s.SelectCompaction(t.Context(), owner.ID, 4, &last.ID); !errors.Is(err, ErrConflict) {
				t.Fatalf("non-ancestor selected=%v", err)
			}
			retry := settleCompactionTest(t, s, attempt.ID, last)
			if retry.Selected || retry.Head.Revision != 4 {
				t.Fatal("retry reselected descendant", retry)
			}
			if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
				t.Fatal("selected summary blocked deletion", err)
			}
			if count(t, s, "compactions") != 0 || count(t, s, "context_heads") != 0 {
				t.Fatal("deleted owner retained compaction state")
			}
		})
	}
}

func TestCompactionCancellationAndMissingDraftKeepAccounting(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	compactionHistoryTest(t, s, owner.ID, "raw")
	turn := compactionTurnTest(t, s, owner.ID, "compact")
	without := compactionAttemptTest(t, s, turn.ID, "without")
	value, err := s.SettleCompaction(t.Context(), without.ID, compactionOutcomeTest(), nil)
	if err != nil || value.Compaction != nil || value.Attempt.State != session.AttemptSucceeded {
		t.Fatalf("unusable helper settlement=%+v %v", value, err)
	}
	attempt := compactionAttemptTest(t, s, turn.ID, "cancelled")
	if _, err := s.CancelTurn(t.Context(), turn.ID); err != nil {
		t.Fatal(err)
	}
	draft := session.CompactionDraft{ID: "cancelled_summary", ThroughSequence: 2, Text: "completed despite cancellation"}
	value = settleCompactionTest(t, s, attempt.ID, draft)
	if value.Selected || value.Rejection == nil || value.Compaction == nil || value.Head.Revision != 0 || value.Attempt.State != session.AttemptSucceeded {
		t.Fatalf("cancelled helper settlement=%+v", value)
	}
	if _, err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	head, err := s.ContextHead(t.Context(), owner.ID)
	if err != nil || head.CompactionID != nil || count(t, s, "compactions") != 1 {
		t.Fatalf("recovery selected or erased evidence: %+v %v", head, err)
	}
}

func TestCompactionConcurrentSelectionCommitsBothAttemptsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	other := openTest(t, path)
	_, owner := create(t, s, nil)
	compactionHistoryTest(t, s, owner.ID, "raw")
	turn := compactionTurnTest(t, s, owner.ID, "compact")
	attempts := []session.ModelAttempt{
		compactionAttemptTest(t, s, turn.ID, "attempt_a"),
		compactionAttemptTest(t, s, turn.ID, "attempt_b"),
	}
	start := make(chan struct{})
	results := make(chan session.CompactionSettlement, 2)
	var workers sync.WaitGroup
	for i, database := range []*Store{s, other} {
		workers.Go(func() {
			<-start
			draft := session.CompactionDraft{ID: session.CompactionID(fmt.Sprintf("summary_%d", i)), ThroughSequence: int64(i + 1), Text: "concurrent summary"}
			value, err := database.SettleCompaction(t.Context(), attempts[i].ID, compactionOutcomeTest(), &draft)
			if err != nil {
				t.Error(err)
				return
			}
			results <- value
		})
	}
	close(start)
	workers.Wait()
	close(results)
	selected, settled := 0, 0
	for value := range results {
		settled++
		if value.Selected {
			selected++
		}
		if value.Compaction == nil || value.Attempt.State != session.AttemptSucceeded || value.Attempt.CostNanoUSD == nil {
			t.Fatalf("race lost summary/accounting: %+v", value)
		}
	}
	head, err := s.ContextHead(t.Context(), owner.ID)
	if err != nil || selected != 1 || settled != 2 || head.Revision != 1 || count(t, s, "compactions") != 2 {
		t.Fatalf("selection race selected=%d settled=%d head=%+v err=%v", selected, settled, head, err)
	}
}

func TestCompactionFailedAndUndispatchedAttemptsCannotCreateSummary(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	compactionHistoryTest(t, s, owner.ID, "raw")
	turn := compactionTurnTest(t, s, owner.ID, "compact")
	request := attemptRequest(turn.ID, "reserved")
	request.Request.Purpose = "compaction"
	reserved := reserveTest(t, s, request)
	value, err := s.SettleCompaction(t.Context(), reserved.ID, session.ModelAttemptResult{State: session.AttemptCancelled}, nil)
	if err != nil || value.Attempt.CostNanoUSD == nil || *value.Attempt.CostNanoUSD != 0 || value.Compaction != nil {
		t.Fatalf("undispatched settlement=%+v %v", value, err)
	}
	dispatched := compactionAttemptTest(t, s, turn.ID, "failed")
	outcome := session.ModelAttemptResult{State: session.AttemptFailed, Failure: new("provider rejected request"), ReportedCostNanoUSD: new(int64(100))}
	draft := session.CompactionDraft{ID: "forbidden", ThroughSequence: 1, Text: "not successful"}
	value, err = s.SettleCompaction(t.Context(), dispatched.ID, outcome, &draft)
	if err != nil || value.Compaction != nil || value.Rejection == nil || value.Attempt.CostNanoUSD == nil || *value.Attempt.CostNanoUSD != 100 || count(t, s, "compactions") != 0 {
		t.Fatalf("unsuccessful helper admitted summary/lost billing=%+v %v", value, err)
	}
}
