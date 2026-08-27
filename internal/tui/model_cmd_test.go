package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

func TestBuildAgentCodexAuthNeedsNoAPIKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WHIP_HOME", t.TempDir())
	path := filepath.Join(home, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"tokens":{"access_token":"access","refresh_token":"refresh","account_id":"account"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"codex": {Models: []config.ModelInfoLite{
			{ID: "gpt-5.4", ContextLength: 272000, MaxCompletionTokens: 128000, InputModalities: []string{"text", "image"}},
			{ID: "gpt-5.6-sol", ContextLength: 1050000, ReasoningEfforts: []string{"low", "high"}, InputModalities: []string{"text", "image"}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DefaultModel: "gpt-5.4",
		Providers: map[string]config.Provider{
			"codex": {
				API:     "openai-codex-responses",
				Auth:    "codex",
				BaseURL: "https://chatgpt.com/backend-api",
			},
		},
		Models: map[string]config.Model{
			"gpt-5.4": {Providers: []string{"codex"}, Context: 272000, MaxOut: 128000},
		},
	}
	ag, _, _, err := buildAgent(cfg, "", "", "system")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ag.Client.(*llm.Codex); !ok {
		t.Fatalf("client = %T, want *llm.Codex", ag.Client)
	}
	if ag.MaxTokens != 128000 || ag.ContextLimit != 272000 {
		t.Fatalf("limits = max %d context %d", ag.MaxTokens, ag.ContextLimit)
	}

	// A model advertised only by the authenticated Codex catalog resolves and
	// builds exactly like an OpenRouter catalog model; no per-model route is
	// required in config.json.
	catalogAgent, name, provider, err := buildAgent(cfg, "gpt-5.6-sol", "codex", "system")
	if err != nil {
		t.Fatal(err)
	}
	if name != "gpt-5.6-sol" || provider != "codex" {
		t.Fatalf("catalog route = %q @ %q", name, provider)
	}
	if catalogAgent.ContextLimit != 1050000 || catalogAgent.MaxTokens != 1050000 {
		t.Fatalf("catalog limits = max %d context %d", catalogAgent.MaxTokens, catalogAgent.ContextLimit)
	}
	if !modelSupportsVision(cfg, name, catalogAgent.Model, config.LoadCatalogs(), provider) {
		t.Fatal("Codex catalog image capability should reach the screenshot gate")
	}
}

func TestBuildAgentCodexAuthGivesLoginHint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		DefaultModel: "gpt-5.4",
		Providers: map[string]config.Provider{
			"codex": {API: "openai-codex-responses", Auth: "codex", BaseURL: "https://chatgpt.com/backend-api"},
		},
		Models: map[string]config.Model{"gpt-5.4": {Providers: []string{"codex"}}},
	}
	_, _, _, err := buildAgent(cfg, "", "", "system")
	if err == nil || !strings.Contains(err.Error(), "whip auth codex") {
		t.Fatalf("error = %v, want login hint", err)
	}
}

func TestBuildAgentCodexRejectsCustomEndpoint(t *testing.T) {
	cfg := &config.Config{
		DefaultModel: "gpt-5.4",
		Providers: map[string]config.Provider{
			"codex": {API: "openai-codex-responses", Auth: "codex", BaseURL: "https://example.com"},
		},
		Models: map[string]config.Model{"gpt-5.4": {Providers: []string{"codex"}}},
	}
	_, _, _, err := buildAgent(cfg, "", "", "system")
	if err == nil || !strings.Contains(err.Error(), "must use https://chatgpt.com/backend-api") {
		t.Fatalf("error = %v, want safe Codex endpoint", err)
	}
}

func TestBuildAgentSurfacesUnresolvedSecret(t *testing.T) {
	cfg := &config.Config{
		DefaultModel: "test",
		Providers: map[string]config.Provider{
			"test": {
				APIKey:  "$WHIP_TUI_SECRET_UNSET",
				BaseURL: "https://example.com",
			},
		},
		Models: map[string]config.Model{
			"test": {Providers: []string{"test"}},
		},
	}

	_, _, _, err := buildAgent(cfg, "", "", "system")
	if err == nil || !strings.Contains(err.Error(), "WHIP_TUI_SECRET_UNSET") {
		t.Fatalf("error = %v, want unresolved secret name", err)
	}
}

func modelCmdModel() *model {
	m := &model{
		input: newInput(),
		agent: &agent.Agent{},
		cfg: &config.Config{
			DefaultModel: "kimi-k3-fast",
			Providers:    map[string]config.Provider{"inference": {BaseURL: "https://x", APIKey: "k"}},
			Models: map[string]config.Model{
				"kimi-k3-fast": {Providers: []string{"inference"}},
				"glm-5.2-fast": {Providers: []string{"inference"}},
			},
		},
		modelName: "kimi-k3-fast",
		provName:  "inference",
	}
	m.width = 80
	m.input.SetWidth(78)
	return m
}

func typeStr(t *testing.T, m *model, s string) *model {
	t.Helper()
	for _, r := range s {
		tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = tm.(*model)
	}
	return m
}

// Regression: typing /model and pressing enter must open the interactive
// picker, NOT insert a newline. (The newline bug was KeyCtrlM == KeyEnter
// being forwarded to the textarea; this guards against its return.)
func TestModelBareEnterOpensPicker(t *testing.T) {
	m := modelCmdModel()
	m = typeStr(t, m, "/model")
	if m.menu == nil {
		t.Fatal("typing /model should focus the completion menu")
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if m.mpicker == nil {
		t.Fatalf("/model + enter should open the model picker; input=%q LineCount=%d", m.input.Value(), m.input.LineCount())
	}
	if m.input.Value() != "" || m.input.LineCount() != 1 {
		t.Errorf("enter must not leave a newline in the input: value=%q LineCount=%d", m.input.Value(), m.input.LineCount())
	}
}

// The ctrl+p palette's first suggestion is Model; enter drills into its
// interactive panel without leaving the palette.
func TestModelPaletteEnterOpensPicker(t *testing.T) {
	m := modelCmdModel()
	tm, _ := m.key(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = tm.(*model)
	if m.palette == nil {
		t.Fatal("ctrl+p should open the command palette")
	}
	if len(m.palette.items) == 0 || m.palette.items[0].title != "Model" {
		t.Fatalf("first suggestion should be Model, got %+v", m.palette.items)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	pp := m.palette.top()
	if pp == nil || pp.kind != panelModel {
		t.Fatalf("palette Model + enter should push the model panel; input=%q", m.input.Value())
	}
	if len(pp.items) == 0 {
		t.Fatal("model panel should list the configured routes")
	}
}

// Selecting a model name completes it on the first enter; the second enter
// submits. Neither may insert a newline into the input.
func TestModelArgEnterNeverNewlines(t *testing.T) {
	m := modelCmdModel()
	m = typeStr(t, m, "/model glm")
	if m.menu == nil {
		t.Fatal("expected model-name completion menu")
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // complete the name
	m = tm.(*model)
	if m.input.LineCount() != 1 {
		t.Fatalf("completing a model name must not newline: value=%q", m.input.Value())
	}
	if m.input.Value() == "/model glm" {
		t.Fatalf("enter should have accepted the completion, still %q", m.input.Value())
	}
}
