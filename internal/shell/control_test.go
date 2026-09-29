package shell

import (
	"errors"
	"testing"
)

func TestPauseIdleRejectsOwnedJobsAndBlocksNewReservations(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	owner := capture(t, m, "owner")
	reservation, err := owner.Reserve(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.PauseIdle([]string{"owner"}); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	reservation.Release()
	release, err := m.PauseIdle([]string{"owner", "future"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Reserve(false); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, err := m.Capture("future"); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	unrelated := capture(t, m, "other")
	active, err := unrelated.Reserve(false)
	if err != nil {
		t.Fatal(err)
	}
	active.Release()
	release()
	release()
	active, err = owner.Reserve(false)
	if err != nil {
		t.Fatal(err)
	}
	active.Release()
}
