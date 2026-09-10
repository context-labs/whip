package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
)

type setupTestHost struct {
	setupHost
	list                            protocol.ProviderList
	flows                           []daemon.ProviderLoginStatus
	writes                          []daemon.ConfigurationUpdate
	key                             string
	began                           []string
	cancelled                       []string
	selectedModel, selectedProvider string
	catalogProvider                 string
	catalogs                        protocol.ProviderCatalogsResult
	writeErr                        error
}

func (h *setupTestHost) ListProviders(context.Context) (protocol.ProviderList, error) {
	return h.list, nil
}
func (h *setupTestHost) ListProvidersFor(_ context.Context, model, provider string) (protocol.ProviderList, error) {
	h.selectedModel, h.selectedProvider = model, provider
	return h.list, nil
}
func (h *setupTestHost) DiscoverProviders(ctx context.Context, model, provider string) (protocol.ProviderList, error) {
	return h.ListProvidersFor(ctx, model, provider)
}
func (h *setupTestHost) ListLogins(context.Context) (daemon.ProviderLoginList, error) {
	return daemon.ProviderLoginList{Flows: h.flows}, nil
}
func (h *setupTestHost) UpdateConfiguration(_ context.Context, p daemon.ConfigurationUpdate) (daemon.RuntimeConfiguration, error) {
	h.writes = append(h.writes, p)
	return daemon.RuntimeConfiguration{}, h.writeErr
}
func (h *setupTestHost) SetProviderKey(_ context.Context, p daemon.ProviderKeySetup) (daemon.RuntimeConfiguration, error) {
	h.key = p.Key
	for i := range h.list.Providers {
		if h.list.Providers[i].ID == p.Provider {
			h.list.Providers[i].Status.Available = new(true)
			h.list.Providers[i].Status.KeySource = "literal"
			h.list.Providers[i].SuggestedModel = "coding-model"
		}
	}
	h.list.Revision = "after-key"
	return daemon.RuntimeConfiguration{Revision: h.list.Revision}, nil
}
func (h *setupTestHost) BeginProviderLogin(_ context.Context, provider string) (daemon.ProviderLoginStatus, error) {
	h.began = append(h.began, provider)
	flow := daemon.ProviderLoginStatus{Provider: provider, FlowID: "flow", State: "pending"}
	h.flows = append(h.flows, flow)
	return flow, nil
}
func (h *setupTestHost) CancelLogin(_ context.Context, id string) (daemon.ProviderLoginStatus, error) {
	h.cancelled = append(h.cancelled, id)
	return daemon.ProviderLoginStatus{FlowID: id, State: "cancelled"}, nil
}
func (h *setupTestHost) ProviderCatalogsFor(_ context.Context, provider string, _ bool) (protocol.ProviderCatalogsResult, error) {
	h.catalogProvider = provider
	if h.catalogs.Models != nil {
		return h.catalogs, nil
	}
	return protocol.ProviderCatalogsResult{Catalogs: map[string]config.Catalog{"openrouter": {Models: []config.ModelInfoLite{{ID: "other-model"}, {ID: "coding-model"}}}}}, nil
}
func setupFixture() protocol.ProviderList {
	return protocol.ProviderList{Revision: "original", Selection: &protocol.ProviderSelection{Reason: "provider_unavailable"}, Providers: []protocol.ProviderEntry{
		{ID: "inference-net", Name: "Inference.net", Category: "Popular", Recommended: true, Methods: []string{"login", "api_key"}, Status: protocol.ProviderStatus{Available: new(false)}},
		{ID: "openrouter", Name: "OpenRouter", Category: "Popular", Methods: []string{"api_key"}, Status: protocol.ProviderStatus{Available: new(false)}},
		{ID: "openai-codex", Name: "OpenAI / ChatGPT", Category: "Popular", Methods: []string{"login"}, Status: protocol.ProviderStatus{Available: new(false)}},
	}}
}
func setupKey(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
}
func applySetupCommand(t *testing.T, s *providerSetup, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected an operation")
	}
	_, next := s.Update(cmd())
	return next
}
func newTestSetup(t *testing.T, h *setupTestHost, startup bool) *providerSetup {
	t.Helper()
	s := newProviderSetup(t.Context(), h, startup)
	t.Cleanup(s.cancel)
	return s
}

