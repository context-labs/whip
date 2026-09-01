package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
)

// mcpCommand handles "/mcp [name] [reconnect|enable|disable]".
func (m *model) mcpCommand(fields []string) (tea.Model, tea.Cmd) {
	if m.mcpMgr == nil {
		m.append(dimStyle.Render("no MCP servers configured — add one with `whip mcp add <name> -- <cmd...>`, a .mcp.json, or ~/.codex/config.toml"))
		return m, nil
	}
	if len(fields) == 1 {
		m.append(m.mcpStatusView())
		return m, nil
	}
	name := fields[1]
	action := "reconnect"
	if len(fields) > 2 {
		action = fields[2]
	}
	switch action {
	case "reconnect":
		if !m.mcpMgr.Reconnect(name) {
			m.append(errStyle.Render("no MCP server named " + name))
			return m, nil
		}
		m.append(dimStyle.Render(fmt.Sprintf("↻ reconnecting mcp server %s…", name)))
	case "disable", "enable":
		m.mcpSetEnabled(name, action == "enable")
	default:
		m.append(errStyle.Render("usage: /mcp [name] [reconnect|enable|disable]"))
	}
	return m, nil
}

// mcpSetEnabled persists a toggle into whip's own config and applies it
// live. For imported (claude/codex) servers the FULL definition is copied
// into whip's config first — otherwise a bare {enabled:false} entry would
// shadow the import on next launch and lose the command/url for re-enable.
func (m *model) mcpSetEnabled(name string, enabled bool) {
	if m.mcpMgr.BlockedByPolicy(name) {
		m.append(errStyle.Render(fmt.Sprintf("mcp server %s is blocked by the mcpImport config — edit ~/.whip/config.json (or remove the gate) to enable it", name)))
		return
	}
	live, ok := m.mcpMgr.Config(name)
	if !ok {
		m.append(errStyle.Render("no MCP server named " + name))
		return
	}
	entry := config.MCPServer{
		Command: live.Command, Env: live.Env, Cwd: live.Cwd,
		URL: live.URL, Headers: live.Headers,
		StartupTimeout: live.StartupTimeout, ToolTimeout: live.ToolTimeout,
		Enabled: &enabled,
	}
	if m.cfg.MCPServers == nil {
		m.cfg.MCPServers = map[string]config.MCPServer{}
	}
	m.cfg.MCPServers[name] = entry
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
		return
	}
	if enabled {
		m.mcpMgr.Enable(name)
	} else {
		m.mcpMgr.Disable(name)
	}
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	m.append(dimStyle.Render(fmt.Sprintf("mcp server %s: %s (persisted)", name, state)))
	m.append(m.mcpStatusView())
}

// mcpOnChange returns the manager's OnChange callback — the single
// implementation shared by Run (startup) and mcpSetImport (lazy manager). It
// pushes the current tool set into the current agent, then notifies the UI.
//
// The Send MUST be detached (go …): fireOnChange also runs synchronously on
// the UI goroutine when a palette toggle (mcpSetImport/mcpSetEnabled) adds or
// removes servers from inside Update, and prog.Send blocks on the eventLoop's
// unbuffered msgs channel — called from the event loop itself it deadlocks
// the TUI (the frozen-ctrl-p bug). A stale message is harmless: Update is
// idempotent on mcpStatusMsg.
func (m *model) mcpOnChange() func() {
	manager, ag := m.mcpMgr, m.agent
	return func() {
		ag.SetMCPTools(manager.Tools())
		if m.prog != nil { // nil in headless tests
			go m.prog.Send(mcpStatusMsg{})
		}
	}
}

// buildMCPRows assembles the MCPs palette panel: the two import-source
// toggles first (state from cfg.MCPImport — always shown, even with no
// servers configured, so imports can be switched on from the palette), then
// one row per live server (mcpMgr.Statuses) and per policy-blocked server
// (mcpMgr.Blocked).
func (m *model) buildMCPRows() []mcpRow {
	var rows []mcpRow
	imp := m.cfg.MCPImport
	sourceRow := func(name, detail string, src *config.MCPImportSource) mcpRow {
		on := true
		filtered := false
		if src != nil {
			if src.Enabled != nil {
				on = *src.Enabled
			}
			filtered = len(src.Only) > 0 || len(src.Exclude) > 0
		}
		return mcpRow{name: name, source: true, on: on, detail: detail, filtered: filtered}
	}
	var claudeSrc, codexSrc *config.MCPImportSource
	if imp != nil {
		claudeSrc, codexSrc = imp.Claude, imp.Codex
	}
	rows = append(rows,
		sourceRow("claude", "import from ~/.claude.json, .mcp.json", claudeSrc),
		sourceRow("codex", "import from ~/.codex/config.toml", codexSrc),
	)
	if m.mcpMgr == nil {
		return rows // source toggles still apply (mcpSetImport builds the manager)
	}
	for _, s := range m.mcpMgr.Statuses() {
		detail := s.Status.String()
		switch s.Status {
		case mcp.StatusReady:
			detail = fmt.Sprintf("ready · %d tools", s.Tools)
		case mcp.StatusFailed:
			detail = "failed — " + s.Err
		}
		rows = append(rows, mcpRow{name: s.Name, on: s.Status != mcp.StatusDisabled, detail: detail})
	}
	for _, s := range m.mcpMgr.Blocked() {
		rows = append(rows, mcpRow{name: s.Name, detail: "blocked by mcpImport config", disabled: true})
	}
	return rows
}

