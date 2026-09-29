package tui

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/google/uuid"
)

type nativeSetup struct {
	presets         []protocol.ProviderPreset
	preset          *protocol.ProviderPreset
	editing         bool
	id              protocol.ID
	declaration     protocol.ProviderDeclaration
	keepCredential  bool
	modelID         string
	account         string
	openAI          *protocol.OpenAILoginFlow
	inference       *protocol.InferenceFlow
	openAIStatus    *protocol.OpenAIAccountStatus
	inferenceStatus *protocol.InferenceAccountStatus
	openAIFlows     []protocol.OpenAILoginFlow
	inferenceFlows  []protocol.InferenceFlow
}

func (m *nativeMenu) readSetup() tea.Cmd {
	connection := m.connection
	return m.call("setup-read", false, func(ctx context.Context) nativeMenuReply {
		var presets protocol.ProviderPresetsResult
		if err := connection.Call(ctx, "providers.presets", protocol.EmptyParams{}, &presets); err != nil {
			return nativeMenuReply{err: err}
		}
		var inventory protocol.ProviderInventory
		err := connection.Call(ctx, "providers.list", protocol.EmptyParams{}, &inventory)
		return nativeMenuReply{inventory: &inventory, presets: presets.Items, err: err}
	})
}

func (m *nativeMenu) setupReply(reply nativeMenuReply) tea.Cmd {
	if reply.inventory != nil {
		m.inventory = *reply.inventory
	}
	switch reply.kind {
	case "setup-read":
		m.setup.presets = reply.presets
		m.showSetup()
	case "setup-saved":
		m.showSetup()
		m.message = reply.message
	case "setup-catalog":
		m.catalog = *reply.catalog
		m.showRoute()
		m.message = "Discovery: " + reply.catalog.Discovery + ". Catalog models: " + strconv.Itoa(len(reply.catalog.Models)) + ". Inference remains untested."
		if reply.catalog.Failure != nil {
			m.message += " " + *reply.catalog.Failure
		}
	default:
		return m.accountReply(reply)
	}
	return nil
}

func (m *nativeMenu) showSetup() {
	m.mode, m.title = "setup", "Providers and accounts"
	m.resetInput()
	m.choices = nil
	for _, preset := range m.setup.presets {
		label := preset.Name
		if preset.ID == "inference-net" && preset.Kind == "openai-chat" && preset.BaseURL == "https://api.inference.net/v1" {
			label += " · Recommended"
		}
		for _, route := range m.inventory.Routes {
			if route.ID == preset.ID {
				label = label + " · configured (" + route.Credential.State + ")"
				break
			}
		}
		m.choices = append(m.choices, nativeMenuChoice{id: "preset:" + string(preset.ID), label: label})
	}
	for _, route := range m.inventory.Routes {
		if slices.ContainsFunc(m.setup.presets, func(p protocol.ProviderPreset) bool { return p.ID == route.ID }) {
			continue
		}
		m.choices = append(m.choices, nativeMenuChoice{id: "route:" + string(route.ID), label: string(route.ID) + " · configured (" + route.Credential.State + ")"})
	}
	m.choices = append(m.choices, nativeMenuChoice{id: "custom", label: "Add custom provider…"}, nativeMenuChoice{id: "models", label: "Choose model/default…"})
	m.message = "Configured routes and stored credentials do not prove model access. No account action starts until selected."
}

