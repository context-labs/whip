package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/tui/ui"
)

// The provider form lives only in its owning dialog. Stored secrets are never
// loaded into inputs, and RPC closures capture values rather than this form.
type setupProviderForm struct {
	view            protocol.ProviderConfiguration
	inputs          map[string]textinput.Model
	focus           string
	credential      string
	isEditing       bool
	isManualOnly    bool
	hasManualModel  bool
	isAdvanced      bool
	allowUnverified bool
}

type setupProviderAction struct{ id, label string }

func newSetupProviderForm(view protocol.ProviderConfiguration, editing, manual bool) *setupProviderForm {
	f := &setupProviderForm{
		view: view, inputs: map[string]textinput.Model{}, focus: "name", credential: "api_key",
		isEditing: editing, isManualOnly: manual, hasManualModel: manual,
	}
	for _, name := range []string{"name", "url", "id", "key", "environment", "model", "alias", "context", "output"} {
		input := textinput.New()
		input.Prompt, input.CharLimit = "", 2048
		if name == "key" {
			input.EchoMode, input.CharLimit = textinput.EchoPassword, 16384
		}
		f.inputs[name] = input
	}
	f.set("name", view.Definition.Name)
	f.set("url", view.Definition.BaseURL)
	f.set("id", view.Provider)
	f.set("environment", view.Credential.EnvironmentVariable)
	if editing {
		f.credential = "keep"
	}
	if manual {
		f.focus = "model"
	}
	f.focusField(f.focus)
	return f
}

func (f *setupProviderForm) value(id string) string { return strings.TrimSpace(f.inputs[id].Value()) }

func (f *setupProviderForm) set(id, value string) {
	input := f.inputs[id]
	input.SetValue(value)
	f.inputs[id] = input
}

func (f *setupProviderForm) fields() []string {
	fields := []string{}
	if !f.isManualOnly {
		fields = append(fields, "name")
		if !f.isEditing || f.view.Custom {
			fields = append(fields, "url")
		}
		fields = append(fields, "auth")
		switch f.credential {
		case "api_key":
			fields = append(fields, "key")
		case "environment":
			fields = append(fields, "environment")
		}
		fields = append(fields, "manual")
	}
	if f.hasManualModel {
		fields = append(fields, "model")
	}
	fields = append(fields, "advanced")
	if f.isAdvanced {
		if !f.isEditing {
			fields = append(fields, "id")
		}
		if f.hasManualModel {
			fields = append(fields, "alias", "context", "output")
		}
	}
	if f.hasManualModel || f.credential == "environment" {
		fields = append(fields, "verification")
	}
	return append(fields, "save")
}

func (f *setupProviderForm) focusField(field string) {
	f.focus = field
	for id, input := range f.inputs {
		input.Blur()
		if id == field {
			input.Focus()
		}
		f.inputs[id] = input
	}
}

func (f *setupProviderForm) clearSecret() { f.set("key", "") }

func (s *providerSetup) close() {
	s.input.Reset()
	if s.form != nil {
		s.form.clearSecret()
	}
	s.done = true
	s.cancel()
}

func (s *providerSetup) providerMode() bool {
	switch s.mode {
	case "provider_form", "provider_manage", "provider_remove", "provider_saved":
		return true
	}
	return false
}

func (s *providerSetup) formInputField() string {
	if s.mode == "provider_form" && s.form != nil {
		return s.form.focus
	}
	return ""
}

func (s *providerSetup) updateFormInput(msg tea.Msg) tea.Cmd {
	f := s.form
	if f == nil || s.busy {
		return nil
	}
	input, ok := f.inputs[f.focus]
	if !ok {
		return nil
	}
	var cmd tea.Cmd
	before := input.Value()
	input, cmd = input.Update(msg)
	f.inputs[f.focus] = input
	if f.focus == "model" && input.Value() != before {
		f.prefillModel()
	}
	return s.inputCommand(cmd)
}