func TestSetupUsableRouteSkipsQuestionsAndPreservesExplicitSelection(t *testing.T) {
	h := &setupTestHost{list: setupFixture()}
	h.list.Selection = &protocol.ProviderSelection{Ready: true, Model: "chosen-model", Provider: "openrouter"}
	s := newTestSetup(t, h, true)
	s.automatic = true
	s.selectionModel, s.selectionProvider = "chosen-model", "openrouter"
	cmd := applySetupCommand(t, s, s.Init())
	if !s.done || !s.chosen || cmd != nil || s.model != "chosen-model" || s.provider != "openrouter" {
		t.Fatal("ready route did not return to the normal TUI")
	}
	if h.selectedModel != "chosen-model" || h.selectedProvider != "openrouter" || len(h.writes) != 0 {
		t.Fatalf("explicit route changed: %+v", h)
	}
}
func TestSetupDetectedProviderKeepsStableOrderAndCheckmark(t *testing.T) {
	h := &setupTestHost{list: setupFixture()}
	h.list.Providers[1].Status = protocol.ProviderStatus{Available: new(true), KeySource: "environment", EnvironmentVariable: "OPENROUTER_API_KEY"}
	h.list.Providers[1].SuggestedModel = "coding-model"
	s := newTestSetup(t, h, false)
	applySetupCommand(t, s, s.Init())
	entries := s.entries()
	if entries[0].ID != "inference-net" || entries[1].ID != "openrouter" {
		t.Fatalf("provider order: %+v", entries)
	}
	view := ansi.Strip(s.body())
	if strings.Count(view, "Recommended") != 1 || !strings.Contains(view, "✓ OpenRouter") {
		t.Fatalf("missing truthful metadata: %s", view)
	}
	s.input.SetValue("openrouter")
	if len(s.entries()) != 1 || s.entries()[0].ID != "openrouter" {
		t.Fatal("recommendation displaced search relevance")
	}
}
func TestSetupCleanHomeHasAllPresetsAndNoPreferenceQuestionnaire(t *testing.T) {
	s := newTestSetup(t, &setupTestHost{list: setupFixture()}, true)
	applySetupCommand(t, s, s.Init())
	view := ansi.Strip(s.body())
	for _, name := range []string{"Inference.net", "OpenRouter", "OpenAI / ChatGPT"} {
		if !strings.Contains(view, name) {
			t.Fatalf("missing %s", name)
		}
	}
	for _, removed := range []string{"thinking", "MCP", "Setup complete"} {
		if strings.Contains(view, removed) {
			t.Fatalf("unexpected startup question: %s", removed)
		}
	}
	if s.mode != "providers" || s.entries()[0].ID != "inference-net" {
		t.Fatalf("initial provider chooser: %+v", s)
	}
}
func TestSetupSoleDetectedRouteRequiresProviderChoice(t *testing.T) {
	h := &setupTestHost{list: setupFixture(), catalogs: setupDefaultCatalogs()}
	h.list.Providers[1].Status.Available = new(true)
	s := newTestSetup(t, h, true)
	applySetupCommand(t, s, s.Init())
	if s.done || len(h.writes) != 0 {
		t.Fatal("detected key selected without user choice")
	}
	s.input.SetValue("openrouter")
	providerFormDrain(t, s, s.keypress(setupKey("enter")))
	if !s.done || !s.chosen || s.model != "z-ai/glm-5.3" || s.effort != "max" || len(h.writes) != 1 {
		t.Fatalf("provider choice did not finish setup: %+v", s)
	}
}