// mcpSetImport persists an import-source gate and applies it live: off
// disconnects that source's imported servers and drops their tools from the
// next turn; on re-runs discovery (LoadMergedFiltered) and connects the newly
// admitted servers — all without a restart. Whip-owned servers (the config's
// "mcp" block) never move: whip always wins per name. With no manager yet
// (nothing configured), enabling builds one so a palette toggle can switch
// imports on from zero.
func (m *model) mcpSetImport(source string, enabled bool) {
	// Persist the gate (allocating the block as needed).
	if m.cfg.MCPImport == nil {
		m.cfg.MCPImport = &config.MCPImport{}
	}
	src := &config.MCPImportSource{Enabled: &enabled}
	switch source {
	case "claude":
		src.Only, src.Exclude = importLists(m.cfg.MCPImport.Claude)
		m.cfg.MCPImport.Claude = src
	case "codex":
		src.Only, src.Exclude = importLists(m.cfg.MCPImport.Codex)
		m.cfg.MCPImport.Codex = src
	default:
		m.append(errStyle.Render("unknown MCP import source " + source))
		return
	}
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
		return
	}

	wd, err := os.Getwd()
	if err != nil {
		return
	}
	disc := mcp.LoadMergedFiltered(wd, mcp.FromConfigMap(m.cfg.MCPServers), mcp.ImportPolicyFrom(m.cfg.MCPImport))

	// Attribute by source FILE — two shapes exist: disc.Sources uses short
	// labels ("whip", ".mcp.json", "~/.claude.json", "codex"), but the live
	// manager's Statuses() carry the ABSOLUTE path setSource stamped at
	// discovery (e.g. /home/u/.codex/config.toml). A toggle must match both:
	// matching only the short labels made the OFF path a silent no-op in
	// production (the adversarial review's shipping blocker).
	isSource := func(file string) bool {
		base := filepath.Base(file)
		switch source {
		case "claude":
			return file == ".mcp.json" || file == "~/.claude.json" ||
				base == ".mcp.json" || base == ".claude.json"
		case "codex":
			return file == "codex" ||
				(base == "config.toml" && filepath.Base(filepath.Dir(file)) == ".codex")
		}
		return false
	}

	// A toggle with no manager yet (nothing configured at startup) builds one
	// on the spot — enabling imports from zero must work from the palette.
	// The OnChange wiring mirrors Run's: pushes settle tool sets into the
	// current agent and redraws the status badge.
	if m.mcpMgr == nil {
		m.mcpMgr = mcp.NewManager(nil)
		m.mcpMgr.SetOnChange(m.mcpOnChange()) // same detached-Send contract as Run
	}

	if !enabled {
		// Off: tear down this source's live servers (whip-owned entries report
		// "whip" and are never in this set) and surface the blocked rows.
		var remove []string
		for _, s := range m.mcpMgr.Statuses() {
			if isSource(s.Source) {
				remove = append(remove, s.Name)
			}
		}
		m.mcpMgr.RemoveServers(remove...)
		m.mcpMgr.SetBlocked(disc.Blocked)
		m.append(dimStyle.Render(fmt.Sprintf("%s imports: off (persisted) — %d server(s) disconnected; ctrl+p → MCPs to re-enable", source, len(remove))))
		return
	}

	// On: connect newly admitted servers (AddServers skips names already live,
	// so whip-owned shadow entries and existing sessions win), and surface the
	// still-blocked rows.
	have := map[string]bool{}
	for _, s := range m.mcpMgr.Statuses() {
		have[s.Name] = true
	}
	add := map[string]mcp.ServerConfig{}
	for name, cfg := range disc.Merged {
		if file := disc.Sources[name]; isSource(file) && !have[name] {
			add[name] = cfg
		}
	}
	// context.Background() is deliberate (and the manager's existing contract,
	// manager.go Start/reconnect): the connects must outlive this keypress —
	// canceling them with the handler would leave the toggle half-applied.
	m.mcpMgr.AddServers(context.Background(), add)
	m.mcpMgr.SetBlocked(disc.Blocked)
	m.append(dimStyle.Render(fmt.Sprintf("%s imports: on (persisted) — %d server(s) connecting", source, len(add))))
	m.append(m.mcpStatusView())
}

// importLists carries a source's only/exclude name filters across a toggle so
// flipping enabled never drops the user's filters.
func importLists(src *config.MCPImportSource) (only, exclude []string) {
	if src == nil {
		return nil, nil
	}
	return src.Only, src.Exclude
}

// mcpStatusView renders the /mcp table: one row per server with status,
// tool count, and failure detail.
func (m *model) mcpStatusView() string {
	servers := append(m.mcpMgr.Statuses(), m.mcpMgr.Blocked()...)
	if len(servers) == 0 {
		return dimStyle.Render("no MCP servers")
	}
	var b strings.Builder
	b.WriteString("MCP servers:\n")
	for _, s := range servers {
		icon := "◌"
		detail := ""
		switch s.Status {
		case mcp.StatusReady:
			icon = "●"
			detail = fmt.Sprintf("%d tools", s.Tools)
		case mcp.StatusFailed:
			icon = "✗"
			detail = s.Err
			if s.Source != "" {
				detail += " (" + s.Source + ")"
			}
		case mcp.StatusDisabled:
			icon = "○"
			detail = "disabled"
			if s.Note != "" {
				detail = "disabled — " + s.Note
			}
		case mcp.StatusConnecting:
			icon = "◌"
			detail = "connecting…"
		}
		line := fmt.Sprintf("  %s %-20s %s", icon, s.Name, detail)
		switch s.Status {
		case mcp.StatusReady:
			b.WriteString(line + "\n")
		case mcp.StatusFailed:
			b.WriteString(errStyle.Render(line) + "\n")
		default:
			b.WriteString(dimStyle.Render(line) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
