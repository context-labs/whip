package mcp

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// waitStatus polls until the named server reaches st (or fails the test).
// For servers added post-Start there's no ready channel to wait on from
// outside, so poll the snapshot.
func waitStatus(t *testing.T, m *Manager, name string, st Status) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range m.Statuses() {
			if s.Name == name && s.Status == st {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("server %s never reached %s (statuses: %+v)", name, st, m.Statuses())
}

// TestAddServersLive pins the "source toggled on mid-session" path: a manager
// started with one server absorbs a second later, connects it, and its tools
// join the agent-facing set — all without a restart.
func TestAddServersLive(t *testing.T) {
	m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs")})
	m.Start(context.Background())
	waitReady(t, m)

	m.AddServers(context.Background(), map[string]ServerConfig{"late": testCfg("late")})
	waitStatus(t, m, "late", StatusReady)

	ts := m.Tools()
	if len(ts) != 8 { // 4 tools per test server
		t.Fatalf("expected 8 tools after AddServers, got %d: %v", len(ts), toolNames(ts))
	}
	names := map[string]bool{}
	for _, s := range m.Statuses() {
		names[s.Name] = true
	}
	if !names["docs"] || !names["late"] {
		t.Fatalf("statuses missing servers: %v", names)
	}
}

// TestAddServersKeepsExisting pins that a name already live is never
// reconnected or replaced — whip-owned entries and existing sessions win.
func TestAddServersKeepsExisting(t *testing.T) {
	m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs")})
	m.Start(context.Background())
	waitReady(t, m)

	m.AddServers(context.Background(), map[string]ServerConfig{"docs": testCfg("docs")})
	if got := len(m.Statuses()); got != 1 {
		t.Fatalf("duplicate add should no-op, statuses = %d", got)
	}
}

// TestRemoveServersLive pins the "source toggled off mid-session" path: the
// server's tools leave the agent-facing set and its status row disappears
// immediately, while its sibling keeps serving.
func TestRemoveServersLive(t *testing.T) {
	m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs"), "extra": testCfg("extra")})
	m.Start(context.Background())
	waitReady(t, m)
	if got := len(m.Tools()); got != 8 {
		t.Fatalf("precondition: 8 tools, got %d", got)
	}

	m.RemoveServers("extra")
	if got := len(m.Tools()); got != 4 {
		t.Fatalf("expected 4 tools after RemoveServers, got %d", got)
	}
	for _, s := range m.Statuses() {
		if s.Name == "extra" {
			t.Fatalf("removed server still listed: %+v", s)
		}
	}
	if _, ok := m.Config("extra"); ok {
		t.Fatal("removed server still has a Config entry")
	}
	if m.Reconnect("extra") {
		t.Fatal("reconnect on a removed server should refuse")
	}
}

// TestStaleToolAfterRemoveFailsClean pins the stale-closure case: the agent
// snapshots the tool list at turn start, so a tool bridging a server removed
// mid-turn must fail as tool output (an "Error: …" string), not panic or hang.
func TestStaleToolAfterRemoveFailsClean(t *testing.T) {
	m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs")})
	m.Start(context.Background())
	waitReady(t, m)

	stale := m.Tools() // the set a turn already captured
	m.RemoveServers("docs")

	for _, tool := range stale {
		out, err := tool.Run(context.Background(), nil)
		if err == nil && out == "" {
			t.Errorf("stale tool %s: expected an error, got silence", tool.Def.Function.Name)
		}
		if err != nil && !strings.Contains(err.Error(), "docs") {
			t.Errorf("stale tool %s error should name the server: %v", tool.Def.Function.Name, err)
		}
	}
}

// TestRemoveDuringInFlightConnect is the deterministic mid-connect proof: a
// transport parked on a release channel guarantees the removal lands WHILE
// the connect is in flight, and the gen-guard (connect's startGen/stillOurs
// check) must discard the completed session instead of resurrecting the
// removed server.
func TestRemoveDuringInFlightConnect(t *testing.T) {
	release := make(chan struct{})
	exited := make(chan struct{})
	m := NewManager(nil)
	t.Cleanup(m.Close)
	m.connectTransport = func(ctx context.Context, cfg ServerConfig, _ *ringBuffer) (sdkmcp.Transport, error) {
		defer close(exited)
		select {
		case <-release:
			return serveTestServer(t, cfg.Command[0]), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	m.AddServers(context.Background(), map[string]ServerConfig{"churn": testCfg("churn")})

	// Remove while the connect is parked in the transport, then let the
	// connect finish — the guard must close the session, not store it.
	m.RemoveServers("churn")
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("removed server lifecycle did not stop")
	}
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.Statuses()) == 0 && len(m.Tools()) == 0 {
			// Give the in-flight connect a moment to (wrongly) store, then
			// re-check: a resurrection would appear within the window.
			time.Sleep(50 * time.Millisecond)
			if len(m.Statuses()) == 0 && len(m.Tools()) == 0 {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("removed server leaked through an in-flight connect: statuses=%+v tools=%d", m.Statuses(), len(m.Tools()))
}

// TestRemoveWhileConnecting is the race proof: removal interleaved with an
// in-flight connect and with concurrent Tools()/Statuses() readers must not
// race, panic, or resurrect the removed server.
func TestRemoveWhileConnecting(t *testing.T) {
	m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs")})
	m.Start(context.Background())

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				_ = m.Tools()
				_ = m.Statuses()
				_ = m.InstructionsBlock()
			}
		})
	}
	// Add then immediately remove, several times — the connect goroutine is
	// mid-flight for at least some of them.
	for range 10 {
		m.AddServers(context.Background(), map[string]ServerConfig{"churn": testCfg("churn")})
		m.RemoveServers("churn")
	}
	wg.Wait()
	waitStatus(t, m, "docs", StatusReady)

	for _, s := range m.Statuses() {
		if s.Name == "churn" {
			t.Fatalf("churned server reappeared: %+v", s)
		}
	}
	// A reconnect queued before removal must not resurrect the server.
	if m.Reconnect("churn") {
		t.Fatal("reconnect on a removed server should refuse")
	}
	time.Sleep(20 * time.Millisecond) // let any stale watcher fire
	for _, s := range m.Statuses() {
		if s.Name == "churn" {
			t.Fatalf("stale watcher resurrected a removed server: %+v", s)
		}
	}
}

// TestAddAfterRemoveReconnects pins the full toggle round-trip: off then on
// again re-discovers the server as a fresh entry and reconnects it.
func TestAddAfterRemoveReconnects(t *testing.T) {
	m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs")})
	m.Start(context.Background())
	waitReady(t, m)

	m.RemoveServers("docs")
	if got := len(m.Tools()); got != 0 {
		t.Fatalf("expected no tools after removal, got %d", got)
	}
	m.AddServers(context.Background(), map[string]ServerConfig{"docs": testCfg("docs")})
	waitStatus(t, m, "docs", StatusReady)
	if got := len(m.Tools()); got != 4 {
		t.Fatalf("expected 4 tools after re-add, got %d", got)
	}
}
