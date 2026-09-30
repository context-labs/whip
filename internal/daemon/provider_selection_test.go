package daemon

import (
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"

	providersvc "github.com/context-labs/whip/internal/provider"
)

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
	updated, err := service.UpdateConfiguration(protocol.ConfigurationUpdate{
		Revision: revision, DefaultModel: &model, DefaultProvider: &provider,
	})
	if err != nil {
		t.Fatalf("could not save an explicit catalog-only default pair: %v", err)
	}
	restarted := providersvc.NewProviderService(t.Context(), "catalog-default-restarted")
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