func TestSetupMasksAndClearsKeyThenOffersModels(t *testing.T) {
	h := &setupTestHost{list: setupFixture()}
	s := newTestSetup(t, h, true)
	applySetupCommand(t, s, s.Init())
	s.connect(s.list.Providers[1])
	s.input.SetValue("super-secret-key")
	if strings.Contains(s.body(), "super-secret-key") {
		t.Fatal("key exposed in view")
	}
	cmd := s.keypress(setupKey("enter"))
	if s.input.Value() != "" {
		t.Fatal("submitted key retained in UI")
	}
	next := applySetupCommand(t, s, cmd)
	providerFormDrain(t, s, next)
	if h.key != "super-secret-key" || s.mode != "models" || s.done || s.provider != "openrouter" || len(h.writes) != 0 {
		t.Fatalf("auth changed defaults or did not hand off: %s %+v", s.mode, h.writes)
	}
	if strings.Contains(s.body(), h.key) {
		t.Fatal("secret escaped or draft lost")
	}
}
func TestSetupChoosingModelFinishesImmediately(t *testing.T) {
	h := &setupTestHost{list: setupFixture()}
	h.list.Providers[1].Status.Available = new(true)
	h.list.Providers[1].SuggestedModel = "coding-model"
	s := newTestSetup(t, h, true)
	applySetupCommand(t, s, s.Init())
	s.provider, s.mode = "openrouter", "models"
	providerFormDrain(t, s, s.loadCatalogs(false))
	if s.mode != "models" || len(s.modelOptions()) != 2 || h.catalogProvider != "openrouter" {
		t.Fatalf("model picker not retained: %s %+v", s.mode, s.modelOptions())
	}
	s.selected = 1
	providerFormDrain(t, s, s.keypress(setupKey("enter")))
	if s.model != "other-model" || !s.done || !s.chosen {
		t.Fatalf("selected model lost: %s %s", s.model, s.mode)
	}
}
func TestSetupConflictPreservesConnectionAndDraft(t *testing.T) {
	h := &setupTestHost{list: setupFixture(), writeErr: errors.New("configuration changed")}
	s := newTestSetup(t, h, true)
	s.list = h.list
	s.provider, s.model, s.mode = "openrouter", "coding-model", "models"
	applySetupCommand(t, s, s.useModel())
	if s.done || s.chosen || !strings.Contains(s.message, "configuration changed") {
		t.Fatalf("conflict not recoverable: %+v", s)
	}
	if s.mode != "models" || !strings.Contains(s.message, "ctrl+r") {
		t.Fatal("failed selection did not offer recovery in the model picker")
	}
	h.writeErr = nil
	h.list.Revision = "refreshed"
	providerFormDrain(t, s, s.keypress(providerFormKey("ctrl+r")))
	if s.done || s.model != "coding-model" || s.message != "" {
		t.Fatal("refresh lost the model or accepted it without a new selection")
	}
	providerFormDrain(t, s, s.keypress(setupKey("enter")))
	if !s.done || !s.chosen || len(h.writes) != 2 || h.writes[1].Revision != "refreshed" || *h.writes[1].DefaultModel != "coding-model" {
		t.Fatal("selection retry did not save against the refreshed revision")
	}
}
func TestSetupSessionChoiceDoesNotRewriteDefaults(t *testing.T) {
	h := &setupTestHost{list: setupFixture()}
	s := newTestSetup(t, h, false)
	s.provider, s.model = "openrouter", "coding-model"
	applySetupCommand(t, s, s.useModel())
	if !s.chosen || len(h.writes) != 0 {
		t.Fatalf("session choice wrote host defaults: %+v", h.writes)
	}
}
func TestSetupEscClosesDialogWithoutCreatingOrConfiguringSession(t *testing.T) {
	h := &setupTestHost{list: setupFixture()}
	s := newTestSetup(t, h, true)
	applySetupCommand(t, s, s.Init())
	if cmd := s.keypress(setupKey("esc")); cmd != nil || !s.done || s.chosen || len(h.writes) != 0 {
		t.Fatal("Esc did not close the dialog without configuring a session")
	}
}
func TestSetupReusesActiveLoginAndCancelsOnlyThatFlow(t *testing.T) {
	h := &setupTestHost{list: setupFixture(), flows: []daemon.ProviderLoginStatus{{Provider: "openai-codex", FlowID: "existing", State: "pending"}}}
	s := newTestSetup(t, h, true)
	applySetupCommand(t, s, s.Init())
	applySetupCommand(t, s, s.connect(s.list.Providers[2]))
	if s.login.FlowID != "existing" || len(h.began) != 0 {
		t.Fatalf("duplicate login begun: %+v", h.began)
	}
	applySetupCommand(t, s, s.keypress(setupKey("esc")))
	if len(h.cancelled) != 1 || h.cancelled[0] != "existing" {
		t.Fatalf("wrong flow cancelled: %+v", h.cancelled)
	}
}
func TestSetupStaleRepliesCannotRestoreClosedOrReplacedFlow(t *testing.T) {
	s := newTestSetup(t, &setupTestHost{list: setupFixture()}, true)
	s.request = 2
	s.mode = "providers"
	s.Update(setupReply{owner: s, request: 1, kind: "login", login: daemon.ProviderLoginStatus{State: "pending", FlowID: "old"}})
	if s.mode != "providers" || s.login.FlowID != "" {
		t.Fatal("stale reply restored old flow")
	}
	s.done = true
	s.Update(setupReply{owner: s, request: 2, kind: "selected"})
	if s.chosen {
		t.Fatal("closed setup accepted a late selection")
	}
}
func TestSetupNarrowScreenWrapsInstructions(t *testing.T) {
	s := newTestSetup(t, &setupTestHost{list: setupFixture()}, true)
	applySetupCommand(t, s, s.Init())
	s.Update(tea.WindowSizeMsg{Width: 32, Height: 30})
	for _, line := range strings.Split(s.body(), "\n") {
		if ansi.StringWidth(line) > 28 {
			t.Fatalf("line exceeds narrow screen: %q", line)
		}
	}
}
func TestConnectAndAuthOpenSharedChooserWithoutClearingDraft(t *testing.T) {
	for _, command := range []string{"/connect", "/auth"} {
		t.Run(command, func(t *testing.T) {
			m := authTestModel(t)
			m.input.SetValue("a pending draft")
			m.palette = &palette{}
			m.thinCommand(command)
			if m.providerSetup == nil || m.palette != nil || m.input.Value() != "a pending draft" {
				t.Fatal("chooser not opened or draft discarded")
			}
			m.providerSetup.cancel()
		})
	}
}

