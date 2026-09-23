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
