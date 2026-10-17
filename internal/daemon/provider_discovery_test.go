package daemon

import (
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
)

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
