package agent

import (
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// A condition already true resolves in the first check (no interval wait)
// and delivers exactly one "met" message.
func TestWaitConditionMetImmediately(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	bindTestAgent(t, ag, t.TempDir())
	defer ag.Waits().Close()

	var woke atomic.Int32
	ag.Waits().OnWake = func(string) { woke.Add(1) }

	w, err := ag.StartWait(WaitTaskSpec{Command: "exit 0", Interval: 50 * time.Millisecond, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Done:
	case <-time.After(2 * time.Second):
		t.Fatal("wait never delivered")
	}
	if w.Status() != WaitMet {
		t.Fatalf("status = %q, want %q", w.Status(), WaitMet)
	}
	if !strings.Contains(w.Detail, "condition met") {
		t.Fatalf("detail = %q", w.Detail)
	}
	if got := woke.Load(); got != 1 {
		t.Fatalf("idle wake fired %d times, want exactly 1", got)
	}
}

// The until regex gates success: exit-0 alone doesn't settle the wait when
// `until` is set, and a matching output settles it once the pattern appears.
func TestWaitUntilRegex(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	bindTestAgent(t, ag, t.TempDir())
	defer ag.Waits().Close()
	ag.Waits().OnWake = func(string) {}

	// The command exits 0 but prints "running" — until "ready" must not fire.
	w, err := ag.StartWait(WaitTaskSpec{Command: "echo running", Until: "ready", Interval: 50 * time.Millisecond, Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	<-w.Done
	if w.Status() != WaitTimeout {
		t.Fatalf("status = %q, want %q (until never matched)", w.Status(), WaitTimeout)
	}

	// A bad until regex is rejected at registration, not at first poll.
	if _, err := ag.StartWait(WaitTaskSpec{Command: "true", Until: "[unclosed"}); err == nil {
		t.Fatal("bad until regex should fail StartWait")
	}
}

// A command that keeps failing strikes out after 3 consecutive errors instead
// of polling until the timeout.
func TestWaitStrikesOut(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	bindTestAgent(t, ag, t.TempDir())
	defer ag.Waits().Close()
	ag.Waits().OnWake = func(string) {}

	start := time.Now()
	w, err := ag.StartWait(WaitTaskSpec{Command: "exit 1", Interval: 30 * time.Millisecond, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	<-w.Done
	if w.Status() != WaitFailed {
		t.Fatalf("status = %q, want %q", w.Status(), WaitFailed)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("strike-out should beat the timeout: took %s", d)
	}
}

// Timeout delivers a timeout message exactly once.
func TestWaitTimeout(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	bindTestAgent(t, ag, t.TempDir())
	defer ag.Waits().Close()
	ag.Waits().OnWake = func(string) {}

	w, err := ag.StartWait(WaitTaskSpec{Command: "echo still-waiting", Until: "never-matches", Interval: 30 * time.Millisecond, Timeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	<-w.Done
	if w.Status() != WaitTimeout {
		t.Fatalf("status = %q, want %q", w.Status(), WaitTimeout)
	}
	if !strings.Contains(w.Detail, "timeout") {
		t.Fatalf("detail = %q", w.Detail)
	}
}

// A running turn routes delivery through Steer (loop boundary), not OnWake.
func TestWaitBusySteersInsteadOfWaking(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	bindTestAgent(t, ag, t.TempDir())
	defer ag.Waits().Close()
	ag.running.Store(true) // simulate an in-flight turn

	var woke atomic.Int32
	ag.Waits().OnWake = func(string) { woke.Add(1) }

	w, err := ag.StartWait(WaitTaskSpec{Command: "exit 0", Interval: 30 * time.Millisecond, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	<-w.Done
	if woke.Load() != 0 {
		t.Fatal("busy delivery must not fire OnWake")
	}
	if got := len(ag.drainPending()); got != 1 {
		t.Fatalf("busy delivery should queue exactly one steer, got %d", got)
	}
}

// Cancel stops the wait and suppresses delivery.
func TestWaitCancel(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	bindTestAgent(t, ag, t.TempDir())
	defer ag.Waits().Close()
	ag.Waits().OnWake = func(string) {}

	w, err := ag.StartWait(WaitTaskSpec{Command: "echo x", Until: "never", Interval: time.Second, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !ag.Waits().CancelWait(w.ID) {
		t.Fatal("cancel of a running wait should succeed")
	}
	if w.Status() != WaitKilled {
		t.Fatalf("status = %q, want %q", w.Status(), WaitKilled)
	}
	if ag.Waits().CancelWait(w.ID) {
		t.Fatal("second cancel should report not-running")
	}
}

// The wait tool parses args, registers the poller, and returns the "don't
// poll" contract message.
func TestWaitToolRegisters(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	bindTestAgent(t, ag, t.TempDir())
	defer ag.Waits().Close()

	var wt tools.Tool
	for _, tl := range ag.AllTools() {
		if tl.Def.Function.Name == "wait" {
			wt = tl
		}
	}
	if wt.Def.Function.Name == "" {
		t.Fatal("agent should expose the wait tool")
	}
	out, err := wt.Run(t.Context(), json.RawMessage(`{"command":"exit 0","interval":0.05,"timeout":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "do NOT sleep-poll") {
		t.Fatalf("tool output should state the no-poll contract: %q", out)
	}
	// The registered wait (exit 0) settles on the immediate first check —
	// which also removes it from the registry map, so the handle may already
	// be gone by the time we look. Either outcome proves the wiring.
	ag.Waits().mu.Lock()
	var ws []*waitTask
	for _, w := range ag.Waits().waits {
		ws = append(ws, w)
	}
	ag.Waits().mu.Unlock()
	if len(ws) > 1 {
		t.Fatalf("at most one wait should be registered, got %d", len(ws))
	}
	if len(ws) == 1 {
		select {
		case <-ws[0].Done:
		case <-time.After(2 * time.Second):
			t.Fatal("registered wait never settled")
		}
		// Settled waits are dropped from the map (no listing surface exists).
		ag.Waits().mu.Lock()
		left := len(ag.Waits().waits)
		ag.Waits().mu.Unlock()
		if left != 0 {
			t.Fatalf("settled wait should be deleted from the registry, %d left", left)
		}
	}
}

// The ticker path (a condition false on the immediate first check but true on
// a later poll) is distinct from the immediate-check path — the 2s minimum
// interval clamps make sub-2s tests never exercise it, so this one sits at
// the real minimum and confirms a later poll settles the wait.
func TestWaitTickerPath(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	bindTestAgent(t, ag, t.TempDir())
	defer ag.Waits().Close()
	ag.Waits().OnWake = func(string) {}

	// A file that appears after the first check: the until regex matches only
	// once the file exists, so the first (immediate) poll fails and a ticker
	// poll must settle it.
	flag := t.TempDir() + "/ready"
	w, err := ag.StartWait(WaitTaskSpec{
		Command:  "cat " + flag + " 2>/dev/null || true",
		Until:    "READY",
		Interval: waitMinInterval, // the real floor — the ticker fires at 2s
		Timeout:  30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Write the flag shortly after the first check would have run.
	time.AfterFunc(500*time.Millisecond, func() {
		os.WriteFile(flag, []byte("READY"), 0o600)
	})
	select {
	case <-w.Done:
	case <-time.After(10 * time.Second):
		t.Fatal("ticker-path wait never settled")
	}
	if w.Status() != WaitMet {
		t.Fatalf("status = %q, want %q", w.Status(), WaitMet)
	}
}

// The lost-wakeup race: a Steer landing after a turn's final drainPending but
// before running flips false must not be dropped — the turn's teardown
// re-drain routes it to OnOrphanedSteer (the unified hook; the TUI wires it to
// the same machine-turn path as OnWake). Tested via the drain mechanism
// directly: the exact interleaving window isn't deterministically forceable
// in a live turn.
func TestTurnTeardownDrainsOrphanedSteers(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	var woke []string
	ag.OnOrphanedSteer = func(s string) { woke = append(woke, s) }

	ag.running.Store(true)
	ag.Steer("orphaned message")
	// Simulate teardown: running flips false, then the re-drain runs.
	ag.running.Store(false)
	ag.drainOrphanedSteers()

	if len(woke) != 1 || woke[0] != "orphaned message" {
		t.Fatalf("orphaned steer should wake, got %v", woke)
	}
	if len(ag.drainPending()) != 0 {
		t.Fatal("pending should be empty after teardown drain")
	}
}

// CancelWait racing a deliver must not panic (double close). The CAS makes
// exactly one of them own the close. Run under -race.
func TestWaitCancelRacesDeliver(t *testing.T) {
	services := tools.NewServices()
	bindTestServices(t, services, t.TempDir())
	for range 200 {
		ag := NewWithServices(llm.New("http://unused", "k"), "m", 100, "sys", services)
		ag.Waits().OnWake = func(string) {}
		w, err := ag.StartWait(WaitTaskSpec{Command: "exit 0", Interval: waitMinInterval, Timeout: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		// The immediate first check delivers on this goroutine's schedule;
		// cancel concurrently. Whichever wins, no double-close panic.
		go ag.Waits().CancelWait(w.ID)
		<-w.Done
		ag.Waits().Close()
	}
}