func (f *setupProviderForm) prefillModel() {
	for _, id := range []string{"alias", "context", "output"} {
		f.set(id, "")
	}
	var match *protocol.ProviderConfiguredModel
	for i := range f.view.Models {
		if f.view.Models[i].ID == f.value("model") {
			if match != nil {
				return // Multiple aliases need an explicit choice under Advanced.
			}
			match = &f.view.Models[i]
		}
	}
	if match == nil {
		return
	}
	f.set("alias", match.Alias)
	if match.Context > 0 {
		f.set("context", strconv.Itoa(match.Context))
	}
	if match.MaxOutput > 0 {
		f.set("output", strconv.Itoa(match.MaxOutput))
	}
}

// Refresh advances untouched inputs with the host, while retaining only the
// user's edits. Otherwise an old display value becomes an unintended patch.
func (f *setupProviderForm) refresh(view protocol.ProviderConfiguration) bool {
	previous := f.view
	if f.isEditing {
		for _, field := range []struct{ id, before, after string }{
			{"name", previous.Definition.Name, view.Definition.Name},
			{"url", previous.Definition.BaseURL, view.Definition.BaseURL},
			{"environment", previous.Credential.EnvironmentVariable, view.Credential.EnvironmentVariable},
		} {
			if f.value(field.id) == field.before {
				f.set(field.id, field.after)
			}
		}
		for _, old := range previous.Models {
			if f.value("alias") != old.Alias || f.value("model") != old.ID {
				continue
			}
			for _, next := range view.Models {
				if next.Alias != old.Alias || next.ID != old.ID {
					continue
				}
				for _, field := range []struct {
					id            string
					before, after int
				}{{"context", old.Context, next.Context}, {"output", old.MaxOutput, next.MaxOutput}} {
					before, after := "", ""
					if field.before > 0 {
						before = strconv.Itoa(field.before)
					}
					if field.after > 0 {
						after = strconv.Itoa(field.after)
					}
					if f.value(field.id) == before {
						f.set(field.id, after)
					}
				}
			}
		}
	}
	endpointChanged := f.isEditing && previous.Definition.BaseURL != view.Definition.BaseURL
	if endpointChanged {
		f.credential = "keep"
		f.focusField("auth")
		if f.isManualOnly {
			f.focusField("model")
		}
	}
	f.view = view
	f.clearSecret()
	return endpointChanged
}

func (s *providerSetup) openCustomProvider() {
	if s.form == nil || s.form.isEditing {
		s.form = newSetupProviderForm(protocol.ProviderConfiguration{Revision: s.list.Revision}, false, false)
	}
	s.mode, s.message, s.selected = "provider_form", "", 0
	s.input.Reset()
}

func (s *providerSetup) readProvider(after string) tea.Cmd {
	host, provider := s.host, s.provider
	model, selection := s.selectionModel, s.selectionProvider
	if s.mode == "provider_form" && s.form != nil && !s.form.isEditing {
		provider = s.form.providerID()
	}
	return s.call("provider-"+after, func(ctx context.Context) setupReply {
		if after == "refresh" {
			list, err := host.ListProvidersFor(ctx, model, selection)
			if err != nil {
				return setupReply{err: err}
			}
			found := slices.ContainsFunc(list.Providers, func(entry protocol.ProviderEntry) bool { return entry.ID == provider })
			if !found {
				return setupReply{list: list, configuration: protocol.ProviderConfiguration{Revision: list.Revision}}
			}
			view, err := host.ReadProvider(ctx, provider)
			return setupReply{list: list, configuration: view, err: err}
		}
		view, err := host.ReadProvider(ctx, provider)
		return setupReply{configuration: view, err: err}
	})
}

func (s *providerSetup) providerFailure(reply setupReply) tea.Cmd {
	s.message = reply.err.Error() + " · ctrl+r refresh; your draft is preserved."
	var rpc *protocol.RPCError
	if errors.As(reply.err, &rpc) && rpc.Code == -32601 {
		s.message = "Update Whip on this execution host to configure providers here."
	}
	if s.form != nil && reply.kind == "provider-save" {
		// A failed or uncertain key submission is never replayed from UI state.
		s.form.clearSecret()
		if s.form.credential == "api_key" {
			s.message += " Re-enter the API key before saving again."
		}
	}
	return nil
}

