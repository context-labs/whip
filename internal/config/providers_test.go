package config

import (
	"os"
	"path/filepath"
	"testing"
)

func isolateProviderEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv(InferenceNetEnvVar, "")
	t.Setenv(OpenRouterEnvVar, "")
}

func TestEffectiveProvidersNeverPersistDiscovery(t *testing.T) {
	isolateProviderEnvironment(t)
	cfg, revision, err := ReadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	filename, err := path()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(OpenRouterEnvVar, "environment-fixture-key")
	routes := cfg.EffectiveProviders()
	if routes["openrouter"].APIKeyEnv != OpenRouterEnvVar || routes["openrouter"].APIKey != "" {
		t.Fatal("discovery did not retain an environment reference")
	}
	if _, saved := cfg.Providers["openrouter"]; saved {
		t.Fatal("discovery mutated config")
	}
	_, nextRevision, err := ReadVersioned()
	if err != nil || nextRevision != revision {
		t.Fatalf("discovery changed revision: %v", err)
	}
	after, err := os.ReadFile(filename)
	if err != nil || string(after) != string(before) {
		t.Fatal("discovery wrote config")
	}
	if _, _, err := UpdateVersioned(revision, func(c *Config) error { c.MaxRetries = 7; return nil }); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := reloaded.Providers["openrouter"]; exists {
		t.Fatal("unrelated save persisted discovery")
	}
	t.Setenv(OpenRouterEnvVar, " ")
	if _, exists := reloaded.EffectiveProviders()["openrouter"]; exists {
		t.Fatal("empty key created route")
	}
}

func TestEffectiveProvidersRespectExplicitRoutesAndDisabling(t *testing.T) {
	isolateProviderEnvironment(t)
	t.Setenv(OpenRouterEnvVar, "ambient-key")
	saved := Provider{BaseURL: "https://custom.example/v1", API: "openai-completions", APIKey: "saved-key"}
	cfg := &Config{Providers: map[string]Provider{"openrouter": saved}, Models: map[string]Model{
		"model": {Providers: []string{"openrouter"}},
	}}
	if got := cfg.EffectiveProviders()["openrouter"]; got != saved {
		t.Fatal("discovery overrode explicit route")
	}
	cfg.DisabledProviders = []string{"openrouter"}
	if _, _, _, err := cfg.Resolve("model", "openrouter"); err == nil {
		t.Fatal("disabled route resolved")
	}
	snapshot := cfg.Snapshot()
	cfg.EnableProvider("openrouter")
	if len(snapshot.DisabledProviders) != 1 {
		t.Fatal("snapshot aliased disabled list")
	}
	if _, _, _, err := cfg.Resolve("model", "openrouter"); err != nil {
		t.Fatal(err)
	}
}

func TestProviderKeyStatusMatchesResolutionWithoutExecutingCommands(t *testing.T) {
	isolateProviderEnvironment(t)
	t.Setenv("WHIP_FIXTURE_PROVIDER_KEY", "environment-key")
	for _, test := range []struct {
		name      string
		provider  Provider
		source    string
		available bool
	}{
		{"literal", Provider{APIKey: "saved-key"}, "literal", true},
		{"environment wins", Provider{APIKeyEnv: "WHIP_FIXTURE_PROVIDER_KEY", APIKey: "saved-key"}, "environment", true},
		{"literal fallback", Provider{APIKeyEnv: "WHIP_MISSING_PROVIDER_KEY", APIKey: "saved-key"}, "literal", true},
		{"missing environment", Provider{APIKeyEnv: "WHIP_MISSING_PROVIDER_KEY"}, "environment", false},
		{"reference", Provider{APIKey: "${WHIP_FIXTURE_PROVIDER_KEY}"}, "environment", true},
		{"literal dollar", Provider{APIKey: "$not-an-env!"}, "literal", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			status := test.provider.KeyStatus()
			key, err := test.provider.ResolveKey()
			if err != nil || status.Source != test.source || status.Available != test.available || (key != "") != test.available {
				t.Fatalf("incorrect key status: %+v %v", status, err)
			}
		})
	}
	marker := filepath.Join(t.TempDir(), "secret-command-ran")
	status := (Provider{APIKey: "!touch " + marker}).KeyStatus()
	if status.Source != "command" || status.Available {
		t.Fatalf("command status: %+v", status)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("status executed secret command")
	}
}

func TestProviderAccountFallbackOnlyUsesItsOwnEndpoint(t *testing.T) {
	isolateProviderEnvironment(t)
	directory, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "inference-net.json"), []byte(`{"machineKey":"machine-fixture-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	route := Provider{BaseURL: InferenceNetBaseURL, APIKeyEnv: InferenceNetEnvVar}
	if status := route.KeyStatus(); status.Source != "machine" || !status.Available {
		t.Fatalf("fallback: %+v", status)
	}
	route.BaseURL = "https://api.inference.net.attacker.example/v1"
	if status := route.KeyStatus(); status.Available {
		t.Fatal("account key leaked to lookalike endpoint")
	}
}
