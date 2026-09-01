package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/config"
)

func pickerCfg() *config.Config {
	return &config.Config{
		DefaultModel: "kimi-k3-fast",
		Providers:    map[string]config.Provider{"inference": {BaseURL: "https://api.inference.net/v1"}},
		Models: map[string]config.Model{
			"kimi-k3-fast": {Providers: []string{"inference"}},
		},
	}
}

// Catalog-advertised models without a config entry appear after the
// configured routes, marked fromCatalog; configured models never duplicate.
func TestBuildModelItemsAppendsCatalogRoutes(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	cfg := pickerCfg()
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"inference": {
			FetchedAt: time.Now(),
			BaseURL:   "https://api.inference.net/v1",
			Models: []config.ModelInfoLite{
				{ID: "kimi-k3-fast", ContextLength: 1048576}, // configured: skip
				{ID: "deepseek-v4-pro", ContextLength: 1048576},
				{ID: "glm-5.2", ContextLength: 1000000},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	items := buildModelItems(cfg)
	if len(items) != 3 {
		t.Fatalf("want 1 configured + 2 catalog routes, got %d: %+v", len(items), items)
	}
	if items[0].model != "kimi-k3-fast" || items[0].fromCatalog {
		t.Errorf("configured route first, unmarked: %+v", items[0])
	}
	if items[1].model != "deepseek-v4-pro" || !items[1].fromCatalog {
		t.Errorf("catalog routes sorted after configured: %+v", items[1])
	}
	if items[2].model != "glm-5.2" || !items[2].fromCatalog || items[2].provider != "inference" {
		t.Errorf("catalog route should carry its provider: %+v", items[2])
	}
}

// The picker view marks catalog routes (new) and shows the stale hint when
// the cache is past its TTL.
func TestModelPickerViewMarksCatalogAndStale(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	cfg := pickerCfg()
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"inference": {
			FetchedAt: time.Now().Add(-48 * time.Hour), // past the 24h TTL
			BaseURL:   "https://api.inference.net/v1",
			Models:    []config.ModelInfoLite{{ID: "deepseek-v4-pro"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	m := &model{cfg: cfg, modelName: "kimi-k3-fast", provName: "inference"}
	m.openModelPicker(false)
	if m.mpicker == nil {
		t.Fatal("picker should open")
	}
	view := m.modelPickerView()
	if !strings.Contains(view, "(new)") {
		t.Error("catalog routes should carry a (new) marker")
	}
	if !strings.Contains(view, "deepseek-v4-pro") {
		t.Error("catalog model should be listed")
	}
	if !strings.Contains(view, "stale") || !strings.Contains(view, "/model refresh") {
		t.Error("stale catalog should hint at /model refresh")
	}
}

// Selecting a catalog route runs the normal switchModel path (which persists
// the choice as the default on use).
func TestModelPickerSelectsCatalogRoute(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"inference": {
			FetchedAt: time.Now(),
			BaseURL:   "https://x",
			Models:    []config.ModelInfoLite{{ID: "deepseek-v4-pro", ContextLength: 1048576}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	m := compactCmdModel() // cfg providers carry an API key; switchModel needs it
	services := m.agent.Services
	m.openModelPicker(false)
	p := m.mpicker
	// walk to the catalog route
	for p.idx < len(p.items)-1 && !p.items[p.idx].fromCatalog {
		p.idx++
	}
	if !p.items[p.idx].fromCatalog {
		t.Fatal("no catalog route in picker")
	}
	tm, _ := m.modelPickerKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if m.modelName != "deepseek-v4-pro" || m.provName != "inference" {
		t.Fatalf("enter on a catalog route should switch models, got %s @ %s", m.modelName, m.provName)
	}
	if m.cfg.DefaultModel != "deepseek-v4-pro" {
		t.Errorf("the switch should persist as the new default, got %q", m.cfg.DefaultModel)
	}
	if m.agent.Services != services {
		t.Error("switching models replaced session-scoped tool services")
	}
	if _, ok := m.cfg.Models["deepseek-v4-pro"]; ok {
		t.Error("catalog routes must not be written into cfg.Models")
	}
}

// /model refresh forces a catalog refetch instead of switching models. The
// fixture's provider (BaseURL https://x, no DNS) fails its fetch, so the
// seeded catalog must survive untouched — failure keeps the stale cache.
func TestModelRefreshForcesRefetch(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"inference": {FetchedAt: time.Now(), BaseURL: "https://x", Models: []config.ModelInfoLite{{ID: "seed"}}},
	}); err != nil {
		t.Fatal(err)
	}
	m := modelCmdModel()
	m = typeStr(t, m, "/model refresh")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if m.mpicker != nil {
		t.Fatal("/model refresh must not open the picker")
	}
	if len(m.blocks) == 0 || !strings.Contains(m.blocks[len(m.blocks)-1].text, "refreshing model catalogs") {
		t.Fatalf("refresh notice should be appended, got %+v", m.blocks)
	}
	got := config.LoadCatalogs()["inference"]
	if len(got.Models) != 1 || got.Models[0].ID != "seed" {
		t.Errorf("failed refetch must keep the existing cache, got %+v", got)
	}
}

// refresh is offered as a /model argument, alongside catalog-advertised models.
func TestModelRefreshCompletion(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"inference": {
			FetchedAt: time.Now(), BaseURL: "https://x",
			Models: []config.ModelInfoLite{{ID: "catalog-only-model"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	m := modelCmdModel()
	_, cs := completions("/model r", m.modelCands(), m.providerCands(), nil, nil)
	if len(cs) != 1 || cs[0].Text != "refresh" {
		t.Fatalf("refresh should complete under /model, got %+v", cs)
	}
	_, cs = completions("/model cat", m.modelCands(), m.providerCands(), nil, nil)
	if len(cs) != 1 || cs[0].Text != "catalog-only-model" {
		t.Fatalf("catalog-only models should complete under /model, got %+v", cs)
	}
	// no provider completion after refresh
	_, cs = completions("/model refresh inf", m.modelCands(), m.providerCands(), nil, nil)
	if len(cs) != 0 {
		t.Errorf("refresh takes no provider argument, got %+v", cs)
	}
}

// staleCatalogs names providers with a missing or expired catalog.
func TestStaleCatalogs(t *testing.T) {
	cfg := pickerCfg()
	if got := staleCatalogs(cfg, map[string]config.Catalog{}); len(got) != 1 || got[0] != "inference" {
		t.Errorf("missing catalog is stale: %v", got)
	}
	fresh := map[string]config.Catalog{"inference": {FetchedAt: time.Now()}}
	if got := staleCatalogs(cfg, fresh); len(got) != 0 {
		t.Errorf("fresh catalog is not stale: %v", got)
	}
}
