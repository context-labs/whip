package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferencenet"
	"github.com/context-labs/whip/internal/protocol"
)

func TestProviderSelectionPreservesRoutesAndRecommendsOnlyAvailableModels(t *testing.T) {
	for _, test := range []struct {
		name              string
		inference, router bool
		model, provider   string
		ready             bool
		wantProvider      string
	}{
		{name: "clean", wantProvider: "inference-net"},
		{name: "inference detected", inference: true, ready: true, wantProvider: "inference-net"},
		{name: "router detected leaves default explicit", router: true, wantProvider: "inference-net"},
		{name: "explicit router", inference: true, router: true, model: "moonshotai/kimi-k2.5", provider: "openrouter", ready: true, wantProvider: "openrouter"},
		{name: "unknown explicit model", inference: true, model: "unknown", provider: "inference-net", wantProvider: "inference-net"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := providerConnectionsFixture(t)
			if test.inference {
				t.Setenv(config.InferenceNetEnvVar, "fixture")
			}
			if test.router {
				t.Setenv(config.OpenRouterEnvVar, "fixture")
			}
			if _, err := config.Load(); err != nil {
				t.Fatal(err)
			}
			if err := config.UpdateCatalog("openrouter", config.Catalog{BaseURL: config.OpenRouterBaseURL, FetchedAt: time.Now(), Models: []config.ModelInfoLite{{ID: "alphabetically-first"}, {ID: "moonshotai/kimi-k2.5"}}}); err != nil {
				t.Fatal(err)
			}
			list, err := s.ListProvidersFor(test.model, test.provider)
			if err != nil {
				t.Fatal(err)
			}
			if list.Selection == nil || list.Selection.Ready != test.ready || list.Selection.Provider != test.wantProvider {
				t.Fatalf("selection = %+v", list.Selection)
			}
			if list.Providers[0].ID != config.InferenceNetProvider || !list.Providers[0].Recommended {
				t.Fatal("missing restrained Inference.net preference")
			}
			if test.router && providerEntry(t, s, "openrouter").SuggestedModel != "moonshotai/kimi-k2.5" {
				t.Fatal("did not recommend an available coding candidate")
			}
			cfg, err := config.Load()
			if err != nil || cfg.DefaultModel != "kimi-k3-fast" || cfg.DefaultProvider != "" {
				t.Fatal("inventory changed defaults")
			}
		})
	}
}

