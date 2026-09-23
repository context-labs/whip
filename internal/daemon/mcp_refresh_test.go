package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/protocol"
)

func refreshTestSession(t *testing.T) *Session {
	t.Helper()
	root := controlBoundarySession(t)
	t.Cleanup(func() {
		if manager := root.mcpManager(); manager != nil {
			manager.Close()
		}
		root.supervisor.stop()
		root.supervisor.wait()
	})
	return root
}

func TestMCPRefreshSessionIsolationAndFiltering(t *testing.T) {
	root, other := refreshTestSession(t), refreshTestSession(t)
	disabled := new(false)
	discovery := mcp.Filtered{
		Merged: mcp.FromConfigMap(map[string]config.MCPServer{
			"allowed": {Command: []string{"native"}, Enabled: disabled},
			"outside": {Command: []string{"outside"}, Enabled: disabled},
		}),
		Blocked: map[string]mcp.ServerConfig{"blocked": {Note: "project imports off", Enabled: disabled}},
		Errs:    map[string]error{"broken.json": errors.New("parse error")},
	}
	loader := func(context.Context) (mcp.Filtered, error) { return discovery, nil }
	root.loadMCP, other.loadMCP = loader, loader
	root.mcpServers = []string{"allowed", "imported", "blocked"}
	output, err := root.clientMCP(t.Context(), "mcp.refresh", clientActionPayload{})
	if err != nil {
		t.Fatal(err)
	}
	var result protocol.MCPRefreshResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Added, []string{"allowed"}) || len(result.Blocked) != 1 || len(result.SourceErrors) != 1 || result.Servers[0].Status != "disabled" {
		t.Fatalf("result = %+v", result)
	}
	if other.mcpManager() != nil {
		t.Fatal("refresh touched another session")
	}
	manager := root.mcpManager()
	if cfg, _ := manager.Config("allowed"); !cfg.Trusted {
		t.Fatal("native config lost trust")
	}
	if _, exists := manager.Config("outside"); exists {
		t.Fatal("definition filter bypassed")
	}
	// The same loader observes current config; existing entries remain intact,
	// including an entry disabled in this session but now enabled on disk.
	discovery.Merged["allowed"] = mcp.ServerConfig{Command: []string{"replacement"}, Trusted: true}
	discovery.Merged["imported"] = mcp.ServerConfig{Command: []string{"import"}, Enabled: disabled, Origin: "claude"}
	result2, err := root.refreshMCP(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if root.mcpManager() != manager || !reflect.DeepEqual(result2.Added, []string{"imported"}) || !reflect.DeepEqual(result2.Changed, []string{"allowed"}) {
		t.Fatalf("second = %+v", result2)
	}
	if cfg, _ := manager.Config("allowed"); !cfg.Disabled() || cfg.Command[0] != "native" {
		t.Fatalf("existing config changed: %+v", cfg)
	}
	if cfg, _ := manager.Config("imported"); cfg.Trusted {
		t.Fatal("import acquired native trust")
	}
	if _, err := root.reconnectMCP(t.Context(), "allowed"); err == nil {
		t.Fatal("reconnect enabled disabled server")
	}
	// An explicit empty list differs from nil (all host servers).
	other.mcpServers = []string{}
	if result, err := other.refreshMCP(t.Context()); err != nil || len(result.Added) != 0 || len(result.Blocked) != 0 {
		t.Fatalf("empty filter = %+v, %v", result, err)
	}
}

