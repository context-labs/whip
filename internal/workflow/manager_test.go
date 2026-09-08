package workflow

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestManagerStartRejectsBadRunID pins C5: a model-supplied resumeFromRunId
// that could escape the runs dir (../) is rejected at Start, never reaching
// SaveRun/LoadRun.
func TestManagerStartRejectsBadRunID(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	m := NewManager(echoRunner(nil), "")
	for _, bad := range []string{"../../foo", "..", "a/b", "a b"} {
		if _, err := m.Start(metaHeader+"return await agent('x')", nil, bad); err == nil {
			t.Errorf("Start(bad=%q) succeeded, want error", bad)
		}
	}
}

// TestManagerResumeLoadsJournalBeforeSave pins C2: resuming with the same
// runID must NOT overwrite the prior run's journal before loading it. The
// first run journals one agent() call; a resume with the same runID and
// unchanged script must replay it (zero live calls) instead of re-running
// from scratch.
func TestManagerResumeLoadsJournalBeforeSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	m := NewManager(echoRunner(nil), "")

	script := metaHeader + `const a = await agent('once')` + "\nreturn a"
	r1, err := m.Start(script, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	waitSettled(t, m, r1.ID)

	var calls atomic.Int64
	m.runner = echoRunner(&calls)
	// Resume with the SAME runID: the journal must be loaded before the empty
	// save overwrites it, so the cached call replays and calls stays 0.
	r2, err := m.Start(script, nil, r1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r2.ID != r1.ID {
		t.Fatalf("resume reused runID: got %q want %q", r2.ID, r1.ID)
	}
	waitSettled(t, m, r2.ID)
	if calls.Load() != 0 {
		t.Fatalf("resume re-ran %d agent calls; journal should have replayed them", calls.Load())
	}
}

// TestManagerResumePersistsFullJournal pins N2: after a resume, the on-disk
// journal must contain the replayed prefix + live entries (not just the live
// ones), so a SECOND resume of the same runID still gets a 100% cache hit.
// Without copying result.Journal into persisted on settle, OnJournal fires
// only for live completions and the replayed prefix is lost on save.
func TestManagerResumePersistsFullJournal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	m := NewManager(echoRunner(nil), "")

	script := metaHeader + `
const a = await agent('first')
const b = await agent('second')
return [a, b].join('|')
`
	r1, err := m.Start(script, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	waitSettled(t, m, r1.ID)

	// First resume with the same runID — replays both calls (0 live).
	var calls atomic.Int64
	m.runner = echoRunner(&calls)
	_, err = m.Start(script, nil, r1.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the resume to settle, then check the persisted journal has
	// BOTH entries (the prefix wasn't dropped on save).
	waitSettled(t, m, r1.ID)
	if calls.Load() != 0 {
		t.Fatalf("first resume re-ran %d calls, want 0", calls.Load())
	}
	persisted := LoadRun(r1.ID)
	if len(persisted.Journal) != 2 {
		t.Fatalf("after resume, persisted journal has %d entries, want 2 (replayed prefix + live)", len(persisted.Journal))
	}

	// Second resume of the SAME runID — the persisted journal must still
	// hold both entries, so this resume is also a 100% cache hit.
	calls.Store(0)
	_, err = m.Start(script, nil, r1.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitSettled(t, m, r1.ID)
	if calls.Load() != 0 {
		t.Fatalf("second resume re-ran %d calls; the persisted prefix was dropped", calls.Load())
	}
}

// TestManagerRejectsResumeOfRunningRun pins N3: a second workflow call reusing
// a runID that's still running must be rejected — otherwise it replaces
// m.runs[id], leaves the original goroutine writing the same journal, and
// lets OnSettle fire twice.
func TestManagerRejectsResumeOfRunningRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	release := make(chan struct{})
	// A runner that blocks until released: keeps the run in RunRunning.
	m := NewManager(func(_ context.Context, _ AgentRequest) (any, Usage, error) {
		<-release
		return "ok", Usage{}, nil
	}, "")
	r, err := m.Start(metaHeader+"return await agent('x')", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// The run is in-flight (blocked on the runner). Resuming its own id must
	// be refused, not overwrite it.
	if _, err := m.Start(metaHeader+"return await agent('x')", nil, r.ID); err == nil ||
		!strings.Contains(err.Error(), "still running") {
		t.Fatalf("expected 'still running' error, got %v", err)
	}
	close(release)
	waitSettled(t, m, r.ID)
}

// TestManagerListReturnsSnapshots pins C1: List returns RunSummary values
// (copied under run.mu), not bare *ManagedRun pointers. A status read off a
// returned entry must reflect the settled state without holding the internal
// pointer, and must not race the run goroutine.
func TestManagerListReturnsSnapshots(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	m := NewManager(echoRunner(nil), "")
	r, err := m.Start(metaHeader+"return await agent('x')", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	waitSettled(t, m, r.ID)

	runs := m.List()
	if len(runs) != 1 {
		t.Fatalf("List returned %d runs, want 1", len(runs))
	}
	if runs[0].ID != r.ID {
		t.Fatalf("List[0].ID = %q want %q", runs[0].ID, r.ID)
	}
	if runs[0].Status != RunComplete {
		t.Fatalf("List[0].Status = %q want complete", runs[0].Status)
	}
	snap, ok := m.Snapshot(r.ID)
	if !ok {
		t.Fatalf("Snapshot missing for %s", r.ID)
	}
	if len(snap.Agents) != 1 {
		t.Fatalf("Snapshot has %d agents, want 1", len(snap.Agents))
	}
}

// waitSettled blocks until the run's Done channel closes (it settles) or the
// deadline passes.
func waitSettled(t *testing.T, m *Manager, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r := m.get(id)
		if r == nil {
			t.Fatalf("run %s vanished", id)
		}
		select {
		case <-r.Done:
			return
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s did not settle", id)
}