func (s *providerSetup) providerReply(reply setupReply) tea.Cmd {
	switch reply.kind {
	case "provider-manage", "provider-manual", "provider-refresh":
		if reply.kind == "provider-refresh" && s.form != nil && s.form.isEditing && reply.configuration.Provider == "" {
			s.form.clearSecret()
			s.message = "This connection was removed on the host. Your edits are retained; return to providers to add a new connection."
			return nil
		}
		s.configuration = reply.configuration
		if reply.list.Selection != nil {
			s.list = reply.list
		}
		s.provider, s.list.Revision = reply.configuration.Provider, reply.configuration.Revision
		if reply.kind == "provider-refresh" && s.form != nil {
			// Refresh the base revision, retaining the explicit non-secret edits.
			endpointChanged := s.form.refresh(reply.configuration)
			s.message = "Refreshed configuration. Review your edits before saving."
			if endpointChanged {
				s.message = "The host endpoint changed. Credential replacement was reset; review the URL and choose credentials again if needed."
			}
			if !s.form.isEditing && reply.configuration.Provider != "" {
				s.message = "This provider ID is already saved. Use Manage to inspect it, or choose a different ID."
			}
			if s.form.credential == "api_key" {
				s.message += " Re-enter the key to replace it."
			}
			return nil
		}
		if reply.kind == "provider-manual" {
			s.form = newSetupProviderForm(reply.configuration, true, true)
			s.mode = "provider_form"
			return nil
		}
		s.mode, s.selected = "provider_manage", 0
	case "provider-save":
		s.configuration = reply.configuration
		s.provider, s.model = reply.configuration.Provider, reply.model
		s.list.Revision = reply.configuration.Revision
		if reply.reload {
			s.reloadProvider = s.provider
		}
		if s.form != nil {
			s.form.clearSecret()
		}
		s.form = nil
		return s.refresh("-saved")
	case "provider-disabled", "provider-enabled", "provider-disconnected":
		return s.refresh("-managed")
	case "provider-removed":
		s.configuration = protocol.ProviderConfiguration{}
		s.form = nil
		s.provider, s.model, s.selected = "", "", 0
		s.mode, s.notice = "providers", reply.message
		return s.refresh("-managed")
	}
	return nil
}

func (s *providerSetup) afterProviderRefresh(kind string) tea.Cmd {
	s.input.Reset()
	s.input.EchoMode = textinput.EchoNormal
	s.selected = 0
	if kind == "inventory-managed" {
		if s.provider == "" {
			s.mode = "providers"
			s.message, s.notice = s.notice, ""
			return nil
		}
		return s.readProvider("manage")
	}
	s.message = ""
	entry := s.entry()
	if entry == nil || !setupCanAttempt(*entry) {
		s.mode = "provider_manage"
		s.message += " Credentials are not available yet; update the execution host's environment or connection."
		return nil
	}
	if !s.newSession && s.reloadProvider == s.selectionProvider && s.provider == s.selectionProvider {
		s.mode = "provider_saved"
		return nil
	}
	if s.model != "" {
		return s.useModel()
	}
	return s.prepareModel()
}

func (f *setupProviderForm) providerID() string {
	if f.isEditing {
		return f.view.Provider
	}
	if id := f.value("id"); id != "" {
		return id
	}
	var id strings.Builder
	for _, r := range strings.ToLower(f.value("name")) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			id.WriteRune(r)
		} else if id.Len() > 0 && !strings.HasSuffix(id.String(), "-") {
			id.WriteByte('-')
		}
	}
	return strings.Trim(id.String(), "-")
}

func (f *setupProviderForm) manualModel() (*protocol.ProviderManualModel, error) {
	if !f.hasManualModel {
		return nil, nil //nolint:nilnil // A manual model is optional; nil leaves provider discovery in charge.
	}
	id := f.value("model")
	if id == "" {
		return nil, errors.New("enter the exact API model ID")
	}
	model := &protocol.ProviderManualModel{ID: id, Alias: f.value("alias")}
	if model.Alias == "" {
		model.Alias = f.providerID() + "/" + id
	}
	for name, dest := range map[string]*int{"context": &model.Context, "output": &model.MaxOutput} {
		if value := f.value(name); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("%s must be a positive token count", name)
			}
			*dest = n
		}
	}
	return model, nil
}