func TestMCPRefreshReplacesDiscoveryDiagnostics(t *testing.T) {
	root := refreshTestSession(t)
	root.definition.MCP.Servers = []string{"current"}
	// Starting with an attachment alone must also preserve its refusal.
	if err := root.attachMCP(map[string]mcp.ServerConfig{"attachment": {Command: []string{"never"}}}); err != nil {
		t.Fatal(err)
	}
	discovery := mcp.Filtered{
		Blocked: map[string]mcp.ServerConfig{"removed": {Note: "imports off"}},
		Errs:    map[string]error{"broken.json": errors.New("parse error")},
	}
	root.loadMCP = func(context.Context) (mcp.Filtered, error) { return discovery, nil }
	first, err := root.refreshMCP(t.Context())
	if err != nil || len(first.Blocked) != 2 || len(first.SourceErrors) != 1 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	discovery = mcp.Filtered{Blocked: map[string]mcp.ServerConfig{"current": {Note: "imports off"}}}
	second, err := root.refreshMCP(t.Context())
	if err != nil || len(second.Blocked) != 2 || len(second.SourceErrors) != 0 {
		t.Fatalf("second = %+v, %v", second, err)
	}
	if second.Blocked[0].Name != "attachment" || second.Blocked[1].Name != "current" {
		t.Fatalf("stale discovery or lost attachment refusal: %+v", second.Blocked)
	}
	// Admitting a previously blocked discovery name clears only that row.
	discovery = mcp.Filtered{Merged: map[string]mcp.ServerConfig{
		"current": {Command: []string{"never"}, Enabled: new(false)},
	}}
	third, err := root.refreshMCP(t.Context())
	if err != nil || len(third.Blocked) != 1 || third.Blocked[0].Name != "attachment" {
		t.Fatalf("third = %+v, %v", third, err)
	}
}

func TestMCPReconnectRefusalDiagnostics(t *testing.T) {
	manager := mcp.NewManager(map[string]mcp.ServerConfig{
		"invalid":  {},
		"disabled": {Command: []string{"never"}, Enabled: new(false)},
	})
	t.Cleanup(manager.Close)
	for _, tc := range []struct{ name, want string }{
		{"invalid", "cannot reconnect"},
		{"disabled", "is disabled"},
		{"missing", "no MCP server named"},
	} {
		if _, err := applyMCPAction(manager, "mcp.reconnect", tc.name); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v, want %q", tc.name, err, tc.want)
		}
	}
	manager.Close()
	if _, err := applyMCPAction(manager, "mcp.reconnect", "invalid"); err == nil || !strings.Contains(err.Error(), "cannot reconnect") {
		t.Fatalf("closed manager misreported: %v", err)
	}
}

func TestMCPRefreshConcurrentFirstManager(t *testing.T) {
	root := refreshTestSession(t)
	root.loadMCP = func(context.Context) (mcp.Filtered, error) {
		return mcp.Filtered{Merged: map[string]mcp.ServerConfig{"off": {Command: []string{"never"}, Enabled: new(false)}}}, nil
	}
	const count = 12
	results := make(chan mcp.RefreshResult, count)
	var workers sync.WaitGroup
	for range count {
		workers.Go(func() {
			result, err := root.refreshMCP(t.Context())
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
	if added != 1 || len(root.mcpManager().Statuses()) != 1 {
		t.Fatalf("added %d times", added)
	}
}

func TestMCPRefreshCancellationAndReplacement(t *testing.T) {
	for _, mode := range []string{"cancel", "replace", "stop", "load-error"} {
		t.Run(mode, func(t *testing.T) {
			root := refreshTestSession(t)
			entered, proceed := make(chan struct{}), make(chan struct{})
			root.loadMCP = func(context.Context) (mcp.Filtered, error) {
				close(entered)
				<-proceed
				if mode == "load-error" {
					return mcp.Filtered{}, errors.New("bad host config")
				}
				return mcp.Filtered{Merged: map[string]mcp.ServerConfig{"late": {Enabled: new(false)}}}, nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := root.refreshMCP(ctx); done <- err }()
			<-entered
			switch mode {
			case "cancel":
				cancel()
			case "replace":
				root.swapMCP(mcp.NewManager(nil))
			case "stop":
				root.supervisor.stop()
			}
			close(proceed)
			if err := <-done; err == nil {
				t.Fatal("refresh succeeded across cancellation/replacement/error")
			}
			if manager := root.mcpManager(); manager != nil && len(manager.Statuses()) != 0 {
				t.Fatal("failed discovery changed manager")
			}
		})
	}
	root := refreshTestSession(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	root.loadMCP = func(context.Context) (mcp.Filtered, error) {
		t.Error("cancelled refresh invoked loader")
		return mcp.Filtered{}, nil
	}
	if _, err := root.refreshMCP(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
}
