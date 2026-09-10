package daemon

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/context-labs/whip/internal/config"
)

func pickerInventoryFixture(t *testing.T) *ProviderService {
	t.Helper()
	service := providerConnectionsFixture(t)
	t.Chdir(t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_BASE", "WHIP_OPENCODE_ROUTING_OVERRIDE", "OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG_CONTENT", "OPENCODE_DB", "OPENCODE_TEST_HOME", "OPENCODE_TEST_MANAGED_CONFIG_DIR"} {
		t.Setenv(name, "")
	}
	for _, preset := range config.ProviderPresets() {
		for _, name := range preset.EnvironmentVariables {
			t.Setenv(name, "")
		}
	}
	return service
}

func TestProviderListPresetMetadataAndEndpointCollisions(t *testing.T) {
	service := pickerInventoryFixture(t)
	list, err := service.ListProviders()
	if err != nil || len(list.Providers) != 11 {
		t.Fatalf("inventory: %v", err)
	}
	for _, id := range []string{"openai", "openai-codex"} {
		entry := providerEntry(t, service, id)
		if entry.Family != "openai" || entry.Category != "popular" {
			t.Fatal("OpenAI execution routes omitted their display family")
		}
	}
	entry := providerEntry(t, service, "groq")
	if entry.Category != "providers" || entry.KeyURL == "" || entry.Custom || !slices.Equal(entry.Methods, []string{"api_key"}) {
		t.Fatal("preset omitted its single-key presentation metadata")
	}
	_, _, err = config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers["openai"] = config.Provider{Name: "Existing proxy", BaseURL: "https://proxy.example/v1", APIKey: "private-proxy-key"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	entry = providerEntry(t, service, "openai")
	if !entry.Custom || entry.Family != "" || entry.KeyURL != "" || entry.Name != "Existing proxy" {
		t.Fatal("new preset hid the saved custom route")
	}
}

func TestProviderListIgnoresOpenCodeKeys(t *testing.T) {
	service := pickerInventoryFixture(t)
	directory := filepath.Join(os.Getenv("XDG_DATA_HOME"), "opencode")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(directory, "auth.json")
	auth := []byte(`{"groq":{"type":"api","key":"private-opencode-fixture"}}`)
	if err := os.WriteFile(filename, auth, 0o600); err != nil {
		t.Fatal(err)
	}
	entry := providerEntry(t, service, "groq")
	if entry.Status.Configured || entry.Status.Available == nil || *entry.Status.Available {
		t.Fatal("OpenCode credentials created an available connection")
	}
	if _, err := service.DiscoverProviders(t.Context(), "", ""); err != nil {
		t.Fatal(err)
	}
	entry = providerEntry(t, service, "groq")
	if entry.Status.Configured || *entry.Status.Available {
		t.Fatal("discovery imported OpenCode credentials")
	}
	unchanged, err := os.ReadFile(filename)
	if err != nil || string(unchanged) != string(auth) {
		t.Fatal("Whip modified OpenCode credentials")
	}
}

func TestProviderListEnvironmentAliasesAndOpenAIRouting(t *testing.T) {
	service := pickerInventoryFixture(t)
	t.Setenv("DEEPINFRA_TOKEN", "private-alias-fixture")
	t.Setenv("OPENAI_API_KEY", "private-openai-fixture")
	t.Setenv("OPENAI_BASE_URL", "https://proxy.example/v1")
	entry := providerEntry(t, service, "deepinfra")
	if entry.Status.EnvironmentVariable != "DEEPINFRA_TOKEN" || entry.Status.Available == nil || !*entry.Status.Available {
		t.Fatal("DeepInfra alias not detected")
	}
	entry = providerEntry(t, service, "openai")
	if entry.Status.Available == nil || *entry.Status.Available || slices.Contains(entry.Methods, "environment") {
		t.Fatal("OpenAI key was offered for a conflicting destination")
	}
	entry = providerEntry(t, service, "openai-codex")
	if entry.Status.Available != nil && *entry.Status.Available {
		t.Fatal("API key was interpreted as ChatGPT subscription credentials")
	}
}
