package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func rewindRequest(t *testing.T, s *Store, owner session.SessionID, id session.HistoryEditID, keep int64) session.RewindRequest {
	t.Helper()
	snapshot, err := s.HistorySnapshot(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	return session.RewindRequest{ID: id, SessionID: owner, ExpectedRevision: snapshot.Revision, ObservedThrough: snapshot.ThroughSequence, KeepThrough: keep}
}

func rewindStopped(t *testing.T, s *Store, owner session.SessionID) {
	t.Helper()
	if _, err := s.SetLifecycle(t.Context(), owner, session.Stopped); err != nil {
		t.Fatal(err)
	}
}

func TestRewindWholeGroupsRetriesRestartAndNonreusedSequences(t *testing.T) {
	for _, kind := range []string{"root", "child"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s := openTest(t, path)
			_, owner := create(t, s, nil)
			if kind == "child" {
				child := controlChild(t, s, owner.ID, "child")
				owner = *child.Session
				if _, err := s.CancelInput(t.Context(), child.Admission.Input.ID); err != nil {
					t.Fatal(err)
				}
			}
			compactionHistoryTest(t, s, owner.ID, "first")
			history := compactionHistoryTest(t, s, owner.ID, "second")
			request := rewindRequest(t, s, owner.ID, "edit", 4)
			rewindStopped(t, s, owner.ID)
			writes := count(t, s, "logical_writes")
			edit, err := s.Rewind(t.Context(), request)
			if err != nil || edit.ExpectedRevision != 1 || edit.Revision != 2 || edit.ObservedThrough != 8 || edit.KeepThrough != 4 {
				t.Fatal(edit, err)
			}
			if count(t, s, "logical_writes") != writes {
				t.Fatal("rewind changed cumulative admission charges")
			}
			active, err := s.History(t.Context(), owner.ID, 0, 100)
			if err != nil || !reflect.DeepEqual(active, history[:4]) {
				t.Fatal("rewind changed retained bodies", active, err)
			}
			retired, err := s.ReadHistoryMessage(t.Context(), owner.ID, history[7].ID, 0, 65536)
			if err != nil || retired.Message.RetiredBy == nil || *retired.Message.RetiredBy != edit.ID {
				t.Fatal("retired exact evidence unavailable", retired, err)
			}
			if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Active); err != nil {
				t.Fatal(err)
			}
			compactionHistoryTest(t, s, owner.ID, "third")
			active, err = s.History(t.Context(), owner.ID, 4, 100)
			if err != nil || len(active) != 4 || active[0].Sequence != 9 || active[3].Sequence != 12 {
				t.Fatal("rewind reused a sequence", active, err)
			}
			// Replay succeeds despite a newer tail and active lifecycle. It is an
			// immutable acknowledgement, not an assertion of the current history.
			if again, err := s.Rewind(t.Context(), request); err != nil || !reflect.DeepEqual(again, edit) {
				t.Fatal("lost acknowledgement reapplied edit", again, err)
			}
			rewindStopped(t, s, owner.ID)
			all := rewindRequest(t, s, owner.ID, "keep_all", 12)
			if value, err := s.Rewind(t.Context(), all); err != nil || value.Revision != 3 {
				t.Fatal("keep-all did not explicitly reset history/REPL", value, err)
			}
			zero := rewindRequest(t, s, owner.ID, "keep_none", 0)
			if _, err := s.Rewind(t.Context(), zero); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openTest(t, path)
			if again, err := s.Rewind(t.Context(), request); err != nil || !reflect.DeepEqual(again, edit) {
				t.Fatal("restart/later edits changed original retry", again, err)
			}
			if snapshot, err := s.HistorySnapshot(t.Context(), owner.ID); err != nil || snapshot.Revision != 4 || snapshot.MessageCount != 0 {
				t.Fatal(snapshot, err)
			}
			changed := request
			changed.KeepThrough = 0
			if _, err := s.Rewind(t.Context(), changed); !errors.Is(err, ErrConflict) {
				t.Fatal("changed exact retry", err)
			}
			_, stranger := create(t, s, nil)
			changed = request
			changed.SessionID = stranger.ID
			if _, err := s.Rewind(t.Context(), changed); !errors.Is(err, ErrConflict) {
				t.Fatal("foreign retry claimed edit identity", err)
			}
			if _, err := s.HistoryEdit(t.Context(), stranger.ID, edit.ID); !errors.Is(err, ErrNotFound) {
				t.Fatal("foreign edit evidence was readable", err)
			}
		})
	}
}