func TestSetupDialogPasteCannotEnterSessionDraft(t *testing.T) {
	m := authTestModel(t)
	m.input.SetValue("a pending prompt")
	h := &setupTestHost{list: setupFixture()}
	s := newTestSetup(t, h, false)
	s.list = h.list
	s.connect(h.list.Providers[1])
	m.providerSetup = s
	m.Update(tea.PasteMsg{Content: "secret-from-clipboard"})
	if m.input.Value() != "a pending prompt" || s.input.Value() != "secret-from-clipboard" {
		t.Fatal("paste escaped setup dialog")
	}
	if strings.Contains(s.body(), "secret-from-clipboard") || strings.Contains(m.transcriptText(), "secret-from-clipboard") {
		t.Fatal("pasted secret was rendered")
	}
	s.keypress(setupKey("esc"))
	if s.input.Value() != "" || m.input.Value() != "a pending prompt" {
		t.Fatal("cancel did not clear secret and restore draft")
	}
}

func TestSetupCredentialCommandRemainsAnExplicitUsableChoice(t *testing.T) {
	h := &setupTestHost{list: setupFixture()}
	h.list.Providers[1].Status = protocol.ProviderStatus{Available: new(false), KeySource: "command", AuthState: "unchecked"}
	h.list.Providers[1].SuggestedModel = "coding-model"
	s := newTestSetup(t, h, true)
	applySetupCommand(t, s, s.Init())
	s.input.SetValue("openrouter")
	providerFormDrain(t, s, s.keypress(setupKey("enter")))
	if s.mode != "models" || s.provider != "openrouter" || s.model != "coding-model" || len(h.writes) != 0 {
		t.Fatal("credential command did not reach model choice")
	}
	providerFormDrain(t, s, s.keypress(setupKey("enter")))
	if !s.done || !s.chosen || len(h.writes) != 1 {
		t.Fatal("model choice did not finish without confirmation")
	}
}

