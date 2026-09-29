package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func displayTest(id session.ModelAttemptID, reasoning string) *session.MessagePresentation {
	return &session.MessagePresentation{Version: 1, AttemptID: id, Parts: []session.PresentationPart{{ID: "p0", Type: "reasoning", Text: reasoning}, {ID: "p1", Type: "text", Start: new(0), End: new(4)}}}
}
func TestPresentationSettlementHistoryForkRewindAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	submit(t, s, owner.ID, "question")
	turn := claim(t, s, owner.ID).Turn
	failed := reserveTest(t, s, attemptRequest(turn.ID, "failed"))
	dispatchTest(t, s, failed.ID)
	evidence := displayTest(failed.ID, "failed reasoning")
	evidence.Parts = evidence.Parts[:1]
	failure := session.ModelAttemptResult{State: session.AttemptFailed, Presentation: evidence, Failure: new("provider failure")}
	if _, err := s.SettleModelAttempt(t.Context(), failed.ID, failure, nil); err != nil {
		t.Fatal(err)
	}
	success := reserveTest(t, s, attemptRequest(turn.ID, "success"))
	dispatchTest(t, s, success.ID)
	draft := session.MessageDraft{ID: "answer", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "done"}}, Presentation: displayTest(success.ID, "visible reasoning")}
	outcome := session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: new(int64(3)), Output: new(int64(2))}}
	execTest(t, s, `CREATE TRIGGER presentation_fail BEFORE UPDATE ON model_attempts WHEN NEW.id='success' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := s.SettleModelAttempt(t.Context(), success.ID, outcome, &draft); err == nil {
		t.Fatal("partial settlement committed")
	}
	if count(t, s, "messages") != 1 {
		t.Fatal("message survived failed transaction")
	}
	execTest(t, s, "DROP TRIGGER presentation_fail")
	settled, err := s.SettleModelAttempt(t.Context(), success.ID, outcome, &draft)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(settled.Result.Usage, outcome.Usage) || settled.Result.Presentation != nil {
		t.Fatal("presentation changed accounting or duplicated success", settled)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleModelAttempt(t.Context(), success.ID, outcome, &draft); err != nil {
		t.Fatal("exact retry", err)
	}
	changed := draft
	changed.Presentation = displayTest(success.ID, "changed")
	if _, err := s.SettleModelAttempt(t.Context(), success.ID, outcome, &changed); !errors.Is(err, ErrConflict) {
		t.Fatal("changed presentation retry", err)
	}
	mustFail(t, s, "UPDATE messages SET presentation=NULL WHERE id='answer'")
	assertHistory := func(db *Store, id session.SessionID) {
		t.Helper()
		messages, err := db.History(t.Context(), id, 0, 100)
		if err != nil || len(messages) != 2 || !reflect.DeepEqual(messages[1].Presentation, draft.Presentation) {
			t.Fatal(messages, err)
		}
		snapshot, forward, err := db.HistoryPage(t.Context(), id, 0, 100, nil)
		if err != nil || !reflect.DeepEqual(forward[1].Presentation, draft.Presentation) {
			t.Fatal(forward, err)
		}
		exact, err := db.ReadHistoryMessage(t.Context(), id, messages[1].ID, 0, 65536)
		if err != nil || !reflect.DeepEqual(exact.Message.Presentation, draft.Presentation) {
			t.Fatal(exact, err)
		}
		metadata, err := db.HistoryMetadata(t.Context(), id, 0, snapshot.ThroughSequence, 100)
		if err != nil || !reflect.DeepEqual(metadata.Items[1].Presentation, draft.Presentation) {
			t.Fatal(metadata, err)
		}
		attempts, truncated, err := db.AttemptPresentations(t.Context(), id, 0, snapshot.ThroughSequence, snapshot.Revision)
		if err != nil || truncated || len(attempts) != 1 || !reflect.DeepEqual(attempts[0].Presentation, evidence) {
			t.Fatal(attempts, truncated, err)
		}
	}
	assertHistory(s, owner.ID)
	compactTurn := compactionTurnTest(t, s, owner.ID, "display_compaction")
	compactAttempt := compactionAttemptTest(t, s, compactTurn.ID, "display_compaction_attempt")
	compact := settleCompactionTest(t, s, compactAttempt.ID, session.CompactionDraft{ID: "display_summary", ThroughSequence: 2, Text: "summary"})
	if !compact.Selected {
		t.Fatal(compact)
	}
	if _, err := s.Finish(t.Context(), compactTurn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	assertHistory(s, owner.ID)
	copied := forkTest(t, s, forkRequestTest(t, s, owner.ID, "presentation_fork", 2))
	assertHistory(s, copied.Root.ID)
	imported, _, err := s.AttemptPresentations(t.Context(), copied.Root.ID, 0, 2, 1)
	if err != nil || imported[0].SourceSessionID == nil || *imported[0].SourceSessionID != owner.ID || imported[0].GroupID == session.HistoryGroupID(turn.ID) {
		t.Fatal(imported, err)
	}
	twice := forkTest(t, s, forkRequestTest(t, s, copied.Root.ID, "presentation_fork_twice", 2))
	assertHistory(s, twice.Root.ID)
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	assertHistory(s, copied.Root.ID)
	assertHistory(s, twice.Root.ID)
	history, err := s.History(t.Context(), copied.Root.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	request := rewindRequest(t, s, copied.Root.ID, "presentation_rewind", 0)
	rewindStopped(t, s, copied.Root.ID)
	edit, err := s.Rewind(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := s.ReadHistoryMessage(t.Context(), copied.Root.ID, history[1].ID, 0, 65536)
	if err != nil || retired.Message.RetiredRevision == nil || *retired.Message.RetiredRevision != edit.Revision || !reflect.DeepEqual(retired.Message.Presentation, draft.Presentation) {
		t.Fatal(retired, err)
	}
	attempts, _, err := s.AttemptPresentations(t.Context(), copied.Root.ID, 0, 2, edit.Revision)
	if err != nil || len(attempts) != 0 {
		t.Fatal("retired attempt evidence still active", attempts, err)
	}
	if _, _, err := s.AttemptPresentations(t.Context(), copied.Root.ID, 0, 2, 1); !errors.Is(err, ErrConflict) {
		t.Fatal("stale revision", err)
	}
}

func TestAttemptPresentationObserverAndForkBounds(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	submit(t, s, owner.ID, "bounded")
	turn := claim(t, s, owner.ID).Turn
	for i := range 65 {
		id := session.ModelAttemptID(fmt.Sprintf("attempt_%03d", i))
		attempt := reserveTest(t, s, attemptRequest(turn.ID, string(id)))
		dispatchTest(t, s, attempt.ID)
		p := displayTest(id, "reasoning")
		p.Parts = p.Parts[:1]
		if _, err := s.SettleModelAttempt(t.Context(), id, session.ModelAttemptResult{State: session.AttemptFailed, Presentation: p}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Failed, new("failed"), nil); err != nil {
		t.Fatal(err)
	}
	evidence, truncated, err := s.AttemptPresentations(t.Context(), owner.ID, 1, 1, 1)
	if err != nil || !truncated || len(evidence) != 64 || evidence[0].AttemptID != "attempt_001" || evidence[63].AttemptID != "attempt_064" {
		t.Fatal(evidence, truncated, err)
	}
	before := count(t, s, "sessions")
	if _, err := s.Fork(t.Context(), forkRequestTest(t, s, owner.ID, "too_many_presentations", 1), forkDefaultsTest()); !errors.Is(err, ErrLimit) {
		t.Fatal("oversized imported evidence accepted", err)
	}
	if count(t, s, "sessions") != before {
		t.Fatal("failed fork left partial import")
	}
	exact, err := s.ModelAttempts(t.Context(), turn.ID, "", 100)
	if err != nil || len(exact) != 65 || exact[0].Result.Presentation == nil {
		t.Fatal("bounded observation discarded exact evidence", len(exact), err)
	}
}
