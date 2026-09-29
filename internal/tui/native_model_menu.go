package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/protocol"
)

func (m *nativeMenu) readInventory() tea.Cmd {
	connection := m.connection
	var ownerID protocol.ID
	if m.owner != nil {
		ownerID = m.owner.ID
	}
	return m.call("inventory", false, func(ctx context.Context) nativeMenuReply {
		var inventory protocol.ProviderInventory
		err := connection.Call(ctx, "providers.list", protocol.EmptyParams{}, &inventory)
		reply := nativeMenuReply{inventory: &inventory, err: err}
		if err == nil && ownerID != "" {
			var owner protocol.Session
			readErr := connection.Call(ctx, "sessions.get", protocol.SessionParams{SessionID: ownerID}, &owner)
			reply.owner, reply.err = &owner, readErr
		}
		return reply
	})
}

func (m *nativeMenu) readCatalog(refresh bool) tea.Cmd {
	connection, provider := m.connection, m.provider
	method := "providers.catalog"
	if refresh {
		method = "providers.refresh"
	}
	return m.call("catalog", false, func(ctx context.Context) nativeMenuReply {
		var catalog protocol.ProviderCatalog
		err := connection.Call(ctx, method, protocol.ProviderParams{Provider: provider}, &catalog)
		return nativeMenuReply{catalog: &catalog, err: err}
	})
}

func (m *nativeMenu) modelReply(reply nativeMenuReply) tea.Cmd {
	if reply.inventory != nil {
		m.inventory = *reply.inventory
	}
	if reply.owner != nil && m.owner != nil && reply.owner.ID == m.owner.ID && reply.owner.ConfigRevision >= m.owner.ConfigRevision {
		m.owner = reply.owner
	}
	switch reply.kind {
	case "inventory":
		m.mode, m.title = "model-providers", "Choose model provider"
		m.resetInput()
		m.choices = nil
		for _, route := range m.inventory.Routes {
			m.choices = append(m.choices, nativeMenuChoice{id: string(route.ID), label: string(route.ID), detail: "Credential: " + route.Credential.State + ". Choosing a configured route does not test inference."})
		}
		if len(m.choices) == 0 {
			m.message = "No configured routes. Open /setup to connect a provider."
		}
	case "catalog":
		m.catalog = *reply.catalog
		m.showModels()
	case "model-saved":
		m.mode, m.title = "saved", "Model saved"
		m.resetInput()
		m.choices = []nativeMenuChoice{{id: "close", label: "Done"}}
		m.message = reply.message
	}
	return nil
}

func (m *nativeMenu) showModels() {
	m.mode, m.title = "models", "Choose model · "+string(m.provider)
	m.resetInput()
	byID := map[string]protocol.ProviderModel{}
	for _, model := range m.catalog.Models {
		byID[model.ID] = model
	}
	for _, route := range m.inventory.Routes {
		if route.ID != m.provider {
			continue
		}
		for id, settings := range route.Models {
			if _, exists := byID[id]; !exists {
				byID[id] = protocol.ProviderModel{ID: id, Name: id, Prices: settings.Prices, ContextWindowTokens: settings.ContextWindowTokens}
			}
		}
	}
	m.modelChoices = nil
	for _, model := range byID {
		m.modelChoices = append(m.modelChoices, model)
	}
	slices.SortFunc(m.modelChoices, func(a, b protocol.ProviderModel) int { return strings.Compare(a.ID, b.ID) })
	m.choices = nil
	for _, model := range m.modelChoices {
		label := model.ID
		if model.Name != "" && model.Name != model.ID {
			label += " · " + model.Name
		}
		m.choices = append(m.choices, nativeMenuChoice{id: "model:" + model.ID, label: label, detail: nativeModelDetail(model)})
	}
	m.choices = append(m.choices, nativeMenuChoice{id: "manual", label: "Enter exact model ID…", detail: "Explicit model selection; availability remains untested."}, nativeMenuChoice{id: "refresh", label: "Refresh provider catalog", detail: "Contacts this provider explicitly. Cached metadata does not prove inference readiness."}, nativeMenuChoice{id: "back", label: "Back to providers"})
	m.message = "Catalog: " + m.catalog.State + "; discovery: " + m.catalog.Discovery + "."
	if m.catalog.Stale {
		m.message += " Cached metadata is stale."
	}
	if m.catalog.Failure != nil {
		m.message += " " + *m.catalog.Failure
	}
}

func nativeModelDetail(model protocol.ProviderModel) string {
	context := "Context unknown"
	if model.ContextWindowTokens != nil {
		context = fmt.Sprintf("Context %d tokens", *model.ContextWindowTokens)
	}
	price := "Price unknown"
	if model.Prices.Input != nil && model.Prices.Output != nil {
		price = "Input " + nativeModelPrice(*model.Prices.Input) + " / output " + nativeModelPrice(*model.Prices.Output) + " per million tokens"
	}
	return context + ". " + price + "."
}