func (s *providerSetup) saveProviderForm() tea.Cmd {
	f := s.form
	if !f.isEditing && f.view.Provider != "" && f.providerID() == f.view.Provider {
		s.message = "This provider ID already exists. Choose another ID, or return to Manage to inspect the saved connection."
		return nil
	}
	manual, err := f.manualModel()
	if err != nil {
		s.message = err.Error()
		return nil
	}
	credential := protocol.ProviderCredential{Mode: f.credential}
	switch credential.Mode {
	case "api_key":
		credential.Key = config.TrimKey(f.value("key"))
		if credential.Key == "" {
			s.message = "Paste an API key, or choose another authentication method."
			f.focusField("key")
			return nil
		}
	case "environment":
		credential.EnvironmentVariable = f.value("environment")
	}
	provider, host := f.providerID(), s.host
	create := protocol.ProviderCreateParams{
		Revision: f.view.Revision, Provider: provider,
		Definition: protocol.ProviderDefinition{Name: f.value("name"), BaseURL: f.value("url"), API: "openai-completions"},
		Credential: credential, ManualModel: manual, AllowUnverified: f.allowUnverified,
	}
	update := protocol.ProviderUpdateParams{
		Revision: f.view.Revision, Provider: provider, ManualModel: manual, AllowUnverified: f.allowUnverified,
	}
	if !f.isManualOnly {
		if name := f.value("name"); name != f.view.Definition.Name {
			update.Name = &name
		}
		if url := f.value("url"); url != f.view.Definition.BaseURL {
			update.BaseURL = &url
		}
		update.Credential = &credential
	}
	if f.isEditing && update.BaseURL != nil && credential.Mode == "keep" {
		s.message = "Choose credentials for the new endpoint before saving. The existing key will not be sent there."
		f.focusField("auth")
		return nil
	}
	isEditing := f.isEditing
	reload := isEditing && provider == s.selectionProvider &&
		(update.BaseURL != nil || credential.Mode != "keep" || manual != nil)
	alias := ""
	if manual != nil {
		alias = manual.Alias
	}
	if !isEditing && f.value("id") == "" {
		f.set("id", provider)
	}
	f.clearSecret()
	return s.call("provider-save", func(ctx context.Context) setupReply {
		var view protocol.ProviderConfiguration
		var err error
		if isEditing {
			view, err = host.UpdateProvider(ctx, update)
		} else {
			view, err = host.CreateProvider(ctx, create)
		}
		return setupReply{configuration: view, model: alias, reload: reload, err: err}
	})
}

func (s *providerSetup) setProviderDisabled(disabled bool) tea.Cmd {
	host, provider, revision := s.host, s.provider, s.configuration.Revision
	after := "provider-enabled"
	if disabled {
		after = "provider-disabled"
	}
	return s.call(after, func(ctx context.Context) setupReply {
		cfg, err := host.ReadConfiguration(ctx)
		if err != nil {
			return setupReply{err: err}
		}
		if cfg.Revision != revision {
			return setupReply{err: config.ErrRevisionConflict}
		}
		providers := []string{}
		if cfg.DisabledProviders != nil {
			providers = slices.Clone(*cfg.DisabledProviders)
		}
		providers = slices.DeleteFunc(providers, func(id string) bool { return id == provider })
		if disabled {
			providers = append(providers, provider)
		}
		_, err = host.UpdateConfiguration(ctx, daemon.ConfigurationUpdate{Revision: revision, DisabledProviders: &providers})
		return setupReply{err: err}
	})
}

