package tui

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
	"slices"
	"sort"
	"strings"

	"github.com/context-labs/whip/internal/session"
)

const agentsDockHeight = 6

type runtimeAgentRow struct {
	agent session.RuntimeAgent
	depth int
}

func (m *model) runtimeAgent(id string) (session.RuntimeAgent, bool) {
	for _, value := range m.clientView.agents {
		if value.ID == id {
			return value, true
		}
	}
	return session.RuntimeAgent{}, false
}

func (m *model) visibleAgentReadOnly() bool {
	if m.agentOpen == "" {
		return false
	}
	value, ok := m.runtimeAgent(m.agentOpen)
	if !ok {
		return true
	}
	return value.LifecyclePhase == "terminal" || slices.Contains([]string{"stopped", "deleted", "failed"}, value.Status)
}

func (m *model) runtimeAgentRows() []runtimeAgentRow {
	rootID := m.rootAgentID()
	children := make(map[string][]session.RuntimeAgent)
	for _, value := range m.clientView.agents {
		if value.ParentID != "" {
			children[value.ParentID] = append(children[value.ParentID], value)
		}
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool {
			return children[parent][i].ID < children[parent][j].ID
		})
	}
	var rows []runtimeAgentRow
	seen := map[string]bool{}
	var walk func(string, int)
	walk = func(parent string, depth int) {
		for _, value := range children[parent] {
			if seen[value.ID] {
				continue
			}
			seen[value.ID] = true
			rows = append(rows, runtimeAgentRow{agent: value, depth: depth})
			walk(value.ID, depth+1)
		}
	}
	walk(rootID, 0)

	// Corrupt or partially restored lineage should remain inspectable. Keep
	// those rows deterministic without pretending they belong to the root.
	var orphans []session.RuntimeAgent
	for _, value := range m.clientView.agents {
		if value.ParentID != "" && !seen[value.ID] {
			orphans = append(orphans, value)
		}
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].ID < orphans[j].ID })
	for _, value := range orphans {
		rows = append(rows, runtimeAgentRow{agent: value})
	}
	return rows
}

func (m *model) runtimeChildren() []session.RuntimeAgent {
	rows := m.runtimeAgentRows()
	children := make([]session.RuntimeAgent, len(rows))
	for i := range rows {
		children[i] = rows[i].agent
	}
	return children
}

func (m *model) dockCount() int { return len(m.runtimeChildren()) }

func (m *model) clampAgentSel() {
	if n := m.dockCount(); m.agentSel >= n {
		m.agentSel = max(n-1, 0)
	}
}

func (m *model) agentsDock() string {
	if m.uiMode == opencodeMode && m.sidebarVisible() {
		return "" // the tree lives in the right panel (agentTreeRows)
	}
	agents := m.runtimeAgentRows()
	if len(agents) == 0 {
		return ""
	}
	m.clampAgentSel()
	rows := make([]string, 0, len(agents)+1)
	if m.agentsFocus {
		rows = append(rows, dimStyle.Render(" ⚙ agents — ↑/↓ select · enter open · ctrl+x stop · esc back"))
	}
	budget := agentsDockHeight - len(rows)
	if len(agents) > budget {
		budget--
	}
	lo := 0
	if m.agentsFocus && m.agentSel >= budget {
		lo = m.agentSel - budget + 1
	}
	hi := min(lo+budget, len(agents))
	for index := lo; index < hi; index++ {
		row := agents[index]
		line := strings.Repeat("  ", row.depth) + runtimeAgentLine(row.agent)
		if row.agent.ID == m.agentOpen {
			line += " · open"
		}
		if m.width > 3 { // unsized before the first WindowSizeMsg
			line = ansi.Truncate(line, m.width-3, "…")
		}
		if m.agentsFocus && index == m.agentSel {
			line = botStyle.Render(" → " + line)
		} else {
			line = "   " + line
		}
		rows = append(rows, line)
	}
	if more := len(agents) - hi; more > 0 {
		rows = append(rows, dimStyle.Render(fmt.Sprintf("   … +%d more", more)))
	}
	return strings.Join(rows, "\n")
}