func (m *nativeMenu) showRoute() {
	m.mode, m.title = "setup-route", "Provider · "+string(m.setup.id)
	m.resetInput()
	m.choices = nil
	var route *protocol.ProviderRoute
	for _, value := range m.inventory.Routes {
		if value.ID == m.setup.id {
			routeCopy := value
			route = &routeCopy
			break
		}
	}
	if route != nil {
		m.setup.editing = true
		m.setup.declaration = protocol.ProviderDeclaration{Kind: route.Kind, BaseURL: route.BaseURL, Models: maps.Clone(route.Models)}
		m.setup.keepCredential = true
		m.message = "Endpoint: " + route.BaseURL + ". Credential: " + route.Credential.State + ". Inference untested."
		if m.setup.preset != nil && (route.Kind != m.setup.preset.Kind || route.BaseURL != m.setup.preset.BaseURL) {
			m.message += " This is a custom endpoint, not the preset endpoint. Saving a key does not validate it."
		}
		if route.Kind != "openai-codex" {
			m.choices = append(m.choices, nativeMenuChoice{id: "edit", label: "Edit endpoint/credential…"}, nativeMenuChoice{id: "model-settings", label: "Configure model limits…"})
		}
		m.choices = append(m.choices, nativeMenuChoice{id: "catalog", label: "Refresh provider catalog"}, nativeMenuChoice{id: "model", label: "Choose model…"}, nativeMenuChoice{id: "remove", label: "Remove configured route…", detail: "Credential material is preserved. Saved default references must be changed first."})
	} else {
		m.setup.editing = false
		m.setup.keepCredential = false
		if m.setup.preset != nil {
			m.setup.declaration = protocol.ProviderDeclaration{Kind: m.setup.preset.Kind, BaseURL: m.setup.preset.BaseURL, Models: map[string]protocol.ProviderModelSettings{}}
		}
	}
	if m.setup.preset != nil {
		p := m.setup.preset
		if slices.Contains(p.Methods, "api_key") {
			m.choices = append(m.choices, nativeMenuChoice{id: "key", label: "Paste API key…", detail: p.KeyURL})
		}
		for _, environment := range p.Environments {
			m.choices = append(m.choices, nativeMenuChoice{id: "env:" + environment, label: "Use host environment · " + environment})
		}
		if slices.Contains(p.Methods, "login") {
			m.choices = append(m.choices, nativeMenuChoice{id: "account", label: "Account login and recovery…"})
		}
	}
	m.choices = append(m.choices, nativeMenuChoice{id: "back", label: "Back to providers"})
}

func (m *nativeMenu) setupForm(mode, title, value string, secret bool) {
	m.mode, m.title = mode, title
	m.resetInput()
	m.choices = nil
	m.input.CharLimit = 4000
	m.input.EchoMode = textinput.EchoNormal
	if secret {
		m.input.EchoMode = textinput.EchoPassword
		m.input.CharLimit = 4096
	}
	m.input.SetValue(value)
}

func (m *nativeMenu) setupConfirm() {
	m.mode, m.title = "setup-confirm", "Save provider · "+string(m.setup.id)
	m.resetInput()
	m.choices = []nativeMenuChoice{{id: "save", label: "Save configured route"}, {id: "back", label: "Back without saving"}}
	m.message = "Endpoint: " + m.setup.declaration.BaseURL + ". Saving this declaration does not verify model access."
}

func (m *nativeMenu) saveProvider(key string, environment bool) tea.Cmd {
	connection, revision, id := m.connection, m.inventory.Revision, m.setup.id
	declaration := m.setup.declaration
	declaration.Models = maps.Clone(declaration.Models)
	method := "providers.create"
	if m.setup.editing {
		method = "providers.update"
	}
	var publication *protocol.ProviderKeyPublication
	if key != "" {
		publication = &protocol.ProviderKeyPublication{ID: protocol.ID(uuid.NewString()), Key: key}
		declaration.Credential = &protocol.ProviderCredentialInput{Source: "file"}
	}
	keep := m.setup.keepCredential && publication == nil && declaration.Credential == nil
	canonical := m.setup.preset != nil && (id == "openrouter" || id == "inference-net") && declaration.Kind == m.setup.preset.Kind && declaration.BaseURL == m.setup.preset.BaseURL && (publication != nil || environment)
	m.input.Reset()
	m.input.EchoMode = textinput.EchoNormal
	return m.call("setup-saved", true, func(ctx context.Context) nativeMenuReply {
		var inventory protocol.ProviderInventory
		var err error
		message := "Route saved. Inference has not been tested; catalog refresh is explicit."
		if canonical {
			err = connection.Call(ctx, "providers.setup_key", protocol.ProviderKeySetup{Revision: revision, Provider: string(id), Environment: environment, Key: publication}, &inventory)
			message = "Provider discovery accepted the credential and the route was saved. Inference has not been tested. Choose a model explicitly."
		} else {
			err = connection.Call(ctx, method, protocol.ChangeProviderParams{Revision: revision, Provider: id, Declaration: declaration, KeepCredential: keep, Key: publication}, &inventory)
		}
		if err != nil {
			return nativeMenuReply{err: err}
		}
		return nativeMenuReply{inventory: &inventory, message: message}
	})
}

