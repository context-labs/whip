package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/tui/ui"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
)

var errProviderReadinessUnavailable = errors.New("this execution host cannot report provider readiness; update Whip on the host and restart it, then reconnect")

// providerSetup is the shared floating connection dialog. The normal composer
// owns the draft before and after a session exists; this dialog never submits it.
type providerSetup struct {
	request                           uint64
	pickProvider                      string
	pickKey                           bool
	selectionModel, selectionProvider string
	ctx                               context.Context
	cancel                            context.CancelFunc
	cancelCall                        context.CancelFunc
	host                              setupHost
	newSession                        bool
	automatic                         bool
	persistDefault                    bool
	model, provider                   string
	list                              protocol.ProviderList
	catalogs                          protocol.ProviderCatalogsResult
	login                             daemon.ProviderLoginStatus
	loginFeedback                     string
	openedURL                         string
	mode                              string
	selected                          int
	input                             textinput.Model
	message                           string
	notice                            string
	effort                            string
	autoModel                         bool
	busy                              bool
	done                              bool
	chosen                            bool
	width                             int
	form                              *setupProviderForm
	configuration                     protocol.ProviderConfiguration
	reloadProvider                    string
	reload                            bool
	methods                           []setupConnectionMethod
	manageMethods                     bool
}

type setupReply struct {
	request       uint64
	owner         *providerSetup
	kind          string
	list          protocol.ProviderList
	catalogs      protocol.ProviderCatalogsResult
	login         daemon.ProviderLoginStatus
	err           error
	configuration protocol.ProviderConfiguration
	model         string
	reload        bool
	message       string
}

type setupPoll struct {
	owner   *providerSetup
	request uint64
	flowID  string
}

// Clipboard and cursor commands return through the same owner boundary as
// RPCs. Late input cannot enter another provider dialog or the session draft.
type setupInputMsg struct {
	owner   *providerSetup
	request uint64
	message tea.Msg
	field   string
	mode    string
}

func (s *providerSetup) inputCommand(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	request := s.request
	field, mode := s.formInputField(), s.mode
	return func() tea.Msg {
		return setupInputMsg{owner: s, request: request, field: field, mode: mode, message: cmd()}
	}
}

func newProviderSetup(ctx context.Context, host setupHost, newSession bool) *providerSetup {
	ctx, cancel := context.WithCancel(ctx)
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 16384
	input.Focus()
	return &providerSetup{
		ctx: ctx, cancel: cancel, host: host, newSession: newSession,
		persistDefault: newSession, input: input, mode: "providers", width: 64,
	}
}

func (s *providerSetup) Init() tea.Cmd { return s.refresh("") }

func (s *providerSetup) call(kind string, operation func(context.Context) setupReply) tea.Cmd {
	if s.cancelCall != nil {
		s.cancelCall()
	}
	s.busy = true
	s.request++
	request := s.request
	ctx, cancelCall := context.WithCancel(s.ctx)
	s.cancelCall = cancelCall
	return func() tea.Msg {
		defer cancelCall()
		bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		reply := operation(bounded)
		reply.owner, reply.kind, reply.request = s, kind, request
		return reply
	}
}

func (s *providerSetup) refresh(after string) tea.Cmd {
	host, model, provider := s.host, s.selectionModel, s.selectionProvider
	return s.call("inventory"+after, func(ctx context.Context) setupReply {
		list, err := host.DiscoverProviders(ctx, model, provider)
		if err != nil {
			return setupReply{err: err}
		}
		return setupReply{list: list}
	})
}

