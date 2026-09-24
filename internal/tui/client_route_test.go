package tui

import (
	"testing"

	"github.com/context-labs/whip/internal/config"
)

func TestHostRouteDoesNotRequireLocallyConfiguredProvider(t *testing.T) {
	m := modelCmdModel()
	m.cfg = &config.Config{Models: map[string]config.Model{}, Providers: map[string]config.Provider{}}
	m.catalogs = map[string]config.Catalog{"remote": {Models: []config.ModelInfoLite{{ID: "host-model", ContextLength: 12345}}}}
	m.applyClientRoute("host-model", "remote")
	if m.displayModelID() != "host-model" || m.displayContextLimit() != 12345 {
		t.Fatal("host route lost to local validation")
	}
}