// agentTreeRows is the opencode right-panel form of the dock: an "Agents"
// header (with the key hint while focused), the tree with the same selection
// and open markers, and, for the REPL panel, each agent's running cell.
func (m *model) agentTreeRows(inner int, st replStyles, withCells bool) []string {
	agents := m.runtimeAgentRows()
	if len(agents) == 0 {
		return nil
	}
	m.clampAgentSel()
	cut := func(s string) string { return ansi.Truncate(s, max(inner, 1), "…") }
	header := "Agents"
	if m.agentsFocus {
		header += "  ↑/↓ · enter open · ctrl+x stop · esc"
	}
	rows := []string{st.head.Render(cut(header))}
	budget := agentsDockHeight
	if len(agents) > budget {
		budget--
	}
	lo := 0
	if m.agentsFocus && m.agentSel >= budget {
		lo = m.agentSel - budget + 1
	}
	hi := min(lo+budget, len(agents))
	for index := lo; index < hi; index++ {
		row := agents[index]
		line := strings.Repeat("  ", row.depth) + runtimeAgentLine(row.agent)
		if row.agent.ID == m.agentOpen {
			line += " · open"
		}
		if withCells {
			line += m.replCurrentCell(row.agent.ID)
		}
		style, marker := st.dim, "  "
		switch {
		case m.agentsFocus && index == m.agentSel:
			style, marker = st.accent, "→ "
		case row.agent.LifecyclePhase == "running":
			style = st.text
		}
		rows = append(rows, style.Render(cut(marker+line)))
	}
	if more := len(agents) - hi; more > 0 {
		rows = append(rows, st.dim.Render(fmt.Sprintf("  … +%d more", more)))
	}
	return rows
}

func (m *model) agentDetails() string {
	value, ok := m.runtimeAgent(m.agentOpen)
	if !ok {
		return ""
	}
	rows := []string{
		botStyle.Render("⚙ " + value.Name),
		dimStyle.Render("  id " + value.ID),
		dimStyle.Render("  parent " + value.ParentID),
		dimStyle.Render("  " + value.Model + " @ " + value.Provider + " · " + value.Effort + " · " + value.LifecyclePhase),
		dimStyle.Render("  cwd " + value.CWD),
	}
	if value.BlockingReason != "" {
		rows = append(rows, errStyle.Render("  blocked: "+value.BlockingReason))
	}
	if value.TerminalCause != "" {
		rows = append(rows, dimStyle.Render("  terminal cause "+value.TerminalCause))
	}
	if len(value.AllowedControls) > 0 {
		rows = append(rows, dimStyle.Render("  controls "+strings.Join(value.AllowedControls, ", ")))
	}
	var capabilities []string
	for _, record := range m.clientView.capabilities {
		if record.AgentID == value.ID && record.Status == "active" {
			capabilities = append(capabilities, record.Operations...)
		}
	}
	sort.Strings(capabilities)
	capabilities = slices.Compact(capabilities)
	if len(capabilities) > 0 {
		rows = append(rows, dimStyle.Render("  capabilities "+strings.Join(capabilities, ", ")))
	}
	var budgets []string
	for _, budget := range m.clientView.budgets {
		if budget.AgentID == value.ID {
			budgets = append(budgets, fmt.Sprintf("%s %d/%d", budget.State.Kind, budget.State.Used+budget.State.Reserved, budget.State.Limit))
		}
	}
	if len(budgets) > 0 {
		rows = append(rows, dimStyle.Render("  budgets "+strings.Join(budgets, " · ")))
	}
	pending := 0
	for _, permission := range m.clientView.permissions {
		if permission.AgentID == value.ID && permission.Status == "pending" {
			pending++
		}
	}
	for _, item := range m.clientView.inbox {
		if item.AgentID == value.ID && (item.Status == "queued" || item.Status == "running") {
			pending++
		}
	}
	if pending > 0 {
		rows = append(rows, dimStyle.Render(fmt.Sprintf("  pending permissions/work %d", pending)))
	}
	if value.PendingMail > 0 {
		rows = append(rows, dimStyle.Render(fmt.Sprintf("  pending mail %d", value.PendingMail)))
	}
	if m.visibleAgentReadOnly() {
		rows = append(rows, dimStyle.Render("  read-only — this agent cannot accept more turns"))
	}
	rows = append(rows, dimStyle.Render("  esc returns to root · ctrl+c twice cancels this turn · ctrl+t opens agent tree"))
	if m.width > 0 { // a row wider than the chat column would push the sidebar over
		for index := range rows {
			rows[index] = ansi.Truncate(rows[index], m.width, "…")
		}
	}
	return strings.Join(rows, "\n")
}
