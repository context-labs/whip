package mcp

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestAddServersReportsWithoutReplacing(t *testing.T) {
	original := testCfg("docs")
	disabled := testCfg("off")
	disabled.Enabled = new(false)
	m := newTestManager(t, map[string]ServerConfig{"docs": original, "off": disabled})
	m.Start(t.Context())
	waitStatus(t, m, "docs", StatusReady)
	before, err := m.ResolveTool("docs", "greet")
	if err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.Env = map[string]string{"NEW": "value"}
	result, err := m.AddServers(t.Context(), map[string]ServerConfig{
		"docs": changed, "off": testCfg("off"), "late": testCfg("late"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Added, []string{"late"}) || !reflect.DeepEqual(result.Changed, []string{"docs", "off"}) {
		t.Fatalf("result = %+v", result)
	}
	after, err := m.ResolveTool("docs", "greet")
	if err != nil || before.Generation != after.Generation || before.Definition != after.Definition {
		t.Fatalf("healthy connection replaced: %v", err)
	}
	if cfg, _ := m.Config("docs"); !reflect.DeepEqual(cfg, original) {
		t.Fatalf("config replaced: %+v", cfg)
	}
	if cfg, _ := m.Config("off"); !cfg.Disabled() {
		t.Fatal("disabled server enabled")
	}
	waitStatus(t, m, "late", StatusReady)
	result, err = m.AddServers(t.Context(), map[string]ServerConfig{"docs": original, "late": testCfg("late")})
	if err != nil || len(result.Added) != 0 || !reflect.DeepEqual(result.Existing, []string{"docs", "late"}) {
		t.Fatalf("repeat = %+v, %v", result, err)
	}
}

func TestAddServersConcurrentAndCancellation(t *testing.T) {
	m := newTestManager(t, nil)
	m.Start(t.Context())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.AddServers(ctx, map[string]ServerConfig{"cancelled": testCfg("cancelled")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	if _, ok := m.Config("cancelled"); ok {
		t.Fatal("cancelled request added a server")
	}
	const count = 12
	results := make(chan RefreshResult, count)
	var workers sync.WaitGroup
	for range count {
		workers.Go(func() {
			result, err := m.AddServers(t.Context(), map[string]ServerConfig{"late": testCfg("late")})
			if err != nil {
				t.Error(err)
			}
			results <- result
		})
	}
	workers.Wait()
	close(results)
	added := 0
	for result := range results {
		added += len(result.Added)
	}
	if added != 1 {
		t.Fatalf("added %d times", added)
	}
	waitStatus(t, m, "late", StatusReady)
	acceptedCtx, stop := context.WithCancel(t.Context())
	if _, err := m.AddServers(acceptedCtx, map[string]ServerConfig{"owned": testCfg("owned")}); err != nil {
		t.Fatal(err)
	}
	stop()
	waitStatus(t, m, "owned", StatusReady)
	m.Close()
	if _, err := m.AddServers(t.Context(), map[string]ServerConfig{"closed": testCfg("closed")}); err == nil {
		t.Fatal("closed manager accepted refresh")
	}
}

func TestReconnectRefusesDisabledInvalidAndClosed(t *testing.T) {
	disabled := testCfg("off")
	disabled.Enabled = new(false)
	m := newTestManager(t, map[string]ServerConfig{"off": disabled, "invalid": {}, "docs": testCfg("docs")})
	for _, name := range []string{"off", "invalid", "missing"} {
		if m.Reconnect(name) {
			t.Fatalf("reconnect accepted %s", name)
		}
	}
	if statuses := m.Statuses(); statuses[1].Status != StatusFailed || statuses[2].Status != StatusDisabled {
		t.Fatalf("refusal changed status: %+v", statuses)
	}
	// A valid but not-yet-started entry must actually launch, not stay connecting forever.
	if !m.Reconnect("docs") {
		t.Fatal("reconnect rejected valid server")
	}
	waitStatus(t, m, "docs", StatusReady)
	m.Close()
	if m.Reconnect("docs") {
		t.Fatal("closed manager reconnected")
	}
}

func TestServerLaunchRejectionCanRetry(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"start", "add", "enable", "reconnect"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			cfg := testCfg("docs")
			if operation == "enable" {
				cfg.Enabled = new(false)
			}
			cfgs := map[string]ServerConfig{"docs": cfg}
			if operation == "add" {
				cfgs = nil
			}
			m := newTestManager(t, cfgs)
			reserved := make(chan context.Context, 1)
			m.SetLauncher(func(string, func()) bool {
				// A launcher may inspect its manager; no manager lock may be held.
				_ = m.Statuses()
				m.mu.Lock()
				s := m.servers["docs"]
				s.mu.Lock()
				reserved <- s.runCtx
				s.mu.Unlock()
				m.mu.Unlock()
				return false
			})
			switch operation {
			case "start":
				m.Start(t.Context())
			case "add":
				result, err := m.AddServers(t.Context(), map[string]ServerConfig{"docs": cfg})
				if err != nil || !reflect.DeepEqual(result.Added, []string{"docs"}) {
					t.Fatalf("add = %+v, %v", result, err)
				}
			case "enable":
				if m.Enable("docs") {
					t.Error("rejected enable reported success")
				}
			case "reconnect":
				if m.Reconnect("docs") {
					t.Error("rejected reconnect reported success")
				}
			}
			rejectedCtx := <-reserved
			s := m.servers["docs"]
			select {
			case <-s.ready:
			default:
				t.Error("rejected launch did not settle readiness")
			}
			s.mu.Lock()
			if s.running || s.runCtx != nil || s.stop != nil || s.status != StatusFailed || s.err == "" {
				t.Errorf("rejection left running=%v context=%v stop=%v status=%s error=%q", s.running, s.runCtx != nil, s.stop != nil, s.status, s.err)
			}
			s.mu.Unlock()
			if rejectedCtx == nil || !errors.Is(rejectedCtx.Err(), context.Canceled) {
				t.Error("rejected launch context was not canceled")
			}
			if m.Reconnect("docs") {
				t.Error("retry queued reconnect to a nonexistent runner")
			}
			if t.Failed() {
				return
			}
			m.SetLauncher(nil)
			if !m.Reconnect("docs") {
				t.Fatal("retry with an accepting launcher was rejected")
			}
			waitStatus(t, m, "docs", StatusReady)
			if _, err := m.ResolveTool("docs", "greet"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestServerLaunchRejectionPreservesConcurrentState(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"close", "remove", "disable", "replace", "reenable"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs")})
			s := m.servers["docs"]
			entered := make(chan struct{})
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			t.Cleanup(unblock)
			m.SetLauncher(func(string, func()) bool {
				close(entered)
				<-release
				return false
			})
			result := make(chan bool, 1)
			go func() { result <- m.Reconnect("docs") }()
			<-entered
			s.mu.Lock()
			rejectedCtx := s.runCtx
			s.mu.Unlock()
			var closed chan struct{}
			switch operation {
			case "close":
				closed = make(chan struct{})
				go func() { m.Close(); close(closed) }()
				waitStatus(t, m, "docs", StatusFailed)
			case "disable", "reenable":
				if !m.Disable("docs") {
					t.Fatal("disable failed")
				}
				if operation == "reenable" && !m.Enable("docs") {
					t.Fatal("enable failed while launch was pending")
				}
			case "remove", "replace":
				m.RemoveServers("docs")
				if operation == "replace" {
					m.SetLauncher(nil)
					if _, err := m.AddServers(t.Context(), map[string]ServerConfig{"docs": testCfg("docs")}); err != nil {
						t.Fatal(err)
					}
					waitStatus(t, m, "docs", StatusReady)
				}
			}
			unblock()
			if <-result {
				t.Fatal("rejected launch reported success")
			}
			if closed != nil {
				<-closed
			}
			s.mu.Lock()
			want := StatusDisabled
			if operation == "close" {
				want = StatusFailed
				if s.err != "manager closed" {
					t.Errorf("close error overwritten: %q", s.err)
				}
			}
			if operation == "reenable" {
				want = StatusFailed
			}
			if s.status != want || s.running || s.runCtx != nil || s.stop != nil {
				t.Errorf("terminal state = %s, running=%v context=%v stop=%v", s.status, s.running, s.runCtx != nil, s.stop != nil)
			}
			s.mu.Unlock()
			if !errors.Is(rejectedCtx.Err(), context.Canceled) {
				t.Error("rejected context survived")
			}
			select {
			case <-s.ready:
			default:
				t.Error("terminal state did not settle readiness")
			}
			if operation == "reenable" {
				m.SetLauncher(nil)
				if !m.Reconnect("docs") {
					t.Fatal("reenabled server could not retry")
				}
				waitStatus(t, m, "docs", StatusReady)
			}
			if operation == "replace" || operation == "reenable" {
				if _, err := m.ResolveTool("docs", "greet"); err != nil {
					t.Fatalf("old rejection damaged replacement: %v", err)
				}
			} else if m.Reconnect("docs") {
				t.Fatal("terminal server accepted reconnect")
			}
		})
	}
}

func TestServerLaunchRejectionAfterClose(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, map[string]ServerConfig{"first": testCfg("first"), "second": testCfg("second")})
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	m.SetLauncher(func(string, func()) bool {
		close(entered) // Only the first reserved server may reach the launcher.
		<-release
		return false
	})
	started := make(chan struct{})
	go func() { m.Start(t.Context()); close(started) }()
	<-entered
	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	waitStatus(t, m, "first", StatusFailed)
	waitStatus(t, m, "second", StatusFailed)
	unblock()
	<-started // The second reserved launch now rejects inside m.launch itself.
	<-closed
	for _, s := range m.servers {
		s.mu.Lock()
		if s.running || s.runCtx != nil || s.stop != nil || s.status != StatusFailed || s.err != "manager closed" {
			t.Errorf("%s: running=%v context=%v stop=%v status=%s error=%q", s.name, s.running, s.runCtx != nil, s.stop != nil, s.status, s.err)
		}
		s.mu.Unlock()
	}
}
