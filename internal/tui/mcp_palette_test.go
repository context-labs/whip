package tui

import (
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/daemon"
)

// TestMCPPaletteRowsFollowDaemonState: rows offer only what the daemon can
// honor for each status, and an empty inventory offers to load one.
func TestMCPPaletteRowsFollowDaemonState(t *testing.T) {
	empty := mcpPaletteRows(nil)
	if len(empty) != 1 || empty[0].command != "/mcp status" {
		t.Fatalf("empty inventory rows = %+v", empty)
	}
	rows := mcpPaletteRows([]daemon.MCPStatusResult{
		{Name: "docs", Status: "ready"},
		{Name: "slow", Status: "connecting"},
		{Name: "off", Status: "disabled"},
		{Name: "ghost", Status: "blocked"},
		{Name: "codex config", Status: "unreadable"},
	})
	var titles []string
	for _, row := range rows {
		titles = append(titles, row.title+" → "+row.command)
	}
	got := strings.Join(titles, "\n")
	for _, want := range []string{
		"Reconnect docs → /mcp docs reconnect",
		"Disable docs for this session → /mcp docs disable",
		"Disable slow for this session → /mcp slow disable",
		"Enable off for this session → /mcp off enable",
		"Why is ghost blocked → /mcp status",
		"Why is codex config unreadable → /mcp status",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"Reconnect slow", "Reconnect off", "Enable docs", "Reconnect ghost", "Enable ghost", "Reconnect codex config"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("offered %q, which the daemon cannot honor", forbidden)
		}
	}
}

func TestCacheMCPInventoryParsesStatus(t *testing.T) {
	m := &model{}
	m.cacheMCPInventory(`[{"name":"docs","status":"ready","tools":2}]`)
	if len(m.mcpInventory) != 1 || m.mcpInventory[0].Name != "docs" || m.mcpInventory[0].Tools != 2 {
		t.Fatalf("inventory = %+v", m.mcpInventory)
	}
	m.cacheMCPInventory("not json")
	if len(m.mcpInventory) != 1 {
		t.Fatal("a malformed status must not clear the last good inventory")
	}
}
