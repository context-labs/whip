package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func createGoalTest(t *testing.T, s *Store, owner session.SessionID, id session.GoalID, expected *session.GoalRef, start bool) GoalAdmission {
	t.Helper()
	value, err := s.CreateGoal(t.Context(), owner, id, expected, session.GoalRequest{Text: "objective " + string(id)}, start)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestGoalCreationRetryReplacementDeletionAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	empty, err := s.CurrentGoal(t.Context(), owner.ID)
	if err != nil || empty != nil {
		t.Fatal(empty, err)
	}
	original := createGoalTest(t, s, owner.ID, "first", nil, true)
	if !original.Current || original.Initial == nil || original.Initial.Input.Source != session.GoalInput || original.Initial.Input.Goal == nil || *original.Initial.Input.Goal != original.Goal.GoalRef {
		t.Fatalf("admission %+v", original)
	}
	if original.Initial.Input.Parts[0].Text == original.Goal.Spec.Text {
		t.Fatal("goal template copied into input")
	}
	next := createGoalTest(t, s, owner.ID, "second", &original.Goal.GoalRef, false)
	oldInput, err := s.Input(t.Context(), original.Initial.Input.ID)
	if err != nil || oldInput.State != session.InputCancelled {
		t.Fatal(oldInput, err)
	}
	replay := createGoalTest(t, s, owner.ID, "first", nil, true)
	if replay.Current || replay.Goal.State != session.GoalSuperseded || replay.Initial.Input.ID != original.Initial.Input.ID || count(t, s, "goals") != 2 || count(t, s, "inputs") != 1 || count(t, s, "logical_writes") != 3 {
		t.Fatalf("replay changed selection %+v", replay)
	}
	if _, err := s.CreateGoal(t.Context(), owner.ID, "first", nil, session.GoalRequest{Text: ""}, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("digest must precede content validation %v", err)
	}
	cancelled, err := s.CancelGoal(t.Context(), owner.ID, next.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGoal(t.Context(), owner.ID, "third", nil, session.GoalRequest{Text: "new"}, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("terminal current ignored %v", err)
	}
	current, err := s.CurrentGoal(t.Context(), owner.ID)
	if err != nil || current.GoalRef != cancelled.Goal.GoalRef {
		t.Fatal(current, err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	tombstone := createGoalTest(t, reopened, owner.ID, "first", nil, true)
	if tombstone.DeletedAt == nil || tombstone.Goal != nil || tombstone.Current || tombstone.Initial == nil || tombstone.Initial.Receipt.DeletedAt == nil {
		t.Fatalf("bad tombstone %+v", tombstone)
	}
	if _, err := reopened.Goal(t.Context(), owner.ID, "first"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := reopened.CurrentGoal(t.Context(), owner.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	var retained int
	if err := reopened.db.QueryRowContext(t.Context(), "SELECT count(*) FROM goals WHERE text IS NOT NULL OR stop_reason IS NOT NULL").Scan(&retained); err != nil || retained != 0 {
		t.Fatal(retained, err)
	}
}

func TestGoalConcurrentCreationCAS(t *testing.T) {
	for _, same := range []bool{false, true} {
		t.Run(strconv.FormatBool(same), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			dbs := []*Store{openTest(t, path), openTest(t, path)}
			_, owner := create(t, dbs[0], nil)
			results := make([]GoalAdmission, 2)
			failures := make([]error, 2)
			var wg sync.WaitGroup
			for i, db := range dbs {
				wg.Go(func() {
					id := session.GoalID(fmt.Sprintf("goal%d", i))
					if same {
						id = "same"
					}
					results[i], failures[i] = db.CreateGoal(t.Context(), owner.ID, id, nil, session.GoalRequest{Text: "objective"}, true)
				})
			}
			wg.Wait()
			wins := 0
			for _, err := range failures {
				if err == nil {
					wins++
				} else if !errors.Is(err, ErrConflict) {
					t.Fatal(err)
				}
			}
			want := 1
			if same {
				want = 2
				if results[0].Initial.Input.ID != results[1].Initial.Input.ID {
					t.Fatal("double admission")
				}
			}
			if wins != want || count(t, dbs[0], "goals") != 1 || count(t, dbs[0], "inputs") != 1 || count(t, dbs[0], "logical_writes") != 2 {
				t.Fatal(wins, failures)
			}
		})
	}
}

func TestGoalCreateRollbackAtEveryWrite(t *testing.T) {
	faults := []string{
		"BEFORE UPDATE ON goals", "BEFORE UPDATE ON inputs", "BEFORE INSERT ON goals",
		"BEFORE INSERT ON inputs", "BEFORE INSERT ON receipts",
		"BEFORE INSERT ON logical_writes WHEN NEW.source_kind='goal'",
		"BEFORE INSERT ON logical_writes WHEN NEW.source_kind='input'",
	}
	for i, fault := range faults {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			first := createGoalTest(t, s, owner.ID, "old", nil, true)
			execTest(t, s, "CREATE TRIGGER fail "+fault+" BEGIN SELECT RAISE(ABORT,'fault'); END")
			if _, err := s.CreateGoal(t.Context(), owner.ID, "new", &first.Goal.GoalRef, session.GoalRequest{Text: "replacement"}, true); err == nil {
				t.Fatal("fault ignored")
			}
			old, err := s.Goal(t.Context(), owner.ID, first.ID)
			if err != nil || !reflect.DeepEqual(old, *first.Goal) {
				t.Fatal(old, err)
			}
			input, err := s.Input(t.Context(), first.Initial.Input.ID)
			if err != nil || input.State != session.Queued {
				t.Fatal(input, err)
			}
			if count(t, s, "goals") != 1 || count(t, s, "inputs") != 1 || count(t, s, "receipts") != 1 || count(t, s, "logical_writes") != 2 {
				t.Fatal("partial creation")
			}
			execTest(t, s, "DROP TRIGGER fail")
			createGoalTest(t, s, owner.ID, "new", &first.Goal.GoalRef, true)
		})
	}
}

func TestGoalAdmissionCapacityRollback(t *testing.T) {
	for _, kind := range []string{"queue", "writes", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			switch kind {
			case "queue":
				resourceLimit(t, s, owner.ID, session.ResourceQueuedInputs, 0)
			case "writes":
				budgetLimit(t, s, owner.ID, session.BudgetLogicalWrites, 1)
			case "bytes":
				budgetLimit(t, s, owner.ID, session.BudgetLogicalWriteBytes, 9)
			}
			_, err := s.CreateGoal(t.Context(), owner.ID, "goal", nil, session.GoalRequest{Text: "objective"}, true)
			if !errors.Is(err, ErrLimit) {
				t.Fatal(err)
			}
			if count(t, s, "goals") != 0 || count(t, s, "inputs") != 0 || count(t, s, "receipts") != 0 || count(t, s, "logical_writes") != 0 {
				t.Fatal("partial limited admission")
			}
		})
	}
	// Replacement frees its old queued input before testing the same reusable cap.
	s := fresh(t)
	_, owner := create(t, s, nil)
	resourceLimit(t, s, owner.ID, session.ResourceQueuedInputs, 1)
	old := createGoalTest(t, s, owner.ID, "old", nil, true)
	createGoalTest(t, s, owner.ID, "new", &old.Goal.GoalRef, true)
	if resourceState(t, s, owner.ID, session.ResourceQueuedInputs).Used != 1 {
		t.Fatal("replacement leaked queue capacity")
	}
}

