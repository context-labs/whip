package config

import (
	"errors"
	"strings"
	"testing"
)

// catalogFixture isolates WHIP_HOME and seeds one cached provider catalog.
func catalogFixture(t *testing.T, prov string, models ...ModelInfoLite) {
	t.Helper()
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := SaveCatalogs(map[string]Catalog{prov: {Models: models}}); err != nil {
		t.Fatal(err)
	}
}

func cfgWithProviders(names ...string) *Config {
	c := &Config{
		Providers: map[string]Provider{},
		Models:    map[string]Model{},
	}
	for _, n := range names {
		c.Providers[n] = Provider{BaseURL: "https://" + n, API: "openai-completions"}
	}
	return c
}

// A model advertised by exactly one provider's catalog resolves without a
// config entry, with capabilities populated from the catalog.
func TestResolveCatalogFallbackSingleProvider(t *testing.T) {
	catalogFixture(t, "inference", ModelInfoLite{
		ID:                  "glm-5.2",
		ContextLength:       1000000,
		MaxCompletionTokens: 128000,
		ReasoningEfforts:    []string{"low", "high"},
		InputModalities:     []string{"text"},
	})
	cfg := cfgWithProviders("inference")

	p, m, id, err := cfg.Resolve("glm-5.2", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.BaseURL != "https://inference" || id != "glm-5.2" {
		t.Fatalf("route: %+v id=%q", p, id)
	}
	if m.Context != 1000000 || m.MaxOut != 128000 {
		t.Errorf("synthetic model should carry catalog context/maxOut, got %+v", m)
	}
	if m.Vision {
		t.Error("text-only catalog entry must not mark the model vision-capable")
	}
	if len(m.Providers) != 1 || m.Providers[0] != "inference" {
		t.Errorf("routing: %+v", m.Providers)
	}
}

// Vision is populated from the catalog's input_modalities.
func TestResolveCatalogFallbackVision(t *testing.T) {
	catalogFixture(t, "inference", ModelInfoLite{
		ID:              "kimi-k9",
		ContextLength:   256000,
		InputModalities: []string{"text", "image"},
	})
	cfg := cfgWithProviders("inference")
	_, m, _, err := cfg.Resolve("kimi-k9", "")
	if err != nil || !m.Vision {
		t.Fatalf("image-capable catalog entry should resolve with Vision=true: %+v %v", m, err)
	}
}

// A model id advertised by several providers without a pinned provider errors
// naming the candidates; pinning with a provider disambiguates.
func TestResolveCatalogFallbackAmbiguous(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := SaveCatalogs(map[string]Catalog{
		"alpha": {Models: []ModelInfoLite{{ID: "shared-model", ContextLength: 1000}}},
		"beta":  {Models: []ModelInfoLite{{ID: "shared-model", ContextLength: 2000}}},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := cfgWithProviders("alpha", "beta")

	_, _, _, err := cfg.Resolve("shared-model", "")
	if err == nil || !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), "beta") {
		t.Fatalf("ambiguity error must name both providers, got %v", err)
	}
	if !strings.Contains(err.Error(), "disambiguate") {
		t.Fatalf("error should instruct how to disambiguate, got %v", err)
	}

	p, m, _, err := cfg.Resolve("shared-model", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if p.BaseURL != "https://beta" || m.Context != 2000 {
		t.Fatalf("pinned provider should win with its catalog entry: %+v %+v", p, m)
	}
}

func TestResolveCatalogFallbackUsesDefaultProvider(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := SaveCatalogs(map[string]Catalog{
		"codex":     {Models: []ModelInfoLite{{ID: "gpt-5.6-terra", ContextLength: 1050000}}},
		"inference": {Models: []ModelInfoLite{{ID: "gpt-5.6-terra", ContextLength: 1000000}}},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := cfgWithProviders("codex", "inference")
	cfg.DefaultModel = "gpt-5.6-terra"
	cfg.DefaultProvider = "codex"

	p, m, id, err := cfg.Resolve("", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.BaseURL != "https://codex" || id != "gpt-5.6-terra" || m.Context != 1050000 {
		t.Fatalf("default provider should select the Codex catalog route: provider=%+v model=%+v id=%q", p, m, id)
	}
}

// Config entries stay authoritative: a configured model never falls through
// to the catalog, even when the catalog advertises the same id differently.
func TestResolveConfigWinsOverCatalog(t *testing.T) {
	catalogFixture(t, "inference", ModelInfoLite{ID: "kimi-k3", ContextLength: 999999, MaxCompletionTokens: 999999})
	cfg := cfgWithProviders("inference")
	cfg.Models["kimi-k3"] = Model{Providers: []string{"inference"}, Context: 131072}

	_, m, id, err := cfg.Resolve("kimi-k3", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Context != 131072 || m.MaxOut != 0 {
		t.Errorf("config entry must win over catalog values, got %+v", m)
	}
	if id != "kimi-k3" {
		t.Errorf("id should default to the map key, got %q", id)
	}
}

// Unknown ids still error, and catalogs from unconfigured providers are ignored.
func TestResolveCatalogFallbackMisses(t *testing.T) {
	catalogFixture(t, "ghost", ModelInfoLite{ID: "phantom-model", ContextLength: 1})
	cfg := cfgWithProviders("inference")

	if _, _, _, err := cfg.Resolve("nope", ""); err == nil {
		t.Fatal("unknown model must still error")
	}
	if _, _, _, err := cfg.Resolve("phantom-model", ""); err == nil {
		t.Fatal("catalog for an unconfigured provider must not resolve")
	}
}

// A total miss (neither config nor any catalog) is typed *UnknownModelError
// so startup entrypoints can force-refresh the catalogs and retry once; the
// other Resolve failures (unknown provider, ambiguous catalog id) stay
// untyped so they never trigger a pointless refresh.
func TestResolveUnknownModelErrorTyping(t *testing.T) {
	catalogFixture(t, "inference", ModelInfoLite{ID: "kimi-k3"})
	cfg := cfgWithProviders("inference")
	cfg.Models["glm-5.2-fast"] = Model{Providers: []string{"inference"}}

	_, _, _, err := cfg.Resolve("nope", "")
	var unknown *UnknownModelError
	if !errors.As(err, &unknown) {
		t.Fatalf("miss should be *UnknownModelError, got %T (%v)", err, err)
	}
	if unknown.Model != "nope" {
		t.Errorf("error should carry the missed name, got %q", unknown.Model)
	}
	want := `unknown model "nope" (configured: glm-5.2-fast; catalog models are listed by /model)`
	if err.Error() != want {
		t.Errorf("message changed: got %q want %q", err.Error(), want)
	}

	if _, _, _, err = cfg.Resolve("glm-5.2-fast", "ghost"); errors.As(err, &unknown) {
		t.Error("unknown provider must not be typed as a refreshable miss")
	}
}

// A catalog-only model served by a provider other than DefaultProvider still
// resolves: the catalog scan runs before the default provider applies.
func TestResolveCatalogModelIgnoresDefaultProviderPin(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if err := SaveCatalogs(map[string]Catalog{
		"other": {Models: []ModelInfoLite{{ID: "only-here", ContextLength: 1000}}},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := cfgWithProviders("openrouter", "other")
	cfg.DefaultProvider = "openrouter"
	p, _, id, err := cfg.Resolve("only-here", "")
	if err != nil || id != "only-here" || p.BaseURL != cfg.Providers["other"].BaseURL {
		t.Fatalf("catalog-only model on a non-default provider should resolve there: %+v %q %v", p, id, err)
	}
	// An explicit provider still pins the scan.
	if _, _, _, err := cfg.Resolve("only-here", "openrouter"); err == nil {
		t.Fatal("explicit provider that doesn't advertise the model should fail")
	}
}
