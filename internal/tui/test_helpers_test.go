package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
		next, _ := m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
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
// (and keyRunes in input_test.go) so the Bubble Tea v2 migration changes one
// place, not eighty call sites.

func keyMsg(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }

func mouseMsg(action tea.MouseAction, button tea.MouseButton, x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: action, Button: button, X: x, Y: y}
}

func clickMsg(x, y int) tea.MouseMsg {
	return mouseMsg(tea.MouseActionPress, tea.MouseButtonLeft, x, y)
}

func dragMsg(x, y int) tea.MouseMsg {
	return mouseMsg(tea.MouseActionMotion, tea.MouseButtonLeft, x, y)
}

func releaseMsg(x, y int) tea.MouseMsg {
	return mouseMsg(tea.MouseActionRelease, tea.MouseButtonLeft, x, y)
}

func wheelMsg(x, y int, up bool) tea.MouseMsg {
	button := tea.MouseButtonWheelDown
	if up {
		button = tea.MouseButtonWheelUp
	}
	return mouseMsg(tea.MouseActionPress, button, x, y)
}

// viewStr renders a model to its frame string.
func viewStr(m tea.Model) string { return m.View() }
