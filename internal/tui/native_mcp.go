package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

func nativeMCPStatus(rows []protocol.MCPServerStatus) string {
	lines := make([]string, 0, 1+len(rows))
	lines = append(lines, "MCP servers (status does not start connections)")
	for _, row := range rows {
		line := fmt.Sprintf("%s · %s · %d tools · %s", row.Name, row.State, row.Tools, row.Source)
		if row.Note != "" {
			line += "\n" + row.Note
		}
		if row.Failure != nil {
			line += "\n" + *row.Failure
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m *nativeModel) mcpCommand(args string) tea.Cmd {
	owner := m.owner.ID
	fields := strings.Fields(args)
	if len(fields) == 0 || args == "status" || args == "list" {
		return m.control("MCP status", false, func(ctx context.Context) nativeControlResult {
			var result protocol.MCPStatusResult
			err := m.connection.Call(ctx, "mcp.status", protocol.SessionParams{SessionID: owner}, &result)
			return nativeControlResult{notice: nativeMCPStatus(result.Items), err: err}
		})
	}
	if fields[0] == "import" {
		return m.mcpImportCommand(fields[1:])
	}
	if !m.nativeAdmissionAvailable() {
		return nil
	}
	method := "mcp.reconnect"
	var params any = protocol.MCPServerParams{SessionID: owner, Server: fields[0]}
	if args == "refresh" || args == "reload" {
		method, params = "mcp."+args, protocol.SessionParams{SessionID: owner}
	} else if len(fields) == 2 && (fields[1] == "reconnect" || fields[1] == "enable" || fields[1] == "disable") {
		method = "mcp." + fields[1]
	} else if len(fields) != 1 {
		m.status = "usage: /mcp [status|refresh|reload|<server> reconnect|enable|disable|import <source> on|off]"
		return nil
	}
	// Human connection actions own their host lifetime, but have no durable
	// request receipt. Losing an acknowledgment must never offer generic replay.
	return m.control("MCP connection action", false, func(ctx context.Context) nativeControlResult {
		var result protocol.MCPRefreshResult
		if err := m.connection.Call(ctx, method, params, &result); err != nil {
			return nativeControlResult{err: fmt.Errorf("connection outcome may be partial or unknown; /mcp status inspects it before another explicit action: %w", err)}
		}
		rows := append(result.Servers, result.Blocked...)
		rows = append(rows, result.SourceErrors...)
		return nativeControlResult{notice: nativeMCPStatus(rows) + "\nAdded declarations: " + strings.Join(result.Added, ", ") + "\nChanged declarations requiring reload: " + strings.Join(result.Changed, ", ")}
	})
}

func (m *nativeModel) mcpImportCommand(fields []string) tea.Cmd {
	if len(fields) == 0 || len(fields) == 1 && fields[0] == "status" {
		return m.control("MCP import policy", false, func(ctx context.Context) nativeControlResult {
			var value protocol.MCPConfiguration
			if err := m.connection.Call(ctx, "mcp.configuration", protocol.EmptyParams{}, &value); err != nil {
				return nativeControlResult{err: err}
			}
			raw, err := json.MarshalIndent(value.Imports, "", "  ")
			return nativeControlResult{notice: "Host MCP import policy\n" + string(raw), err: err}
		})
	}
	if len(fields) != 2 || fields[1] != "on" && fields[1] != "off" || fields[0] != "claude" && fields[0] != "codex" && fields[0] != "project" && fields[0] != "opencode" {
		m.status = "usage: /mcp import [status|claude|codex|project|opencode on|off]"
		return nil
	}
	if !m.nativeAdmissionAvailable() {
		return nil
	}
	var params *protocol.ConfigureMCPParams
	return m.control("Update MCP import policy", true, func(ctx context.Context) nativeControlResult {
		if params == nil {
			var value protocol.MCPConfiguration
			if err := m.connection.Call(ctx, "mcp.configuration", protocol.EmptyParams{}, &value); err != nil {
				return nativeControlResult{err: err}
			}
			var target **protocol.MCPImportSource
			switch fields[0] {
			case "claude":
				target = &value.Imports.Claude
			case "codex":
				target = &value.Imports.Codex
			case "project":
				target = &value.Imports.Project
			case "opencode":
				target = &value.Imports.Opencode
			}
			if *target == nil {
				*target = &protocol.MCPImportSource{Only: []string{}, Exclude: []string{}}
			}
			(*target).Enabled = new(fields[1] == "on")
			params = &protocol.ConfigureMCPParams{Revision: value.Revision, Imports: &value.Imports}
		}
		var updated protocol.MCPConfiguration
		err := m.connection.Call(ctx, "mcp.configure", *params, &updated)
		return nativeControlResult{notice: "Host MCP import policy saved. /mcp refresh discovers added declarations; /mcp reload explicitly replaces live configuration. Imported connections still require their canonical trust and permission checks.", err: err}
	})
}
