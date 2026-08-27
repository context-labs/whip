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
