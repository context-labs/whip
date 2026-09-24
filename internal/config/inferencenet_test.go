package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpsertInferenceNetPreservesOtherProviders(t *testing.T) {
	cfg := &Config{Providers: map[string]Provider{"inference": {Name: "User-defined provider"}}}
	cfg.UpsertInferenceNet("", false)
	if cfg.Providers["inference"].Name != "User-defined provider" {
		t.Fatal("upsert rewrote a user-defined provider")
	}
	if _, ok := cfg.Providers[InferenceNetProvider]; !ok {
		t.Fatal("canonical provider missing")
	}
}

func TestUpsertInferenceNetModes(t *testing.T) {
	cfg := &Config{}
	cfg.UpsertInferenceNet("inf-literal", false)
	p := cfg.Providers[InferenceNetProvider]
	if p.APIKey != "inf-literal" || p.APIKeyEnv != "" {
		t.Errorf("literal mode: %+v", p)
	}
	cfg.UpsertInferenceNet("", true)
	p = cfg.Providers[InferenceNetProvider]
	if p.APIKeyEnv != InferenceNetEnvVar || p.APIKey != "" {
		t.Errorf("env mode: %+v", p)
	}
	// Machine-key login: no key material on the entry.
	cfg.UpsertInferenceNet("", false)
	p = cfg.Providers[InferenceNetProvider]
	if p.APIKey != "" || p.APIKeyEnv != "" {
		t.Errorf("machine-key mode should leave key fields empty: %+v", p)
	}
}

func TestProviderKeyFallsBackToStoredMachineKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIPCODE_HOME", home)
	keyFile := filepath.Join(home, "inference-net.json")
	if err := os.WriteFile(keyFile, []byte(`{"machineKey":"mk-stored"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := Provider{BaseURL: InferenceNetBaseURL}
	if got := p.Key(); got != "mk-stored" {
		t.Errorf("machine-key fallback: got %q", got)
	}
	// An explicit env var wins over the stored key.
	t.Setenv(InferenceNetEnvVar, "env-key")
	p.APIKeyEnv = InferenceNetEnvVar
	if got := p.Key(); got != "env-key" {
		t.Errorf("env should win: got %q", got)
	}
}