func TestSetupCancellingBeginIgnoresItsLateReply(t *testing.T) {
	h := &setupTestHost{list: setupFixture()}
	s := newTestSetup(t, h, true)
	s.list = h.list
	cmd := s.connect(h.list.Providers[0])
	s.keypress(setupKey("esc"))
	applySetupCommand(t, s, cmd)
	if s.mode != "providers" || s.login.FlowID != "" {
		t.Fatal("late begin reopened cancelled view")
	}
}

func TestSetupReconnectObservesBeginThatFinishedWhileClosed(t *testing.T) {
	h := &setupTestHost{list: setupFixture()}
	s := newTestSetup(t, h, true)
	s.list = h.list
	begun := s.connect(h.list.Providers[0])
	s.keypress(setupKey("esc"))
	applySetupCommand(t, s, begun)
	applySetupCommand(t, s, s.connect(h.list.Providers[0]))
	if len(h.began) != 1 || s.login.FlowID != "flow" {
		t.Fatalf("duplicate reconnect login: %v %s", h.began, s.login.FlowID)
	}
}

func TestSetupUncheckedCommandWithoutSuggestionAllowsModelChoice(t *testing.T) {
	h := &setupTestHost{list: setupFixture(), catalogs: protocol.ProviderCatalogsResult{
		Models: map[string]protocol.ModelDescriptor{
			"coding-a": {Providers: []string{"openrouter"}},
			"coding-b": {Providers: []string{"openrouter"}},
		},
	}}
	h.list.Providers[1].Status = protocol.ProviderStatus{Available: new(false), KeySource: "command", AuthState: "unchecked"}
	s := newTestSetup(t, h, true)
	next := applySetupCommand(t, s, s.Init())
	if next != nil || h.catalogProvider != "" || h.key != "" || s.mode != "providers" {
		t.Fatal("inventory ran an unchecked credential command or catalog request")
	}
	applySetupCommand(t, s, s.connect(h.list.Providers[1]))
	if s.mode != "models" || h.catalogProvider != "openrouter" || len(s.modelOptions()) != 2 {
		t.Fatalf("unchecked command bypassed model choice: %s %+v", s.mode, s.modelOptions())
	}
	if !strings.Contains(setupProviderDescription(h.list.Providers[1]), "credential command") {
		t.Fatal("unchecked credential description lost")
	}
	s.selected = 1
	providerFormDrain(t, s, s.keypress(setupKey("enter")))
	if s.model != "coding-b" || !s.done || !s.chosen {
		t.Fatalf("wrong model selected: %s %s", s.model, s.mode)
	}
	if !s.chosen || len(h.writes) != 1 || *h.writes[0].DefaultModel != "coding-b" || *h.writes[0].DefaultProvider != "openrouter" {
		t.Fatal("model choice did not persist the chosen route")
	}
	if h.key != "" || h.list.Providers[1].Status.KeySource != "command" {
		t.Fatal("model selection replaced the configured credential command")
	}
}