func TestGoalResumeExactReceiptsAllowanceAndRollback(t *testing.T) {
	for _, fault := range []string{"BEFORE UPDATE ON goals", "BEFORE INSERT ON inputs", "BEFORE INSERT ON receipts", "BEFORE INSERT ON logical_writes"} {
		t.Run(fault, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			goal := createGoalTest(t, s, owner.ID, "goal", nil, false)
			identity := session.RequestIdentity{ClientID: "client", RequestID: "resume"}
			execTest(t, s, "CREATE TRIGGER fail "+fault+" BEGIN SELECT RAISE(ABORT,'fault'); END")
			if _, err := s.ResumeGoal(t.Context(), identity, owner.ID, goal.Goal.GoalRef); err == nil {
				t.Fatal("fault ignored")
			}
			current, _ := s.Goal(t.Context(), owner.ID, goal.ID)
			if !reflect.DeepEqual(current, *goal.Goal) || count(t, s, "inputs") != 0 || count(t, s, "receipts") != 0 || count(t, s, "logical_writes") != 1 {
				t.Fatal("partial resume")
			}
			execTest(t, s, "DROP TRIGGER fail")
			admitted, err := s.ResumeGoal(t.Context(), identity, owner.ID, goal.Goal.GoalRef)
			if err != nil {
				t.Fatal(err)
			}
			changed, err := s.CancelGoal(t.Context(), owner.ID, goal.ID)
			if err != nil {
				t.Fatal(err)
			}
			createGoalTest(t, s, owner.ID, "next", &changed.Goal.GoalRef, false)
			replay, err := s.ResumeGoal(t.Context(), identity, owner.ID, goal.Goal.GoalRef)
			if err != nil || replay.Input.ID != admitted.Input.ID || replay.Input.State != session.InputCancelled {
				t.Fatal(replay, err)
			}
			if _, err := s.ResumeGoal(t.Context(), identity, owner.ID, changed.Goal.GoalRef); !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
			if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
				t.Fatal(err)
			}
			replay, err = s.ResumeGoal(t.Context(), identity, owner.ID, goal.Goal.GoalRef)
			if err != nil || replay.Receipt.DeletedAt == nil {
				t.Fatal(replay, err)
			}
		})
	}
	s := fresh(t)
	_, owner := create(t, s, nil)
	goal, err := s.CreateGoal(t.Context(), owner.ID, "zero", nil, session.GoalRequest{Text: "initial only", MaxContinuations: new(int64(0))}, false)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "c", RequestID: "first"}, owner.ID, goal.Goal.GoalRef)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := s.CurrentGoal(t.Context(), owner.ID)
	if _, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "c", RequestID: "busy"}, owner.ID, current.GoalRef); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, err := s.CancelInput(t.Context(), first.Input.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "c", RequestID: "exhausted"}, owner.ID, current.GoalRef); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}