func TestRewindAdmissionBoundaries(t *testing.T) {
	for _, kind := range []string{"active_idle", "queued", "running", "active_queued", "active_running", "unseen_append", "stale_revision", "midgroup", "missing", "empty"} {
		t.Run(kind, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			if kind != "empty" {
				compactionHistoryTest(t, s, owner.ID, "history")
			}
			request := rewindRequest(t, s, owner.ID, "rewind", 0)
			want := ErrConflict
			switch kind {
			case "queued", "running", "active_queued", "active_running":
				submit(t, s, owner.ID, "pending")
				if kind == "running" || kind == "active_running" {
					claim(t, s, owner.ID)
					request = rewindRequest(t, s, owner.ID, request.ID, 0)
				}
				want = ErrBusy
			case "unseen_append":
				compactionHistoryTest(t, s, owner.ID, "unseen")
			case "stale_revision":
				request.ExpectedRevision++
			case "midgroup":
				request.KeepThrough = 3
				want = session.ErrInvalid
			case "missing":
				request.SessionID = "missing"
				want = ErrNotFound
			case "empty", "active_idle":
				want = nil
			}
			if kind != "active_idle" && kind != "active_queued" && kind != "active_running" {
				rewindStopped(t, s, owner.ID)
			}
			_, err := s.Rewind(t.Context(), request)
			if !errors.Is(err, want) {
				t.Fatalf("rewind=%v want=%v", err, want)
			}
			if want != nil && count(t, s, "history_edits") != 0 {
				t.Fatal("refused edit left a durable mutation")
			}
		})
	}
}

