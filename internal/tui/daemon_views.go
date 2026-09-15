package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/session"
)

func renderMCPStatus(raw string) (string, error) {
	var servers []daemon.MCPStatusResult
	if err := json.Unmarshal([]byte(raw), &servers); err != nil {
		return "", err
	}
	if len(servers) == 0 {
		return "MCP: no servers configured", nil
	}
	lines := []string{"MCP servers"}
	for _, server := range servers {
		line := fmt.Sprintf("  %s  %s", server.Name, server.Status)
		if server.Source != "" {
			line += " · " + server.Source
		}
		if server.Tools > 0 {
			line += fmt.Sprintf(" · %d tools", server.Tools)
		}
		if server.Error != "" {
			line += " · " + server.Error
		} else if server.Note != "" {
			line += " · " + server.Note
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

// cacheMCPInventory keeps the daemon's latest status rows for the MCP palette.
func (m *model) cacheMCPInventory(raw string) {
	var servers []daemon.MCPStatusResult
	if err := json.Unmarshal([]byte(raw), &servers); err == nil {
		m.mcpInventory = servers
	}
}

// mcpPaletteRows derives the MCP palette's server rows from the daemon's last
// status snapshot rather than from local config: imported, attached and
// remote-daemon servers appear in status while a name in local config may not
// be live at all. Each row offers only what the daemon can honor for that
// state; blocked and unreadable rows point at the status view that says why.
func mcpPaletteRows(inventory []daemon.MCPStatusResult) []struct{ title, command string } {
	type row = struct{ title, command string }
	if len(inventory) == 0 {
		return []row{{"Load MCP server status (fills this list)", "/mcp status"}}
	}
	rows := make([]row, 0, 2*len(inventory))
	for _, server := range inventory {
		name := server.Name
		switch server.Status {
		case "blocked":
			rows = append(rows, row{"Why is " + name + " blocked", "/mcp status"})
		case "unreadable":
			rows = append(rows, row{"Why is " + name + " unreadable", "/mcp status"})
		case "disabled":
			rows = append(rows, row{"Enable " + name + " for this session", "/mcp " + name + " enable"})
		case "connecting":
			rows = append(rows, row{"Disable " + name + " for this session", "/mcp " + name + " disable"})
		default:
			rows = append(rows,
				row{"Reconnect " + name, "/mcp " + name + " reconnect"},
				row{"Disable " + name + " for this session", "/mcp " + name + " disable"},
			)
		}
	}
	return rows
}

func renderLSPStatus(raw string) (string, error) {
	var servers []daemon.LSPStatusResult
	if err := json.Unmarshal([]byte(raw), &servers); err != nil {
		return "", err
	}
	if len(servers) == 0 {
		return "LSP: no servers configured", nil
	}
	lines := []string{"Language servers"}
	for _, server := range servers {
		line := fmt.Sprintf("  %s  %s", server.Name, server.State)
		if server.Root != "" {
			line += " · " + server.Root
		}
		if server.Error != "" {
			line += " · " + server.Error
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

func renderSchedules(raw string) (string, error) {
	var schedules []session.Schedule
	if err := json.Unmarshal([]byte(raw), &schedules); err != nil {
		return "", err
	}
	if len(schedules) == 0 {
		return "Schedules: none", nil
	}
	lines := []string{"Schedules"}
	for _, schedule := range schedules {
		lines = append(lines, fmt.Sprintf("  %d  %s · %s", schedule.ID, schedule.Schedule, oneLine(schedule.Prompt)))
	}
	return strings.Join(lines, "\n"), nil
}

func renderContextAudit(raw string) (string, error) {
	var audit daemon.ContextAuditResult
	if err := json.Unmarshal([]byte(raw), &audit); err != nil {
		return "", err
	}
	lines := []string{"Recursive-runtime context audit", "  workspace  " + audit.WorkingDirectory}
	total := 0
	for _, row := range audit.Rows {
		total += row.Bytes
		line := fmt.Sprintf("  %-26s ~%d tok", row.Label, (row.Bytes+3)/4)
		if row.Note != "" {
			line += " · " + row.Note
		}
		lines = append(lines, line)
	}
	lines = append(lines, fmt.Sprintf("  %-26s ~%d tok", "TOTAL visible context", (total+3)/4))
	return strings.Join(lines, "\n"), nil
}

func renderCompactionLog(raw string) (string, error) {
	var events []session.Compaction
	if err := json.Unmarshal([]byte(raw), &events); err != nil {
		return "", err
	}
	if len(events) == 0 {
		return "Compactions: none", nil
	}
	lines := []string{"Compactions — raw history is preserved; /compact retry undoes the latest"}
	for _, event := range events {
		lines = append(lines, fmt.Sprintf("  #%d folded through message %d · %s", event.Seq, event.Cutoff, oneLine(event.Summary)))
	}
	return strings.Join(lines, "\n"), nil
}