func TestGoalCancellationTargetsExactGoalTurnAndRollsBack(t *testing.T) {
	for _, fault := range []string{"BEFORE UPDATE ON goals", "BEFORE UPDATE ON inputs", "BEFORE UPDATE ON turns"} {
		t.Run(fault, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			goal := createGoalTest(t, s, owner.ID, "goal", nil, true)
			var running *Claim
			if strings.Contains(fault, "turns") {
				turn := claim(t, s, owner.ID)
				running = &turn
			}
			execTest(t, s, "CREATE TRIGGER fail "+fault+" BEGIN SELECT RAISE(ABORT,'fault'); END")
			if _, err := s.CancelGoal(t.Context(), owner.ID, goal.ID); err == nil {
				t.Fatal("fault ignored")
			}
			unchanged, _ := s.Goal(t.Context(), owner.ID, goal.ID)
			if !reflect.DeepEqual(unchanged, *goal.Goal) {
				t.Fatal("partial cancel")
			}
			execTest(t, s, "DROP TRIGGER fail")
			cancelled, err := s.CancelGoal(t.Context(), owner.ID, goal.ID)
			if err != nil {
				t.Fatal(err)
			}
			if running != nil {
				if cancelled.CancelTurnID == nil || *cancelled.CancelTurnID != running.Turn.ID {
					t.Fatal("missing exact turn")
				}
				if _, err := s.Finish(t.Context(), running.Turn.ID, session.Cancelled, nil, nil); err != nil {
					t.Fatal(err)
				}
			} else if cancelled.CancelTurnID != nil {
				t.Fatal("unclaimed input had turn")
			}
			submit(t, s, owner.ID, "human")
			human := claim(t, s, owner.ID)
			replay, err := s.CancelGoal(t.Context(), owner.ID, goal.ID)
			if err != nil || replay.CancelTurnID != nil || !reflect.DeepEqual(replay.Goal, cancelled.Goal) {
				t.Fatal(replay, err)
			}
			turn, _ := s.Turn(t.Context(), human.Turn.ID)
			if turn.State != session.Running {
				t.Fatal("cancelled unrelated human turn")
			}
		})
	}
}