func (s *providerSetup) Update(message tea.Msg) (*providerSetup, tea.Cmd) {
	switch msg := message.(type) {
	case setupInputMsg:
		if msg.owner != s || msg.request != s.request || msg.field != s.formInputField() || msg.mode != s.mode || s.done || !s.editing() {
			return s, nil
		}
		if s.mode == "provider_form" {
			return s, s.updateFormInput(msg.message)
		}
		var cmd tea.Cmd
		before := s.input.Value()
		s.input, cmd = s.input.Update(msg.message)
		if s.input.Value() != before {
			s.selected = 0
		}
		return s, s.inputCommand(cmd)
	case tea.WindowSizeMsg:
		s.width = max(20, min(68, msg.Width-4))
		s.input.SetWidth(max(10, s.width-4))
	case setupPoll:
		if msg.owner != s || msg.request != s.request || msg.flowID != s.login.FlowID ||
			s.busy || s.done || s.mode != "login" || s.ctx.Err() != nil {
			return s, nil
		}
		host, id := s.host, s.login.FlowID
		return s, s.call("login", func(ctx context.Context) setupReply {
			status, err := host.LoginStatus(ctx, id)
			return setupReply{login: status, err: err}
		})
	case setupReply:
		if msg.owner != s || msg.request != s.request || s.done || s.ctx.Err() != nil {
			return s, nil
		}
		s.busy = false
		if msg.err != nil {
			if strings.HasPrefix(msg.kind, "provider-") {
				return s, s.providerFailure(msg)
			}
			s.message = msg.err.Error()
			if msg.kind == "selected" {
				s.mode, s.autoModel = "models", false
				s.message += ". Press ctrl+r to refresh, then select a model to retry."
			} else if msg.kind != "key" {
				s.message += " — retry when ready. Your draft is preserved."
			}
			if msg.kind == "login" && s.login.FlowID != "" {
				return s, s.poll()
			}
			return s, nil
		}
		s.message = ""
		if strings.HasPrefix(msg.kind, "provider-") {
			return s, s.providerReply(msg)
		}
		switch msg.kind {
		case "inventory", "inventory-connected", "inventory-saved", "inventory-managed":
			selectedID := s.selectedProviderID()
			s.list = msg.list
			if s.list.Selection == nil {
				s.mode, s.message = "unsupported", errProviderReadinessUnavailable.Error()
				return s, nil
			}
			if s.automatic && s.list.Selection.Ready {
				s.model, s.provider = s.list.Selection.Model, s.list.Selection.Provider
				s.done, s.chosen = true, true
				return s, nil
			}
			s.automatic = false
			if msg.kind == "inventory-saved" || msg.kind == "inventory-managed" {
				return s, s.afterProviderRefresh(msg.kind)
			}
			if s.pickProvider != "" {
				s.provider, s.pickProvider = s.pickProvider, ""
				if entry := s.entry(); entry != nil {
					if s.pickKey && slices.Contains(entry.Methods, "api_key") {
						s.pickKey, s.mode = false, "key"
						s.input.EchoMode = textinput.EchoPassword
						return s, nil
					}
					return s, s.connect(*entry)
				}
				s.input.Reset()
				s.input.EchoMode = textinput.EchoNormal
				s.pickKey = false
				s.message = "Provider " + s.provider + " was not found on this host. Choose a provider below."
			}
			if msg.kind == "inventory-connected" {
				return s, s.prepareModel()
			}
			s.mode, s.selected = "providers", 0
			s.restoreProviderSelection(selectedID)
		case "catalogs":
			s.catalogs = msg.catalogs
			s.list = msg.list
			s.message = ""
			if s.list.Selection == nil {
				s.mode, s.message = "unsupported", errProviderReadinessUnavailable.Error()
				return s, nil
			}
			if failure := msg.catalogs.Errors[s.provider]; failure != "" {
				s.message = "Model list unavailable: " + failure + ". Press ctrl+r to retry."
			}
			if s.autoModel {
				return s, s.selectDefaultModel()
			}
			s.mode = "models"
			s.selected = max(0, slices.Index(s.modelOptions(), s.model))
		case "login":
			return s, s.applyLogin(msg.login)
		case "key":
			s.notice = ""
			if s.provider == s.selectionProvider {
				s.reloadProvider = s.provider
			}
			return s, s.refresh("-connected")
		case "selected":
			s.done, s.chosen = true, true
		}
	case tea.KeyPressMsg:
		return s, s.keypress(msg)
	case tea.PasteMsg:
		if s.mode == "provider_form" && !s.busy {
			return s, s.updateFormInput(msg)
		}
		if s.editing() {
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			s.selected = 0
			return s, s.inputCommand(cmd)
		}
	}
	return s, nil
}