func TestProviderSelectionMatchesCatalogRoutePrecedence(t *testing.T) {
	for _, test := range []struct {
		name, explicit string
		shared         bool
		want           string
	}{
		{name: "catalog route precedes host default", want: "catalog-provider"},
		{name: "host default does not resolve ambiguity", shared: true},
		{name: "explicit route resolves ambiguity", shared: true, explicit: "default-provider", want: "default-provider"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := providerConnectionsFixture(t)
			_, _, err := config.UpdateVersioned("", func(cfg *config.Config) error {
				cfg.DefaultProvider = "default-provider"
				for _, name := range []string{"default-provider", "catalog-provider"} {
					cfg.Providers[name] = config.Provider{BaseURL: "https://" + name + ".test/v1", APIKey: "fixture"}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"default-provider", "catalog-provider"} {
				catalog := config.Catalog{BaseURL: "https://" + name + ".test/v1"}
				if name == "catalog-provider" || test.shared {
					catalog.Models = []config.ModelInfoLite{{ID: "catalog-only-model"}}
				}
				if err := config.UpdateCatalog(name, catalog); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			route, _, _, _, routeErr := cfg.ResolveRoute("catalog-only-model", test.explicit)
			list, err := s.ListProvidersFor("catalog-only-model", test.explicit)
			if err != nil {
				t.Fatal(err)
			}
			selection := list.Selection
			if route != test.want || (routeErr == nil) != (test.want != "") || selection.Ready != (routeErr == nil) || selection.Provider != route {
				t.Fatalf("runtime route=%q err=%v, readiness=%+v, want route=%q", route, routeErr, selection, test.want)
			}
		})
	}
}

func TestProviderSelectionRespectsDisabledCustomAndCredentialCommands(t *testing.T) {
	s := providerConnectionsFixture(t)
	marker := filepath.Join(t.TempDir(), "secret-command-executed")
	_, _, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.DefaultProvider, cfg.DefaultModel = "custom", "coding"
		cfg.Providers["custom"] = config.Provider{BaseURL: "https://custom.test/v1", APIKey: "!touch " + marker}
		cfg.Models["coding"] = config.Model{Providers: []string{"custom"}}
		cfg.DisabledProviders = []string{"inference-net"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListProviders()
	if err != nil || !list.Selection.Ready || list.Selection.Provider != "custom" {
		t.Fatalf("selection = %+v, %v", list.Selection, err)
	}
	if list.Providers[0].Recommended {
		t.Fatal("promoted a disabled provider")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("executed a credential command during discovery")
	}
	_, _, err = config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.DisabledProviders = nil
		cfg.Providers["inference-net"] = config.Provider{BaseURL: "https://custom.test/v1", APIKey: "fixture"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if providerEntry(t, s, "inference-net").Recommended {
		t.Fatal("promoted a custom endpoint as Inference.net")
	}
}

func TestProviderDefaultPairValidationAndPersistence(t *testing.T) {
	s := providerConnectionsFixture(t)
	t.Setenv(config.OpenRouterEnvVar, "fixture")
	before, err := s.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.UpdateCatalog("openrouter", config.Catalog{BaseURL: config.OpenRouterBaseURL, Models: []config.ModelInfoLite{{ID: "moonshotai/kimi-k2.5", ReasoningEfforts: []string{"low"}}}}); err != nil {
		t.Fatal(err)
	}
	model, provider := "moonshotai/kimi-k2.5", "openrouter"
	_, err = s.UpdateConfiguration(ConfigurationUpdate{Revision: before.Revision, DefaultModel: new("not-advertised"), DefaultProvider: &provider})
	if err == nil {
		t.Fatal("accepted an unavailable model")
	}
	updated, err := s.UpdateConfiguration(ConfigurationUpdate{Revision: before.Revision, DefaultModel: &model, DefaultProvider: &provider, DefaultEffort: new("")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateConfiguration(ConfigurationUpdate{Revision: before.Revision, DefaultModel: &model, DefaultProvider: &provider}); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatalf("stale update = %v", err)
	}
	restarted := NewProviderService(t.Context(), "restarted")
	t.Cleanup(restarted.Close)
	list, err := restarted.ListProviders()
	if err != nil || !list.Selection.Ready || list.Selection.Model != model || list.Selection.Provider != provider || list.Revision != updated.Revision {
		t.Fatalf("restarted selection = %+v, %v", list.Selection, err)
	}
}

func TestProviderLoginSkipsOnlySingletonChoices(t *testing.T) {
	s := providerConnectionsFixture(t)
	s.login = func(context.Context, func(string, string)) (providerLoginIdentity, error) {
		return providerLoginIdentity{token: "fixture-secret", teams: []inferencenet.Team{{ID: "team", Name: "Only workspace"}}}, nil
	}
	s.projects = func(context.Context, string, inferencenet.Team) ([]inferencenet.Project, error) {
		return []inferencenet.Project{{ID: "project", Name: "Only project"}}, nil
	}
	finished := make(chan inferencenet.Auth, 1)
	s.finish = func(_ context.Context, auth inferencenet.Auth) error { finished <- auth; return nil }
	s.create = func(context.Context, string, inferencenet.Team, string) (inferencenet.Project, error) {
		t.Error("automatically created a project")
		return inferencenet.Project{}, errors.New("unexpected create")
	}
	flow, err := s.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	status := waitProviderState(t, s, flow.FlowID, "succeeded")
	if status.TeamID != "team" || status.ProjectID != "project" {
		t.Fatalf("lost selected context: %+v", status)
	}
	if auth := <-finished; auth.ProjectID != "project" || auth.TeamID != "team" {
		t.Fatalf("wrong provisioned context: %+v", auth)
	}
	if _, err := s.SelectLoginProject(flow.FlowID, "project"); err == nil {
		t.Fatal("duplicate provisioning allowed")
	}
}

func TestConcurrentInferenceLoginReusesActiveFlow(t *testing.T) {
	s := providerConnectionsFixture(t)
	var calls atomic.Int32
	s.login = func(ctx context.Context, _ func(string, string)) (providerLoginIdentity, error) {
		calls.Add(1)
		<-ctx.Done()
		return providerLoginIdentity{}, ctx.Err()
	}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for range 8 {
		wg.Go(func() {
			flow, err := s.BeginLogin()
			if err != nil {
				t.Error(err)
				return
			}
			ids <- flow.FlowID
		})
	}
	wg.Wait()
	close(ids)
	var first string
	for id := range ids {
		if first != "" && id != first {
			t.Fatalf("concurrent login created different flows: %q and %q", first, id)
		}
		first = id
	}
	if first == "" {
		t.Fatal("no login flow returned")
	}
	s.Close()
	if calls.Load() != 1 {
		t.Fatalf("started %d login attempts", calls.Load())
	}
}

func TestProviderListRPCSelectionParameters(t *testing.T) {
	params, err := json.Marshal(protocol.ProviderListParams{Model: "explicit", Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.ValidateRPC("provider.list", params); err != nil {
		t.Fatal(err)
	}
}

func TestProviderCatalogDefaultPairSurvivesSharedModelsAndRestart(t *testing.T) {
	service := pickerInventoryFixture(t)
	_, revision, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		for _, id := range []string{"alpha", "beta"} {
			cfg.Providers[id] = config.Provider{BaseURL: "https://" + id + ".example/v1", APIKey: "fixture"}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	catalogs := map[string]config.Catalog{}
	for _, id := range []string{"alpha", "beta"} {
		catalogs[id] = config.Catalog{
			BaseURL: "https://" + id + ".example/v1",
			Models:  []config.ModelInfoLite{{ID: "shared-catalog-model", ContextLength: 32768}},
		}
	}
	if err := config.SaveCatalogs(catalogs); err != nil {
		t.Fatal(err)
	}
	model, provider := "shared-catalog-model", "beta"
	updated, err := service.UpdateConfiguration(ConfigurationUpdate{
		Revision: revision, DefaultModel: &model, DefaultProvider: &provider,
	})
	if err != nil {
		t.Fatalf("could not save an explicit catalog-only default pair: %v", err)
	}
	restarted := NewProviderService(t.Context(), "catalog-default-restarted")
	t.Cleanup(restarted.Close)
	list, err := restarted.ListProviders()
	if err != nil || list.Revision != updated.Revision || list.Selection == nil || !list.Selection.Ready || list.Selection.Model != model || list.Selection.Provider != provider {
		t.Fatalf("restart lost catalog default pair: %+v, %v", list.Selection, err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	id, route, _, apiID, err := cfg.ResolveRoute("", "")
	if err != nil || id != provider || apiID != model || route.BaseURL != "https://beta.example/v1" {
		t.Fatalf("runtime config did not resolve saved pair: %q %q %v", id, apiID, err)
	}
	created, err := resolveSessionDefaults(t.Context(), nil, CreateSession{Kind: "agent"})
	if err != nil || created.Model != model || created.Provider != provider {
		t.Fatalf("new session did not retain concrete pair: %+v, %v", created, err)
	}
	if _, _, _, _, err := cfg.ResolveRoute(model, ""); err == nil {
		t.Fatal("explicit ambiguous model silently inherited host provider")
	}
}
