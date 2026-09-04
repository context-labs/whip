package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/config"
)

func compactCmdModel() *model {
	cfg := &config.Config{
		DefaultModel: "kimi-k3-fast",
		Providers: map[string]config.Provider{
			"inference": {BaseURL: "https://example.invalid", APIKey: "test"},
		},
		Models: map[string]config.Model{
			"kimi-k3-fast": {Providers: []string{"inference"}},
			"glm-5.2-fast": {Providers: []string{"inference"}},
		},
	}
	m := &model{
		cfg: cfg, input: newInput(), mouseOn: true, follow: true, hoverIdx: -1,
		client:    &Client{},
		modelName: "kimi-k3-fast", provName: "inference", now: time.Now,
		clientView: clientPresentation{modelID: "kimi-k3-fast", contextLimit: 131072},
		catalogs: map[string]config.Catalog{
			"inference": {Models: []config.ModelInfoLite{{ID: "kimi-k3-fast", ContextLength: 131072}}},
		},
	}
	m.width = 80
	m.input.SetWidth(78)
	return m
}

func modelCmdModel() *model {
	m := compactCmdModel()
	m.mouseOn = false
	return m
}

func typeStr(t *testing.T, m *model, value string) *model {
	t.Helper()
	for _, char := range value {
		next, _ := m.key(keyRunes(string(char)))
		m = next.(*model)
	}
	return m
}

func authTestModel(t *testing.T) *model {
	t.Helper()
	t.Setenv("WHIP_HOME", t.TempDir())
	m := compactCmdModel()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m.cfg = cfg
	return m
}

func (m *model) transcriptText() string {
	var out strings.Builder
	for _, block := range m.blocks {
		out.WriteString(block.text)
		out.WriteByte('\n')
	}
	return out.String()
}

// Input and view helpers: tests build key and mouse messages through these
// (and keyRunes in input_test.go) so a Bubble Tea API change touches one
// place, not eighty call sites.

func keyMsg(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// ctrlKey is ctrl plus a letter, e.g. ctrlKey('x').
func ctrlKey(letter rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: letter, Mod: tea.ModCtrl} }

func shiftTab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift} }

func mouseAt(x, y int, button tea.MouseButton) tea.Mouse {
	return tea.Mouse{X: x, Y: y, Button: button}
}

func clickMsg(x, y int) tea.MouseClickMsg { return tea.MouseClickMsg(mouseAt(x, y, tea.MouseLeft)) }

func dragMsg(x, y int) tea.MouseMotionMsg { return tea.MouseMotionMsg(mouseAt(x, y, tea.MouseLeft)) }

func releaseMsg(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg(mouseAt(x, y, tea.MouseLeft))
}

func wheelMsg(x, y int, up bool) tea.MouseWheelMsg {
	button := tea.MouseWheelDown
	if up {
		button = tea.MouseWheelUp
	}
	return tea.MouseWheelMsg(mouseAt(x, y, button))
}

// viewStr renders a model to its frame string.
func viewStr(m tea.Model) string { return m.View().Content }