func (s *providerSetup) providerActions() []setupProviderAction {
	switch s.mode {
	case "provider_remove":
		return []setupProviderAction{{"back", "Keep connection"}, {"remove", "Remove custom provider"}}
	case "provider_saved":
		if s.model != "" && s.model != s.selectionModel {
			return []setupProviderAction{
				{"use-model", "Use " + s.model}, {"use", "Choose another model"}, {"back", "Back to providers"},
			}
		}
		return []setupProviderAction{
			{"reload", "Reload current session"}, {"use", "Choose a model"}, {"back", "Back to providers"},
		}
	}
	actions := []setupProviderAction{{"use", "Use connection / choose model"}}
	if s.configuration.Definition.API != "openai-codex" {
		actions = append(actions, setupProviderAction{"edit", "Edit connection"})
	}
	if entry := s.entry(); entry != nil {
		if slices.Contains(entry.Methods, "login") {
			actions = append(actions, setupProviderAction{"login", "Sign in again"})
		}
		if entry.Status.Disabled {
			actions = append(actions, setupProviderAction{"enable", "Enable connection"})
		} else {
			actions = append(actions, setupProviderAction{"disable", "Disable connection"})
		}
		if slices.Contains([]string{"literal", "machine", "subscription"}, entry.Status.KeySource) {
			actions = append(actions, setupProviderAction{"disconnect", "Disconnect and remove saved credential"})
		}
	}
	if s.configuration.Custom {
		actions = append(actions, setupProviderAction{"remove-check", "Remove custom provider…"})
	}
	return append(actions, setupProviderAction{"back", "Back to providers"})
}

func (s *providerSetup) providerKeypress(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == "esc" {
		if s.cancelCall != nil {
			s.cancelCall()
		}
		s.request++
		if s.busy {
			s.message = "The operation may have saved. Refresh the connection before retrying."
		}
		s.busy, s.selected = false, 0
		s.input.Reset()
		switch {
		case s.mode == "provider_remove":
			s.mode = "provider_manage"
		case s.mode == "provider_form" && s.form.isManualOnly:
			s.mode = "models"
		case s.mode == "provider_form" && s.form.isEditing:
			s.mode = "provider_manage"
		default:
			s.mode = "providers"
		}
		return nil
	}
	if s.busy {
		return nil
	}
	if key == "ctrl+r" {
		if s.mode == "provider_form" {
			return s.readProvider("refresh")
		}
		return s.refresh("-managed")
	}
	if s.mode == "provider_form" {
		return s.formKeypress(msg)
	}
	actions := s.providerActions()
	switch key {
	case "up", "shift+tab":
		s.selected = (s.selected + len(actions) - 1) % len(actions)
	case "down", "tab":
		s.selected = (s.selected + 1) % len(actions)
	case "enter":
		s.message = ""
		switch actions[min(s.selected, len(actions)-1)].id {
		case "use-model":
			return s.useModel()
		case "reload":
			s.reload, s.done = true, true
		case "use":
			entry := s.entry()
			if entry != nil {
				if entry.Status.Disabled {
					s.message = "Enable this connection before choosing a model."
					return nil
				}
				return s.connect(*entry)
			}
		case "edit":
			if s.form == nil || !s.form.isEditing || s.form.isManualOnly || s.form.view.Provider != s.provider {
				s.form = newSetupProviderForm(s.configuration, true, false)
			}
			s.mode = "provider_form"
		case "login":
			if entry := s.entry(); entry != nil {
				loginEntry := *entry
				loginEntry.Status = protocol.ProviderStatus{}
				return s.connect(loginEntry)
			}
		case "disable":
			return s.setProviderDisabled(true)
		case "enable":
			return s.setProviderDisabled(false)
		case "disconnect":
			host := s.host
			params := protocol.ProviderDisconnectParams{Provider: s.provider, Revision: s.configuration.Revision}
			return s.call("provider-disconnected", func(ctx context.Context) setupReply {
				_, err := host.DisconnectProvider(ctx, params)
				return setupReply{err: err}
			})
		case "remove-check":
			if len(s.configuration.RemovalBlockers) > 0 {
				s.message = "Still referenced by " + strings.Join(s.configuration.RemovalBlockers, ", ") + ". Disable it instead."
				return nil
			}
			s.mode, s.selected = "provider_remove", 0
		case "remove":
			host := s.host
			params := protocol.ProviderRemoveParams{Provider: s.provider, Revision: s.configuration.Revision}
			return s.call("provider-removed", func(ctx context.Context) setupReply {
				result, err := host.RemoveProvider(ctx, params)
				return setupReply{err: err, message: result.Warning}
			})
		case "back":
			if s.mode == "provider_remove" {
				s.mode = "provider_manage"
			} else {
				s.mode = "providers"
			}
			s.selected = 0
		}
	}
	return nil
}