func (m *nativeMenu) removeProvider() tea.Cmd {
	connection, params := m.connection, protocol.RemoveProviderParams{Revision: m.inventory.Revision, Provider: m.setup.id}
	return m.call("setup-saved", true, func(ctx context.Context) nativeMenuReply {
		var inventory protocol.ProviderInventory
		err := connection.Call(ctx, "providers.remove", params, &inventory)
		if err != nil {
			return nativeMenuReply{err: err}
		}
		return nativeMenuReply{inventory: &inventory, message: "Configured route removed; stored credential material was preserved."}
	})
}

func (m *nativeMenu) setupCatalog() tea.Cmd {
	connection, id := m.connection, m.setup.id
	return m.call("setup-catalog", false, func(ctx context.Context) nativeMenuReply {
		var catalog protocol.ProviderCatalog
		err := connection.Call(ctx, "providers.refresh", protocol.ProviderParams{Provider: id}, &catalog)
		return nativeMenuReply{catalog: &catalog, err: err}
	})
}

func (m *nativeMenu) chooseSetup(choice nativeMenuChoice) tea.Cmd {
	switch m.mode {
	case "setup":
		m.setup.preset = nil
		m.setup.account = ""
		m.setup.editing = false
		switch {
		case choice.id == "custom":
			m.setup.id = ""
			m.setup.declaration = protocol.ProviderDeclaration{Kind: "openai-chat", Models: map[string]protocol.ProviderModelSettings{}}
			m.setup.keepCredential = false
			m.setupForm("setup-id", "Stable provider ID", "", false)
		case choice.id == "models":
			return m.readInventory()
		case strings.HasPrefix(choice.id, "preset:"):
			id := strings.TrimPrefix(choice.id, "preset:")
			for _, preset := range m.setup.presets {
				if string(preset.ID) == id {
					presetCopy := preset
					m.setup.preset = &presetCopy
					m.setup.id = preset.ID
					break
				}
			}
			m.showRoute()
		default:
			m.setup.id = protocol.ID(strings.TrimPrefix(choice.id, "route:"))
			m.showRoute()
		}
	case "setup-route":
		switch {
		case choice.id == "back":
			m.showSetup()
		case choice.id == "key":
			m.setupForm("setup-key", "API key · "+string(m.setup.id), "", true)
			m.message = "Endpoint: " + m.setup.declaration.BaseURL + ". Key is masked and cleared after send. A lost acknowledgement is inspected, never automatically retried."
		case strings.HasPrefix(choice.id, "env:"):
			m.setup.declaration.Credential = &protocol.ProviderCredentialInput{Source: "env", Environment: strings.TrimPrefix(choice.id, "env:")}
			m.setup.keepCredential = false
			return m.saveProvider("", true)
		case choice.id == "edit":
			m.setupForm("setup-url", "Provider endpoint", m.setup.declaration.BaseURL, false)
		case choice.id == "model-settings":
			m.setupForm("setup-model-id", "Exact model ID to configure", "", false)
		case choice.id == "catalog":
			return m.setupCatalog()
		case choice.id == "model":
			m.provider = m.setup.id
			return m.readCatalog(false)
		case choice.id == "remove":
			m.mode, m.title = "setup-remove", "Remove configured route?"
			m.resetInput()
			m.choices = []nativeMenuChoice{{id: "remove", label: "Remove route; keep credentials"}, {id: "back", label: "Keep route"}}
			m.message = "Removal is refused while a saved model or compaction default references this route."
		case choice.id == "account":
			m.setup.account = "inference"
			if m.setup.id == "openai-codex" {
				m.setup.account = "openai"
			}
			return m.readAccount()
		}
	case "setup-kind":
		m.setup.declaration.Kind = choice.id
		m.setupForm("setup-url", "Provider endpoint", m.setup.declaration.BaseURL, false)
	case "setup-credential":
		m.setup.keepCredential = false
		switch choice.id {
		case "none":
			m.setup.declaration.Credential = &protocol.ProviderCredentialInput{Source: "none"}
			m.setupConfirm()
		case "keep":
			m.setup.keepCredential = true
			m.setup.declaration.Credential = nil
			m.setupConfirm()
		case "key":
			m.setupForm("setup-key", "API key · "+string(m.setup.id), "", true)
		case "env":
			m.setupForm("setup-env", "Host environment variable", "", false)
		case "file":
			m.setupForm("setup-file", "Private credential file on host", "", false)
		}
	case "setup-confirm":
		if choice.id == "save" {
			return m.saveProvider("", false)
		}
		m.showSetup()
	case "setup-remove":
		if choice.id == "remove" {
			return m.removeProvider()
		}
		m.showRoute()
	default:
		return m.chooseAccount(choice)
	}
	return nil
}

