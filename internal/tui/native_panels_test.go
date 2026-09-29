package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativePanelsPersistSelectionPreserveDraftAndStayWithinFrame(t *testing.T) {
	m := &nativeModel{owner: protocol.Session{ID: "owner"}, input: newInput(), width: 160, height: 35, preferencesDirectory: t.TempDir()}
	m.preferences.Theme = "custom"
	if err := saveNativePreferences(m.preferencesDirectory, m.preferences); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("keep my unsent draft")
	m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	if m.openPane() != paneContext || m.input.Value() != "keep my unsent draft" {
		t.Fatal(m.openPane(), m.input.Value(), m.status)
	}
	preferences, err := readNativePreferences(m.preferencesDirectory)
	if err != nil || preferences.Panel != "context" || preferences.Theme != "custom" {
		t.Fatal(preferences, err)
	}
	for _, name := range []string{"agents", "context", "lsp"} {
		m.commandKeepingDraft("/panel " + name)
		for _, size := range [][2]int{{160, 35}, {150, 10}, {121, 4}, {80, 24}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			view := m.View().Content
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatalf("%s exceeds height at %v", name, size)
			}
			for row := range strings.Lines(view) {
				if ansi.StringWidth(strings.TrimSuffix(row, "\n")) > size[0] {
					t.Fatalf("%s exceeds width at %v", name, size)
				}
			}
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m.preferences.Panel = "agents"
	heights := paneHeights(m.height, paneAgents)
	y := heights[0] + 1 + currentTheme().Space.PadY
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: y})
	if m.openPane() != paneContext || m.input.Value() != "keep my unsent draft" {
		t.Fatal("sidebar heading did not select context or preserve draft")
	}
}

func TestNativeContextPanelSeparatesPrefillFromCumulativeAndUnknowns(t *testing.T) {
	m := &nativeModel{owner: protocol.Session{ID: "owner"}, contextUsage: protocol.ContextUsage{SessionID: "owner", ThroughSequence: 5, Prefill: &protocol.ContextPrefill{InputTokens: 91, InputSource: "estimated", ThroughSequence: 2, Stale: true}}, usage: protocol.Usage{SessionID: "owner", InputTokens: protocol.UsageQuantity{Value: 1234, MissingAttempts: 2}, OutputTokens: protocol.UsageQuantity{Overflow: true}, ReportedCost: protocol.UsageCost{Value: 1, Attempts: 1}, EstimatedCost: protocol.UsageCost{Value: 1234567890, Attempts: 2}, UnknownCost: 3}}
	text := strings.Join(m.panelRows(paneContext, 160), "\n")
	for _, want := range []string{"91 tokens (estimated)", "capacity unknown", "earlier history tail", "Captured through #2; current tail #5", "not post-response occupancy", "Session cumulative usage", "1234 known; 2 attempt(s) missing", "overflow · incomplete", "$0.000000001", "$1.23456789", "Unknown-cost attempts: 3"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing truthful accounting label", want, text)
		}
	}
	m.owner.ID = "other"
	text = strings.Join(m.panelRows(paneContext, 160), "\n")
	if strings.Contains(text, "91 tokens") || strings.Contains(text, "1234") {
		t.Fatal("panel leaked previous owner's evidence", text)
	}
}

