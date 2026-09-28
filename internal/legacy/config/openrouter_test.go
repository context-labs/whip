package config

import "testing"

func TestUpsertOpenRouterAddsProvider(t *testing.T) {
	cfg := &Config{
		Providers: map[string]Provider{
			"inference": {Name: "Inference.net", BaseURL: "https://api.inference.net/v1", API: "openai-completions"},
		},
		Models: map[string]Model{
			"kimi-k3": {Providers: []string{"inference"}, Context: 1048576},
		},
	}
	cfg.UpsertOpenRouter("sk-or-test", false)

	p, ok := cfg.Providers["openrouter"]
	if !ok {
		t.Fatal("openrouter provider missing after upsert")
	}
	if p.BaseURL != OpenRouterBaseURL || p.API != "openai-completions" {
		t.Errorf("unexpected provider shape: %+v", p)
	}
	if p.APIKey != "sk-or-test" || p.APIKeyEnv != "" {
		t.Errorf("literal mode should set apiKey only, got %+v", p)
	}
	// Other providers and models untouched.
	if _, ok := cfg.Providers["inference"]; !ok {
		t.Error("existing provider clobbered")
	}
	if _, ok := cfg.Models["kimi-k3"]; !ok {
		t.Error("existing model clobbered")
	}
}

func TestUpsertOpenRouterIdempotentAndModeSwitch(t *testing.T) {
	cfg := &Config{}
	cfg.UpsertOpenRouter("sk-or-one", false)
	cfg.UpsertOpenRouter("sk-or-two", true) // re-auth in env mode

	p := cfg.Providers["openrouter"]
	if p.APIKey != "" {
		t.Errorf("switching to env mode must clear the literal key, got %q", p.APIKey)
	}
	if p.APIKeyEnv != OpenRouterEnvVar {
		t.Errorf("env mode should set apiKeyEnv=%q, got %q", OpenRouterEnvVar, p.APIKeyEnv)
	}
	if len(cfg.Providers) != 1 {
		t.Errorf("re-auth must not duplicate providers: %d", len(cfg.Providers))
	}
}

func TestUpsertOpenRouterNilProviders(t *testing.T) {
	cfg := &Config{}
	cfg.UpsertOpenRouter("sk-or-x", false)
	if cfg.Providers["openrouter"].APIKey != "sk-or-x" {
		t.Error("upsert on nil Providers map failed")
	}
}

func TestOpenRouterConfigured(t *testing.T) {
	cfg := &Config{}
	if cfg.OpenRouterConfigured() {
		t.Error("empty config should not report configured")
	}
	cfg.UpsertOpenRouter("sk-or-x", false)
	if !cfg.OpenRouterConfigured() {
		t.Error("literal key should resolve")
	}

	cfg.UpsertOpenRouter("", true)
	t.Setenv(OpenRouterEnvVar, "")
	if cfg.OpenRouterConfigured() {
		t.Error("env mode without the var set should not report configured")
	}
	t.Setenv(OpenRouterEnvVar, "sk-or-env")
	if !cfg.OpenRouterConfigured() {
		t.Error("env mode with the var set should resolve")
	}
}

func TestTrimKey(t *testing.T) {
	cases := map[string]string{
		"  sk-or-x\n":     "sk-or-x",
		"Bearer sk-or-x":  "sk-or-x",
		"Bearer  sk-or-x": "sk-or-x",
		"sk-or-x":         "sk-or-x",
		"":                "",
	}
	for in, want := range cases {
		if got := TrimKey(in); got != want {
			t.Errorf("TrimKey(%q) = %q, want %q", in, got, want)
		}
	}
}