func TestRewindSQLRollbackAndSummaryPinRetention(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	history := compactionHistoryTest(t, s, owner.ID, "first")
	turn := compactionTurnTest(t, s, owner.ID, "summarize_first")
	attempt := compactionAttemptTest(t, s, turn.ID, "summary_first")
	first := settleCompactionTest(t, s, attempt.ID, session.CompactionDraft{ID: "first", ThroughSequence: 2, PinnedMessageIDs: []session.MessageID{history[0].ID}, Text: "first prefix"})
	finishMailTest(t, s, turn.ID, session.Succeeded)
	compactionHistoryTest(t, s, owner.ID, "second")
	rewindStopped(t, s, owner.ID)
	request := rewindRequest(t, s, owner.ID, "keep_first", 4)
	if _, err := s.Rewind(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if head, err := s.ContextHead(t.Context(), owner.ID); err != nil || !reflect.DeepEqual(head, first.Head) {
		t.Fatal("wholly retained summary/pins lost selection", head, err)
	}
	request = rewindRequest(t, s, owner.ID, "clear", 0)
	for _, point := range []string{"INSERT ON history_edits", "UPDATE OF history_revision ON sessions", "UPDATE ON messages", "UPDATE ON context_heads"} {
		execTest(t, s, "CREATE TRIGGER fail_rewind BEFORE "+point+" BEGIN SELECT RAISE(ABORT,'injected'); END")
		if _, err := s.Rewind(t.Context(), request); err == nil {
			t.Fatal("failed write accepted", point)
		}
		execTest(t, s, "DROP TRIGGER fail_rewind")
		if snapshot, err := s.HistorySnapshot(t.Context(), owner.ID); err != nil || snapshot.Revision != 2 || snapshot.MessageCount != 4 || count(t, s, "history_edits") != 1 {
			t.Fatal("partial retirement survived SQL rollback", snapshot, err)
		}
		if head, err := s.ContextHead(t.Context(), owner.ID); err != nil || !reflect.DeepEqual(head, first.Head) {
			t.Fatal("context head survived partial write", head, err)
		}
	}
	if _, err := s.Rewind(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	head, err := s.ContextHead(t.Context(), owner.ID)
	if err != nil || head.CompactionID != nil || head.Revision != first.Head.Revision+1 {
		t.Fatal("incompatible selection survived", head, err)
	}
	if _, err := s.SelectCompaction(t.Context(), owner.ID, head.Revision, &first.Compaction.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("retired summary/pin could be reselected", err)
	}
	if exact, err := s.Compaction(t.Context(), owner.ID, first.Compaction.ID); err != nil || !reflect.DeepEqual(exact, *first.Compaction) {
		t.Fatal("summary evidence changed", exact, err)
	}
}

func TestRewindResumeRaceAndExactConcurrentRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	other := openTest(t, path)
	for i := range 8 {
		_, owner := create(t, s, nil)
		rewindStopped(t, s, owner.ID)
		request := rewindRequest(t, s, owner.ID, session.HistoryEditID("race_"+strconv.Itoa(i)), 0)
		start := make(chan struct{})
		var workers sync.WaitGroup
		var rewindErr, resumeErr error
		workers.Go(func() { <-start; _, rewindErr = s.Rewind(t.Context(), request) })
		workers.Go(func() { <-start; _, resumeErr = other.SetLifecycle(t.Context(), owner.ID, session.Active) })
		close(start)
		workers.Wait()
		if resumeErr != nil || (rewindErr != nil && !errors.Is(rewindErr, ErrConflict)) {
			t.Fatal(rewindErr, resumeErr)
		}
		current, err := s.Session(t.Context(), owner.ID)
		if err != nil || current.Lifecycle != session.Active || (rewindErr == nil) != (current.HistoryRevision == 2) {
			t.Fatal("resume/rewind escaped serialized admission", current, err)
		}
		if rewindErr != nil {
			rewindStopped(t, s, owner.ID)
		}
		var edits [2]session.HistoryEdit
		var failures [2]error
		workers.Go(func() { edits[0], failures[0] = s.Rewind(t.Context(), request) })
		workers.Go(func() { edits[1], failures[1] = other.Rewind(t.Context(), request) })
		workers.Wait()
		if failures[0] != nil || failures[1] != nil || !reflect.DeepEqual(edits[0], edits[1]) {
			t.Fatal("concurrent exact retry diverged", edits, failures)
		}
	}
}

func TestRewindLeavesIndependentMailGoalsStateAndAccounting(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	compactionHistoryTest(t, s, owner.ID, "history")
	goal := createGoalTest(t, s, owner.ID, "goal", nil, false)
	mail := sendMailTest(t, s, session.MailSpec{ID: "mail", SenderID: owner.ID, RecipientID: owner.ID, Delivery: session.MailQueued, Body: "pending mail survives"})
	private := putStateTest(t, s, stateWrite(owner.ID, session.SessionState, "private", "key", `"private"`, 0))
	shared := putStateTest(t, s, stateWrite(owner.ID, session.TreeState, "shared", "key", `"shared"`, 0))
	budgets, err := s.Budgets(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	rewindStopped(t, s, owner.ID)
	if _, err := s.Rewind(t.Context(), rewindRequest(t, s, owner.ID, "rewind", 0)); err != nil {
		t.Fatal(err)
	}
	if current, err := s.CurrentGoal(t.Context(), owner.ID); err != nil || !reflect.DeepEqual(current, goal.Goal) {
		t.Fatal("rewind changed goal", current, err)
	}
	if current, err := s.ReadMail(t.Context(), owner.ID, mail.ID); err != nil || !reflect.DeepEqual(current.MailMetadata, mail) {
		t.Fatal("rewind presented or changed pending mail", current, err)
	}
	for _, value := range []session.StateValue{private, shared} {
		if current, err := s.StateValue(t.Context(), owner.ID, value.ID); err != nil || !reflect.DeepEqual(current, value) {
			t.Fatal("rewind changed state", current, err)
		}
	}
	if current, err := s.Budgets(t.Context(), owner.ID); err != nil || !reflect.DeepEqual(current, budgets) {
		t.Fatal("rewind changed accounting", current, err)
	}
}

func TestRewindImportedGroupBoundarySurvivesSourceDeletion(t *testing.T) {
	s := fresh(t)
	_, source := create(t, s, nil)
	compactionHistoryTest(t, s, source.ID, "first")
	history := compactionHistoryTest(t, s, source.ID, "second")
	_, destination := create(t, s, nil)
	// At sequence five the first group is complete, but the second group began
	// earlier and still has later messages. A single final-message check is not
	// sufficient for an imported transcript with interleaved groups.
	interleaved := append([]session.Message{history[0], history[4]}, history[1:4]...)
	interleaved = append(interleaved, history[5:]...)
	importHistoryTest(t, s, destination.ID, interleaved)
	if err := s.DeleteSubtree(t.Context(), source.ID); err != nil {
		t.Fatal(err)
	}
	rewindStopped(t, s, destination.ID)
	if _, err := s.Rewind(t.Context(), rewindRequest(t, s, destination.ID, "split", 5)); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("rewind split a different imported group", err)
	}
	if _, err := s.Rewind(t.Context(), rewindRequest(t, s, destination.ID, "all", 8)); err != nil {
		t.Fatal("complete imported groups required live source turns", err)
	}
}

func TestIdleRewindAndSubmissionSerializeWithoutStopping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	for i := range 8 {
		_, owner := create(t, s, nil)
		compactionHistoryTest(t, s, owner.ID, "history_"+strconv.Itoa(i))
		request := rewindRequest(t, s, owner.ID, session.HistoryEditID("idle_"+strconv.Itoa(i)), 0)
		start := make(chan struct{})
		var workers sync.WaitGroup
		var edit session.HistoryEdit
		var rewindErr, submitErr error
		workers.Go(func() { <-start; edit, rewindErr = s.Rewind(t.Context(), request) })
		workers.Go(func() {
			<-start
			_, submitErr = other.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "next_" + strconv.Itoa(i)}, Submission{
				SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "continue"}},
			})
		})
		close(start)
		workers.Wait()
		if submitErr != nil || (rewindErr != nil && !errors.Is(rewindErr, ErrBusy)) {
			t.Fatal(rewindErr, submitErr)
		}
		current, err := s.Session(t.Context(), owner.ID)
		if err != nil || current.Lifecycle != session.Active {
			t.Fatal("rewind changed lifecycle", current, err)
		}
		turn := claim(t, s, owner.ID).Turn
		if turn.HistoryRevision != current.HistoryRevision || (rewindErr == nil) != (turn.HistoryRevision == 2) {
			t.Fatal("turn crossed rewind boundary", turn, current)
		}
		if rewindErr == nil {
			retried, err := other.Rewind(t.Context(), request)
			if err != nil || !reflect.DeepEqual(retried, edit) {
				t.Fatal("exact retry changed during active work", retried, err)
			}
		}
	}
}