func TestNativeLSPPanelIsPassiveAndRejectsChangedConfiguration(t *testing.T) {
	owner := protocol.Session{ID: "owner", ConfigRevision: 1, WorkingDirectory: "/host/current"}
	calls := []string{}
	changed := false
	reader := nativeAgentReadFixture{call: func(method string, params, result any) error {
		calls = append(calls, method)
		if params.(protocol.SessionParams).SessionID != owner.ID {
			t.Fatal("wrong owner")
		}
		switch method {
		case "lsp.status":
			*result.(*protocol.LanguageServersResult) = protocol.LanguageServersResult{Items: []protocol.LanguageServerStatus{{Name: "gopls", State: "not_started"}}}
		case "sessions.get":
			*result.(*protocol.Session) = owner
			if changed {
				result.(*protocol.Session).ConfigRevision++
			}
		default:
			t.Fatal("panel called an effect", method)
		}
		return nil
	}}
	status := readNativeLSP(t.Context(), reader, owner)
	if status.err != nil || len(status.items) != 1 || strings.Join(calls, ",") != "lsp.status,sessions.get" {
		t.Fatal(status, calls)
	}
	changed = true
	status = readNativeLSP(t.Context(), reader, owner)
	if status.err == nil || len(status.items) != 0 {
		t.Fatal("mixed configuration observation accepted", status)
	}
	m, _ := nativeUIFixture(t)
	m.preferencesDirectory = t.TempDir()
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m.command("/panel lsp")
	nativeUIRead(t, m)
	if m.lsp == nil || m.lsp.err != nil || !strings.Contains(m.View().Content, "LSP") {
		t.Fatal(m.lsp, m.status)
	}
	inputs, err := m.handle.Inputs(t.Context(), "all", nil, 100)
	if err != nil || len(inputs.Items) != 0 {
		t.Fatal("opening LSP panel admitted work", inputs, err)
	}
	m.lsp = &nativeLSPStatus{owner: m.owner, err: errors.New("fixture unavailable")}
	if got := strings.Join(m.panelRows(paneLSP, 160), "\n"); !strings.Contains(got, "Unavailable") || strings.Contains(got, "No language servers") {
		t.Fatal("unavailable status represented as empty", got)
	}
}

func TestNativeSidebarAndDockMouseOpenExactChildAndKeepDraft(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.preferencesDirectory = t.TempDir()
	root := m.owner
	child := nativeAgentChild(t, m, root.ID, "mouse-child")
	for _, sidebar := range []bool{true, false} {
		if m.owner.ID != root.ID {
			nativeNavigate(t, m, string(root.ID))
		}
		m.preferences.Sidebar, m.dock = new(sidebar), true
		m.preferences.Panel = "agents"
		m.Update(tea.WindowSizeMsg{Width: 160, Height: 35})
		m.polls = 0
		nativeUIRead(t, m)
		m.input.SetValue("retained root draft")
		m.refresh()
		y := currentTheme().Space.PadY + 2 + 1
		if !sidebar {
			y = m.vp.Height() + 1 + m.completionHeight() + m.input.Height() + 2
		}
		_, command := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: y})
		if command == nil {
			t.Fatal("child click did not capture navigation", sidebar, y, m.status)
		}
		_, read := m.Update(command())
		if read == nil {
			t.Fatal("child attachment did not read", m.status)
		}
		m.Update(read())
		if m.owner.ID != child.ID || m.drafts[root.ID].text != "retained root draft" {
			t.Fatal("click lost exact child/draft", sidebar, m.owner.ID, m.drafts)
		}
	}
}

func TestNativePanelMouseDoesNotEscapeModalOrScrollTranscript(t *testing.T) {
	m := &nativeModel{owner: protocol.Session{ID: "owner", TreeID: "tree"}, input: newInput(), width: 160, height: 35, preferencesDirectory: t.TempDir(), preferences: nativePreferences{Panel: "context"}, agents: &nativeAgentTree{tree: "tree"}}
	for index := range 30 {
		m.agents.rows = append(m.agents.rows, nativeAgentRow{id: protocol.ID(fmt.Sprintf("child-%02d", index))})
	}
	m.refresh()
	m.vp.setTotal(100)
	m.vp.SetYOffset(10)
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 5, Y: 10})
	if m.vp.YOffset() != 10 {
		t.Fatal("sidebar wheel scrolled transcript")
	}
	m.palette = &nativeCommandPalette{}
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 1})
	if m.openPane() != paneContext {
		t.Fatal("covered panel accepted click")
	}
	m.palette = nil
	m.preferences.Mouse = new(false)
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 1})
	if m.openPane() != paneContext {
		t.Fatal("disabled mouse selected a panel")
	}
}