func (s *providerSetup) entry() *protocol.ProviderEntry {
	for i := range s.list.Providers {
		if s.list.Providers[i].ID == s.provider {
			return &s.list.Providers[i]
		}
	}
	return nil
}

func (s *providerSetup) prepareModel() tea.Cmd {
	s.input.Reset()
	s.input.EchoMode = textinput.EchoNormal
	s.selected = 0
	s.message, s.effort = "", ""
	s.autoModel = false
	entry := s.entry()
	if entry == nil {
		s.mode = "providers"
		return nil
	}
	if preset, ok := setupDefaultPreset(s.provider); ok && setupCanAttempt(*entry) {
		s.autoModel = true
		s.model = ""
		s.mode = "models"
		return s.loadCatalogs(entry.SuggestedModel != preset.SuggestedModels[0])
	}
	s.model = entry.SuggestedModel
	if s.list.Selection != nil && s.list.Selection.Ready && s.list.Selection.Provider == s.provider {
		s.model = s.list.Selection.Model
	}
	s.mode = "models"
	return s.loadCatalogs(false)
}

func (s *providerSetup) loadCatalogs(refresh bool) tea.Cmd {
	host, model, provider, selected := s.host, s.selectionModel, s.selectionProvider, s.provider
	return s.call("catalogs", func(ctx context.Context) setupReply {
		catalogs, err := host.ProviderCatalogsFor(ctx, selected, refresh)
		if err != nil {
			return setupReply{err: err}
		}
		list, err := host.ListProvidersFor(ctx, model, provider)
		return setupReply{catalogs: catalogs, list: list, err: err}
	})
}

func (s *providerSetup) modelOptions() []string {
	models := []string{}
	for name, model := range s.catalogs.Models {
		if slices.Contains(model.Providers, s.provider) {
			models = append(models, name)
		}
	}
	for _, model := range s.catalogs.Catalogs[s.provider].Models {
		if !slices.Contains(models, model.ID) {
			models = append(models, model.ID)
		}
	}
	slices.Sort(models)
	filter := strings.ToLower(s.input.Value())
	return slices.DeleteFunc(models, func(model string) bool { return !strings.Contains(strings.ToLower(model), filter) })
}

func (s *providerSetup) connect(entry protocol.ProviderEntry) tea.Cmd {
	return s.connectMethod(entry, "")
}

func (s *providerSetup) connectMethod(entry protocol.ProviderEntry, method string) tea.Cmd {
	s.provider, s.model, s.effort, s.selected = entry.ID, "", "", 0
	s.autoModel = false
	s.notice, s.message, s.loginFeedback = "", "", ""
	s.login, s.openedURL = daemon.ProviderLoginStatus{}, ""
	s.pickKey = false
	s.input.Reset()
	s.input.EchoMode = textinput.EchoNormal
	if method == "" && setupCanAttempt(entry) {
		return s.prepareModel()
	}
	if method != "api_key" && slices.Contains(entry.Methods, "login") {
		s.mode = "login"
		host := s.host
		return s.call("login", func(ctx context.Context) setupReply {
			flows, err := host.ListLogins(ctx)
			if err != nil {
				return setupReply{err: err}
			}
			for _, flow := range flows.Flows {
				if flow.Provider == entry.ID && setupLoginActive(flow.State) {
					return setupReply{login: flow}
				}
			}
			status, err := host.BeginProviderLogin(ctx, entry.ID)
			return setupReply{login: status, err: err}
		})
	}
	if slices.Contains(entry.Methods, "api_key") {
		s.mode = "key"
		s.input.EchoMode = textinput.EchoPassword
		return nil
	}
	s.message = "This connection needs configuration on the execution host. Use Settings or edit the host config, then ctrl+r."
	return nil
}

func setupLoginActive(state string) bool {
	return state != "succeeded" && state != "failed" && state != "expired" && state != "cancelled" && state != "interrupted"
}