func (s *providerSetup) formKeypress(msg tea.KeyPressMsg) tea.Cmd {
	f := s.form
	key := msg.String()
	fields := f.fields()
	index := slices.Index(fields, f.focus)
	if f.focus == "auth" {
		if key == "down" {
			key = "right"
		}
		if key == "up" {
			key = "left"
		}
	}
	switch key {
	case "tab", "down":
		f.focusField(fields[(index+1)%len(fields)])
		return nil
	case "shift+tab", "up":
		f.focusField(fields[(index+len(fields)-1)%len(fields)])
		return nil
	}
	_, isInput := f.inputs[f.focus]
	if isInput {
		if key == "enter" {
			f.advanceField()
			return nil
		}
		return s.updateFormInput(msg)
	}
	if key != "enter" && key != "space" && key != "left" && key != "right" {
		return nil
	}
	switch f.focus {
	case "auth":
		if key == "enter" {
			f.advanceField()
			return nil
		}
		modes := []string{"api_key", "environment", "none"}
		if f.isEditing {
			modes = append([]string{"keep"}, modes...)
		}
		direction := 1
		if key == "left" {
			direction = len(modes) - 1
		}
		f.credential = modes[(slices.Index(modes, f.credential)+direction)%len(modes)]
		f.clearSecret()
	case "manual":
		f.hasManualModel = !f.hasManualModel
	case "advanced":
		f.isAdvanced = !f.isAdvanced
	case "verification":
		f.allowUnverified = !f.allowUnverified
	case "save":
		if key == "enter" || key == "space" {
			return s.saveProviderForm()
		}
	}
	return nil
}

// Normal setup advances straight to the save action. Manual models and limits
// remain optional actions on the review page instead of extra required steps.
func (f *setupProviderForm) advanceField() {
	fields := f.fields()
	next := fields[(slices.Index(fields, f.focus)+1)%len(fields)]
	if next == "manual" || next == "advanced" {
		next = "save"
	}
	f.focusField(next)
}

func (f *setupProviderForm) fieldLabel(id string) string {
	labels := map[string]string{
		"name": "Name", "url": "Base URL", "id": "Provider ID", "key": "API key (masked)",
		"environment": "Environment variable on host", "model": "API model ID", "alias": "Model alias",
		"context": "Context tokens", "output": "Output tokens",
	}
	if label := labels[id]; label != "" {
		return label
	}
	switch id {
	case "auth":
		return "Authentication: " + map[string]string{
			"keep": "Keep existing credential", "api_key": "API key", "environment": "Environment variable", "none": "None",
		}[f.credential]
	case "manual":
		if f.hasManualModel {
			return "[x] Enter a model manually"
		}
		return "[ ] Enter a model manually"
	case "advanced":
		if f.isAdvanced {
			return "▾ Advanced"
		}
		return "▸ Advanced"
	case "verification":
		if f.allowUnverified {
			return "[x] Save without verification"
		}
		return "[ ] Save without verification"
	case "save":
		if f.isManualOnly {
			return "Save model"
		}
		if f.isEditing {
			return "Save changes"
		}
		return "Save and choose model"
	}
	return id
}

func (f *setupProviderForm) hint() string {
	switch f.focus {
	case "url":
		return "API root, e.g. https://api.example.com/v1 (Chat Completions)."
	case "id":
		return "Stable ID: " + f.providerID() + ". Cannot be renamed after saving."
	case "auth":
		return "← → change method. None is only for endpoints that require no credentials."
	case "environment":
		return "Resolved on the execution host. Restart that daemon after changing its environment."
	case "key":
		return "Saved only on the execution host. Never shown again in this form."
	case "manual", "model":
		return "Use the exact API model ID if /models is unavailable."
	case "verification":
		return "Explicitly save without a successful model listing; no inference test is sent."
	case "alias":
		return "Blank suggests " + f.providerID() + "/" + f.value("model") + ". Existing aliases are preserved."
	case "context", "output":
		return "Optional token limit. Leave blank to use automatic limits."
	}
	return "Changes are saved on this execution host."
}

