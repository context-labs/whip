package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"

	providersvc "github.com/context-labs/whip/internal/provider"
)

func providerConnectionsFixture(t *testing.T) *providersvc.ProviderService {
	t.Helper()
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.InferenceNetEnvVar, "")
	t.Setenv(config.OpenRouterEnvVar, "")
	service := providersvc.NewProviderService(t.Context(), "connections")
	t.Cleanup(service.Close)
	return service
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
	if _, err := service.UpdateConfiguration(protocol.ConfigurationUpdate{Revision: before.Revision, DisabledProviders: &disabled}); err != nil {
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
	if _, _, err := runtime.rootNode.GenerateTitle(t.Context(), "Inspect model accounting"); !llm.IsPermanentRequestError(err) {
		t.Fatalf("compact/title route bypassed disable: %v", err)
	}
	if requests.Load() != 1 || runTurnCount(runs, id) != 1 {
		t.Fatal("disabled route was dispatched or automatically retried")
	}
}
