package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestTurnPermitsCompetingClaimsKeepRejectedInputQueued(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	_, root := create(t, s, []session.ResourceLimit{{Kind: session.ResourceRunnableDescendants, Limit: new(int64(1))}})
	children := []ChildAdmission{
		spawnChildTest(t, s, "left", childRequest(root.ID)),
		spawnChildTest(t, s, "right", childRequest(root.ID)),
	}
	results := make([]Claim, 2)
	failures := make([]error, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i, db := range []*Store{s, other} {
		workers.Go(func() {
			<-start
			results[i], failures[i] = db.Claim(t.Context(), children[i].Session.ID)
		})
	}
	close(start)
	workers.Wait()
	winner, loser := 0, 1
	if failures[0] != nil {
		winner, loser = 1, 0
	}
	if failures[winner] != nil || !errors.Is(failures[loser], ErrLimit) {
		t.Fatalf("sibling claim results: %v", failures)
	}
	if count(t, s, "turns") != 1 || count(t, s, "messages") != 1 || count(t, s, "turn_permits") != 1 {
		t.Fatal("rejected claim left partial turn, history, or permit")
	}
	if input := children[loser].Admission.Input; input == nil {
		t.Fatal("missing rejected input")
	} else {
		stored, err := s.Input(t.Context(), input.ID)
		if err != nil || stored.TurnID != nil || stored.State != session.Queued {
			t.Fatalf("denied claim consumed input: %+v %v", stored, err)
		}
	}
	if _, err := s.Finish(t.Context(), results[winner].Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Claim(t.Context(), children[loser].Session.ID); err != nil {
		t.Fatalf("finished sibling did not release permit: %v", err)
	}
}

func TestTurnPermitsExcludeOwnerAndHonorEveryProperAncestor(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, []session.ResourceLimit{{Kind: session.ResourceRunnableDescendants, Limit: new(int64(0))}})
	submit(t, s, root.ID, "root")
	rootTurn := claim(t, s, root.ID).Turn
	child := spawnChildTest(t, s, "child", childRequest(root.ID)).Session
	if _, err := s.Claim(t.Context(), child.ID); !errors.Is(err, ErrLimit) {
		t.Fatalf("zero descendant cap admitted child: %v", err)
	}
	if resourceState(t, s, root.ID, session.ResourceRunnableDescendants).Used != 0 || count(t, s, "turn_permits") != 1 {
		t.Fatal("root's own execution was charged as a descendant")
	}
	resourceLimit(t, s, root.ID, session.ResourceRunnableDescendants, 1)
	resourceLimit(t, s, child.ID, session.ResourceRunnableDescendants, 0)
	childTurn := claim(t, s, child.ID).Turn
	grandchild := spawnChildTest(t, s, "grandchild", childRequest(child.ID)).Session
	if err := s.YieldTurn(t.Context(), childTurn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(t.Context(), grandchild.ID); !errors.Is(err, ErrLimit) {
		t.Fatalf("free root capacity bypassed direct-parent zero cap: %v", err)
	}
	if err := s.ResumeTurn(t.Context(), childTurn.ID); err != nil {
		t.Fatalf("child's own zero cap prevented its resumption: %v", err)
	}
	if err := s.YieldTurn(t.Context(), rootTurn.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ResumeTurn(t.Context(), rootTurn.ID); err != nil {
		t.Fatalf("descendant occupying cap blocked root itself: %v", err)
	}
}

func TestTurnPermitsYieldResumePolicyChangesAndCancellation(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, []session.ResourceLimit{{Kind: session.ResourceRunnableDescendants, Limit: new(int64(1))}})
	left := spawnChildTest(t, s, "left", childRequest(root.ID)).Session
	right := spawnChildTest(t, s, "right", childRequest(root.ID)).Session
	first := claim(t, s, left.ID)
	if err := s.YieldTurn(t.Context(), first.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if turn, err := s.Turn(t.Context(), first.Turn.ID); err != nil || turn.State != session.Running {
		t.Fatalf("yield changed durable turn ownership: %+v %v", turn, err)
	}
	second := claim(t, s, right.ID)
	if err := s.ResumeTurn(t.Context(), first.Turn.ID); !errors.Is(err, ErrLimit) {
		t.Fatalf("resume bypassed occupied sibling capacity: %v", err)
	}
	if _, err := s.SetResource(t.Context(), root.ID, 1, session.ResourceLimit{Kind: session.ResourceRunnableDescendants, Limit: new(int64(0))}); !errors.Is(err, ErrLimit) {
		t.Fatalf("tightened below held permit: %v", err)
	}
	if _, err := s.Finish(t.Context(), second.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	changed := resourceLimit(t, s, root.ID, session.ResourceRunnableDescendants, 0)
	if err := s.ResumeTurn(t.Context(), first.Turn.ID); !errors.Is(err, ErrLimit) {
		t.Fatalf("yielded turn ignored changed policy: %v", err)
	}
	if _, err := s.SetResource(t.Context(), root.ID, changed.Revision-1, session.ResourceLimit{Kind: session.ResourceRunnableDescendants, Limit: new(int64(1))}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale cap update succeeded: %v", err)
	}
	resourceLimit(t, s, root.ID, session.ResourceRunnableDescendants, 1)
	for range 2 {
		if err := s.ResumeTurn(t.Context(), first.Turn.ID); err != nil {
			t.Fatal(err)
		}
	}
	if count(t, s, "turn_permits") != 1 || resourceState(t, s, root.ID, session.ResourceRunnableDescendants).Used != 1 {
		t.Fatal("resume retry duplicated capacity")
	}
	if _, err := s.CancelTurn(t.Context(), first.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ResumeTurn(t.Context(), first.Turn.ID); err == nil {
		t.Fatal("cancelled turn reacquired execution authority")
	}
	if count(t, s, "turn_permits") != 1 {
		t.Fatal("cancel request released executing work before settlement")
	}
	for range 2 {
		if _, err := s.Finish(t.Context(), first.Turn.ID, session.Cancelled, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if count(t, s, "turn_permits") != 0 {
		t.Fatal("cancel settlement retained permit")
	}
}

func TestTurnPermitsYieldRejectsUnfinishedWork(t *testing.T) {
	t.Run("model", func(t *testing.T) {
		s := fresh(t)
		_, root := create(t, s, nil)
		turn := budgetTurn(t, s, root.ID)
		attempt := reserveTest(t, s, attemptRequest(turn, "attempt"))
		for _, dispatched := range []bool{false, true} {
			if dispatched {
				dispatchTest(t, s, attempt.ID)
			}
			if err := s.YieldTurn(t.Context(), turn); !errors.Is(err, ErrBusy) {
				t.Fatalf("yield while attempt dispatched=%v: %v", dispatched, err)
			}
		}
		if count(t, s, "turn_permits") != 1 {
			t.Fatal("failed yield released permit")
		}
		if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, session.ModelAttemptResult{State: session.AttemptUncertain}, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.YieldTurn(t.Context(), turn); err != nil {
			t.Fatalf("settled model kept execution busy: %v", err)
		}
	})
	t.Run("cell and host operation", func(t *testing.T) {
		s := fresh(t)
		_, cell := operationCell(t, s)
		if err := s.YieldTurn(t.Context(), cell.TurnID); !errors.Is(err, ErrBusy) {
			t.Fatalf("yielded running cell: %v", err)
		}
		op := admitOperation(t, s, operationSpec(cell, "pending"))
		if err := s.YieldTurn(t.Context(), cell.TurnID); !errors.Is(err, ErrBusy) {
			t.Fatalf("yielded permission waiter: %v", err)
		}
		if _, err := s.SettleOperation(t.Context(), op.ID, session.OperationResult{State: session.OperationCancelled}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SettleCell(t.Context(), cell.ID, session.CellFailed, session.ToolResult{CallID: cell.CallID, Output: "cancelled", IsError: true}, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.YieldTurn(t.Context(), cell.TurnID); err != nil {
			t.Fatalf("settled cell kept execution busy: %v", err)
		}
	})
}

func TestTurnPermitsRequiredBeforeNewOrPreviouslyAdmittedEffects(t *testing.T) {
	t.Run("model", func(t *testing.T) {
		s := fresh(t)
		_, root := create(t, s, nil)
		turn := budgetTurn(t, s, root.ID)
		attempt := reserveTest(t, s, attemptRequest(turn, "old"))
		// Simulate revoked execution authorization independently of the work row;
		// every dispatch boundary must check the permit, not just turn status.
		execTest(t, s, "DELETE FROM turn_permits WHERE turn_id=?", turn)
		if _, err := s.ReserveModelAttempt(t.Context(), attemptRequest(turn, "new")); err == nil {
			t.Fatal("yielded turn admitted new provider work")
		}
		if dispatched, err := s.DispatchModelAttempt(t.Context(), attempt.ID); err == nil || dispatched {
			t.Fatalf("old attempt dispatched without permit: %v %v", dispatched, err)
		}
		if err := s.ResumeTurn(t.Context(), turn); err != nil {
			t.Fatal(err)
		}
		dispatchTest(t, s, attempt.ID)
	})
	t.Run("cell", func(t *testing.T) {
		s := fresh(t)
		_, root := create(t, s, nil)
		turn, message := cellCall(t, s, root.ID)
		if err := s.YieldTurn(t.Context(), turn.ID); err != nil {
			t.Fatal(err)
		}
		spec := session.CellSpec{ID: "cell", TurnID: turn.ID, CallMessageID: message.ID, CallID: "call"}
		if _, dispatched, err := s.BeginCell(t.Context(), spec); err == nil || dispatched {
			t.Fatalf("new cell dispatched without permit: %v %v", dispatched, err)
		}
		if count(t, s, "cells") != 0 {
			t.Fatal("rejected cell left a row")
		}
		if err := s.ResumeTurn(t.Context(), turn.ID); err != nil {
			t.Fatal(err)
		}
		if _, dispatched, err := s.BeginCell(t.Context(), spec); err != nil || !dispatched {
			t.Fatalf("resumed cell: %v %v", dispatched, err)
		}
	})
	t.Run("host operation", func(t *testing.T) {
		s := fresh(t)
		owner, cell := operationCell(t, s)
		standingGrant(t, s, owner.ID, "grant")
		op := admitOperation(t, s, operationSpec(cell, "old"))
		execTest(t, s, "DELETE FROM turn_permits WHERE turn_id=?", cell.TurnID)
		if _, err := s.AdmitOperation(t.Context(), operationSpec(cell, "new")); err == nil {
			t.Fatal("yielded turn admitted a host effect")
		}
		if dispatched, err := s.DispatchOperation(t.Context(), op.ID); err == nil || dispatched {
			t.Fatalf("old host operation dispatched without permit: %v %v", dispatched, err)
		}
		if err := s.ResumeTurn(t.Context(), cell.TurnID); err != nil {
			t.Fatal(err)
		}
		if dispatched, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !dispatched {
			t.Fatalf("resumed host operation: %v %v", dispatched, err)
		}
	})
}

func TestTurnPermitsFinishRollbackAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, root := create(t, s, nil)
	submit(t, s, root.ID, "root")
	turn := claim(t, s, root.ID).Turn
	execTest(t, s, `CREATE TRIGGER fail_yield BEFORE DELETE ON turn_permits BEGIN SELECT RAISE(ABORT,'yield failure'); END`)
	if err := s.YieldTurn(t.Context(), turn.ID); err == nil {
		t.Fatal("injected yield succeeded")
	}
	if count(t, s, "turn_permits") != 1 {
		t.Fatal("failed yield released execution authorization")
	}
	execTest(t, s, "DROP TRIGGER fail_yield")
	execTest(t, s, `CREATE TRIGGER fail_turn BEFORE UPDATE ON turns BEGIN SELECT RAISE(ABORT,'finish failure'); END`)
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err == nil {
		t.Fatal("injected settlement succeeded")
	}
	if count(t, s, "turn_permits") != 1 {
		t.Fatal("failed settlement released permit")
	}
	execTest(t, s, "DROP TRIGGER fail_turn")
	child := spawnChildTest(t, s, "child", childRequest(root.ID)).Session
	childTurn := claim(t, s, child.ID).Turn
	if err := s.YieldTurn(t.Context(), childTurn.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	for range 2 {
		if _, err := reopened.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if count(t, reopened, "turn_permits") != 0 {
		t.Fatal("recovery retained execution authorization")
	}
	for _, id := range []session.TurnID{turn.ID, childTurn.ID} {
		value, err := reopened.Turn(t.Context(), id)
		if err != nil || value.State != session.Interrupted {
			t.Fatalf("recovered turn: %+v %v", value, err)
		}
		if err := reopened.ResumeTurn(t.Context(), id); err == nil {
			t.Fatal("recovery allowed old turn to resume")
		}
	}
	submit(t, reopened, child.ID, "new")
	claim(t, reopened, child.ID)
}

func TestTurnPermitsResumeRacesPolicyAndCancellation(t *testing.T) {
	for _, mutation := range []string{"policy", "cancel"} {
		t.Run(mutation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s, other := openTest(t, path), openTest(t, path)
			_, root := create(t, s, []session.ResourceLimit{{Kind: session.ResourceRunnableDescendants, Limit: new(int64(1))}})
			child := spawnChildTest(t, s, "child", childRequest(root.ID)).Session
			turn := claim(t, s, child.ID).Turn
			if err := s.YieldTurn(t.Context(), turn.ID); err != nil {
				t.Fatal(err)
			}
			failures := make([]error, 2)
			start := make(chan struct{})
			var workers sync.WaitGroup
			workers.Go(func() {
				<-start
				failures[0] = s.ResumeTurn(t.Context(), turn.ID)
			})
			workers.Go(func() {
				<-start
				if mutation == "policy" {
					_, failures[1] = other.SetResource(t.Context(), root.ID, 1, session.ResourceLimit{Kind: session.ResourceRunnableDescendants, Limit: new(int64(0))})
				} else {
					_, failures[1] = other.CancelTurn(t.Context(), turn.ID)
				}
			})
			close(start)
			workers.Wait()
			if mutation == "policy" {
				winner, loser := 0, 1
				if failures[0] != nil {
					winner, loser = 1, 0
				}
				if failures[winner] != nil || !errors.Is(failures[loser], ErrLimit) {
					t.Fatalf("policy/resume race: %v", failures)
				}
				value := resourceState(t, s, root.ID, session.ResourceRunnableDescendants)
				if value.Limit == nil || value.Used > *value.Limit {
					t.Fatalf("resume escaped changed policy: %+v", value)
				}
				if _, err := s.CancelTurn(t.Context(), turn.ID); err != nil {
					t.Fatal(err)
				}
			} else if failures[1] != nil || failures[0] != nil && !errors.Is(failures[0], ErrStopped) {
				t.Fatalf("cancel/resume race: %v", failures)
			}
			if _, err := s.Finish(t.Context(), turn.ID, session.Cancelled, nil, nil); err != nil {
				t.Fatal(err)
			}
			if count(t, s, "turn_permits") != 0 {
				t.Fatal("terminal turn leaked a racing resumption")
			}
		})
	}
}
