package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferencenet"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
)

func providerConnectionsFixture(t *testing.T) *ProviderService {
	t.Helper()
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.InferenceNetEnvVar, "")
	t.Setenv(config.OpenRouterEnvVar, "")
	service := NewProviderService(t.Context(), "connections")
	t.Cleanup(service.Close)
	return service
}

func providerEntry(t *testing.T, service *ProviderService, id string) protocol.ProviderEntry {
	t.Helper()
	list, err := service.ListProviders()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range list.Providers {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("missing provider %q", id)
	return protocol.ProviderEntry{}
}

func TestProviderInventoryReportsWinningSourceWithoutExecutingSecrets(t *testing.T) {
	service := providerConnectionsFixture(t)
	t.Setenv(config.OpenRouterEnvVar, "private-environment-key")
	marker := filepath.Join(t.TempDir(), "executed")
	cfg, _, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers["command"] = config.Provider{BaseURL: "https://example.test/v1", APIKey: "!touch " + marker}
		cfg.Providers["custom"] = config.Provider{Name: "Custom", BaseURL: "https://example.test/v1", APIKey: "private-saved-key"}
		cfg.Providers["bad-url"] = config.Provider{BaseURL: "not-a-url", APIKey: "private-saved-key"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(os.Getenv("WHIP_HOME"), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		t.Error("inventory made an upstream request")
		return nil, nil
	}
	list, err := service.ListProviders()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(list)
	if err != nil || strings.Contains(string(encoded), "private-") {
		t.Fatal("inventory exposed a credential")
	}
	for id, want := range map[string]struct {
		source, state string
		available     bool
	}{
		"inference-net": {"environment", "key_required", false},
		"openrouter":    {"environment", "connected", true},
		"custom":        {"literal", "connected", true},
		"command":       {"command", "unchecked", false},
		"bad-url":       {"literal", "configuration_error", false},
	} {
		entry := providerEntry(t, service, id)
		if entry.Status.KeySource != want.source || entry.Status.AuthState != want.state || entry.Status.Available == nil || *entry.Status.Available != want.available {
			t.Fatalf("%s status = %+v", id, entry.Status)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inventory executed a secret command")
	}
	after, _ := os.ReadFile(filepath.Join(os.Getenv("WHIP_HOME"), "config.json"))
	if string(before) != string(after) {
		t.Fatal("inventory changed saved configuration")
	}
	if _, ok := cfg.Providers["openrouter"]; ok {
		t.Fatal("discovery persisted")
	}
}

func TestProviderCatalogRefreshIsExplicitAndRetainsLastGoodModels(t *testing.T) {
	service := providerConnectionsFixture(t)
	var requests atomic.Int32
	var fail atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		if fail.Load() {
			http.Error(w, "private-provider-error", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"new-model"}]}`))
	}))
	t.Cleanup(upstream.Close)
	_, _, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers["custom"] = config.Provider{BaseURL: upstream.URL, APIKey: "fixture-key"}
		cfg.DefaultModel, cfg.DefaultProvider = "alias", ""
		cfg.Models["alias"] = config.Model{Providers: []string{"custom"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := service.ListProviders()
	if err != nil || list.DefaultProvider != "custom" {
		t.Fatal("inventory omitted the implicit default route")
	}
	if err := config.UpdateCatalog("custom", config.Catalog{BaseURL: upstream.URL, FetchedAt: time.Now(), Models: []config.ModelInfoLite{{ID: "cached-model"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := queryProviderCatalogs(t.Context(), service, json.RawMessage(`{}`)); err != nil || requests.Load() != 0 {
		t.Fatal("ordinary query did not reuse fresh catalog")
	}
	if _, err := queryProviderCatalogs(t.Context(), service, json.RawMessage(`{"refresh":true}`)); err != nil || requests.Load() != 1 {
		t.Fatal("explicit refresh did not fetch models")
	}
	fail.Store(true)
	output, err := queryProviderCatalogs(t.Context(), service, json.RawMessage(`{"refresh":true}`))
	if err != nil || !strings.Contains(output, "new-model") || strings.Contains(output, "private-provider-error") {
		t.Fatal("failed refresh discarded models or exposed upstream error")
	}
}

func TestProviderConnectPreservesCustomEndpointAndDefaults(t *testing.T) {
	service := providerConnectionsFixture(t)
	_, revision, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers["openrouter"] = config.Provider{Name: "Proxy", BaseURL: "https://proxy.test/v1", API: "openai-completions", APIKeyEnv: "PROXY_TEST_KEY"}
		cfg.DisabledProviders = []string{"openrouter", "unrelated"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := config.Load()
	service.validate = func(_ context.Context, endpoint, key string) ([]llm.ModelInfo, error) {
		if endpoint != "https://proxy.test/v1" || key != "private-new-key" {
			t.Fatal("setup replaced the configured endpoint or key")
		}
		return []llm.ModelInfo{{ID: "fixture-model"}}, nil
	}
	if _, err := service.SetProviderKey(t.Context(), ProviderKeySetup{Revision: revision, Provider: "openrouter", Key: "private-new-key"}); err != nil {
		t.Fatal(err)
	}
	after, _ := config.Load()
	if after.DefaultModel != before.DefaultModel || after.DefaultProvider != before.DefaultProvider || !reflect.DeepEqual(after.Models, before.Models) {
		t.Fatal("connect changed model defaults or aliases")
	}
	if after.Providers["openrouter"].Name != "Proxy" || after.Providers["openrouter"].APIKeyEnv != "" || !reflect.DeepEqual(after.DisabledProviders, []string{"unrelated"}) {
		t.Fatal("connect changed route metadata or unrelated disabled entries")
	}
	if _, err := service.SetProviderKey(t.Context(), ProviderKeySetup{Revision: revision, Provider: "openrouter", Key: "private-other-key"}); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatalf("stale setup = %v", err)
	}
}

func TestProviderDisconnectDoesNotFallBackAndSurvivesRestart(t *testing.T) {
	service := providerConnectionsFixture(t)
	t.Setenv(config.OpenRouterEnvVar, "private-environment-key")
	before, revision, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers["openrouter"] = config.Provider{BaseURL: config.OpenRouterBaseURL, APIKey: "private-saved-key"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := config.UpdateCatalog("openrouter", config.Catalog{BaseURL: config.OpenRouterBaseURL, Models: []config.ModelInfoLite{{ID: "fixture"}}}); err != nil {
		t.Fatal(err)
	}
	status, err := service.DisconnectProvider(t.Context(), protocol.ProviderDisconnectParams{Provider: "openrouter", Revision: revision})
	if err != nil || !status.Disabled || status.Available == nil || *status.Available {
		t.Fatalf("disconnect = %+v, %v", status, err)
	}
	cfg, err := config.Load()
	if err != nil || cfg.Providers["openrouter"].APIKey != "" || cfg.DefaultModel != before.DefaultModel || !reflect.DeepEqual(cfg.Models, before.Models) {
		t.Fatal("disconnect removed routes/defaults or retained the saved key")
	}
	if _, exists := cfg.EffectiveProviders()["openrouter"]; exists {
		t.Fatal("disconnected provider fell back to environment")
	}
	if _, exists := config.LoadCatalogs()["openrouter"]; exists {
		t.Fatal("disconnect retained cache")
	}
	restarted := NewProviderService(t.Context(), "restarted")
	t.Cleanup(restarted.Close)
	if !providerEntry(t, restarted, "openrouter").Status.Disabled {
		t.Fatal("restart lost opt-out")
	}
	if _, err := restarted.DisconnectProvider(t.Context(), protocol.ProviderDisconnectParams{Provider: "openrouter", Revision: revision}); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatal("stale disconnect accepted")
	}
	current, err := restarted.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.UpdateConfiguration(ConfigurationUpdate{Revision: current.Revision, DisabledProviders: new([]string{})}); err != nil {
		t.Fatal(err)
	}
	if err := restarted.checkModelProvider("openrouter"); !llm.IsPermanentRequestError(err) {
		t.Fatal("enabling a disconnected route made a retained session key usable again")
	}
	if !slices.Contains(providerEntry(t, restarted, "openrouter").Methods, "environment") {
		t.Fatal("host environment was not offered as an explicit reconnection method")
	}
	restarted.validate = func(_ context.Context, _, key string) ([]llm.ModelInfo, error) {
		if key != "private-environment-key" {
			t.Fatal("environment reconnection resolved the wrong key")
		}
		return []llm.ModelInfo{}, nil
	}
	current, err = restarted.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.SetProviderKey(t.Context(), ProviderKeySetup{Revision: current.Revision, Provider: "openrouter", Environment: true}); err != nil {
		t.Fatal(err)
	}
	if status := providerEntry(t, restarted, "openrouter").Status; status.KeySource != "environment" || !*status.Available {
		t.Fatal("explicit environment reconnection did not take effect")
	}
}

func TestProviderEnvironmentDisablePreservesKeysAndInterruptsLogin(t *testing.T) {
	service := providerConnectionsFixture(t)
	t.Setenv(config.OpenRouterEnvVar, "private-environment-key")
	before, err := service.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DisconnectProvider(t.Context(), protocol.ProviderDisconnectParams{Provider: "openrouter", Revision: before.Revision}); err == nil {
		t.Fatal("allowed removing an external credential")
	}
	ready := make(chan struct{})
	service.openAILogin = func(ctx context.Context, _ func(string, string)) (openaiauth.Credentials, error) {
		close(ready)
		<-ctx.Done()
		return openaiauth.Credentials{}, ctx.Err()
	}
	flow, err := service.BeginProviderLogin(openaiauth.Provider)
	if err != nil {
		t.Fatal(err)
	}
	<-ready
	disabled := []string{"openrouter", openaiauth.Provider}
	updated, err := service.UpdateConfiguration(ConfigurationUpdate{Revision: before.Revision, DisabledProviders: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if state, _ := service.LoginStatus(flow.FlowID); state.State != "interrupted" {
		t.Fatal("disable left login able to reconnect provider")
	}
	if os.Getenv(config.OpenRouterEnvVar) != "private-environment-key" {
		t.Fatal("disable changed environment")
	}
	if _, err := service.UpdateConfiguration(ConfigurationUpdate{Revision: before.Revision, DisabledProviders: new([]string{})}); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatal("stale enable accepted")
	}
	if _, err := service.UpdateConfiguration(ConfigurationUpdate{Revision: updated.Revision, DisabledProviders: new([]string{})}); err != nil {
		t.Fatal(err)
	}
	if !*providerEntry(t, service, "openrouter").Status.Available {
		t.Fatal("enable did not restore environment route")
	}
}

func TestProviderDisabledDuringCatalogFetchCannotRepublishModels(t *testing.T) {
	service := providerConnectionsFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture-model"}]}`))
	}))
	t.Cleanup(upstream.Close)
	cfg, revision, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers["custom"] = config.Provider{BaseURL: upstream.URL, APIKey: "fixture"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- service.refreshCatalog(t.Context(), "custom", cfg.Providers["custom"]) }()
	<-started
	disabled := []string{"custom"}
	_, disableErr := service.UpdateConfiguration(ConfigurationUpdate{Revision: revision, DisabledProviders: &disabled})
	close(release)
	if disableErr != nil {
		t.Fatal(disableErr)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("late catalog was accepted")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("catalog fetch did not finish")
	}
	if _, exists := service.Catalogs()["custom"]; exists {
		t.Fatal("disabled models republished")
	}
}

