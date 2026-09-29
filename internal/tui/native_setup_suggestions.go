package tui

import (
	"context"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/protocol"
)

// Suggestions are only a first-run convenience. The saved host revision and
// explicit scope confirmation remain the authority for changing a default.
func (m *nativeMenu) readSetupSuggestions() tea.Cmd {
	if m.owner != nil || m.inventory.Defaults != nil || m.setup.preset == nil {
		return nil
	}
	preset := *m.setup.preset
	if len(preset.SuggestedModels) == 0 {
		return nil
	}
	canonical := slices.ContainsFunc(m.inventory.Routes, func(route protocol.ProviderRoute) bool {
		return route.ID == preset.ID && route.Kind == preset.Kind && route.BaseURL == preset.BaseURL
	})
	if !canonical {
		return nil
	}
	connection := m.connection
	return m.call("setup-suggestions", false, func(ctx context.Context) nativeMenuReply {
		var catalog protocol.ProviderCatalog
		err := connection.Call(ctx, "providers.catalog", protocol.ProviderParams{Provider: preset.ID}, &catalog)
		return nativeMenuReply{catalog: &catalog, err: err}
	})
}

func (m *nativeMenu) showSetupSuggestions(catalog protocol.ProviderCatalog) {
	m.showSetup()
	m.message = "Provider saved. Choose a model explicitly; no default was changed."
	if m.setup.preset == nil || catalog.Provider != m.setup.preset.ID || catalog.State != "cached" || catalog.ScopeState != "current" || catalog.Stale || catalog.Discovery == "not_checked" || catalog.Discovery == "failed" {
		return
	}
	choices := []nativeMenuChoice{}
	for _, id := range m.setup.preset.SuggestedModels {
		for _, model := range catalog.Models {
			if model.ID == id {
				choices = append(choices, nativeMenuChoice{id: "suggest:" + id, label: "Suggested · " + id, detail: nativeModelDetail(model)})
				break
			}
		}
	}
	if len(choices) == 0 {
		return
	}
	m.catalog, m.provider = catalog, catalog.Provider
	m.modelChoices = catalog.Models
	m.mode, m.title = "setup-suggestions", "Choose a first model"
	m.resetInput()
	m.choices = append(choices, nativeMenuChoice{id: "other", label: "Choose another model…"}, nativeMenuChoice{id: "back", label: "Back without setting a default"})
	m.message = "Suggestions match this provider's current catalog. Inference remains untested. Saving a host default requires your next confirmation."
}

func (m *nativeMenu) chooseSuggestion(choice nativeMenuChoice) tea.Cmd {
	if choice.id == "back" {
		m.showSetup()
		return nil
	}
	if choice.id == "other" {
		m.showModels()
		return nil
	}
	id := choice.id[len("suggest:"):]
	m.selectModel(id)
	if m.setup.preset != nil && slices.ContainsFunc(m.modelChoices, func(model protocol.ProviderModel) bool {
		return model.ID == id && slices.Contains(model.ReasoningEfforts, m.setup.preset.SuggestedEffort)
	}) {
		m.selection.Effort = m.setup.preset.SuggestedEffort
	}
	m.showModelScope()
	return nil
}
