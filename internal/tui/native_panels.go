package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/tui/ui"
)

type nativeLSPStatus struct {
	owner protocol.Session
	items []protocol.LanguageServerStatus
	err   error
}

func (s *nativeLSPStatus) matches(owner protocol.Session) bool {
	return s.owner.ID == owner.ID && s.owner.ConfigRevision == owner.ConfigRevision && s.owner.WorkingDirectory == owner.WorkingDirectory
}

func readNativeLSP(ctx context.Context, connection nativeAgentReader, owner protocol.Session) *nativeLSPStatus {
	value := &nativeLSPStatus{owner: owner}
	var result protocol.LanguageServersResult
	value.err = connection.Call(ctx, "lsp.status", protocol.SessionParams{SessionID: owner.ID}, &result)
	if value.err != nil {
		return value
	}
	if len(result.Items) > 16 {
		value.err = errors.New("language-server status exceeded its bound")
		return value
	}
	var after protocol.Session
	value.err = connection.Call(ctx, "sessions.get", protocol.SessionParams{SessionID: owner.ID}, &after)
	if value.err == nil && !value.matches(after) {
		value.err = errors.New("session configuration changed during language-server observation")
	}
	if value.err == nil {
		value.items = result.Items
	}
	return value
}

func (m *nativeModel) openPane() int {
	if m.agentsFocus {
		return paneAgents
	}
	return paneIndex(m.preferences.Panel)
}

func (m *nativeModel) panelCommand(name string) tea.Cmd {
	if name != "agents" && name != "context" && name != "lsp" {
		m.status = "usage: /panel agents|context|lsp · Ctrl+X 1/2/3 · click a sidebar heading"
		return nil
	}
	value, err := updateNativePreferences(m.preferencesDirectory, func(p *nativePreferences) { p.Panel = name })
	if err != nil {
		m.status = "Panel preference: " + err.Error()
		return nil
	}
	m.preferences, m.agentsFocus, m.polls = value, false, 0
	m.input.Reset()
	m.refresh()
	return nil
}

func (m *nativeModel) sidebarView() string {
	th := currentTheme()
	open := m.openPane()
	var panels []string
	for pane, height := range paneHeights(m.height, open) {
		if height == 0 {
			continue
		}
		panel := ui.Panel{Key: strconv.Itoa(pane + 1), Title: [...]string{"Agents", "Context", "LSP"}[pane], Width: 42, Height: height, Collapsed: pane != open, Focused: pane == paneAgents && m.agentsFocus, Band: pane == paneAgents}
		bodyHeight := max(height-2*th.Space.PadY-2, 0)
		var body string
		if pane == open && bodyHeight > 0 {
			if pane == paneAgents {
				body = m.agentRows(panel.Inner(th)+2, bodyHeight)
			} else {
				rows := m.panelRows(pane, panel.Inner(th))
				offset := max(min(m.panelOffsets[pane], max(len(rows)-bodyHeight, 0)), 0)
				body = strings.Join(rows[offset:min(offset+bodyHeight, len(rows))], "\n")
				if len(rows) > bodyHeight {
					panel.Count = fmt.Sprintf("%d/%d", offset+1, len(rows))
				}
			}
		}
		panels = append(panels, panel.Render(th, body))
	}
	return strings.Join(panels, "\n")
}

func (m *nativeModel) panelRows(pane, width int) []string {
	var rows []string
	if pane == paneLSP {
		rows = []string{"Passive status · does not start servers"}
		switch {
		case m.lsp == nil || !m.lsp.matches(m.owner):
			rows = append(rows, "Status not yet observed for this configuration.")
		case m.lsp.err != nil:
			rows = append(rows, "Unavailable: "+m.lsp.err.Error())
		case len(m.lsp.items) == 0:
			rows = append(rows, "No language servers configured.")
		default:
			for _, item := range m.lsp.items {
				rows = append(rows, item.Name+" · "+item.State)
				if item.WorkspaceRoot != nil {
					rows = append(rows, *item.WorkspaceRoot)
				}
				if item.Failure != nil {
					rows = append(rows, *item.Failure)
				}
			}
		}
	} else {
		value := m.contextUsage
		if value.SessionID != m.owner.ID {
			value = protocol.ContextUsage{}
		}
		rows = []string{nativeContextLabel(value)}
		if value.SessionID == m.owner.ID && value.Prefill != nil {
			rows = append(rows, fmt.Sprintf("Captured through #%d; current tail #%d", value.Prefill.ThroughSequence, value.ThroughSequence), "A prefill measurement, not post-response occupancy.")
		} else {
			rows = append(rows, "No current matching evidence: "+value.UnavailableReason)
		}
		rows = append(rows, "", "Session cumulative usage")
		if m.usage.SessionID != m.owner.ID {
			rows = append(rows, "Not yet observed.")
		} else {
			usage := m.usage
			rows = append(rows, "Input: "+nativeUsageQuantity(usage.InputTokens), "Output: "+nativeUsageQuantity(usage.OutputTokens), "Reasoning: "+nativeUsageQuantity(usage.ReasoningTokens), "Cached input: "+nativeUsageQuantity(usage.CachedInput), fmt.Sprintf("Reported cost: %s · %d attempts", nativeCost(usage.ReportedCost.Value), usage.ReportedCost.Attempts), fmt.Sprintf("Estimated cost: %s · %d attempts", nativeCost(usage.EstimatedCost.Value), usage.EstimatedCost.Attempts), fmt.Sprintf("Unknown-cost attempts: %d", usage.UnknownCost))
			if usage.ReportedCost.Overflow || usage.EstimatedCost.Overflow {
				rows = append(rows, "Cost overflow: values are incomplete.")
			}
		}
		rows = append(rows, "", "/context-doctor inspects captured sources; it does not preview a future request.")
	}
	return nativePlainRows(strings.Join(rows, "\n"), width, false)
}

