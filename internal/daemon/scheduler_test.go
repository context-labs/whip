package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/schedule"
	"github.com/context-labs/whip/internal/session"
)

func TestNextScheduleSlotEdges(t *testing.T) {
	at := time.Date(2026, time.August, 31, 12, 0, 5, 500_000_000, time.UTC)
	anchor := at.Add(-5 * time.Second)
	for _, test := range []struct {
		name   string
		parsed schedule.Schedule
		task   session.Schedule
		want   time.Time
		due    bool
	}{
		{name: "unfired recurring starts at anchor", parsed: schedule.Schedule{Every: 5 * time.Second}, task: session.Schedule{Anchor: anchor}, want: anchor, due: true},
		{name: "future recurring slot", parsed: schedule.Schedule{Every: 5 * time.Second}, task: session.Schedule{Anchor: anchor, LastFire: at}, want: at.Add(5 * time.Second)},
		{name: "one-shot is due within current second", parsed: schedule.Schedule{At: at.Add(400 * time.Millisecond)}, want: at.Add(400 * time.Millisecond), due: true},
		{name: "fired one-shot has no next slot", parsed: schedule.Schedule{At: anchor}, task: session.Schedule{Anchor: anchor, LastFire: anchor}, want: time.Time{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, due := nextScheduleSlot(test.parsed, test.task, at)
			if !got.Equal(test.want) || due != test.due {
				t.Fatalf("slot=%v due=%v, want %v/%v", got, due, test.want, test.due)
			}
		})
	}
}

func TestSchedulerClaimsDueFireWithoutClient(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	if _, err := store.AddSchedule(rootID, "@at "+time.Now().Add(-time.Second).UTC().Format(time.RFC3339), "scheduled work", time.Now()); err != nil {
		t.Fatal(err)
	}
	fired := make(chan bool, 1)
	runner := &fakeRunner{turn: func(_ context.Context, input string, authored bool) (string, error) {
		if input != "scheduled work" {
			t.Errorf("schedule input=%q", input)
		}
		fired <- authored
		return "done", nil
	}}
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	if _, err := daemon.Open(rootID); err != nil {
		t.Fatal(err)
	}
	select {
	case authored := <-fired:
		if authored {
			t.Fatal("scheduled turn was authored")
		}
	case <-time.After(time.Second):
		t.Fatal("due schedule did not fire")
	}
	time.Sleep(20 * time.Millisecond)
	if runner.calls.Load() != 1 {
		t.Fatalf("one-shot schedule calls=%d", runner.calls.Load())
	}
}