func (s *providerSetup) applyLogin(status daemon.ProviderLoginStatus) tea.Cmd {
	s.login = status
	if status.VerificationURL != "" && s.openedURL != status.VerificationURL {
		s.openedURL = status.VerificationURL
		openBrowserURL(status.VerificationURL)
	}
	s.selected = 0
	switch status.State {
	case "choose_team":
		s.mode = "teams"

	case "choose_project":
		s.mode = "projects"

	case "succeeded":
		if s.provider == s.selectionProvider {
			s.reloadProvider = s.provider
		}
		s.mode = "models"
		return s.refresh("-connected")
	case "failed", "expired", "cancelled", "interrupted":
		s.mode = "providers"
		s.message = "Sign-in " + status.State + ". " + status.Error
	default:
		s.mode = "login"
		return s.poll()
	}
	return nil
}

func (s *providerSetup) poll() tea.Cmd {
	msg := setupPoll{owner: s, request: s.request, flowID: s.login.FlowID}
	return tea.Tick(750*time.Millisecond, func(time.Time) tea.Msg { return msg })
}

func (s *providerSetup) chooseLogin(choice string, project bool) tea.Cmd {
	host, id := s.host, s.login.FlowID
	return s.call("login", func(ctx context.Context) setupReply {
		var status daemon.ProviderLoginStatus
		var err error
		if project {
			status, err = host.SelectLoginProject(ctx, id, choice)
		} else {
			status, err = host.SelectLoginTeam(ctx, id, choice)
		}
		return setupReply{login: status, err: err}
	})
}

func (s *providerSetup) useModel() tea.Cmd {
	if s.model == "" || s.provider == "" {
		return nil
	}
	s.mode = "models"
	s.input.Reset()
	s.selected = max(0, slices.Index(s.modelOptions(), s.model))
	host, model, provider, revision := s.host, s.model, s.provider, s.list.Revision
	persist := s.persistDefault
	var effort *string
	if s.effort != "" {
		effort = new(s.effort)
	}
	return s.call("selected", func(ctx context.Context) setupReply {
		if !persist {
			return setupReply{}
		}
		_, err := host.UpdateConfiguration(ctx, daemon.ConfigurationUpdate{
			Revision: revision, DefaultModel: &model, DefaultProvider: &provider,
			DefaultEffort: effort,
		})
		return setupReply{err: err}
	})
}

func (s *providerSetup) editing() bool {
	return s.mode == "provider_form" || s.mode == "providers" || s.mode == "models" || s.mode == "key" || s.mode == "project_name"
}

