package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
)

// Live check of discovery against the developer's configured HTTP servers.
// Opt-in with WHIP_TEST_REAL_MCP=1: it reads $WHIP_HOME/config.json and
// reaches the network. It logs server names, counts and byte sizes only,
// never header values. WHIP_TEST_REAL_MCP_QUERY overrides the search.
func TestRealConfigDiscoverySmoke(t *testing.T) {
	if os.Getenv("WHIP_TEST_REAL_MCP") != "1" {
		t.Skip("set WHIP_TEST_REAL_MCP=1 to connect the configured MCP servers")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	servers := FromConfigMap(cfg.MCPServers)
	for name, server := range servers {
		if server.URL == "" {
			delete(servers, name) // stdio servers need the daemon's process manager
		}
	}
	if len(servers) == 0 {
		t.Skip("no native HTTP MCP servers configured")
	}
	m := NewManager(servers)
	defer m.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m.Start(ctx)
	for name, s := range m.servers {
		select {
		case <-s.ready:
		case <-ctx.Done():
			t.Fatalf("%s did not settle", name)
		}
	}
	for _, status := range m.Statuses() {
		t.Logf("%-14s %-10s tools=%-4d %s", status.Name, status.Status, status.Tools, status.Err)
	}
	query := os.Getenv("WHIP_TEST_REAL_MCP_QUERY")
	if query == "" {
		query = "halo run"
	}
	started := time.Now()
	matches, err := m.Search("", query, 0)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(matches)
	t.Logf("search %q: %d matches, %d bytes, %s", query, len(matches), len(encoded), time.Since(started).Round(time.Microsecond))
	for _, match := range matches[:min(len(matches), 5)] {
		t.Logf("  %s/%s: %s", match.Server, match.Name, match.Summary)
	}
	if len(matches) > 0 {
		tool, err := m.Describe(matches[0].Server, matches[0].Name)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ = json.Marshal(tool)
		t.Logf("describe %s/%s: %d bytes", matches[0].Server, tool.Name, len(encoded))
	}
	for _, status := range m.Statuses() {
		if status.Status != StatusReady {
			continue
		}
		listed, err := m.ListTools(status.Name)
		if err != nil {
			continue
		}
		withSchemas, _ := json.Marshal(listed)
		light := make([]Tool, len(listed))
		for i, tool := range listed {
			light[i] = Tool{Name: tool.Name, Title: tool.Title, Description: tool.Description}
		}
		lightBytes, _ := json.Marshal(light)
		t.Logf("%s: %d tools, %d bytes with schemas, %d bytes without", status.Name, len(listed), len(withSchemas), len(lightBytes))
	}
}