func (m *nativeMenu) submitSetup() tea.Cmd {
	value := strings.TrimSpace(m.input.Value())
	if value == "" && m.mode != "setup-context" && m.mode != "setup-output" {
		m.message = "A value is required."
		return nil
	}
	switch m.mode {
	case "setup-id":
		m.setup.id = protocol.ID(value)
		m.mode, m.title = "setup-kind", "Provider API"
		m.resetInput()
		m.choices = []nativeMenuChoice{{id: "openai-chat", label: "OpenAI-compatible chat"}, {id: "openai-responses", label: "OpenAI Responses"}}
	case "setup-url":
		changed := m.setup.declaration.BaseURL != value
		m.setup.declaration.BaseURL = value
		m.mode, m.title = "setup-credential", "Credential source"
		m.resetInput()
		m.choices = []nativeMenuChoice{{id: "key", label: "Paste private key…"}, {id: "env", label: "Host environment variable…"}, {id: "file", label: "Private host file…"}, {id: "none", label: "No authentication"}}
		if !changed && m.setup.editing {
			m.choices = append(m.choices, nativeMenuChoice{id: "keep", label: "Keep current credential source"})
		}
		if changed {
			m.setup.keepCredential = false
			m.setup.declaration.Credential = nil
			m.message = "Endpoint changed. Choose credentials explicitly; existing secrets are not forwarded."
		}
	case "setup-key":
		return m.saveProvider(value, false)
	case "setup-env":
		m.setup.declaration.Credential = &protocol.ProviderCredentialInput{Source: "env", Environment: value}
		m.setupConfirm()
	case "setup-file":
		m.setup.declaration.Credential = &protocol.ProviderCredentialInput{Source: "file", File: value}
		m.setupConfirm()
	case "setup-model-id":
		m.setup.modelID = value
		settings := m.setup.declaration.Models[value]
		initial := ""
		if settings.ContextWindowTokens != nil {
			initial = strconv.FormatInt(int64(*settings.ContextWindowTokens), 10)
		}
		m.setupForm("setup-context", "Context tokens (empty means unknown)", initial, false)
	case "setup-context":
		settings := m.setup.declaration.Models[m.setup.modelID]
		settings.ContextWindowTokens = nil
		if value != "" {
			number, err := strconv.ParseUint(value, 10, 63)
			if err != nil || number == 0 {
				m.message = "Enter a positive whole token count."
				return nil
			}
			settings.ContextWindowTokens = new(protocol.Counter(number))
		}
		m.setup.declaration.Models[m.setup.modelID] = settings
		m.setupForm("setup-output", "Maximum output tokens (0 means default)", strconv.FormatInt(int64(settings.MaxOutputTokens), 10), false)
	case "setup-output":
		number := uint64(0)
		var err error
		if value != "" {
			number, err = strconv.ParseUint(value, 10, 63)
		}
		if err != nil {
			m.message = "Enter a nonnegative whole token count."
			return nil
		}
		settings := m.setup.declaration.Models[m.setup.modelID]
		settings.MaxOutputTokens = protocol.Counter(number)
		m.setup.declaration.Models[m.setup.modelID] = settings
		m.setupConfirm()
	default:
		return m.submitAccount(value)
	}
	return nil
}

func (m *nativeMenu) setupInput() bool {
	switch m.mode {
	case "setup-id", "setup-url", "setup-key", "setup-env", "setup-file", "setup-model-id", "setup-context", "setup-output", "account-project-name":
		return true
	}
	return false
}

func (m *nativeMenu) setupUnknown() {
	m.mode, m.title = "setup-unknown", "Provider action outcome unknown"
	m.input.Reset()
	m.input.EchoMode = textinput.EchoNormal
	m.choices = []nativeMenuChoice{{id: "inspect", label: "Inspect current providers/accounts"}}
}

func nativeAccountText(label string, value *string) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s. ", label, *value)
}
