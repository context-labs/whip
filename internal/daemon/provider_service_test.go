package daemon

import (
	"testing"

	"github.com/context-labs/whip/internal/config"
	providersvc "github.com/context-labs/whip/internal/provider"
)

func compactionConfigurationFixture(t *testing.T) (*providersvc.ProviderService, *config.Config) {
	t.Helper()
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	t.Setenv("WHIP_TEST_MISSING_COMPACTION_KEY", "")
	cfg := &config.Config{
		DefaultModel: "main",
		Providers: map[string]config.Provider{
			"ready":    {BaseURL: "https://ready.test/v1", Auth: "none"},
			"other":    {BaseURL: "https://other.test/v1", Auth: "none"},
			"offline":  {BaseURL: "https://offline.test/v1", APIKeyEnv: "WHIP_TEST_MISSING_COMPACTION_KEY"},
			"disabled": {BaseURL: "https://disabled.test/v1", Auth: "none"},
		},
		Models: map[string]config.Model{
			"main":             {Providers: []string{"ready"}},
			"summary":          {Providers: []string{"ready"}},
			"offline-summary":  {Providers: []string{"offline"}},
			"disabled-summary": {Providers: []string{"disabled"}},
		},
		DisabledProviders: []string{"disabled"},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"ready": {BaseURL: "https://ready.test/v1", Models: []config.ModelInfoLite{
			{ID: "catalog-only"}, {ID: "ambiguous"},
		}},
		"other": {BaseURL: "https://other.test/v1", Models: []config.ModelInfoLite{{ID: "ambiguous"}}},
	}); err != nil {
		t.Fatal(err)
	}
	s := providersvc.NewProviderService(t.Context(), "compaction-settings")
	t.Cleanup(s.Close)
	return s, cfg
}