func (m *nativeMenu) selectModel(id string) {
	selection := protocol.ModelSelection{}
	if m.owner != nil {
		selection = m.owner.Configuration.Model
	} else if m.inventory.Defaults != nil {
		selection = *m.inventory.Defaults
	}
	selection.Provider, selection.Name, selection.Effort = m.provider, id, ""
	m.selection = selection
	m.mode, m.title = "model-effort", "Reasoning effort · "+id
	m.resetInput()
	m.choices = []nativeMenuChoice{{id: "", label: "Provider default"}}
	for _, model := range m.modelChoices {
		if model.ID == id {
			for _, effort := range model.ReasoningEfforts {
				if effort != "" {
					m.choices = append(m.choices, nativeMenuChoice{id: effort, label: effort})
				}
			}
		}
	}
	if len(m.choices) == 1 {
		m.showModelScope()
	}
}

func (m *nativeMenu) showModelScope() {
	m.mode, m.title = "model-scope", "Apply "+string(m.selection.Provider)+"/"+m.selection.Name
	m.resetInput()
	m.choices = nil
	if m.owner != nil {
		m.choices = append(m.choices, nativeMenuChoice{id: "session", label: "Use for this session only", detail: "Only the selected session changes; other roots and children keep their configuration."})
	}
	if m.options.Kind != "model-for-session" {
		label := "Save host default"
		if m.owner != nil {
			label += " and use for this session"
		}
		m.choices = append(m.choices, nativeMenuChoice{id: "default", label: label, detail: "Changes the saved default for future sessions, using the captured host revision."})
	}
	m.message = "Effort: " + m.selection.Effort
	if m.selection.Effort == "" {
		m.message = "Effort: provider default"
	}
}

func (m *nativeMenu) saveModel(hostDefault bool) tea.Cmd {
	connection, selection, revision := m.connection, m.selection, m.inventory.Revision
	var params *protocol.UpdateConfigurationParams
	if m.owner != nil {
		params = &protocol.UpdateConfigurationParams{SessionID: m.owner.ID, ExpectedRevision: m.owner.ConfigRevision, Patch: protocol.ConfigPatch{Model: &selection}}
	}
	return m.call("model-saved", true, func(ctx context.Context) nativeMenuReply {
		reply := nativeMenuReply{message: "Model saved. Inference has not been tested."}
		if hostDefault {
			var inventory protocol.ProviderInventory
			err := connection.Call(ctx, "providers.defaults", protocol.ProviderDefaultsParams{Revision: revision, Defaults: protocol.ProviderDefaults{Selection: &selection}}, &inventory)
			if err != nil {
				reply.err = err
				return reply
			}
			reply.inventory = &inventory
		}
		if params != nil {
			var owner protocol.Session
			err := connection.Call(ctx, "sessions.configure", *params, &owner)
			if err != nil {
				if hostDefault {
					err = fmt.Errorf("host default was saved; session update did not acknowledge success: %w", err)
				}
				reply.err = err
				return reply
			}
			reply.owner = &owner
		}
		return reply
	})
}

func (m *nativeMenu) resetInput() {
	m.input.EchoMode = textinput.EchoNormal
	m.input.CharLimit = 256
	m.generation++
	m.input.Reset()
	m.selected = 0
	m.message = ""
}

func (m *nativeMenu) refreshMenu() tea.Cmd {
	if m.options.Kind == "rename" {
		return m.readRename()
	}
	if m.mode == "setup-unknown" && m.setup.account != "" {
		return m.readAccount()
	}
	if strings.HasPrefix(m.mode, "setup") {
		return m.readSetup()
	}
	if strings.HasPrefix(m.mode, "account") {
		return m.refreshAccount()
	}
	if m.mode == "theme" || m.mode == "settings" {
		m.openLocalSettings()
		return nil
	}
	return m.readInventory()
}

func (m *nativeMenu) choose(choice nativeMenuChoice) tea.Cmd {
	if strings.HasPrefix(m.mode, "setup") || strings.HasPrefix(m.mode, "account") {
		if m.mode == "setup-unknown" {
			if m.setup.account != "" {
				return m.readAccount()
			}
			return m.readSetup()
		}
		return m.chooseSetup(choice)
	}
	switch m.mode {
	case "model-providers":
		m.provider = protocol.ID(choice.id)
		return m.readCatalog(false)
	case "models":
		switch choice.id {
		case "back":
			return m.readInventory()
		case "refresh":
			return m.readCatalog(true)
		case "manual":
			m.mode, m.title = "model-id", "Exact model ID"
			m.resetInput()
			m.choices = nil
			m.message = "Enter the provider's exact model ID, then press Enter. Availability is untested."
		default:
			m.selectModel(strings.TrimPrefix(choice.id, "model:"))
		}
	case "model-effort":
		m.selection.Effort = choice.id
		m.showModelScope()
	case "model-scope":
		return m.saveModel(choice.id == "default")
	case "theme", "settings":
		m.chooseLocalSetting(choice)
	case "saved":
		m.Close()
	}
	return nil
}

func nativeModelPrice(value protocol.Counter) string {
	if value%1_000_000_000 == 0 {
		return fmt.Sprintf("$%d", value/1_000_000_000)
	}
	return strings.TrimRight(fmt.Sprintf("$%d.%09d", value/1_000_000_000, value%1_000_000_000), "0")
}
