package store

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestLifecycleChangeCapturesOnlyCommittedCancellation(t *testing.T) {
	for _, kind := range []string{"root", "child"} {
		t.Run(kind, func(t *testing.T) {
			s := fresh(t)
			_, target := create(t, s, nil)
			if kind == "child" {
				target = *controlChild(t, s, target.ID, "initial").Session
			} else {
				submit(t, s, target.ID, "initial")
			}
			turn := claim(t, s, target.ID).Turn
			queued := submit(t, s, target.ID, "queued")
			execTest(t, s, `CREATE TRIGGER fail_lifecycle BEFORE UPDATE ON turns WHEN NEW.state='cancelling' BEGIN SELECT RAISE(ABORT,'injected lifecycle failure'); END`)
			if _, err := s.SetLifecycle(t.Context(), target.ID, session.Stopped); err == nil {
				t.Fatal("injected cancellation failure succeeded")
			}
			current, err := s.Session(t.Context(), target.ID)
			if err != nil || current.Lifecycle != session.Active {
				t.Fatalf("stop survived rollback: %+v %v", current, err)
			}
			active, err := s.Turn(t.Context(), turn.ID)
			if err != nil || active.State != session.Running || count(t, s, "turn_permits") != 1 {
				t.Fatalf("cancellation survived rollback: %+v %v", active, err)
			}
			execTest(t, s, "DROP TRIGGER fail_lifecycle")
			for range 2 {
				change, err := s.SetLifecycle(t.Context(), target.ID, session.Stopped)
				if err != nil || change.Session.ID != target.ID || change.Session.Lifecycle != session.Stopped || change.CancelTurnID == nil || *change.CancelTurnID != turn.ID {
					t.Fatalf("wrong cancellation target: %+v %v", change, err)
				}
			}
			change, err := s.SetLifecycle(t.Context(), target.ID, session.Active)
			if err != nil || change.CancelTurnID != nil {
				t.Fatalf("reactivation repeated cancellation: %+v %v", change, err)
			}
			if _, err := s.Claim(t.Context(), target.ID); !errors.Is(err, ErrBusy) {
				t.Fatalf("reactivation overlapped old execution: %v", err)
			}
			if _, err := s.Finish(t.Context(), turn.ID, session.Cancelled, nil, nil); err != nil {
				t.Fatal(err)
			}
			next := claim(t, s, target.ID)
			if next.Input.ID != queued.Input.ID {
				t.Fatal("stop discarded queued work")
			}
			change, err = s.SetLifecycle(t.Context(), target.ID, session.Stopped)
			if err != nil || change.CancelTurnID == nil || *change.CancelTurnID != next.Turn.ID {
				t.Fatalf("fresh stop targeted old execution: %+v %v", change, err)
			}
			if _, err := s.Finish(t.Context(), next.Turn.ID, session.Cancelled, nil, nil); err != nil {
				t.Fatal(err)
			}
			change, err = s.SetLifecycle(t.Context(), target.ID, session.Stopped)
			if err != nil || change.CancelTurnID != nil {
				t.Fatalf("idle stop invented cancellation: %+v %v", change, err)
			}
		})
	}
}
