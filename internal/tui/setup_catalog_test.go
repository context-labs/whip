package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
)

func TestSetupSelectsNewlyDiscoveredCerebrasModel(t *testing.T) {
	h := &setupTestHost{
		list: protocol.ProviderList{
			Revision: "fixture", Selection: &protocol.ProviderSelection{},
			Providers: []protocol.ProviderEntry{{
				ID: "cerebras", Name: "Cerebras", Methods: []string{"api_key"},
				Status: protocol.ProviderStatus{Available: new(true)},
			}},
		},
		catalogs: protocol.ProviderCatalogsResult{
			Models: map[string]protocol.ModelDescriptor{},
			Catalogs: map[string]config.Catalog{"cerebras": {
				Models: []config.ModelInfoLite{{ID: "gpt-oss-120b"}, {ID: "qwen-3.8-27b"}, {ID: "future-model"}},
			}},
		},
	}
	s := newTestSetup(t, h, true)
	applySetupCommand(t, s, s.Init())
	s.input.SetValue("cerebras")
	providerFormDrain(t, s, s.keypress(setupKey("enter")))
	if s.mode != "models" || h.catalogProvider != "cerebras" || len(s.modelOptions()) != 3 {
		t.Fatalf("live catalog did not reach picker: %s %v", s.mode, s.modelOptions())
	}
	view := ansi.Strip(s.body())
	for _, id := range []string{"gpt-oss-120b", "qwen-3.8-27b", "future-model"} {
		if !strings.Contains(view, id) {
			t.Errorf("picker did not render %s", id)
		}
	}
	s.selected = slices.Index(s.modelOptions(), "qwen-3.8-27b")
	providerFormDrain(t, s, s.keypress(setupKey("enter")))
	if !s.chosen || s.model != "qwen-3.8-27b" || s.provider != "cerebras" || len(h.writes) != 1 {
		t.Fatalf("new model was not selected: %s/%s, writes %+v", s.provider, s.model, h.writes)
	}
	if h.writes[0].DefaultModel == nil || *h.writes[0].DefaultModel != "qwen-3.8-27b" {
		t.Fatal("new model was not persisted as the onboarding default")
	}
}
