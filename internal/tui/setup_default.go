package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/config"
)

func setupDefaultPreset(provider string) (config.ProviderPreset, bool) {
	for _, preset := range config.ProviderPresets() {
		if preset.ID == provider && preset.OnboardingEffort != "" && len(preset.SuggestedModels) > 0 {
			return preset, true
		}
	}
	return config.ProviderPreset{}, false
}

// Only an explicit provider choice takes this shortcut. Startup's saved route
// and custom endpoints keep their existing model selection behavior.
func (s *providerSetup) selectDefaultModel() tea.Cmd {
	s.autoModel = false
	s.mode = "models"
	entry := s.entry()
	if entry == nil || !setupCanAttempt(*entry) {
		s.message = "Reconnect this provider to choose a model."
		return nil
	}
	preset, ok := setupDefaultPreset(s.provider)
	if !ok || strings.TrimRight(s.catalogs.Providers[s.provider].BaseURL, "/") != preset.Provider.BaseURL {
		s.model = entry.SuggestedModel
		if s.list.Selection != nil && s.list.Selection.Ready && s.list.Selection.Provider == s.provider {
			s.model = s.list.Selection.Model
		}
		s.selected = max(0, slices.Index(s.modelOptions(), s.model))
		return nil
	}
	if s.message != "" {
		return nil
	}
	model := preset.SuggestedModels[0]
	if alias, exists := s.catalogs.Models[model]; exists && alias.ID != "" && alias.ID != model {
		s.message = model + " is configured as a different model. Choose a model below."
		return nil
	}
	info := s.catalogs.Catalogs[s.provider].Find(model)
	if info == nil {
		s.message = model + " is unavailable on this connection. Choose another model."
		return nil
	}
	if len(info.ReasoningEfforts) > 0 && !slices.Contains(info.ReasoningEfforts, preset.OnboardingEffort) {
		s.message = model + " does not offer the requested thinking level. Choose another model."
		return nil
	}
	s.model, s.effort = model, preset.OnboardingEffort
	return s.useModel()
}
