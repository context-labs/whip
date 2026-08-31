package tui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

func compactCmdModel() *model {
	// NOTE: any test that drives setEffort/switchModel/compactCommand writes
	// through cfg.Save(); TestMain points WHIP_HOME at a scratch dir so
	// those writes can never reach the real ~/.whip/config.json.
	// serve the compaction summary so a bare /compact completes in-test
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"sim"}}]}`))
	}))
	m := &model{
		input:   newInput(),
		mouseOn: true, // matches the Run() default (wheel scroll + native drag-copy)
		agent:   agent.New(llm.New(srv.URL, "k"), "kimi-k3-fast", 100, "sys"),
		cfg: &config.Config{
			DefaultModel: "kimi-k3-fast",
			Providers:    map[string]config.Provider{"inference": {BaseURL: "https://x", APIKey: "k"}},
			Models: map[string]config.Model{
				"kimi-k3-fast": {Providers: []string{"inference"}},
				"glm-5.2-fast": {Providers: []string{"inference"}},
				// the built-in compaction default, routable on inference
				config.DefaultCompactModel: {Providers: []string{"inference"}},
			},
		},
		modelName: "kimi-k3-fast",
		provName:  "inference",
		catalogs: map[string]config.Catalog{
			"inference": {Models: []config.ModelInfoLite{
				{ID: "kimi-k3-fast", ContextLength: 131072},
			}},
		},
	}
	m.width = 80
	m.input.SetWidth(78)
	return m
}

// Regression guard for the config corruption bug: running a persistence
// command from a test must write under the isolated WHIP_HOME, never the
// user's real ~/.whip.
func TestCompactCommandNeverTouchesRealHome(t *testing.T) {
	m := compactCmdModel()
	m.compactCommand([]string{"glm-5.2-fast"}) // triggers cfg.Save()
	dir := os.Getenv("WHIP_HOME")
	if dir == "" || dir == filepath.Join(os.Getenv("HOME"), ".whip") {
		t.Fatalf("tests must run with an isolated WHIP_HOME, got %q", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("expected the save to land under WHIP_HOME: %v", err)
	}
}

func TestCompactCommandSelectsModel(t *testing.T) {
	m := compactCmdModel()
	m.compactCommand([]string{"glm-5.2-fast"})
	if m.compactModel != "glm-5.2-fast" || m.compactProv != "" {
		t.Fatalf("compact model state: %q @ %q", m.compactModel, m.compactProv)
	}
	if m.agent.CompactModel != "glm-5.2-fast" || m.agent.CompactClient == nil {
		t.Fatalf("agent should summarize with glm-5.2-fast on its own client")
	}
	if m.cfg.CompactModel != "glm-5.2-fast" {
		t.Fatalf("config should persist the pick, got %q", m.cfg.CompactModel)
	}
	m.compactCommand([]string{"off"})
	if m.compactModel != "" || m.agent.CompactModel != config.DefaultCompactModel || m.agent.CompactClient == nil {
		t.Fatalf("off should restore the default compaction model: %q", m.compactModel)
	}
	if !strings.Contains(m.blocks[len(m.blocks)-1].text, "default ("+config.DefaultCompactModel+")") {
		t.Fatalf("off should note the default, got %v", m.blocks[len(m.blocks)-1].text)
	}
}

// An empty compactModel resolves the built-in default from the config at
// apply time, so users who never picked one compact on deepseek-v4-flash.
func TestCompactModelEmptyResolvesDefault(t *testing.T) {
	m := compactCmdModel()
	m.applyCompactModel()
	if m.agent.CompactModel != config.DefaultCompactModel || m.agent.CompactClient == nil {
		t.Fatalf("empty compactModel should resolve the default, got %q", m.agent.CompactModel)
	}
}

// When the default model isn't in the user's config, the override clears and
// compaction falls back to the conversation's own model — no error note.
func TestCompactModelDefaultFallsBack(t *testing.T) {
	m := compactCmdModel()
	delete(m.cfg.Models, config.DefaultCompactModel)
	blocks := len(m.blocks)
	m.applyCompactModel()
	if m.agent.CompactClient != nil || m.agent.CompactModel != "" {
		t.Fatal("unresolvable default should fall back to the current model")
	}
	if len(m.blocks) != blocks {
		t.Fatal("a missing default should not nag — only picked models earn an error note")
	}
}

func TestCompactCommandRejectsUnknownModel(t *testing.T) {
	m := compactCmdModel()
	m.compactCommand([]string{"nope"})
	if m.compactModel != "" || m.agent.CompactModel != "" {
		t.Fatal("unknown model must not become the compaction model")
	}
	if !strings.Contains(m.blocks[len(m.blocks)-1].text, "unknown model") {
		t.Fatalf("expected an error note, got %v", m.blocks)
	}
}

// A catalog-advertised id with no config entry is a valid pick: the catalog
// fallback in Resolve routes it to the advertising provider.
func TestCompactCommandSelectsCatalogModel(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"inference": {Models: []config.ModelInfoLite{{ID: "deepseek-v4-pro", ContextLength: 1048576}}},
	}); err != nil {
		t.Fatal(err)
	}
	m := compactCmdModel()
	m.compactCommand([]string{"deepseek-v4-pro"})
	if m.compactModel != "deepseek-v4-pro" || m.agent.CompactModel != "deepseek-v4-pro" || m.agent.CompactClient == nil {
		t.Fatalf("catalog model should be picked and applied: %q / %q", m.compactModel, m.agent.CompactModel)
	}
	if m.cfg.CompactModel != "deepseek-v4-pro" {
		t.Fatalf("config should persist the catalog pick, got %q", m.cfg.CompactModel)
	}
	if !strings.Contains(m.blocks[len(m.blocks)-1].text, "deepseek-v4-pro @ inference") {
		t.Fatalf("the note should name the resolved provider, got %v", m.blocks[len(m.blocks)-1].text)
	}
}

// A typo of a catalog id resolves fuzzy instead of dying on "unknown model".
func TestCompactCommandResolvesFuzzy(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"inference": {Models: []config.ModelInfoLite{{ID: "deepseek-v4-pro", ContextLength: 1048576}}},
	}); err != nil {
		t.Fatal(err)
	}
	m := compactCmdModel()
	m.compactCommand([]string{"deepseek-v4-pr"})
	if m.compactModel != "deepseek-v4-pro" {
		t.Fatalf("a fuzzy hit should pick the catalog model, got %q", m.compactModel)
	}
}

func TestContextLimitFromCatalog(t *testing.T) {
	m := compactCmdModel()
	if got := m.contextLimitFor("inference", "kimi-k3-fast"); got != 131072 {
		t.Fatalf("contextLimitFor: %d", got)
	}
	if got := m.contextLimitFor("inference", "unknown"); got != 0 {
		t.Fatalf("unknown model: %d", got)
	}
	// a fresh /models fetch re-resolves the agent's limit
	cats := map[string]config.Catalog{
		"inference": {Models: []config.ModelInfoLite{{ID: "kimi-k3-fast", ContextLength: 262144}}},
	}
	m.updateCatalogs(cats)
	if m.agent.ContextLimit != 262144 {
		t.Fatalf("agent limit should follow the catalog, got %d", m.agent.ContextLimit)
	}
}

// Bare /compact with no history reports there's nothing to fold rather than
// touching the compaction-model selection. (The busy path is exercised
// end-to-end in the running TUI; here m.prog is nil so we stay on the
// synchronous error branch.)
func TestCompactBareKeepsSelection(t *testing.T) {
	m := compactCmdModel()
	m.compactModel, m.compactProv = "glm-5.2-fast", ""
	m.applyCompactModel()
	m.busy = true // busy path: synchronous, never starts the goroutine
	m.command("/compact")
	if m.compactModel != "glm-5.2-fast" || m.agent.CompactModel != "glm-5.2-fast" {
		t.Fatal("bare /compact must not change the compaction-model selection")
	}
}

func TestCompactThresholdFor(t *testing.T) {
	cases := []struct {
		pct  int
		want float64
	}{
		{0, 0.5},   // unset → built-in default
		{70, 0.7},  // user preference
		{5, 0.1},   // clamped to the floor
		{99, 0.9},  // clamped to the ceiling
		{-30, 0.1}, // garbage clamps too
	}
	for _, tc := range cases {
		cfg := &config.Config{CompactPct: tc.pct}
		if got := compactThresholdFor(cfg); got != tc.want {
			t.Errorf("compactThresholdFor(%d) = %v, want %v", tc.pct, got, tc.want)
		}
	}
}

func TestSetCompactPct(t *testing.T) {
	m := compactCmdModel()
	m.agent.CompactThreshold = 0.5

	m.setCompactPct(60)
	if m.agent.CompactThreshold != 0.6 || m.cfg.CompactPct != 60 || m.compactPct() != 60 {
		t.Fatalf("setCompactPct(60): agent=%v cfg=%d", m.agent.CompactThreshold, m.cfg.CompactPct)
	}

	m.setCompactPct(120) // clamps to the 90 ceiling
	if m.agent.CompactThreshold != 0.9 || m.cfg.CompactPct != 90 {
		t.Fatalf("setCompactPct(120) should clamp to 90: agent=%v cfg=%d", m.agent.CompactThreshold, m.cfg.CompactPct)
	}
	m.setCompactPct(0) // clamps to the 10 floor
	if m.agent.CompactThreshold != 0.1 || m.cfg.CompactPct != 10 {
		t.Fatalf("setCompactPct(0) should clamp to 10: agent=%v cfg=%d", m.agent.CompactThreshold, m.cfg.CompactPct)
	}
}