func TestSetupOlderHostRequiresUpdateWithoutStartingConfirmation(t *testing.T) {
	h := &setupTestHost{list: setupFixture()}
	h.list.Selection = nil
	h.list.Providers[1].Status.Available = new(true)
	h.list.Providers[1].SuggestedModel = "coding-model"
	s := newTestSetup(t, h, false)
	applySetupCommand(t, s, s.Init())
	if s.mode != "unsupported" || !strings.Contains(s.body(), "Update this execution host") {
		t.Fatalf("older host entered model confirmation: %s", s.body())
	}
	if cmd := s.keypress(setupKey("enter")); cmd != nil || s.chosen || len(h.writes) != 0 {
		t.Fatal("older host attempted a doomed configuration write")
	}
	s.keypress(setupKey("esc"))
	if !s.done {
		t.Fatal("older-host warning did not dismiss")
	}
}

func TestSetupDelayedPollCannotReplaceNewLogin(t *testing.T) {
	h := &setupTestHost{list: setupFixture()}
	s := newTestSetup(t, h, true)
	s.list = h.list
	s.request = 1
	s.login = daemon.ProviderLoginStatus{FlowID: "old", State: "pending"}
	s.mode = "login"
	late := setupPoll{owner: s, request: s.request, flowID: s.login.FlowID}
	applySetupCommand(t, s, s.keypress(setupKey("esc")))
	begin := s.connect(h.list.Providers[0])
	request := s.request
	_, command := s.Update(late)
	if command != nil || s.request != request || s.login.FlowID != "" {
		t.Fatal("old poll displaced the new login request")
	}
	applySetupCommand(t, s, begin)
	if s.login.FlowID != "flow" || s.mode != "login" {
		t.Fatal("new login was discarded")
	}
}

func TestConnectInlineKeyUsesSharedMaskedDialog(t *testing.T) {
	m := authTestModel(t)
	m.input.SetValue("keep this draft")
	m.thinCommand("/auth openrouter inline-secret")
	s := m.providerSetup
	if s == nil || s.mode != "key" || strings.Contains(s.body(), "inline-secret") {
		t.Fatal("inline key did not open a masked dialog")
	}
	h := &setupTestHost{list: setupFixture()}
	s.host = h
	_, next := m.Update(s.refresh("")())
	if next != nil || s.mode != "key" || s.input.Value() != "inline-secret" || h.key != "" {
		t.Fatal("inline key bypassed masked confirmation")
	}
	if strings.Contains(s.body(), "inline-secret") || m.input.Value() != "keep this draft" {
		t.Fatal("inline key leaked or changed the draft")
	}
	_, command := m.Update(setupKey("enter"))
	m.Update(command())
	if h.key != "inline-secret" {
		t.Fatal("shared key confirmation did not save on the host")
	}
	s.cancel()
}

func TestConnectInlineKeyCancelAndRefreshNeverUnmasksInput(t *testing.T) {
	m := authTestModel(t)
	m.thinCommand("/auth openrouter inline-secret")
	s := m.providerSetup
	s.host = &setupTestHost{list: setupFixture()}
	m.Update(setupKey("esc"))
	m.Update(s.refresh("")())
	if s.mode != "providers" || s.pickKey || s.pickProvider != "" || s.input.Value() != "" {
		t.Fatal("cancel retained a deferred credential entry")
	}
	m.Update(setupKey("down"))
	m.Update(setupKey("enter"))
	m.Update(tea.PasteMsg{Content: "replacement-secret"})
	if s.mode != "key" || strings.Contains(s.body(), "replacement-secret") {
		t.Fatal("reopened credential input rendered a secret")
	}
	s.cancel()
}