func (s *providerSetup) keypress(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+c" {
		s.close()
		return nil
	}
	if s.providerMode() {
		return s.providerKeypress(msg)
	}
	if key == "esc" {
		s.pickProvider, s.pickKey = "", false
		s.message = ""
		s.input.Reset()
		s.input.EchoMode = textinput.EchoNormal
		if s.mode == "login" || s.mode == "teams" || s.mode == "projects" || s.mode == "project_name" {
			if s.login.FlowID != "" {
				host, id := s.host, s.login.FlowID
				return s.call("login", func(ctx context.Context) setupReply {
					status, err := host.CancelLogin(ctx, id)
					return setupReply{login: status, err: err}
				})
			}
		}
		// An observation can close while an RPC finishes; ignore that reply.
		s.request++
		if s.cancelCall != nil {
			s.cancelCall()
		}
		s.busy = false
		if s.mode == "providers" || s.mode == "unsupported" {
			s.close()
			return nil
		} else {
			s.mode, s.selected = "providers", 0
		}
		return nil
	}
	if s.mode == "login" && (key == "o" || key == "c") {
		if key == "o" && s.login.VerificationURL != "" {
			if !openBrowserURL(s.login.VerificationURL) {
				s.loginFeedback = "Open the link above in your browser."
			}
		}
		if key == "c" && s.login.UserCode != "" {
			copyText(s.login.UserCode)
			s.loginFeedback = "Code copied."
		}
		return nil
	}
	if s.busy {
		return nil
	}
	if key == "ctrl+e" && s.mode == "providers" {
		entries := s.entries()
		if s.selected < len(entries) {
			return s.chooseProvider(entries[s.selected], true)
		}
		return nil
	}
	if key == "ctrl+r" {
		if s.mode == "key" {
			s.input.Reset()
			s.input.EchoMode = textinput.EchoNormal
		}
		if s.mode == "models" {
			return s.loadCatalogs(true)
		}
		return s.refresh("")
	}
	if key == "ctrl+k" && s.mode == "providers" {
		entries := s.entries()
		if s.selected < len(entries) {
			entry := entries[s.selected]
			if slices.Contains(entry.Methods, "api_key") {
				s.provider = entry.ID
				s.input.Reset()
				s.input.EchoMode = textinput.EchoPassword
				s.mode = "key"
			}
		}
		return nil
	}
	count := s.optionCount()
	if key == "up" && count > 0 {
		s.selected = (s.selected + count - 1) % count
		return nil
	}
	if key == "down" && count > 0 {
		s.selected = (s.selected + 1) % count
		return nil
	}
	if key == "enter" {
		s.message = ""
		switch s.mode {
		case "providers":
			entries := s.entries()
			if s.selected == len(entries) {
				s.openCustomProvider()
				return nil
			}
			if len(entries) > 0 {
				return s.chooseProvider(entries[min(s.selected, len(entries)-1)], false)
			}
		case "methods":
			if s.selected < len(s.methods) {
				method := s.methods[s.selected]
				if s.manageMethods || method.entry.Status.Disabled {
					s.provider = method.entry.ID
					return s.readProvider("manage")
				}
				return s.connectMethod(method.entry, method.method)
			}
		case "models":
			models := s.modelOptions()
			if s.selected == len(models) {
				s.effort, s.autoModel = "", false
				return s.readProvider("manual")
			}
			if len(models) > 0 {
				chosen := models[min(s.selected, len(models)-1)]
				if chosen != s.model {
					s.effort = ""
				}
				s.model = chosen
				return s.useModel()
			}
		case "key":
			key := config.TrimKey(s.input.Value())
			s.input.Reset()
			if key == "" {
				s.message = "Paste an API key to continue."
				return nil
			}
			host, revision, provider := s.host, s.list.Revision, s.provider
			return s.call("key", func(ctx context.Context) setupReply {
				_, err := host.SetProviderKey(ctx, daemon.ProviderKeySetup{Revision: revision, Provider: provider, Key: key})
				return setupReply{err: err}
			})
		case "teams":
			if len(s.login.Teams) > 0 {
				return s.chooseLogin(s.login.Teams[s.selected].ID, false)
			}
		case "projects":
			if s.selected == len(s.login.Projects) {
				s.mode = "project_name"
				s.input.Reset()
				return nil
			}
			return s.chooseLogin(s.login.Projects[s.selected].ID, true)
		case "project_name":
			name := strings.TrimSpace(s.input.Value())
			if name == "" {
				return nil
			}
			host, id := s.host, s.login.FlowID
			return s.call("login", func(ctx context.Context) setupReply {
				status, err := host.CreateLoginProject(ctx, id, name)
				return setupReply{login: status, err: err}
			})

		}
		return nil
	}
	if s.editing() {
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		s.selected = 0
		return s.inputCommand(cmd)
	}
	return nil
}

func (s *providerSetup) optionCount() int {
	switch s.mode {
	case "providers":
		return len(s.entries()) + 1
	case "methods":
		return len(s.methods)
	case "models":
		return len(s.modelOptions()) + 1
	case "teams":
		return len(s.login.Teams)
	case "projects":
		return len(s.login.Projects) + 1
	}
	return 0
}