func TestProviderInferenceLoginPreservesCustomOverride(t *testing.T) {
	service := providerConnectionsFixture(t)
	_, _, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers[config.InferenceNetProvider] = config.Provider{BaseURL: "https://custom.test/v1", APIKey: "fixture"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := providerEntry(t, service, config.InferenceNetProvider)
	if !entry.Custom || slices.Contains(entry.Methods, "login") {
		t.Fatal("custom endpoint advertised account login")
	}
	if _, err := service.BeginLogin(); err == nil {
		t.Fatal("account login accepted a custom endpoint")
	}
}

func TestProviderCustomDisconnectLeavesUnrelatedAccountIntact(t *testing.T) {
	service := providerConnectionsFixture(t)
	auth := inferencenet.Auth{MachineKey: "private-unrelated-machine-key"}
	if err := inferencenet.SaveAuth(auth); err != nil {
		t.Fatal(err)
	}
	_, revision, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers[config.InferenceNetProvider] = config.Provider{BaseURL: "https://custom.test/v1", APIKey: "private-custom-key"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DisconnectProvider(t.Context(), protocol.ProviderDisconnectParams{Provider: config.InferenceNetProvider, Revision: revision}); err != nil {
		t.Fatal(err)
	}
	retained, err := inferencenet.LoadAuth()
	if err != nil || retained.MachineKey != auth.MachineKey {
		t.Fatal("custom disconnect removed unrelated account")
	}
}

type providerFixtureTransport func(*http.Request) (*http.Response, error)

func (f providerFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestProviderAccountReplacementCannotRepublishOldCatalog(t *testing.T) {
	service := providerConnectionsFixture(t)
	if err := inferencenet.SaveAuth(inferencenet.Auth{MachineKey: "private-old-machine-key"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	previous := http.DefaultTransport
	http.DefaultTransport = providerFixtureTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != config.InferenceNetBaseURL+"/models" || request.Header.Get("Authorization") != "Bearer private-old-machine-key" {
			t.Error("unexpected fixture provider request")
		}
		close(started)
		select {
		case <-release:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"old-account-model"}]}`))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	done := make(chan error, 1)
	go func() {
		done <- service.refreshCatalog(t.Context(), config.InferenceNetProvider, cfg.Providers[config.InferenceNetProvider])
	}()
	<-started
	service.provisionMu.Lock()
	err = service.finish(t.Context(), inferencenet.Auth{MachineKey: "private-new-machine-key"})
	service.provisionMu.Unlock()
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("old account catalog was accepted after replacement")
	}
	if _, exists := config.LoadCatalogs()[config.InferenceNetProvider]; exists {
		t.Fatal("old account models survived replacement")
	}
}

func TestProviderDisableBlocksRetainedRootChildAndHelperClients(t *testing.T) {
	service := providerConnectionsFixture(t)
	var requests atomic.Int32
	_, root, runtime := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		request, ok := modelAccountingRequest(t, w, r)
		if !ok {
			return
		}
		requests.Add(1)
		modelAccountingReply(w, request, "completed", modelAccountingUsage())
	}, nil, func(value *agent.Agent) {
		value.CompactClient, value.CompactModel, value.CompactProvider = value.Client, "title-model", "helper"
		if _, _, err := config.UpdateVersioned("", func(cfg *config.Config) error {
			for _, name := range []string{"provider", "helper"} {
				cfg.Providers[name] = config.Provider{BaseURL: value.Client.BaseURL, APIKey: value.Client.APIKey}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	root.providers = service
	runs := &sync.Map{}
	runtime.setRunTurnHook(observeRunTurn(runs))
	receipt, err := root.Submit(t.Context(), "initial work")
	if err != nil {
		t.Fatal(err)
	}
	if completed := waitReceipt(t, receipt); completed.Err != nil {
		t.Fatal(completed.Err)
	}
	before, err := service.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	disabled := []string{"provider", "helper"}
	if _, err := service.UpdateConfiguration(ConfigurationUpdate{Revision: before.Revision, DisabledProviders: &disabled}); err != nil {
		t.Fatal(err)
	}
	spawned, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "child", "prompt": "blocked work", "report": "message"})
	if err != nil {
		t.Fatal(err)
	}
	id := spawned.(map[string]any)["id"].(string)
	waitRunTurn(t, runs, id, 1)
	runtime.mu.RLock()
	child := runtime.agents[id]
	runtime.mu.RUnlock()
	waitAgentIdle(t, child)
	receipt, err = root.Submit(t.Context(), "must not use retained key")
	if err != nil {
		t.Fatal(err)
	}
	if completed := waitReceipt(t, receipt); !llm.IsPermanentRequestError(completed.Err) {
		t.Fatalf("disabled turn must fail permanently: %v", completed.Err)
	}
	if _, _, err := runtime.rootNode.complete(t.Context(), "helper", 128); !llm.IsPermanentRequestError(err) {
		t.Fatalf("helper bypassed disable: %v", err)
	}
	if _, _, err := runtime.rootNode.GenerateTitle(t.Context()); !llm.IsPermanentRequestError(err) {
		t.Fatalf("compact/title route bypassed disable: %v", err)
	}
	if requests.Load() != 1 || runTurnCount(runs, id) != 1 {
		t.Fatal("disabled route was dispatched or automatically retried")
	}
}
