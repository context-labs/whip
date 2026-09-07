package config

import "testing"

func TestUpsertCodexAddsSelectableRoute(t *testing.T) {
	cfg := &Config{
		DefaultModel: "kimi-k3-fast",
		Providers: map[string]Provider{
			"inference": {Name: "Inference.net", BaseURL: "https://api.inference.net/v1", API: "openai-completions"},
		},
		Models: map[string]Model{
			"kimi-k3-fast": {Providers: []string{"inference"}, Context: 1048576},
		},
	}

	cfg.UpsertCodex()

	p, ok := cfg.Providers[CodexProviderName]
	if !ok {
		t.Fatal("codex provider missing after upsert")
	}
	if p.Name != "Codex" || p.BaseURL != CodexBaseURL || p.API != "openai-codex-responses" || p.Auth != "codex" {
		t.Errorf("unexpected codex provider: %+v", p)
	}
	m, ok := cfg.Models[CodexDefaultModel]
	if !ok {
		t.Fatal("codex model route missing after upsert")
	}
	if len(m.Providers) != 1 || m.Providers[0] != CodexProviderName {
		t.Errorf("providers = %v, want [%s]", m.Providers, CodexProviderName)
	}
	if m.Context != CodexDefaultContext || m.MaxOut != CodexDefaultMaxOut {
		t.Errorf("limits = context %d maxOut %d, want %d %d", m.Context, m.MaxOut, CodexDefaultContext, CodexDefaultMaxOut)
	}
	if cfg.DefaultModel != "kimi-k3-fast" {
		t.Errorf("upsert changed default model to %q", cfg.DefaultModel)
	}
	if _, ok := cfg.Providers["inference"]; !ok {
		t.Error("upsert clobbered an existing provider")
	}
}

func TestUpsertCodexPreservesExistingRoute(t *testing.T) {
	cfg := &Config{
		Providers: map[string]Provider{
			CodexProviderName: {Name: "old"},
			"alternate":       {Name: "Alternate"},
		},
		Models: map[string]Model{
			CodexDefaultModel: {
				Providers: []string{"alternate", CodexProviderName},
				Context:   123,
				MaxOut:    456,
			},
		},
	}

	cfg.UpsertCodex()
	cfg.UpsertCodex()

	m := cfg.Models[CodexDefaultModel]
	if len(m.Providers) != 2 || m.Providers[0] != "alternate" || m.Providers[1] != CodexProviderName {
		t.Errorf("route providers changed or duplicated: %v", m.Providers)
	}
	if m.Context != 123 || m.MaxOut != 456 {
		t.Errorf("explicit limits changed: %+v", m)
	}
	if got := cfg.Providers[CodexProviderName]; got.BaseURL != CodexBaseURL || got.Auth != "codex" {
		t.Errorf("codex provider was not refreshed: %+v", got)
	}
}

func TestUpsertCodexInitializesEmptyConfig(t *testing.T) {
	cfg := &Config{}

	cfg.UpsertCodex()

	if cfg.Providers[CodexProviderName].Auth != "codex" {
		t.Fatalf("provider = %+v", cfg.Providers[CodexProviderName])
	}
	model := cfg.Models[CodexDefaultModel]
	if len(model.Providers) != 1 || model.Providers[0] != CodexProviderName || model.Context != CodexDefaultContext || model.MaxOut != CodexDefaultMaxOut {
		t.Fatalf("model = %+v", model)
	}
}

func TestRemoveProviderDropsRoutesPinsAndCatalog(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := SaveCatalogs(map[string]Catalog{"codex": {Models: []ModelInfoLite{{ID: "gpt-5.5"}}}, "openrouter": {}}); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		DefaultProvider: "codex",
		CompactProvider: "codex",
		TaskProvider:    "openrouter",
		Providers:       map[string]Provider{"codex": {}, "openrouter": {}},
		Models: map[string]Model{
			"gpt-5.5": {Providers: []string{"codex"}},
			"shared":  {Providers: []string{"codex", "openrouter"}},
		},
	}
	cfg.RemoveProvider("codex")
	if _, ok := cfg.Providers["codex"]; ok {
		t.Fatal("provider entry should be gone")
	}
	if _, ok := cfg.Models["gpt-5.5"]; ok {
		t.Fatal("route with no remaining provider should be dropped")
	}
	if got := cfg.Models["shared"].Providers; len(got) != 1 || got[0] != "openrouter" {
		t.Fatalf("shared route providers = %v", got)
	}
	if cfg.DefaultProvider != "" || cfg.CompactProvider != "" || cfg.TaskProvider != "openrouter" {
		t.Fatalf("pins: default=%q compact=%q task=%q", cfg.DefaultProvider, cfg.CompactProvider, cfg.TaskProvider)
	}
	cats := LoadCatalogs()
	if _, ok := cats["codex"]; ok || len(cats) != 1 {
		t.Fatalf("catalogs after remove = %v", cats)
	}
}

func TestNormalizeRenamesLegacyCodexProvider(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := SaveCatalogs(map[string]Catalog{"codex": {Models: []ModelInfoLite{{ID: "gpt-5.5"}}}}); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		DefaultProvider: "codex",
		Providers: map[string]Provider{
			"codex":  {BaseURL: CodexBaseURL, API: "openai-codex-responses", Auth: "codex"},
			"custom": {API: "openai-completions"},
		},
		Models: map[string]Model{"gpt-5.5": {Providers: []string{"codex"}}},
	}
	cfg.normalize()
	if _, ok := cfg.Providers["codex"]; ok {
		t.Fatal("legacy codex key should be renamed")
	}
	if p := cfg.Providers[CodexProviderName]; p.Auth != "codex" {
		t.Fatalf("renamed provider = %+v", p)
	}
	if got := cfg.Models["gpt-5.5"].Providers; len(got) != 1 || got[0] != CodexProviderName {
		t.Fatalf("route providers = %v", got)
	}
	if cfg.DefaultProvider != CodexProviderName {
		t.Fatalf("default provider = %q", cfg.DefaultProvider)
	}
	if cats := LoadCatalogs(); cats[CodexProviderName].Models == nil || len(cats) != 1 {
		t.Fatalf("catalog should move with the provider: %v", cats)
	}
	// An unrelated provider that happens to be named "codex" is left alone.
	other := &Config{Providers: map[string]Provider{"codex": {API: "openai-completions"}}}
	other.normalize()
	if _, ok := other.Providers["codex"]; !ok {
		t.Fatal("non-codex-API provider named codex must not be renamed")
	}
}
