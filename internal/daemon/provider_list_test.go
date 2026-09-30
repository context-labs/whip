package daemon

import (
	"testing"

	"github.com/context-labs/whip/internal/config"
	providersvc "github.com/context-labs/whip/internal/provider"
)

func pickerInventoryFixture(t *testing.T) *providersvc.ProviderService {
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
