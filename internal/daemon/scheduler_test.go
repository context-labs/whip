package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

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
