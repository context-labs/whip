package tui

import (
	"fmt"
	"image/color"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tui/theme"
	"github.com/context-labs/whip/internal/tui/ui"
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
	walk(rootID, 1)
	// The root heads the tree so there is always a row to return to — and so
	// the Agents panel shows what the session is doing before any child exists.
	if root, ok := m.runtimeAgent(rootID); ok {
		if root.Name == "" {
			root.Name = "root"
		}
		rows = append([]runtimeAgentRow{{agent: root}}, rows...)
	}

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
		rows = append(rows, runtimeAgentRow{agent: value, depth: 1})
	}
	return rows
}

// firstChildSel is where tree focus starts: the first descendant, so enter
// opens something new rather than the root you are already looking at.
func (m *model) firstChildSel() int {
	rows := m.runtimeAgentRows()
	if len(rows) > 1 && rows[0].agent.ParentID == "" {
		return 1
	}
	return 0
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

// agentBadge is the state word and colour for an agent row: the lifecycle
// phase, or the status when the phase is not known yet.
func agentBadge(th *theme.Theme, a session.RuntimeAgent) (string, color.Color) {
	phase := a.LifecyclePhase
	if phase == "" {
		phase = a.Status
	}
	switch phase {
	case "running":
		return "running", th.Success
	case "blocked":
		return "blocked", th.Warning
	case "idle":
		return "idle", th.Muted
	case "terminal":
		switch a.TerminalCause {
		case "failed", "interrupted":
			return a.TerminalCause, th.Error
		case "succeeded", "":
			return "done", th.Text
		}
		return a.TerminalCause, th.Muted
	case "failed", "interrupted":
		return phase, th.Error
	case "succeeded":
		return "done", th.Text
	case "":
		return "queued", th.Info
	}
	return phase, th.Muted
}

// agentRows renders the tree as ui.ListRows of the given band width on bg:
// badge, name (short id when unnamed), depth indent, and the agent's current
// activity or pending mail on the right. budget rows are shown, windowed
// around the selection; the boolean reports whether rows were left out.
func (m *model) agentRows(width int, bg color.Color, budget int) ([]string, bool) {
	agents := m.runtimeAgentRows()
	if len(agents) == 0 {
		return nil, false
	}
	m.clampAgentSel()
	th := currentTheme()
	lo, hi := 0, len(agents)
	if budget > 0 && budget < len(agents) {
		lo, hi = ui.ListWindow(len(agents), m.agentSel, budget)
	}
	badges := make([]string, len(agents))
	colors := make([]color.Color, len(agents))
	widest := 0
	for i := lo; i < hi; i++ {
		badges[i], colors[i] = agentBadge(th, agents[i].agent)
		widest = max(widest, len(badges[i]))
	}
	rows := make([]string, 0, hi-lo)
	for i := lo; i < hi; i++ {
		a := agents[i].agent
		name := a.Name
		if name == "" {
			name = shortAgentID(a.ID)
		}
		right := m.agentActivity(a.ID)
		if right == "" && a.PendingMail > 0 {
			right = fmt.Sprintf("mail %d", a.PendingMail)
		}
		rows = append(rows, ui.ListRow{
			Badge: badges[i] + strings.Repeat(" ", widest-len(badges[i])), BadgeColor: colors[i],
			Label: name, Right: right, Depth: agents[i].depth,
			Selected: m.agentsFocus && i == m.agentSel, Open: a.ID == m.agentOpen && a.ParentID != "",
			Width: width,
		}.Render(th, bg))
	}
	return rows, hi-lo < len(agents)
}

// agentsDock is the narrow-terminal form of the tree: rows glued under the
// prompt when there is no left column to hold the Agents panel.
func (m *model) agentsDock() string {
	if m.leftVisible() {
		return "" // the tree lives in the Agents panel
	}
	width := m.width
	if width < 20 { // unsized before the first WindowSizeMsg
		width = 80
	}
	rows, more := m.agentRows(width, nil, agentsDockHeight)
	if len(rows) == 0 {
		return ""
	}
	if more {
		th := currentTheme()
		rows = append(rows, th.On(th.Muted, nil).Render(" …"))
	}
	return strings.Join(rows, "\n")
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
	if m.width > 0 { // a row wider than the chat column would push the sidebar over
		for index := range rows {
			rows[index] = ansi.Truncate(rows[index], m.width, "…")
		}
	}
	return strings.Join(rows, "\n")
}

// anyAgentRunning reports whether some agent in the tree is mid-turn (the
// spinner and activity rows keep ticking for sub-agents while the root idles).
func (m *model) anyAgentRunning() bool {
	for _, a := range m.clientView.agents {
		if a.LifecyclePhase == "running" {
			return true
		}
	}
	return false
}

// agentLine is the plain one-line form of an agent for transcript output
// (the /agents list): "<state> <name>".
func agentLine(a session.RuntimeAgent) string {
	badge, _ := agentBadge(currentTheme(), a)
	name := a.Name
	if name == "" {
		name = shortAgentID(a.ID)
	}
	return badge + " " + name
}