func (s *providerSetup) body() string {
	if s.providerMode() {
		return strings.Join(s.providerFormRows(s.width, 0), "\n")
	}
	var body strings.Builder
	line := func(text string) { body.WriteString(text + "\n") }
	switch s.mode {
	case "unsupported":
		line("Update this execution host")
		line("Provider setup requires a newer host version.")
		line("ctrl+r check again · Esc close")
	case "providers":
		return strings.Join(s.providerRows(s.width, 0), "\n")
	case "methods":
		return strings.Join(s.methodRows(s.width, 0), "\n")
	case "models", "teams", "projects":
		return strings.Join(s.choiceRows(s.width, 0), "\n")
	case "key":
		return strings.Join(s.keyRows(s.width, 0), "\n")
	case "login":
		return strings.Join(s.loginRows(s.width, 0), "\n")
	case "project_name":
		return strings.Join(s.projectNameRows(s.width, 0), "\n")

	}
	if s.busy {
		line("")
		line("Working…")
	}
	if s.message != "" {
		line("")
		line(s.message)
	}
	return wrap(body.String(), s.width)
}

func setupCanAttempt(entry protocol.ProviderEntry) bool {
	if entry.Status.Disabled {
		return false
	}
	if entry.Status.Available != nil && *entry.Status.Available {
		return true
	}
	return entry.Status.AuthState == "unchecked"
}

func setupProviderDescription(entry protocol.ProviderEntry) string {
	if setupCanAttempt(entry) {
		switch entry.Status.KeySource {
		case "none":
			return "No authentication required"
		case "environment":
			return "Found in this host's environment: " + entry.Status.EnvironmentVariable
		case "command":
			return "Configured credential command; checked when a request is made"
		case "env_file":
			return "Environment file on this host: " + entry.Status.CredentialPath
		case "key_file":
			return "Key file on this host: " + entry.Status.CredentialPath
		case "external":
			return "Managed by Inference CLI on this host"
		default:
			return "Saved connection on this host"
		}
	}
	if entry.Status.AuthState == "setup_required" {
		return "Finish connecting with saved credentials"
	}
	if len(entry.Status.Warnings) > 0 {
		return strings.Join(entry.Status.Warnings, " ")
	}
	if slices.Contains(entry.Methods, "login") {
		if entry.ID == "openai-codex" {
			return "Use your ChatGPT subscription's Codex access"
		}
		return "Sign in with your browser"
	}
	if slices.Contains(entry.Methods, "api_key") {
		return "Paste an API key"
	}
	return "Manage configuration on this host"
}

// The same component runs as a floating dialog after a session exists.
func (s *providerSetup) key(m *model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	_, cmd := s.Update(msg)
	return m, m.finishProviderSetup(cmd)
}

func (s *providerSetup) rows(m *model) []string {
	s.width = m.dialogWidth()
	s.input.SetWidth(max(10, s.width-6))
	if s.providerMode() {
		return s.providerFormRows(s.width, m.dialogHeight())
	}
	switch s.mode {
	case "providers":
		return s.providerRows(s.width, m.dialogHeight())
	case "methods":
		return s.methodRows(s.width, m.dialogHeight())
	case "key":
		return s.keyRows(s.width, m.dialogHeight())
	case "login":
		return s.loginRows(s.width, m.dialogHeight())
	case "project_name":
		return s.projectNameRows(s.width, m.dialogHeight())
	case "models", "teams", "projects":
		return s.choiceRows(s.width, m.dialogHeight())
	}
	s.width -= 4
	th := currentTheme()
	rows := strings.Split(s.body(), "\n")
	for i, line := range rows {
		rows[i] = ui.PadRow(th.On(th.Text, th.Surface.Panel).Render("  "+ansi.Truncate(line, s.width, "…")), s.width+4, th.Surface.Panel)
	}
	return rows
}

func (s *providerSetup) choiceRows(width, height int) []string {
	title, detail, empty := "Choose a model", "", "No matching models. ctrl+r refresh"
	options := s.modelOptions()
	if s.mode == "models" {
		options = append(options, "Enter model manually…")
	}
	if s.mode != "models" {
		title, detail, empty = "Choose workspace", s.login.Email, "No workspaces available"
		choices := s.login.Teams
		if s.mode == "projects" {
			title, choices = "Choose project", s.login.Projects
		}
		options = nil
		for _, choice := range choices {
			options = append(options, choice.Name)
		}
		if s.mode == "projects" {
			options = append(options, "Create a new project…")
		}
	}
	items := make([]ui.ListItem, len(options))
	for i, option := range options {
		items[i] = ui.ListItem{Left: option}
	}
	if s.busy {
		detail = "Working…"
	}
	if s.message != "" {
		detail = s.message
	}
	footer := []string{"enter", "continue"}
	if s.mode == "models" {
		footer[1] = "use"
	}
	if width >= 50 {
		footer = append(footer, "↑↓", "choose")
	}
	return s.listRows(ui.List{
		Title: title, Hint: "esc", Search: s.mode == "models", SearchView: s.searchView(width),
		Groups: []ui.ListGroup{{Items: items}}, Sel: s.selected, Empty: empty, Footer: footer,
		Width: width, Height: height,
	}, detail)
}

