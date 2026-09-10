package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
)

func setupDefaultCatalogs() protocol.ProviderCatalogsResult {
	result := protocol.ProviderCatalogsResult{
		Models: map[string]protocol.ModelDescriptor{}, Providers: map[string]protocol.ProviderDescriptor{},
		Catalogs: map[string]config.Catalog{},
	}
	for _, preset := range config.ProviderPresets() {
		if preset.OnboardingEffort == "" {
			continue
		}
		result.Providers[preset.ID] = protocol.ProviderDescriptor{BaseURL: preset.Provider.BaseURL}
		result.Catalogs[preset.ID] = config.Catalog{BaseURL: preset.Provider.BaseURL, Models: []config.ModelInfoLite{{
			ID: preset.SuggestedModels[0], ReasoningEfforts: []string{preset.OnboardingEffort},
		}}}
	}
	return result
}

func TestSetupProviderDefaultsCloseWithoutModelConfirmation(t *testing.T) {
	for _, tc := range []struct{ provider, model, effort string }{
		{"inference-net", "kimi-k3-fast", "high"},
		{"openrouter", "z-ai/glm-5.3", "max"},
		{"openai", "gpt-6-astra", "medium"},
		{"openai-codex", "gpt-6-astra", "medium"},
	} {
		for _, source := range []string{"detected", "key", "login"} {
			if source == "key" && tc.provider == "openai-codex" || source == "login" && tc.provider != "openai-codex" && tc.provider != "inference-net" {
				continue
			}
			for _, fresh := range []bool{true, false} {
				t.Run(tc.provider+"/"+source+"/"+map[bool]string{true: "fresh", false: "existing"}[fresh], func(t *testing.T) {
					h := &setupTestHost{list: pickerFixture(), catalogs: setupDefaultCatalogs()}
					s := newTestSetup(t, h, fresh)
					applySetupCommand(t, s, s.Init())
					entry := pickerEntry(t, &h.list, tc.provider)
					s.provider = tc.provider
					s.notice = "Models loaded; inference has not been tested."
					switch source {
					case "detected":
						entry.Status.Available = new(true)
						providerFormDrain(t, s, s.connect(*entry))
					case "key":
						s.connectMethod(*entry, "api_key")
						s.input.SetValue("fixture-only-key")
						providerFormDrain(t, s, s.keypress(setupKey("enter")))
					case "login":
						entry.Status.Available = new(true)
						providerFormDrain(t, s, s.applyLogin(daemon.ProviderLoginStatus{State: "succeeded"}))
					}
					if !s.done || !s.chosen || s.model != tc.model || s.provider != tc.provider || s.effort != tc.effort || s.message != "" {
						t.Fatalf("selection=%s/%s/%s done=%v message=%q", s.provider, s.model, s.effort, s.done, s.message)
					}
					if fresh {
						if len(h.writes) != 1 || *h.writes[0].DefaultModel != tc.model || *h.writes[0].DefaultProvider != tc.provider || h.writes[0].DefaultEffort == nil || *h.writes[0].DefaultEffort != tc.effort {
							t.Fatalf("defaults not saved together: %+v", h.writes)
						}
					} else if len(h.writes) != 0 {
						t.Fatal("existing session changed global defaults")
					}
				})
			}
		}
	}
}

func TestSetupDefaultFailuresRetainModelChoice(t *testing.T) {
	for _, problem := range []string{"missing-model", "effort", "catalog", "custom-endpoint", "alias", "save"} {
		t.Run(problem, func(t *testing.T) {
			h := &setupTestHost{list: pickerFixture(), catalogs: setupDefaultCatalogs()}
			entry := pickerEntry(t, &h.list, "openai")
			entry.Status.Available = new(true)
			switch problem {
			case "missing-model":
				h.catalogs.Catalogs["openai"] = config.Catalog{Models: []config.ModelInfoLite{{ID: "gpt-4.1"}}}
			case "effort":
				h.catalogs.Catalogs["openai"].Models[0].ReasoningEfforts = []string{"high"}
			case "catalog":
				h.catalogs.Errors = map[string]string{"openai": "connection failed"}
			case "custom-endpoint":
				h.catalogs.Providers["openai"] = protocol.ProviderDescriptor{BaseURL: "https://example.test/v1"}
			case "alias":
				h.catalogs.Models["gpt-6-astra"] = protocol.ModelDescriptor{ID: "different-model", Providers: []string{"openai"}}
			case "save":
				h.writeErr = errors.New("configuration changed")
			}
			s := newTestSetup(t, h, true)
			applySetupCommand(t, s, s.Init())
			providerFormDrain(t, s, s.connect(*entry))
			if s.done || s.chosen || problem != "save" && len(h.writes) != 0 {
				t.Fatal("unavailable default was accepted")
			}
			if problem != "custom-endpoint" && s.message == "" {
				t.Fatal("actionable error was hidden")
			}
		})
	}
}

func TestSetupModelPickerOmitsConnectionNotices(t *testing.T) {
	h := &setupTestHost{list: setupFixture()}
	s := newTestSetup(t, h, false)
	s.provider, s.mode = "openrouter", "models"
	s.notice = "Models loaded; inference has not been tested."
	providerFormDrain(t, s, s.loadCatalogs(false))
	view := ansi.Strip(s.body())
	if strings.Contains(view, "inference has not been tested") || strings.Contains(view, "openrouter") {
		t.Fatalf("model picker still has a notice: %s", view)
	}
	s.message = "Connection failed"
	if !strings.Contains(ansi.Strip(s.body()), s.message) {
		t.Fatal("real error was hidden")
	}
}

func TestSetupManualModelAfterFailedDefaultClearsPresetEffort(t *testing.T) {
	h := &setupTestHost{list: pickerFixture(), catalogs: setupDefaultCatalogs(), writeErr: errors.New("configuration changed")}
	entry := pickerEntry(t, &h.list, "openai")
	entry.Status.Available = new(true)
	catalog := h.catalogs.Catalogs["openai"]
	catalog.Models = append(catalog.Models, config.ModelInfoLite{ID: "gpt-4.1"})
	h.catalogs.Catalogs["openai"] = catalog
	s := newTestSetup(t, h, true)
	applySetupCommand(t, s, s.Init())
	providerFormDrain(t, s, s.connect(*entry))
	if s.done || s.effort != "medium" {
		t.Fatal("expected failed default save")
	}
	if s.mode != "models" {
		t.Fatal("failed save did not return to model choice")
	}
	s.input.SetValue("gpt-4.1")
	s.selected = 0
	h.writeErr = nil
	providerFormDrain(t, s, s.keypress(setupKey("enter")))
	if !s.done || s.model != "gpt-4.1" || s.effort != "" || h.writes[len(h.writes)-1].DefaultEffort != nil {
		t.Fatal("manual model inherited preset thinking")
	}
}