func TestGoalOwnershipEligibilityLifecycleAndNamespace(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	child := controlChild(t, s, root.ID, "child")
	if _, err := s.CancelInput(t.Context(), child.Admission.Input.ID); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []session.Session{root, *child.Session} {
		id := session.GoalID("goal_" + owner.ID)
		goal := createGoalTest(t, s, owner.ID, id, nil, false)
		other := root.ID
		if owner.ID == root.ID {
			other = child.Session.ID
		}
		if _, err := s.Goal(t.Context(), other, id); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if _, err := s.CancelGoal(t.Context(), other, id); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if _, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "client", RequestID: string(id)}, other, goal.Goal.GoalRef); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "client", RequestID: string(id)}, owner.ID, goal.Goal.GoalRef); !errors.Is(err, ErrStopped) {
			t.Fatal(err)
		}
		createGoalTest(t, s, owner.ID, id, nil, false) // Retry ignores stopped lifecycle.
		if _, err := s.CancelGoal(t.Context(), owner.ID, id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Active); err != nil {
			t.Fatal(err)
		}
	}
	current, _ := s.CurrentGoal(t.Context(), root.ID)
	root, err := s.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(false)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGoal(t.Context(), root.ID, "disabled", &current.GoalRef, session.GoalRequest{Text: "denied"}, false); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
	identity := goalInitialIdentity("reserved")
	if _, err := s.Admit(t.Context(), identity, Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "collision"}}}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.SpawnChild(t.Context(), identity, childRequest(root.ID)); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.ResumeGoal(t.Context(), identity, root.ID, current.GoalRef); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "c", RequestID: "fake"}, Submission{SessionID: root.ID, Source: session.GoalInput, Goal: &current.GoalRef, Parts: []session.Part{{Type: "text", Text: "fake"}}}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestGoalEligibilityCapturedForExistingChildAndTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, root := create(t, s, nil)
	child := controlChild(t, s, root.ID, "child")
	turn := claim(t, s, child.Session.ID)
	if !turn.Configuration.GoalsEnabled {
		t.Fatal("assistant eligibility missing")
	}
	if _, err := s.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(false)}); err != nil {
		t.Fatal(err)
	}
	childNow, err := s.Session(t.Context(), child.Session.ID)
	if err != nil || !childNow.Config.GoalsEnabled {
		t.Fatal("parent changed copied child", err)
	}
	if _, err := s.UpdateConfiguration(t.Context(), childNow.ID, childNow.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(false)}); err != nil {
		t.Fatal(err)
	}
	saved, err := s.Configuration(t.Context(), turn.Turn.SessionID, turn.Turn.ConfigRevision)
	if err != nil || !saved.GoalsEnabled {
		t.Fatal("active snapshot changed", err)
	}
	if _, err := s.Finish(t.Context(), turn.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	submit(t, reopened, childNow.ID, "next")
	next := claim(t, reopened, childNow.ID)
	if next.Configuration.GoalsEnabled {
		t.Fatal("new turn did not capture eligibility change")
	}
}

func TestGoalBusyAndResumeCapacityKeepGoalUnchanged(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	goal := createGoalTest(t, s, owner.ID, "goal", nil, false)
	submit(t, s, owner.ID, "human")
	active := claim(t, s, owner.ID)
	if _, err := s.CreateGoal(t.Context(), owner.ID, "other", &goal.Goal.GoalRef, session.GoalRequest{Text: "other"}, false); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "c", RequestID: "r"}, owner.ID, goal.Goal.GoalRef); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), active.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	resourceLimit(t, s, owner.ID, session.ResourceQueuedInputs, 0)
	if _, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "c", RequestID: "r"}, owner.ID, goal.Goal.GoalRef); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	unchanged, _ := s.Goal(t.Context(), owner.ID, goal.ID)
	if !reflect.DeepEqual(unchanged, *goal.Goal) {
		t.Fatal("limited resume changed goal")
	}
	resourceLimit(t, s, owner.ID, session.ResourceQueuedInputs, 1)
	budgetLimit(t, s, owner.ID, session.BudgetLogicalWrites, 1)
	if _, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "c", RequestID: "r"}, owner.ID, goal.Goal.GoalRef); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	unchanged, _ = s.Goal(t.Context(), owner.ID, goal.ID)
	if !reflect.DeepEqual(unchanged, *goal.Goal) {
		t.Fatal("write limit changed goal")
	}
}

