package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
)

func TestPresetDiscoveryFallbackAndPublicCatalog(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "fixture")
	defer service.Close()
	for _, preset := range config.ProviderPresets() {
		if preset.ID == openaiauth.Provider {
			continue
		}
		t.Run(preset.ID, func(t *testing.T) {
			service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
				return nil, &net.DNSError{Err: "network unavailable private-key"}
			}
			models, discovery, err := service.discoverProviderModels(t.Context(), preset.ID, preset.Provider, "fake-key")
			if err != nil || len(models) == 0 || discovery.Status != "unverified" || !strings.Contains(discovery.Message, "bundled") || strings.Contains(discovery.Message, "private-key") {
				t.Fatalf("fallback: %+v %+v %v", models, discovery, err)
			}
			service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
				return []llm.ModelInfo{{ID: models[0].ID}}, nil
			}
			_, discovery, err = service.discoverProviderModels(t.Context(), preset.ID, preset.Provider, "fake-key")
			want := "loaded"
			if preset.ID == "deepinfra" {
				want = "unverified"
			}
			if err != nil || discovery.Status != want {
				t.Fatalf("loaded: %+v %v", discovery, err)
			}
		})
	}
}

type catalogTransport func(*http.Request) (*http.Response, error)

func (f catalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenRouterSetupAuthenticatesBeforeSavingKey(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Setenv("WHIP_HOME", t.TempDir())
			cfg := config.Default()
			cfg.UpsertOpenRouter("previous-key", false)
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			service := NewProviderService(t.Context(), "auth-fixture")
			defer service.Close()
			var paths []string
			previous := http.DefaultTransport
			http.DefaultTransport = catalogTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "openrouter.ai" || r.Header.Get("Authorization") != "Bearer submitted-key" {
					t.Fatal("incorrect authentication destination or credential")
				}
				paths = append(paths, r.URL.Path)
				code, body := http.StatusOK, `{"data":[{"id":"z-ai/glm-5.3"}]}`
				switch r.URL.Path {
				case "/api/v1/key":
					if r.Method != http.MethodGet {
						t.Fatal("credential validation must be read-only")
					}
					code, body = status, `{"data":{"label":"redacted"}}`
					if status != http.StatusOK {
						body = `{"error":{"message":"Missing Authentication header submitted-key"}}`
					}
				case "/api/v1/models":
					// This public endpoint succeeds even when the key is invalid.
				case "/api/v1/chat/completions":
					body = "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"
				default:
					t.Fatal("unexpected provider request")
				}
				return &http.Response{StatusCode: code, Status: fmt.Sprintf("%d %s", code, http.StatusText(code)), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = previous })
			before, err := service.ReadConfiguration()
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.SetProviderKey(t.Context(), ProviderKeySetup{Revision: before.Revision, Provider: "openrouter", Key: "submitted-key"})
			if status != http.StatusOK {
				if err == nil || !strings.Contains(err.Error(), "API key") || strings.Contains(err.Error(), "submitted-key") {
					t.Fatalf("invalid key accepted or exposed: %v", err)
				}
				after, readErr := service.ReadConfiguration()
				if readErr != nil || after.Revision != before.Revision || len(config.LoadCatalogs()) != 0 {
					t.Fatal("rejected credential changed configuration or cached the public catalog")
				}
				if strings.Join(paths, ",") != "/api/v1/key" {
					t.Fatalf("invalid key reached model discovery: %v", paths)
				}
				return
			}
			if err != nil || result.Discovery == nil || result.Discovery.ModelCount != 1 || result.Discovery.Status != "loaded" {
				t.Fatalf("valid key did not connect: %+v %v", result.Discovery, err)
			}
			cfg, err = config.Load()
			if err != nil {
				t.Fatal(err)
			}
			client, err := service.ModelClient(cfg.Providers["openrouter"])
			if err != nil {
				t.Fatal(err)
			}
			message, _, err := client.Stream(t.Context(), llm.Request{Model: "z-ai/glm-5.3"}, nil, nil, nil)
			if err != nil || message.Content != "hello" || strings.Join(paths, ",") != "/api/v1/key,/api/v1/models,/api/v1/chat/completions" {
				t.Fatalf("saved credential did not reach inference: %v %v", paths, err)
			}
		})
	}
}

