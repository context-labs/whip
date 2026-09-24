package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
)

func TestDiscoveredFileKeyPersistsReferenceAndReachesInference(t *testing.T) {
	service := pickerInventoryFixture(t)
	filename := filepath.Join(t.TempDir(), "providers.env")
	writeKey := func(value string) {
		t.Helper()
		if err := os.WriteFile(filename, []byte("OPENROUTER_API_KEY="+value+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeKey("fixture-file-key")
	_, _, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.ProviderKeySources.EnvFiles = []string{filename}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before := providerEntry(t, service, "openrouter")
	if before.Status.Configured || !*before.Status.Available || before.Status.KeySource != "env_file" {
		t.Fatal("read-only inventory did not resolve a file-backed key")
	}
	list, err := service.DiscoverProviders(t.Context(), "", "")
	if err != nil || list.DiscoveryError != "" {
		t.Fatalf("discovery: %v %s", err, list.DiscoveryError)
	}
	entry := providerEntry(t, service, "openrouter")
	if !entry.Status.Configured || entry.Status.CredentialPath != filename || entry.Status.EnvironmentVariable != "OPENROUTER_API_KEY" {
		t.Fatal("discovery did not persist the named reference and provenance")
	}
	managed, err := service.ReadProvider("openrouter")
	if err != nil || managed.Credential.Mode != "environment" || managed.Credential.CredentialPath != filename {
		t.Fatal("management lost the editable named reference")
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("WHIP_HOME"), "config.json"))
	if err != nil || strings.Contains(string(data), "fixture-file-key") {
		t.Fatal("configuration persisted resolved credential material")
	}
	encoded, err := json.Marshal(list)
	if err != nil || strings.Contains(string(encoded), "fixture-file-key") {
		t.Fatal("provider inventory exposed resolved credential material")
	}
	second, err := service.DiscoverProviders(t.Context(), "", "")
	if err != nil || second.Revision != list.Revision {
		t.Fatal("no-op discovery changed the revision")
	}
	unchanged, _ := os.ReadFile(filepath.Join(os.Getenv("WHIP_HOME"), "config.json"))
	if string(unchanged) != string(data) {
		t.Fatal("no-op discovery rewrote configuration")
	}

	expectedKey := "fixture-file-key"
	var paths []string
	previous := http.DefaultTransport
	http.DefaultTransport = catalogTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "openrouter.ai" || r.Header.Get("Authorization") != "Bearer "+expectedKey {
			t.Fatal("file credential did not reach the expected provider")
		}
		paths = append(paths, r.URL.Path)
		body := `{"data":{"label":"fixture"}}`
		switch r.URL.Path {
		case "/api/v1/key":
		case "/api/v1/models":
			body = `{"data":[{"id":"new-live-model"}]}`
		case "/api/v1/chat/completions":
			body = "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"
		default:
			t.Fatal("unexpected provider request")
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	for _, key := range []string{"fixture-file-key", "fixture-rotated-key"} {
		writeKey(key)
		expectedKey = key
		// A new owner/constructor rereads references after a restart or reload.
		restarted := NewProviderService(t.Context(), key)
		cfg, loadErr := config.Load()
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		provider := cfg.Providers["openrouter"]
		if err := restarted.refreshCatalog(t.Context(), "openrouter", provider); err != nil {
			t.Fatal(err)
		}
		catalog := restarted.Catalogs()["openrouter"]
		if len(catalog.Models) != 1 || catalog.Models[0].ID != "new-live-model" {
			t.Fatal("bundled catalog replaced live membership")
		}
		client, err := restarted.ModelClient(provider)
		if err != nil {
			t.Fatal(err)
		}
		message, _, err := client.Stream(t.Context(), llm.Request{Model: "new-live-model"}, nil, nil, nil)
		restarted.Close()
		if err != nil || message.Content != "hello" {
			t.Fatalf("file-key inference: %v", err)
		}
	}
	if len(paths) != 6 {
		t.Fatal("expected authenticated discovery followed by inference on each construction")
	}
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	entry = providerEntry(t, service, "openrouter")
	if !entry.Status.Configured || *entry.Status.Available || len(entry.Status.Warnings) == 0 {
		t.Fatal("removed key source must retain its route with recovery information")
	}
}

func TestProviderDiscoveryRPCUsesHostSourcesAndPreservesOptOut(t *testing.T) {
	_, client, root, _ := providerBehaviorFixture(t)
	for _, preset := range config.ProviderPresets() {
		for _, name := range preset.EnvironmentVariables {
			t.Setenv(name, "")
		}
	}
	t.Setenv("GROQ_API_KEY", "fixture-discovered-key")
	list, err := client.DiscoverProviders(t.Context(), "selected-model", "groq")
	if err != nil || list.DiscoveryError != "" || list.Selection.Model != "selected-model" || list.Selection.Provider != "groq" {
		t.Fatalf("discover RPC: %v", err)
	}
	disabled := []string{"groq"}
	if _, err := client.UpdateConfiguration(t.Context(), protocol.ConfigurationUpdate{Revision: list.Revision, DisabledProviders: &disabled}); err != nil {
		t.Fatal(err)
	}
	list, err = root.DiscoverProviders(t.Context(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range list.Providers {
		if entry.ID == "groq" {
			if !entry.Status.Disabled || *entry.Status.Available {
				t.Fatal("discovery undid the saved opt-out")
			}
			return
		}
	}
	t.Fatal("disabled provider disappeared")
}

func TestDiscoveredOpenRouterKeyStillRequiresAuthentication(t *testing.T) {
	service := pickerInventoryFixture(t)
	t.Setenv("OPENROUTER_API_KEY", "fixture-invalid-key")
	if _, err := service.DiscoverProviders(t.Context(), "", ""); err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	http.DefaultTransport = catalogTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/key" {
			t.Fatal("public catalog fetched before rejecting the credential")
		}
		return &http.Response{StatusCode: http.StatusUnauthorized, Status: fmt.Sprintf("%d Unauthorized", 401), Body: io.NopCloser(strings.NewReader("fixture-invalid-key"))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.refreshCatalog(t.Context(), "openrouter", cfg.Providers["openrouter"]); err == nil || strings.Contains(err.Error(), "fixture-invalid-key") {
		t.Fatal("discovered invalid key was accepted or exposed")
	}
	if len(service.Catalogs()) != 0 {
		t.Fatal("failed authentication populated a bundled model catalog")
	}
}