func TestSetupChoiceListsKeepSelectionVisibleOnSmallTerminals(t *testing.T) {
	for _, mode := range []string{"providers", "models", "teams", "projects"} {
		for _, width := range []int{32, 48, 100} {
			t.Run(fmt.Sprintf("%s/%d", mode, width), func(t *testing.T) {
				m := compactCmdModel()
				m.Update(mkWinSize(width, 18))
				s := newTestSetup(t, &setupTestHost{}, true)
				s.mode, s.provider = mode, "test"
				s.catalogs.Models = make(map[string]protocol.ModelDescriptor)
				for i := range 40 {
					name := fmt.Sprintf("Option %02d", i)
					s.list.Providers = append(s.list.Providers, protocol.ProviderEntry{ID: name, Name: name})
					s.catalogs.Models[name] = protocol.ModelDescriptor{Providers: []string{"test"}}
					s.login.Teams = append(s.login.Teams, daemon.ProviderChoice{ID: name, Name: name})
					s.login.Projects = append(s.login.Projects, daemon.ProviderChoice{ID: name, Name: name})
				}
				s.selected = 39
				rows := s.rows(m)
				if len(rows) > m.dialogHeight() || !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "Option 39") {
					t.Fatalf("selection hidden or panel exceeds height: %d > %d\n%s", len(rows), m.dialogHeight(), strings.Join(rows, "\n"))
				}
				for _, row := range rows {
					if ansi.StringWidth(row) > m.dialogWidth() {
						t.Fatalf("panel exceeds width: %q", row)
					}
				}
				if mode == "projects" {
					s.selected++
					if !strings.Contains(ansi.Strip(strings.Join(s.rows(m), "\n")), "Create a new project") {
						t.Fatal("create-project option scrolled out of view")
					}
				}
			})
		}
	}
}

func TestSetupLongProviderErrorsFitTerminal(t *testing.T) {
	m := compactCmdModel()
	for _, height := range []int{14, 18, 24} {
		m.Update(mkWinSize(48, height))
		s := newTestSetup(t, &setupTestHost{}, true)
		s.list = setupFixture()
		s.message = strings.Repeat("Provider connection unavailable. ", 30)
		rows := s.rows(m)
		if len(rows) > m.dialogHeight() {
			t.Fatalf("provider error exceeds height: %d > %d", len(rows), m.dialogHeight())
		}
	}
}

func TestSetupFirstProviderHasNoAnotherConnectionHeading(t *testing.T) {
	s := newTestSetup(t, &setupTestHost{}, true)
	s.list = setupFixture()
	view := ansi.Strip(strings.Join(s.providerRows(64, 24), "\n"))
	if strings.Contains(view, "another provider") {
		t.Fatal("first connection implies a provider is already connected")
	}
}

func TestSetupSearchRendersFocusedInputAndFiltersFromKeyboard(t *testing.T) {
	m := authTestModel(t)
	m.Update(mkWinSize(100, 36))
	m.input.SetValue("keep my prompt")
	s := newTestSetup(t, &setupTestHost{}, true)
	s.list = setupFixture()
	m.providerSetup = s
	view := ansi.Strip(strings.Join(s.rows(m), "\n"))
	if !strings.Contains(view, "Search") || strings.Contains(view, "> Search") {
		t.Error("search does not look like an editable input")
	}
	for _, r := range "router" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if s.input.Value() != "router" || len(s.entries()) != 1 || s.entries()[0].ID != "openrouter" {
		t.Fatal("keyboard typing did not filter providers")
	}
	view = ansi.Strip(strings.Join(s.rows(m), "\n"))
	if !strings.Contains(view, "router") || strings.Contains(view, "> router") || strings.Contains(view, "Inference.net") {
		t.Fatal("search input or filtered results were not rendered")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if s.input.Value() != "route" {
		t.Fatal("search cannot be edited")
	}
	m.Update(setupKey("enter"))
	if s.mode != "key" || s.provider != "openrouter" || m.input.Value() != "keep my prompt" {
		t.Fatal("filtered connection did not open or search escaped into the draft")
	}
}

func TestSetupPastedSearchResetsSelection(t *testing.T) {
	m := authTestModel(t)
	s := newTestSetup(t, &setupTestHost{}, true)
	s.list, s.selected = setupFixture(), 2
	m.providerSetup = s
	m.Update(tea.PasteMsg{Content: "router"})
	if s.selected != 0 || len(s.entries()) != 1 {
		t.Fatal("pasted filter leaves selection outside the matching providers")
	}
}