func (s *providerSetup) searchView(width int) string {
	return setupInputView(s.input, "Search", max(width-4, 1))
}

func (s *providerSetup) listRows(list ui.List, detail string) []string {
	th := currentTheme()
	width, height := list.Width, list.Height
	var details []string
	if detail != "" {
		details = strings.Split(wrap(detail, max(width-4, 1)), "\n")
	}
	if height > 0 {
		if height < 12 {
			list.Footer = nil
		}
		budget := max(height-11, 1)
		if len(details) > budget {
			details = details[:budget]
			details[budget-1] = ansi.Truncate(details[budget-1], max(width-5, 1), "") + "…"
		}
		if len(details) > 0 {
			list.Height = max(height-len(details)-1, 1)
		}
		if height < 10 {
			details, list.Height = nil, height
		}
	}
	rows := list.Render(th)
	for _, line := range details {
		rows = append(rows, ui.PadRow(th.On(th.Muted, th.Surface.Panel).Render("  "+line), width, th.Surface.Panel))
	}
	if len(details) > 0 {
		rows = append(rows, ui.PadRow("", width, th.Surface.Panel))
	}
	for i, row := range rows {
		rows[i] = ui.PadRow(ansi.Truncate(row, width, ""), width, th.Surface.Panel)
	}
	return rows
}

func (m *model) openProviderSetup() tea.Cmd { return m.openProviderSetupFor("") }

func (m *model) openProviderSetupFor(provider string) tea.Cmd {
	if m.clientClosed {
		return m.toastError("Connection ended. Use /quit and relaunch Whip to retry.")
	}
	if m.startup != nil && (m.startup.creating || m.startup.preparing) {
		return m.toastError("Opening your session…")
	}
	if m.providerSetup != nil {
		m.providerSetup.automatic = false
		return nil
	}
	m.palette = nil
	m.providerSetup = newProviderSetup(m.setupContext(), m.client, m.beforeSession())
	m.providerSetup.pickProvider = provider
	m.providerSetup.selectionModel, m.providerSetup.selectionProvider = m.modelName, m.provName
	m.providerSetup.persistDefault = m.beforeSession() && m.modelName == "" && m.provName == ""
	return m.providerSetup.Init()
}

func (m *model) finishProviderSetup(cmd tea.Cmd) tea.Cmd {
	s := m.providerSetup
	if s != nil && m.startup != nil && !s.automatic {
		// A launch prompt becomes an ordinary draft when setup is needed.
		m.initialPrompt = ""
	}
	if s == nil || !s.done {
		return cmd
	}
	m.providerSetup = nil
	s.close()
	if s.reload {
		_, reload := m.submitClientAction("session.reload", protocol.Empty{}, "")
		return tea.Batch(cmd, reload)
	}
	if !s.chosen {
		return cmd
	}
	if m.beforeSession() {
		m.startup.effort = s.effort
		return m.startFirstSession(s.model, s.provider)
	}
	if s.reloadProvider == s.provider && s.model == s.selectionModel && s.provider == s.selectionProvider && s.effort == "" {
		_, reload := m.submitClientAction("session.reload", protocol.Empty{}, "")
		return tea.Batch(cmd, reload)
	}
	_, change := m.submitClientAction("session.model", protocol.ModelParams{Model: s.model, Provider: s.provider, Effort: s.effort}, "")
	return tea.Batch(cmd, change)
}