func nativeCost(value protocol.Counter) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("$%d.%09d", value/1_000_000_000, value%1_000_000_000), "0"), ".")
}

func nativeUsageQuantity(value protocol.UsageQuantity) string {
	if value.Overflow {
		return "overflow · incomplete"
	}
	if value.MissingAttempts > 0 {
		return fmt.Sprintf("%d known; %d attempt(s) missing", value.Value, value.MissingAttempts)
	}
	return fmt.Sprintf("%d", value.Value)
}

// Panel geometry owns clicks/wheels only. An existing transcript drag still
// receives motion/release even when it crosses a column or dock boundary.
func (m *nativeModel) panelMouse(message tea.MouseMsg) (tea.Cmd, bool) {
	mouse := message.Mouse()
	if m.messageActions != nil || m.historyDialog != nil || m.menu != nil || m.picker != nil || m.decision != nil || m.palette != nil || m.completion != nil || nativePreferenceLabel(m.preferences.Mouse, true) == "off" || mouse.Mod != 0 {
		return nil, false
	}
	_, click := message.(tea.MouseClickMsg)
	_, wheel := message.(tea.MouseWheelMsg)
	if !click && !wheel || click && mouse.Button != tea.MouseLeft {
		return nil, false
	}
	th := currentTheme()
	if m.sidebarVisible() && mouse.X >= 0 && mouse.X < 42 {
		y := 0
		for pane, height := range paneHeights(m.height, m.openPane()) {
			if height == 0 {
				continue
			}
			if mouse.Y >= y && mouse.Y < y+height {
				m.selection = nil
				if click && mouse.Y < y+th.Space.PadY+2 {
					return m.commandKeepingDraft("/panel " + [...]string{"agents", "context", "lsp"}[pane]), true
				}
				if pane == m.openPane() {
					bodyY, bodyHeight := y+th.Space.PadY+2, max(height-2*th.Space.PadY-2, 0)
					if pane == paneAgents {
						return m.agentMouse(mouse, click, bodyY, bodyHeight), true
					}
					if wheel {
						rows := m.panelRows(pane, (ui.Panel{Width: 42}).Inner(th))
						delta := 3
						if mouse.Button == tea.MouseWheelUp {
							delta = -3
						}
						m.panelOffsets[pane] = max(min(m.panelOffsets[pane]+delta, max(len(rows)-bodyHeight, 0)), 0)
					}
				}
				return nil, true
			}
			y += height + 1
		}
		return nil, true
	}
	if height := m.dockHeight(); height > 0 && mouse.X >= 0 && mouse.X < m.transcriptWidth() {
		top := m.vp.Height() + 1 + m.completionHeight() + m.input.Height()
		if mouse.Y >= top && mouse.Y < top+height {
			m.selection = nil
			return m.agentMouse(mouse, click, top+1, height-1), true
		}
	}
	return nil, false
}

func (m *nativeModel) agentMouse(mouse tea.Mouse, click bool, bodyY, height int) tea.Cmd {
	if m.agents == nil || m.agents.tree != m.owner.TreeID || height <= 0 {
		return nil
	}
	lo, hi := nativeAgentWindow(m.agents.rows, m.agentSelection, max(height-2, 1))
	if click {
		index := lo + mouse.Y - bodyY
		if mouse.Y >= bodyY && index >= lo && index < hi && mouse.Y < bodyY+height {
			id := m.agents.rows[index].id
			m.agentSelection = id
			return m.commandKeepingDraft("/agents open " + string(id))
		}
		return nil
	}
	index := 0
	for i, row := range m.agents.rows {
		if row.id == m.agentSelection {
			index = i
			break
		}
	}
	switch mouse.Button {
	case tea.MouseWheelUp:
		index--
	case tea.MouseWheelDown:
		index++
	}
	if len(m.agents.rows) > 0 {
		m.agentSelection = m.agents.rows[max(min(index, len(m.agents.rows)-1), 0)].id
		m.agentsFocus, m.polls = true, 0
	}
	return nil
}