func TestProviderValidationKeepsCustomAndOtherRoutesOnModels(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	for _, baseURL := range []string{
		"https://openrouter.ai/custom/v1",
		"https://openrouter.ai.example.test/api/v1",
		"https://api.cerebras.ai/v1",
	} {
		t.Run(baseURL, func(t *testing.T) {
			calls := 0
			http.DefaultTransport = catalogTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != baseURL+"/models" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Fatal("route used an unrelated credential check")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"chat-model"}]}`))}, nil
			})
			models, err := validateProviderModels(t.Context(), baseURL, "fixture-key")
			if err != nil || len(models) != 1 || calls != 1 {
				t.Fatalf("model discovery changed: %v (%d requests)", err, calls)
			}
		})
	}
}

func TestCerebrasLiveCatalogConnectRefreshAndRestart(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "catalog-fixture")
	defer service.Close()
	// Cerebras documents this sparse /v1/models shape. Include a future model
	// to ensure this test cannot be fixed by just extending the bundled subset.
	body := `{"data":[{"id":"gpt-oss-120b"},{"id":"qwen-3.8-27b"},{"id":"future-chat-model"}]}`
	calls := 0
	service.validate = func(ctx context.Context, baseURL, key string) ([]llm.ModelInfo, error) {
		client := llm.New(baseURL, key)
		client.HTTP = &http.Client{Transport: catalogTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Method != http.MethodGet || r.URL.String() != "https://api.cerebras.ai/v1/models" || r.Header.Get("Authorization") != "Bearer fixture-key" {
				t.Fatal("incorrect discovery destination or authentication")
			}
			return &http.Response{
				StatusCode: 200, Status: "200 OK", Header: http.Header{},
				Body: io.NopCloser(strings.NewReader(body)),
			}, nil
		})}
		return client.Models(ctx)
	}
	before, err := service.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	connected, err := service.SetProviderKey(t.Context(), ProviderKeySetup{
		Revision: before.Revision, Provider: "cerebras", Key: "fixture-key",
	})
	if err != nil || connected.Discovery == nil || connected.Discovery.ModelCount != 3 || calls != 1 {
		t.Fatalf("key connection lost live models: %+v %v (%d requests)", connected, err, calls)
	}
	want := []string{"gpt-oss-120b", "qwen-3.8-27b", "future-chat-model"}
	read := func(refresh bool, wantCalls int) {
		t.Helper()
		body, err := clientProviderCatalogsFor(t.Context(), service, refresh, "cerebras")
		if err != nil {
			t.Fatal(err)
		}
		var result protocol.ProviderCatalogsResult
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, model := range result.Catalogs["cerebras"].Models {
			got = append(got, model.ID)
		}
		if !reflect.DeepEqual(got, want) || calls != wantCalls || len(result.Errors) != 0 {
			t.Fatalf("picker catalog = %v, errors %v, requests %d (want %v, %d)", got, result.Errors, calls, want, wantCalls)
		}
	}
	read(false, 1)
	restarted := NewProviderService(t.Context(), "restarted-catalog-fixture")
	defer restarted.Close()
	restarted.validate = service.validate
	service = restarted
	read(false, 1)
	// An old binary cached a truncated list moments ago. Normal reads must
	// refresh it once, even though its 24-hour TTL has not elapsed.
	legacy := config.LoadCatalogs()
	old := legacy["cerebras"]
	old.DiscoveryVersion = 0
	old.Models = old.Models[:1]
	legacy["cerebras"] = old
	if err := config.SaveCatalogs(legacy); err != nil {
		t.Fatal(err)
	}
	read(false, 2)
	read(false, 2)
	body = `{"data":[{"id":"qwen-3.8-27b"},{"id":"newly-released-model"}]}`
	want = []string{"qwen-3.8-27b", "newly-released-model"}
	read(true, 3)
	read(false, 3)
	stale := config.LoadCatalogs()["cerebras"]
	stale.FetchedAt = time.Now().Add(-25 * time.Hour)
	if err := config.UpdateCatalog("cerebras", stale); err != nil {
		t.Fatal(err)
	}
	read(false, 4)
	// A transient outage must not replace the last live list with one bundled
	// model or mark the old cache as newly fetched.
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		return nil, &net.DNSError{Err: "offline"}
	}
	prior := config.LoadCatalogs()["cerebras"]
	cfg, _ := config.Load()
	if err := service.refreshCatalog(t.Context(), "cerebras", cfg.Providers["cerebras"]); err == nil {
		t.Fatal("outage was reported as fresh discovery")
	}
	if got := config.LoadCatalogs()["cerebras"]; !reflect.DeepEqual(got, prior) {
		t.Fatalf("outage replaced live cache: %+v", got)
	}
}

func TestEmptyLiveCatalogDoesNotInsertBundledModels(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "catalog-fixture")
	defer service.Close()
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) { return nil, nil }
	provider := config.Provider{BaseURL: "https://api.cerebras.ai/v1", API: "openai-completions"}
	models, _, err := service.discoverProviderModels(t.Context(), "cerebras", provider, "fixture-key")
	if !errors.Is(err, errNoCompatibleProviderModels) || len(models) != 0 {
		t.Fatalf("empty live list was replaced by bundled models: %+v %v", models, err)
	}
}

func TestPresetDiscoveryDoesNotBypassRejectionOrCustomRoute(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "fixture")
	defer service.Close()
	provider := config.Provider{BaseURL: config.OpenRouterBaseURL, API: "openai-completions"}
	for _, status := range []string{"400 Bad Request", "401 Unauthorized", "402 Payment Required", "403 Forbidden", "429 Too Many Requests"} {
		t.Run(status, func(t *testing.T) {
			service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
				return nil, &llm.HTTPError{Status: status}
			}
			models, _, err := service.discoverProviderModels(t.Context(), "openrouter", provider, "fake-key")
			if err == nil || len(models) != 0 {
				t.Fatalf("rejection bypassed: %+v %v", models, err)
			}
		})
	}
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) { return nil, context.Canceled }
	if _, _, err := service.discoverProviderModels(t.Context(), "openrouter", provider, "fake-key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		return nil, &net.DNSError{Err: "offline"}
	}
	provider.BaseURL += "/custom"
	if _, _, err := service.discoverProviderModels(t.Context(), "openrouter", provider, "fake-key"); err == nil {
		t.Fatal("custom endpoint inherited bundled catalog")
	}
	provider.BaseURL = config.OpenRouterBaseURL
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		return []llm.ModelInfo{{ID: "audio", SupportsTools: new(false), OutputModalities: []string{"audio"}}}, nil
	}
	models, _, err := service.discoverProviderModels(t.Context(), "openrouter", provider, "fake-key")
	if !errors.Is(err, errNoCompatibleProviderModels) || len(models) != 0 || providerValidationError(err) != errNoCompatibleProviderModels {
		t.Fatalf("incompatible live catalog was replaced or blamed on the key: %+v %v", models, err)
	}
}

func TestProviderKeySetupPersistsWithHonestFallback(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "fixture")
	defer service.Close()
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		return nil, &net.DNSError{Err: "offline"}
	}
	before, err := service.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	after, err := service.SetProviderKey(t.Context(), ProviderKeySetup{Revision: before.Revision, Provider: "groq", Key: "fixture-key"})
	if err != nil || after.Discovery == nil || after.Discovery.Status != "unverified" {
		t.Fatalf("setup = %+v %v", after, err)
	}
	cfg, err := config.Load()
	if err != nil || cfg.Providers["groq"].APIKey != "fixture-key" {
		t.Fatalf("saved key: %v", err)
	}
	if len(service.CatalogsFor(cfg)["groq"].Models) == 0 {
		t.Fatal("saved key has no model choices")
	}
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		return nil, &llm.HTTPError{Status: "401 Unauthorized", Body: "private"}
	}
	_, err = service.SetProviderKey(t.Context(), ProviderKeySetup{Revision: after.Revision, Provider: "groq", Key: "rejected-key"})
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("rejection: %v", err)
	}
	cfg, _ = config.Load()
	if cfg.Providers["groq"].APIKey != "fixture-key" {
		t.Fatal("rejected key replaced saved key")
	}
}

func TestProviderKeySetupSelectsAvailableEnvironmentAlias(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv("DEEPINFRA_API_KEY", "")
	t.Setenv("DEEPINFRA_TOKEN", "fixture-key")
	service := NewProviderService(t.Context(), "fixture")
	defer service.Close()
	service.validate = func(_ context.Context, endpoint, key string) ([]llm.ModelInfo, error) {
		if endpoint != "https://api.deepinfra.com/v1/openai" || key != "fixture-key" {
			t.Fatal("wrong environment key or destination")
		}
		return config.PresetModels("deepinfra"), nil
	}
	before, err := service.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SetProviderKey(t.Context(), ProviderKeySetup{Revision: before.Revision, Provider: "deepinfra", Environment: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil || cfg.Providers["deepinfra"].APIKeyEnv != "DEEPINFRA_TOKEN" || cfg.Providers["deepinfra"].APIKey != "" {
		t.Fatalf("saved environment reference: %+v %v", cfg.Providers["deepinfra"], err)
	}
}

func TestRefreshCatalogReplacesPriorMembershipWithEmptyLiveResponse(t *testing.T) {
	for _, kind := range []string{"empty", "all incompatible"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("WHIP_HOME", t.TempDir())
			cfg := config.Default()
			provider := config.Provider{Name: "Cerebras", BaseURL: "https://api.cerebras.ai/v1", API: "openai-completions", APIKey: "fixture-key"}
			cfg.Providers["cerebras"] = provider
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			prior := config.Catalog{FetchedAt: time.Now().Add(-time.Hour), BaseURL: provider.BaseURL, Models: []config.ModelInfoLite{{ID: "old-model"}}}
			if err := config.UpdateCatalog("cerebras", prior); err != nil {
				t.Fatal(err)
			}
			service := NewProviderService(t.Context(), "empty-live")
			defer service.Close()
			service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
				if kind == "all incompatible" {
					return []llm.ModelInfo{{ID: "non-chat-model", SupportsTools: new(false)}}, nil
				}
				return []llm.ModelInfo{}, nil
			}
			err := service.refreshCatalog(t.Context(), "cerebras", provider)
			if !errors.Is(err, errNoCompatibleProviderModels) {
				t.Fatalf("missing empty selection guidance: %v", err)
			}
			got, exists := config.LoadCatalogs()["cerebras"]
			if !exists || len(got.Models) != 0 || !got.FetchedAt.After(prior.FetchedAt) {
				t.Fatal("successful empty response retained prior/bundled membership")
			}
			if len(service.CatalogsFor(cfg)["cerebras"].Models) != 0 {
				t.Fatal("metadata enrichment repopulated an empty catalog")
			}
		})
	}
}

func TestSetProviderKeyDoesNotCacheModelsAfterNamedFileRotation(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv("CEREBRAS_API_KEY", "")
	filename := filepath.Join(t.TempDir(), "cerebras.key")
	if err := os.WriteFile(filename, []byte("first-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.ProviderKeySources = config.ProviderKeySources{KeyFiles: map[string]string{"CEREBRAS_API_KEY": filename}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	service := NewProviderService(t.Context(), "rotate-key")
	defer service.Close()
	service.validate = func(_ context.Context, endpoint, key string) ([]llm.ModelInfo, error) {
		if endpoint != "https://api.cerebras.ai/v1" || key != "first-fixture" {
			t.Fatal("validation used wrong named key")
		}
		if err := os.WriteFile(filename, []byte("second-fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
		return []llm.ModelInfo{{ID: "old-key-only-model"}}, nil
	}
	before, err := service.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	connected, err := service.SetProviderKey(t.Context(), ProviderKeySetup{Revision: before.Revision, Provider: "cerebras", Environment: true})
	if err != nil {
		t.Fatal(err)
	}
	if connected.Discovery == nil || !strings.Contains(connected.Discovery.Message, "credentials changed") {
		t.Fatal("rotated source lacked refresh guidance")
	}
	if _, exists := config.LoadCatalogs()["cerebras"]; exists {
		t.Fatal("published catalog fetched with an obsolete credential")
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Providers["cerebras"].APIKeyEnv != "CEREBRAS_API_KEY" || saved.Providers["cerebras"].APIKey != "" {
		t.Fatal("lost named reference on rotation")
	}
	client, err := service.ModelClient(saved.Providers["cerebras"])
	if err != nil || client.APIKey != "second-fixture" {
		t.Fatal("new client did not use rotated key")
	}
}

func TestModelClientUsesSuppliedConfigurationSnapshot(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv("SNAPSHOT_FIXTURE_KEY", "")
	directory := t.TempDir()
	oldFile, newFile := filepath.Join(directory, "old.key"), filepath.Join(directory, "new.key")
	if err := os.WriteFile(oldFile, []byte("old-route-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("new-route-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldCfg := config.Default()
	oldCfg.ProviderKeySources.KeyFiles = map[string]string{"SNAPSHOT_FIXTURE_KEY": oldFile}
	oldProvider := config.Provider{BaseURL: "https://old.example/v1", APIKeyEnv: "SNAPSHOT_FIXTURE_KEY"}
	oldCfg.Providers["custom"] = oldProvider
	newer := oldCfg.Snapshot()
	newer.ProviderKeySources.KeyFiles["SNAPSHOT_FIXTURE_KEY"] = newFile
	newer.Providers["custom"] = config.Provider{BaseURL: "https://new.example/v1", APIKeyEnv: "SNAPSHOT_FIXTURE_KEY"}
	if err := newer.Save(); err != nil {
		t.Fatal(err)
	}
	service := NewProviderService(t.Context(), "coherent-config")
	defer service.Close()
	client, err := service.ModelClient(oldProvider, oldCfg)
	if err != nil || client.BaseURL != oldProvider.BaseURL || client.APIKey != "old-route-fixture" {
		t.Fatal("old endpoint was combined with newer source configuration")
	}
	invoked := false
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) { invoked = true; return nil, nil }
	if err := service.refreshCatalog(t.Context(), "custom", oldProvider); err == nil || invoked {
		t.Fatal("obsolete route reached provider HTTP before the preflight guard")
	}
}