func (s *providerSetup) providerFormRows(width, height int) []string {
	if s.mode != "provider_form" || s.form == nil {
		title, detail := "Manage "+s.configuration.Definition.Name, s.configuration.Definition.BaseURL
		if entry := s.entry(); entry != nil {
			detail += "\n" + setupProviderDescription(*entry)
		}
		if s.mode == "provider_remove" {
			title, detail = "Remove custom provider?", "Remove "+s.provider+" from this host's configuration."
		}
		if s.mode == "provider_saved" {
			title, detail = "Connection saved", "Reload to apply this connection to the current session. Other sessions keep their live clients."
		}
		if s.message != "" {
			detail = s.message
		}
		// A provider-specific warning already explains a broken key source.
		if entry := s.entry(); s.mode == "provider_manage" && s.list.DiscoveryError != "" && (entry == nil || len(entry.Status.Warnings) == 0) {
			detail += "\n" + s.list.DiscoveryError
		}
		if s.busy {
			detail = "Working…"
		}
		actions := s.providerActions()
		items := make([]ui.ListItem, 0, len(actions))
		for _, action := range actions {
			items = append(items, ui.ListItem{Left: action.label})
		}
		return s.listRows(ui.List{
			Title: title, Hint: "esc", Groups: []ui.ListGroup{{Items: items}}, Sel: s.selected,
			Width: width, Height: height, Footer: []string{"enter", "select", "ctrl+r", "refresh"},
		}, detail)
	}
	f := s.form
	title := "Add custom provider"
	if f.isEditing {
		title = "Edit " + f.view.Provider
	}
	if f.isManualOnly {
		title = "Enter a model manually"
	}
	hint := f.hint()
	if s.message != "" {
		hint = s.message
	}
	if s.busy {
		hint = "Saving…"
	}
	if input, ok := f.inputs[f.focus]; ok {
		placeholder := map[string]string{
			"name": "My provider", "url": "https://api.example.com/v1", "key": "Paste API key",
			"environment": "MY_PROVIDER_API_KEY", "model": "Model ID", "alias": "Optional alias",
			"context": "Automatic", "output": "Automatic", "id": f.providerID(),
		}[f.focus]
		return setupPromptRows(title, f.fieldLabel(f.focus), setupInputView(input, placeholder, width-4),
			[]string{"enter", "continue", "shift+tab", "back"}, hint, width, height)
	}
	if f.focus == "auth" {
		modes := []string{"api_key", "environment", "none"}
		if f.isEditing {
			modes = append([]string{"keep"}, modes...)
		}
		labels := map[string]string{"keep": "Keep existing credential", "api_key": "API key", "environment": "Environment variable", "none": "No authentication"}
		items := make([]ui.ListItem, len(modes))
		for i, mode := range modes {
			items[i].Left = labels[mode]
		}
		return s.listRows(ui.List{
			Title: "Authentication", Hint: "esc", Groups: []ui.ListGroup{{Items: items}},
			Sel: slices.Index(modes, f.credential), Width: width, Height: height,
			Footer: []string{"enter", "continue", "↑↓", "choose"},
		}, hint)
	}
	// Optional model settings share one compact review page. Input fields open
	// their own prompt, so the panel never grows with expanded advanced options.
	fields, items, selected := f.fields(), []ui.ListItem{}, 0
	for _, id := range fields {
		if _, isInput := f.inputs[id]; isInput || id == "auth" {
			continue
		}
		if id == f.focus {
			selected = len(items)
		}
		items = append(items, ui.ListItem{Left: f.fieldLabel(id)})
	}
	return s.listRows(ui.List{
		Title: title, Hint: "esc", Groups: []ui.ListGroup{{Items: items}}, Sel: selected,
		Width: width, Height: height, Footer: []string{"tab", "next", "enter", "select"},
	}, hint)
}
