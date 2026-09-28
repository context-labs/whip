package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestForkCompactionChainPinsAndSelectionCoverage(t *testing.T) {
	s := fresh(t)
	_, source := create(t, s, nil)
	history := compactionHistoryTest(t, s, source.ID, "first")
	turn := compactionTurnTest(t, s, source.ID, "compact")
	baseAttempt := compactionAttemptTest(t, s, turn.ID, "base_attempt")
	base := settleCompactionTest(t, s, baseAttempt.ID, session.CompactionDraft{ID: "base", ThroughSequence: 1, PinnedMessageIDs: []session.MessageID{history[0].ID}, Text: "base summary"})
	selectedAttempt := compactionAttemptTest(t, s, turn.ID, "selected_attempt")
	selected := settleCompactionTest(t, s, selectedAttempt.ID, session.CompactionDraft{ID: "selected", BaseID: new(base.Compaction.ID), ExpectedRevision: 1, ThroughSequence: 2, PinnedMessageIDs: []session.MessageID{history[0].ID}, Text: "selected summary"})
	if !selected.Selected {
		t.Fatal(selected)
	}
	finishMailTest(t, s, turn.ID, session.Succeeded)
	attempts, writes := count(t, s, "model_attempts"), count(t, s, "logical_writes")
	request := forkRequestTest(t, s, source.ID, "summarized", 4)
	// Selection failure is part of the same transaction as copied history.
	execTest(t, s, `CREATE TRIGGER fail_fork_head BEFORE INSERT ON context_heads BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := s.Fork(t.Context(), request, forkDefaultsTest()); err == nil {
		t.Fatal("selection failure ignored")
	}
	if count(t, s, "sessions") != 1 || count(t, s, "compactions") != 2 || count(t, s, "forks") != 0 {
		t.Fatal("summary import escaped rollback")
	}
	execTest(t, s, "DROP TRIGGER fail_fork_head")
	result := forkTest(t, s, request)
	head, err := s.ContextHead(t.Context(), result.Root.ID)
	if err != nil || head.CompactionID == nil || *head.CompactionID == selected.Compaction.ID || head.Revision != 1 {
		t.Fatal(head, err)
	}
	copySelected, err := s.Compaction(t.Context(), result.Root.ID, *head.CompactionID)
	if err != nil || copySelected.Source == nil || *copySelected.Source != (session.CompactionSource{SessionID: source.ID, CompactionID: selected.Compaction.ID}) || copySelected.Text != selected.Compaction.Text || copySelected.TurnID != "" || copySelected.AttemptID != "" || copySelected.HistoryRevision != 1 || copySelected.BaseID == nil || *copySelected.BaseID == base.Compaction.ID {
		t.Fatal(copySelected, err)
	}
	copyBase, err := s.Compaction(t.Context(), result.Root.ID, *copySelected.BaseID)
	if err != nil || copyBase.Source == nil || copyBase.Source.CompactionID != base.Compaction.ID || copyBase.Text != base.Compaction.Text {
		t.Fatal(copyBase, err)
	}
	copied, err := s.History(t.Context(), result.Root.ID, 0, 100)
	if err != nil || !reflect.DeepEqual(copySelected.PinnedMessageIDs, []session.MessageID{copied[0].ID}) || !reflect.DeepEqual(copyBase.PinnedMessageIDs, copySelected.PinnedMessageIDs) {
		t.Fatal(copySelected.PinnedMessageIDs, err)
	}
	if pin, err := s.ContextBoundaryPin(t.Context(), result.Root.ID, 2); err != nil || pin == nil || *pin != copied[0].ID {
		t.Fatal("partial imported group lost opening pin", pin, err)
	}
	if pin, err := s.ContextBoundaryPin(t.Context(), result.Root.ID, 4); err != nil || pin != nil {
		t.Fatal("whole imported group retained required pin", pin, err)
	}
	if count(t, s, "model_attempts") != attempts || count(t, s, "logical_writes") != writes {
		t.Fatal("summary fabricated accounting")
	}
	if err := s.DeleteSubtree(t.Context(), source.ID); err != nil {
		t.Fatal(err)
	}
	if value, err := s.Compaction(t.Context(), result.Root.ID, copySelected.ID); err != nil || !reflect.DeepEqual(value, copySelected) {
		t.Fatal("source deletion changed summary", value, err)
	}
	// Undo follows destination summary identities after source deletion.
	if _, err := s.SelectCompaction(t.Context(), result.Root.ID, head.Revision, copySelected.BaseID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Compaction(t.Context(), result.Root.ID, selected.Compaction.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("source summary gained destination ownership", err)
	}
}

func TestForkDoesNotImportSelectionBeyondBoundaryOrRetiredHistory(t *testing.T) {
	s := fresh(t)
	_, source := create(t, s, nil)
	compactionHistoryTest(t, s, source.ID, "first")
	compactionHistoryTest(t, s, source.ID, "second")
	turn := compactionTurnTest(t, s, source.ID, "compact")
	attempt := compactionAttemptTest(t, s, turn.ID, "summary_attempt")
	settleCompactionTest(t, s, attempt.ID, session.CompactionDraft{ID: "summary", ThroughSequence: 8, Text: "includes second group"})
	finishMailTest(t, s, turn.ID, session.Succeeded)
	result := forkTest(t, s, forkRequestTest(t, s, source.ID, "prefix", 4))
	if head, err := s.ContextHead(t.Context(), result.Root.ID); err != nil || head.CompactionID != nil || head.Revision != 0 {
		t.Fatal("later summary selected", head, err)
	}
	retireHistoryTest(t, s, source.ID, 4)
	// Use a later native group so raw sequence gaps and post-rewind history are
	// copied without restoring the retired group or selecting its summary.
	compactionHistoryTest(t, s, source.ID, "replacement")
	request := forkRequestTest(t, s, source.ID, "revised", 12)
	result = forkTest(t, s, request)
	history, err := s.History(t.Context(), result.Root.ID, 0, 100)
	if err != nil || len(history) != 8 || history[4].Sequence != 9 || history[4].Source.Sequence != 9 {
		t.Fatal("retired history or sequence gap lost", history, err)
	}
	if head, err := s.ContextHead(t.Context(), result.Root.ID); err != nil || head.CompactionID != nil {
		t.Fatal("retired summary selected", head, err)
	}
}