func TestGoalPausedResumePreservesUsageAndTerminalCurrent(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	goal, err := s.CreateGoal(t.Context(), owner.ID, "goal", nil, session.GoalRequest{Text: "objective", MaxContinuations: new(int64(3))}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelInput(t.Context(), goal.Initial.Input.ID); err != nil {
		t.Fatal(err)
	}
	// Execution hooks arrive in the next slice; establish their persisted paused
	// outcome directly to test this admission boundary against nonzero usage.
	if _, err := s.db.ExecContext(t.Context(), `UPDATE goals SET state='paused',revision=revision+1,continuations_used=2,stop_reason='provider failure' WHERE id=?`, goal.ID); err != nil {
		t.Fatal(err)
	}
	paused, err := s.Goal(t.Context(), owner.ID, goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "client", RequestID: "resume"}, owner.ID, paused.GoalRef)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := s.Goal(t.Context(), owner.ID, goal.ID)
	if err != nil || resumed.ContinuationsUsed != 2 || resumed.State != session.GoalArmed || resumed.StopReason != nil || resumed.Revision != paused.Revision+1 || admitted.Input.Goal.Revision != resumed.Revision {
		t.Fatal(resumed, err)
	}
	if _, err := s.CancelInput(t.Context(), admitted.Input.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), `UPDATE goals SET state='paused',revision=revision+1,continuations_used=3 WHERE id=?`, goal.ID); err != nil {
		t.Fatal(err)
	}
	exhausted, _ := s.Goal(t.Context(), owner.ID, goal.ID)
	if _, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "client", RequestID: "exhausted"}, owner.ID, exhausted.GoalRef); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	for _, update := range []string{`text='changed'`, `max_continuations=4`, `continuations_used=1`, `initial_digest='changed'`, `ordinal=ordinal+1`} {
		if _, err := s.db.ExecContext(t.Context(), "UPDATE goals SET revision=revision+1,"+update+" WHERE id=?", goal.ID); err == nil {
			t.Fatal("immutable goal change admitted", update)
		}
	}
	if _, err := s.db.ExecContext(t.Context(), `UPDATE goals SET state='completed',revision=revision+1 WHERE id=?`, goal.ID); err != nil {
		t.Fatal(err)
	}
	completed, _ := s.CurrentGoal(t.Context(), owner.ID)
	cancelled, err := s.CancelGoal(t.Context(), owner.ID, goal.ID)
	if err != nil || !reflect.DeepEqual(cancelled.Goal, *completed) {
		t.Fatal("terminal cancel changed record", err)
	}
	if _, err := s.CreateGoal(t.Context(), owner.ID, "next", nil, session.GoalRequest{Text: "new"}, false); !errors.Is(err, ErrConflict) {
		t.Fatal("completed was not current", err)
	}
	createGoalTest(t, s, owner.ID, "next", &completed.GoalRef, false)
}

func TestGoalChildDeletionRetainsTombstoneWithoutChangingParent(t *testing.T) {
	s := fresh(t)
	_, parent := create(t, s, nil)
	child := controlChild(t, s, parent.ID, "child")
	if _, err := s.CancelInput(t.Context(), child.Admission.Input.ID); err != nil {
		t.Fatal(err)
	}
	rootGoal := createGoalTest(t, s, parent.ID, "root-goal", nil, false)
	childGoal := createGoalTest(t, s, child.Session.ID, "child-goal", nil, true)
	if err := s.DeleteSubtree(t.Context(), child.Session.ID); err != nil {
		t.Fatal(err)
	}
	replay := createGoalTest(t, s, child.Session.ID, childGoal.ID, nil, true)
	if replay.DeletedAt == nil || replay.Goal != nil {
		t.Fatal("missing child tombstone")
	}
	selected, err := s.CurrentGoal(t.Context(), parent.ID)
	if err != nil || !reflect.DeepEqual(*selected, *rootGoal.Goal) {
		t.Fatal("child deletion changed parent", err)
	}
}
