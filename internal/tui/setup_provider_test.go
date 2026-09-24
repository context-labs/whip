package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

type providerFormHost struct {
	*setupTestHost
	view    protocol.ProviderConfiguration
	creates []protocol.ProviderCreateParams
	updates []protocol.ProviderUpdateParams
	removes []protocol.ProviderRemoveParams
	err     error
	reads   []string
}

func newProviderFormTest(t *testing.T, newSession bool) (*providerSetup, *providerFormHost) {
	t.Helper()
	h := &providerFormHost{setupTestHost: &setupTestHost{list: setupFixture()}}
	s := newProviderSetup(t.Context(), h, newSession)
	t.Cleanup(s.close)
	applySetupCommand(t, s, s.Init())
	return s, h
}

func (h *providerFormHost) ReadProvider(_ context.Context, provider string) (protocol.ProviderConfiguration, error) {
	h.reads = append(h.reads, provider)
	return h.view, nil
}

func (h *providerFormHost) CreateProvider(_ context.Context, p protocol.ProviderCreateParams) (protocol.ProviderConfiguration, error) {
	h.creates = append(h.creates, p)
	if h.err != nil {
		return protocol.ProviderConfiguration{}, h.err
	}
	h.view = protocol.ProviderConfiguration{Provider: p.Provider, Definition: p.Definition, Custom: true}
	return h.saved(p.Credential, p.ManualModel), nil
}

func (h *providerFormHost) UpdateProvider(_ context.Context, p protocol.ProviderUpdateParams) (protocol.ProviderConfiguration, error) {
	h.updates = append(h.updates, p)
	if h.err != nil {
		return protocol.ProviderConfiguration{}, h.err
	}
	if p.Name != nil {
		h.view.Definition.Name = *p.Name
	}
	if p.BaseURL != nil {
		h.view.Definition.BaseURL = *p.BaseURL
	}
	credential := protocol.ProviderCredential{Mode: "keep"}
	if p.Credential != nil {
		credential = *p.Credential
	}
	return h.saved(credential, p.ManualModel), nil
}

func (h *providerFormHost) saved(credential protocol.ProviderCredential, manual *protocol.ProviderManualModel) protocol.ProviderConfiguration {
	h.view.Revision = "saved"
	if credential.Mode != "keep" {
		h.view.Credential = protocol.ProviderCredentialSummary{Mode: credential.Mode, Configured: true, Available: new(true), EnvironmentVariable: credential.EnvironmentVariable}
	}
	h.list.Revision = h.view.Revision
	h.list.Providers = slices.DeleteFunc(h.list.Providers, func(p protocol.ProviderEntry) bool { return p.ID == h.view.Provider })
	entry := protocol.ProviderEntry{ID: h.view.Provider, Name: h.view.Definition.Name, Custom: true, Methods: []string{"api_key"}, Status: protocol.ProviderStatus{Available: new(true), KeySource: "literal", Configured: true}}
	if manual != nil {
		entry.SuggestedModel = manual.Alias
	}
	h.list.Providers = append(h.list.Providers, entry)
	h.catalogs = protocol.ProviderCatalogsResult{Catalogs: map[string]config.Catalog{h.view.Provider: {Models: []config.ModelInfoLite{{ID: "model-one"}, {ID: "model-two"}}}}, Models: map[string]protocol.ModelDescriptor{}}
	return h.view
}

func (h *providerFormHost) RemoveProvider(_ context.Context, p protocol.ProviderRemoveParams) (protocol.ProviderRemoveResult, error) {
	h.removes = append(h.removes, p)
	h.list.Providers = slices.DeleteFunc(h.list.Providers, func(entry protocol.ProviderEntry) bool { return entry.ID == p.Provider })
	return protocol.ProviderRemoveResult{Revision: "removed"}, h.err
}

func (h *providerFormHost) ReadConfiguration(context.Context) (daemon.RuntimeConfiguration, error) {
	return daemon.RuntimeConfiguration{Revision: h.view.Revision, DisabledProviders: new([]string{"unrelated"})}, nil
}

func providerFormKey(key string) tea.KeyPressMsg {
	switch key {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+e", "ctrl+r", "ctrl+c", "ctrl+d":
		return tea.KeyPressMsg{Code: []rune(key)[5], Mod: tea.ModCtrl}
	}
	return setupKey(key)
}

func providerFormFocus(t *testing.T, s *providerSetup, field string) {
	t.Helper()
	for range 20 {
		if s.form != nil && s.form.focus == field {
			return
		}
		s.Update(providerFormKey("tab"))
	}
	t.Fatalf("field %q is unreachable by Tab", field)
}

func providerFormPaste(t *testing.T, s *providerSetup, field, value string) {
	t.Helper()
	providerFormFocus(t, s, field)
	s.Update(tea.PasteMsg{Content: value})
}

func providerFormDrain(t *testing.T, s *providerSetup, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil; i++ {
		if i == 8 {
			t.Fatal("provider operation did not settle")
		}
		cmd = applySetupCommand(t, s, cmd)
	}
}

func providerFormAdd(t *testing.T, s *providerSetup) {
	t.Helper()
	s.Update(tea.PasteMsg{Content: "my unmatched custom endpoint"})
	if !strings.Contains(ansi.Strip(s.body()), "Custom endpoint") {
		t.Fatal("custom action disappeared when search had no results")
	}
	s.Update(setupKey("enter"))
	if s.mode != "provider_form" || s.form == nil {
		t.Fatal("custom action did not open the form")
	}
}

func providerFormManage(t *testing.T, s *providerSetup, h *providerFormHost) {
	t.Helper()
	h.view = protocol.ProviderConfiguration{
		Revision: "original", Provider: "custom", Custom: true,
		Definition: protocol.ProviderDefinition{Name: "Existing endpoint", BaseURL: "https://old.example/v1", API: "openai-completions"},
		Credential: protocol.ProviderCredentialSummary{Mode: "api_key", Configured: true, Available: new(true)},
	}
	h.list.Providers = append(h.list.Providers, protocol.ProviderEntry{ID: "custom", Name: "Existing endpoint", Custom: true, Status: protocol.ProviderStatus{Available: new(true), KeySource: "literal"}})
	s.list = h.list
	s.Update(tea.PasteMsg{Content: "Existing endpoint"})
	_, cmd := s.Update(providerFormKey("ctrl+e"))
	providerFormDrain(t, s, cmd)
	if s.mode != "provider_manage" {
		t.Fatal("Manage did not open")
	}
}

func providerFormAction(t *testing.T, s *providerSetup, action string) tea.Cmd {
	t.Helper()
	for range 10 {
		if s.providerActions()[s.selected].id == action {
			_, cmd := s.Update(setupKey("enter"))
			return cmd
		}
		s.Update(providerFormKey("tab"))
	}
	t.Fatalf("management action %q is unreachable", action)
	return nil
}

func TestSetupProviderKeyboardCreateChoosesModelThenSavesDefaults(t *testing.T) {
	s, h := newProviderFormTest(t, true)
	providerFormAdd(t, s)
	providerFormPaste(t, s, "name", "My Endpoint")
	providerFormPaste(t, s, "url", "https://models.example/v1")
	providerFormPaste(t, s, "key", "fixture-private-key")
	s.Update(providerFormKey("backspace"))
	s.Update(setupKey("y"))
	if strings.Contains(ansi.Strip(s.body()), "fixture-private-key") {
		t.Fatal("API key is visible in form")
	}
	providerFormFocus(t, s, "save")
	_, cmd := s.Update(setupKey("enter"))
	if s.form.value("key") != "" {
		t.Fatal("submitted secret remains in the form")
	}
	providerFormDrain(t, s, cmd)
	if len(h.creates) != 1 || h.creates[0].Provider != "my-endpoint" || h.creates[0].Revision != "original" || h.creates[0].Credential.Key != "fixture-private-key" || h.creates[0].Definition.BaseURL != "https://models.example/v1" {
		t.Fatalf("wrong create request: %+v", h.creates)
	}
	if s.mode != "models" || s.model != "" || s.done || len(h.writes) != 0 {
		t.Fatalf("save chose an arbitrary model or changed defaults: mode=%s, model=%q, writes=%v", s.mode, s.model, h.writes)
	}
	s.Update(tea.PasteMsg{Content: "model-two"})
	_, cmd = s.Update(setupKey("enter"))
	providerFormDrain(t, s, cmd)
	if !s.done || !s.chosen || len(h.writes) != 1 || h.writes[0].Revision != "saved" || *h.writes[0].DefaultProvider != "my-endpoint" || *h.writes[0].DefaultModel != "model-two" {
		t.Fatalf("selected pair was not saved atomically: %+v", h.writes)
	}
}

func TestSetupProviderManualNoAuthRequiresExplicitChoice(t *testing.T) {
	s, h := newProviderFormTest(t, true)
	providerFormAdd(t, s)
	providerFormPaste(t, s, "name", "Local")
	providerFormPaste(t, s, "url", "http://127.0.0.1:8000/v1")
	providerFormFocus(t, s, "save")
	if _, cmd := s.Update(setupKey("enter")); cmd != nil || s.form.focus != "key" || len(h.creates) != 0 {
		t.Fatal("empty key silently allowed an unauthenticated connection")
	}
	providerFormFocus(t, s, "auth")
	s.Update(providerFormKey("left"))
	providerFormFocus(t, s, "manual")
	s.Update(setupKey("enter"))
	providerFormPaste(t, s, "model", "exact/model-id")
	providerFormFocus(t, s, "advanced")
	s.Update(setupKey("enter"))
	providerFormPaste(t, s, "context", "32768")
	providerFormPaste(t, s, "output", "2048")
	providerFormFocus(t, s, "verification")
	s.Update(setupKey("enter"))
	providerFormFocus(t, s, "save")
	_, cmd := s.Update(setupKey("enter"))
	providerFormDrain(t, s, cmd)
	if len(h.creates) != 1 {
		t.Fatalf("creates=%d", len(h.creates))
	}
	p := h.creates[0]
	if p.Credential.Mode != "none" || p.Credential.Key != "" || !p.AllowUnverified || p.ManualModel == nil || p.ManualModel.Alias != "local/exact/model-id" || p.ManualModel.ID != "exact/model-id" || p.ManualModel.Context != 32768 || p.ManualModel.MaxOutput != 2048 {
		t.Fatalf("manual connection is incomplete: %+v", p)
	}
	if !s.done || !s.chosen || s.model != "local/exact/model-id" || len(h.writes) != 1 {
		t.Fatal("manual model did not finish onboarding")
	}
}

func TestSetupProviderEnvironmentIsExecutionHostVariable(t *testing.T) {
	s, h := newProviderFormTest(t, true)
	providerFormAdd(t, s)
	providerFormPaste(t, s, "name", "Host env")
	providerFormPaste(t, s, "url", "https://models.example/v1")
	providerFormFocus(t, s, "auth")
	s.Update(providerFormKey("right"))
	providerFormPaste(t, s, "environment", "CUSTOM_PROVIDER_KEY")
	if !strings.Contains(ansi.Strip(s.body()), "execution host") {
		t.Fatal("environment scope is unclear")
	}
	providerFormFocus(t, s, "save")
	_, cmd := s.Update(setupKey("enter"))
	providerFormDrain(t, s, cmd)
	if len(h.creates) != 1 || h.creates[0].Credential.Mode != "environment" || h.creates[0].Credential.EnvironmentVariable != "CUSTOM_PROVIDER_KEY" || h.creates[0].Credential.Key != "" {
		t.Fatalf("wrong environment request: %+v", h.creates)
	}
}

func TestSetupProviderMissingHostEnvironmentStaysUnavailableAfterSaving(t *testing.T) {
	s, h := newProviderFormTest(t, true)
	providerFormAdd(t, s)
	providerFormPaste(t, s, "name", "Future environment")
	providerFormPaste(t, s, "url", "https://models.example/v1")
	providerFormFocus(t, s, "auth")
	s.Update(providerFormKey("right"))
	providerFormPaste(t, s, "environment", "NOT_AVAILABLE_ON_HOST")
	providerFormFocus(t, s, "verification")
	s.Update(setupKey("enter"))
	providerFormFocus(t, s, "save")
	_, cmd := s.Update(setupKey("enter"))
	if cmd == nil {
		t.Fatal("unverified environment save did not start")
	}
	reply := cmd()
	for i := range h.list.Providers {
		if h.list.Providers[i].ID == "future-environment" {
			h.list.Providers[i].Status = protocol.ProviderStatus{Available: new(false), KeySource: "environment", EnvironmentVariable: "NOT_AVAILABLE_ON_HOST", AuthState: "unavailable"}
		}
	}
	_, next := s.Update(reply)
	providerFormDrain(t, s, next)
	if len(h.creates) != 1 || !h.creates[0].AllowUnverified || s.mode != "provider_manage" || s.done || s.chosen || len(h.writes) != 0 || !strings.Contains(s.message, "Credentials are not available yet") {
		t.Fatal("saving an unresolved host variable claimed readiness or changed the default route")
	}
}

func TestSetupProviderManualModelFromEmptyCatalogDoesNotRewriteConnection(t *testing.T) {
	s, h := newProviderFormTest(t, false)
	providerFormManage(t, s, h)
	h.catalogs = protocol.ProviderCatalogsResult{Models: map[string]protocol.ModelDescriptor{}, Errors: map[string]string{"custom": "model listing unavailable"}}
	providerFormDrain(t, s, providerFormAction(t, s, "use"))
	if s.mode != "models" || !strings.Contains(ansi.Strip(s.body()), "Enter model manually") {
		t.Fatal("empty catalog did not offer manual model entry")
	}
	_, cmd := s.Update(setupKey("enter"))
	providerFormDrain(t, s, cmd)
	providerFormPaste(t, s, "model", "exact-model")
	providerFormFocus(t, s, "advanced")
	s.Update(setupKey("enter"))
	providerFormPaste(t, s, "alias", "custom-vision")
	providerFormPaste(t, s, "context", "0")
	providerFormFocus(t, s, "save")
	if _, cmd := s.Update(setupKey("enter")); cmd != nil || len(h.updates) != 0 || !strings.Contains(s.message, "positive") {
		t.Fatal("invalid context limit reached the host")
	}
	providerFormFocus(t, s, "context")
	s.Update(providerFormKey("backspace"))
	s.Update(tea.PasteMsg{Content: "16384"})
	providerFormFocus(t, s, "verification")
	s.Update(setupKey("enter"))
	providerFormFocus(t, s, "save")
	_, cmd = s.Update(setupKey("enter"))
	providerFormDrain(t, s, cmd)
	if len(h.updates) != 1 {
		t.Fatalf("updates=%d", len(h.updates))
	}
	p := h.updates[0]
	if p.Provider != "custom" || p.Name != nil || p.BaseURL != nil || p.Credential != nil || p.ManualModel == nil || p.ManualModel.ID != "exact-model" || p.ManualModel.Alias != "custom-vision" || p.ManualModel.Context != 16384 || !p.AllowUnverified {
		t.Fatalf("manual model rewrote connection or lost route: %+v", p)
	}
	if s.model != "custom-vision" || !s.done || !s.chosen || len(h.writes) != 0 {
		t.Fatal("manual alias did not finish model/default selection")
	}
}

func TestSetupProviderManualModelKeepsExistingLimitsOnlyForTheMatchingModel(t *testing.T) {
	for _, changeModel := range []bool{false, true} {
		t.Run(fmt.Sprintf("change=%t", changeModel), func(t *testing.T) {
			s, h := newProviderFormTest(t, false)
			providerFormManage(t, s, h)
			h.view.Models = []protocol.ProviderConfiguredModel{{ID: "vision-model", Alias: "my-vision", Context: 32768, MaxOutput: 1024}}
			h.catalogs = protocol.ProviderCatalogsResult{Models: map[string]protocol.ModelDescriptor{}}
			providerFormDrain(t, s, providerFormAction(t, s, "use"))
			_, cmd := s.Update(setupKey("enter"))
			providerFormDrain(t, s, cmd)
			providerFormPaste(t, s, "model", "vision-model")
			if changeModel {
				s.Update(tea.PasteMsg{Content: "-next"})
			}
			providerFormFocus(t, s, "save")
			_, cmd = s.Update(setupKey("enter"))
			providerFormDrain(t, s, cmd)
			if len(h.updates) != 1 || h.updates[0].ManualModel == nil {
				t.Fatal("manual model was not saved")
			}
			model := h.updates[0].ManualModel
			if !changeModel && (model.Alias != "my-vision" || model.Context != 32768 || model.MaxOutput != 1024) {
				t.Fatalf("existing hidden overrides were cleared: %+v", model)
			}
			if changeModel && (model.ID != "vision-model-next" || model.Alias != "custom/vision-model-next" || model.Context != 0 || model.MaxOutput != 0) {
				t.Fatalf("different model inherited an unrelated alias or limits: %+v", model)
			}
		})
	}
}

func TestSetupProviderSelectionKeepsDefaultScope(t *testing.T) {
	for _, newSession := range []bool{false, true} {
		t.Run(fmt.Sprintf("new=%t", newSession), func(t *testing.T) {
			s, h := newProviderFormTest(t, newSession)
			s.provider, s.model = "custom", "chosen-model"
			providerFormDrain(t, s, s.useModel())
			if !s.chosen || !s.done {
				t.Fatal("model selection did not finish")
			}
			if !newSession && len(h.writes) != 0 {
				t.Fatal("existing session choice changed host defaults")
			}
			if newSession && (len(h.writes) != 1 || *h.writes[0].DefaultProvider != "custom" || *h.writes[0].DefaultModel != "chosen-model") {
				t.Fatal("onboarding did not save the exact model/provider pair")
			}
		})
	}
}

func TestSetupProviderEditKeepsIdentityAndRequiresNewEndpointCredential(t *testing.T) {
	s, h := newProviderFormTest(t, false)
	providerFormManage(t, s, h)
	providerFormAction(t, s, "edit")
	if s.form.value("key") != "" || s.form.credential != "keep" || slices.Contains(s.form.fields(), "id") {
		t.Fatal("editor prefills a secret or allows changing provider identity")
	}
	providerFormPaste(t, s, "name", " renamed")
	providerFormFocus(t, s, "url")
	s.form.set("url", "https://new.example/v1")
	providerFormFocus(t, s, "save")
	if _, cmd := s.Update(setupKey("enter")); cmd != nil || len(h.updates) != 0 || s.form.focus != "auth" || !strings.Contains(s.message, "existing key will not be sent") {
		t.Fatal("new destination used the old credential without an explicit choice")
	}
	s.Update(providerFormKey("left"))
	providerFormFocus(t, s, "save")
	_, cmd := s.Update(setupKey("enter"))
	providerFormDrain(t, s, cmd)
	if len(h.updates) != 1 || h.updates[0].Provider != "custom" || h.updates[0].Credential.Mode != "none" || h.updates[0].BaseURL == nil || *h.updates[0].BaseURL != "https://new.example/v1" {
		t.Fatalf("edit lost identity or explicit credential choice: %+v", h.updates)
	}
}

func TestSetupProviderNameOnlyEditKeepsStoredCredential(t *testing.T) {
	s, h := newProviderFormTest(t, false)
	providerFormManage(t, s, h)
	providerFormAction(t, s, "edit")
	providerFormPaste(t, s, "name", " renamed")
	providerFormFocus(t, s, "save")
	_, cmd := s.Update(setupKey("enter"))
	providerFormDrain(t, s, cmd)
	if len(h.updates) != 1 || h.updates[0].Name == nil || *h.updates[0].Name != "Existing endpoint renamed" || h.updates[0].BaseURL != nil || h.updates[0].Credential == nil || h.updates[0].Credential.Mode != "keep" || h.updates[0].Credential.Key != "" || s.reloadProvider != "" {
		t.Fatalf("name edit changed connection credentials or requested reload: %+v", h.updates)
	}
}

func TestSetupProviderRefreshRebasesRemoteRenameAndPreservesOnlyLocalNameEdits(t *testing.T) {
	for _, editName := range []bool{false, true} {
		t.Run(fmt.Sprintf("local-name=%t", editName), func(t *testing.T) {
			s, h := newProviderFormTest(t, false)
			providerFormManage(t, s, h)
			providerFormAction(t, s, "edit")
			if editName {
				providerFormPaste(t, s, "name", " local edit")
			}
			providerFormFocus(t, s, "auth")
			s.Update(providerFormKey("right"))
			providerFormPaste(t, s, "key", "unsubmitted-key")
			h.view.Revision, h.list.Revision = "remote-revision", "remote-revision"
			h.view.Definition.Name = "Renamed remotely"
			_, cmd := s.Update(providerFormKey("ctrl+r"))
			providerFormDrain(t, s, cmd)
			name := "Renamed remotely"
			if editName {
				name = "Existing endpoint local edit"
			}
			if s.form.value("name") != name || s.form.value("key") != "" || len(h.updates) != 0 {
				t.Fatal("refresh lost a local edit, retained a key, or failed to advance an untouched name")
			}
			providerFormPaste(t, s, "key", "reviewed-replacement-key")
			providerFormFocus(t, s, "save")
			_, cmd = s.Update(setupKey("enter"))
			providerFormDrain(t, s, cmd)
			if len(h.updates) != 1 {
				t.Fatalf("updates=%d", len(h.updates))
			}
			update := h.updates[0]
			if update.Revision != "remote-revision" || update.BaseURL != nil || update.Credential == nil || update.Credential.Key != "reviewed-replacement-key" {
				t.Fatalf("key rotation was not rebased onto refreshed configuration: %+v", update)
			}
			if editName && (update.Name == nil || *update.Name != name) {
				t.Fatal("explicit local name edit was discarded")
			}
			if !editName && update.Name != nil {
				t.Fatal("untouched stale name became a patch that would undo the remote rename")
			}
		})
	}
}

func TestSetupProviderRefreshRebasesEndpointAndResetsLocalEnvironmentReplacement(t *testing.T) {
	s, h := newProviderFormTest(t, false)
	providerFormManage(t, s, h)
	providerFormAction(t, s, "edit")
	providerFormFocus(t, s, "auth")
	s.Update(providerFormKey("right"))
	s.Update(providerFormKey("right"))
	providerFormPaste(t, s, "environment", "LOCAL_DRAFT_KEY")
	h.view.Definition.BaseURL = "https://changed-on-host.example/v1"
	h.view.Credential = protocol.ProviderCredentialSummary{Mode: "environment", EnvironmentVariable: "HOST_KEY", Configured: true, Available: new(true)}
	h.view.Revision, h.list.Revision = "host-endpoint-change", "host-endpoint-change"
	_, cmd := s.Update(providerFormKey("ctrl+r"))
	providerFormDrain(t, s, cmd)
	if s.form.value("url") != "https://changed-on-host.example/v1" || s.form.credential != "keep" || s.form.focus != "auth" || !strings.Contains(s.message, "endpoint changed") || len(h.updates) != 0 {
		t.Fatal("remote endpoint change kept a stale URL or silently transferred the local credential choice")
	}
	providerFormFocus(t, s, "save")
	_, cmd = s.Update(setupKey("enter"))
	providerFormDrain(t, s, cmd)
	if len(h.updates) != 1 || h.updates[0].Revision != "host-endpoint-change" || h.updates[0].BaseURL != nil || h.updates[0].Credential == nil || h.updates[0].Credential.Mode != "keep" || h.updates[0].Credential.EnvironmentVariable != "" {
		t.Fatalf("refreshed save undid the host endpoint or replaced its credential without a new decision: %+v", h.updates)
	}
}

func TestSetupProviderRefreshRebasesUntouchedManualLimitsAndRetainsEditedLimits(t *testing.T) {
	for _, editOutput := range []bool{false, true} {
		t.Run(fmt.Sprintf("local-output=%t", editOutput), func(t *testing.T) {
			s, h := newProviderFormTest(t, false)
			providerFormManage(t, s, h)
			h.view.Models = []protocol.ProviderConfiguredModel{{ID: "vision-model", Alias: "my-vision", Context: 32768, MaxOutput: 1024}}
			h.catalogs = protocol.ProviderCatalogsResult{Models: map[string]protocol.ModelDescriptor{}}
			providerFormDrain(t, s, providerFormAction(t, s, "use"))
			_, cmd := s.Update(setupKey("enter"))
			providerFormDrain(t, s, cmd)
			providerFormPaste(t, s, "model", "vision-model")
			if editOutput {
				providerFormFocus(t, s, "advanced")
				s.Update(setupKey("enter"))
				providerFormPaste(t, s, "output", "0") // Deliberate local change: 1024 -> 10240.
			}
			h.view.Models = []protocol.ProviderConfiguredModel{{ID: "vision-model", Alias: "my-vision", Context: 65536, MaxOutput: 4096}}
			h.view.Revision, h.list.Revision = "updated-limits", "updated-limits"
			_, cmd = s.Update(providerFormKey("ctrl+r"))
			providerFormDrain(t, s, cmd)
			providerFormFocus(t, s, "save")
			_, cmd = s.Update(setupKey("enter"))
			providerFormDrain(t, s, cmd)
			if len(h.updates) != 1 || h.updates[0].ManualModel == nil {
				t.Fatal("refreshed manual model was not saved")
			}
			model := h.updates[0].ManualModel
			output := 4096
			if editOutput {
				output = 10240
			}
			if h.updates[0].Revision != "updated-limits" || model.Alias != "my-vision" || model.Context != 65536 || model.MaxOutput != output {
				t.Fatalf("refresh lost edited limits or reverted untouched remote limits: %+v", model)
			}
		})
	}
}

func TestSetupProviderRefreshRetainsIDIfAnotherClientRemovedTheConnection(t *testing.T) {
	s, h := newProviderFormTest(t, false)
	providerFormManage(t, s, h)
	providerFormAction(t, s, "edit")
	providerFormPaste(t, s, "name", " unsaved")
	h.list.Providers = slices.DeleteFunc(h.list.Providers, func(p protocol.ProviderEntry) bool { return p.ID == "custom" })
	h.list.Revision = "removed-elsewhere"
	_, cmd := s.Update(providerFormKey("ctrl+r"))
	providerFormDrain(t, s, cmd)
	if s.mode != "provider_form" || s.form.providerID() != "custom" || s.form.value("name") != "Existing endpoint unsaved" || !s.form.isEditing || !slices.Contains(s.form.fields(), "url") || !strings.Contains(strings.ToLower(s.message), "removed") {
		t.Fatal("refresh lost editing identity or hid the endpoint after external removal")
	}
	if len(h.updates) != 0 || len(h.creates) != 0 {
		t.Fatal("refresh attempted to recreate an externally removed connection")
	}
}

func TestSetupProviderEditingCurrentCredentialOffersExplicitReload(t *testing.T) {
	s, h := newProviderFormTest(t, false)
	providerFormManage(t, s, h)
	s.selectionProvider, s.selectionModel = "custom", "model-one"
	providerFormAction(t, s, "edit")
	providerFormFocus(t, s, "auth")
	s.Update(providerFormKey("right"))
	providerFormPaste(t, s, "key", "replacement-key")
	providerFormFocus(t, s, "save")
	_, cmd := s.Update(setupKey("enter"))
	providerFormDrain(t, s, cmd)
	if s.mode != "provider_saved" || s.done || s.reload || s.reloadProvider != "custom" || !strings.Contains(ansi.Strip(s.body()), "Reload current session") {
		t.Fatal("save applied the current connection without explicit reload or lost the reload action")
	}
	providerFormAction(t, s, "reload")
	if !s.done || !s.reload || len(h.writes) != 0 {
		t.Fatal("reload selected another model or rewrote defaults")
	}
}

func TestSetupProviderEndpointEditWithManualAliasSelectsNewRouteInsteadOfReloadingOldCatalogModel(t *testing.T) {
	s, h := newProviderFormTest(t, false)
	providerFormManage(t, s, h)
	s.selectionProvider, s.selectionModel = "custom", "fixture-coding"
	providerFormAction(t, s, "edit")
	providerFormFocus(t, s, "url")
	s.form.set("url", "http://127.0.0.1:8000/local")
	providerFormFocus(t, s, "auth")
	s.Update(providerFormKey("left"))
	providerFormFocus(t, s, "manual")
	s.Update(setupKey("enter"))
	providerFormPaste(t, s, "model", "fixture-coding")
	providerFormFocus(t, s, "verification")
	s.Update(setupKey("enter"))
	providerFormFocus(t, s, "save")
	_, cmd := s.Update(setupKey("enter"))
	providerFormDrain(t, s, cmd)
	if len(h.updates) != 1 || h.updates[0].ManualModel == nil || h.updates[0].ManualModel.Alias != "custom/fixture-coding" || !h.updates[0].AllowUnverified {
		t.Fatal("endpoint edit did not persist the explicit manual route")
	}
	if s.mode != "provider_saved" || s.model != "custom/fixture-coding" || s.done || s.reload || len(h.writes) != 0 {
		t.Fatal("save discarded the new alias, changed defaults, or applied a session action")
	}
	connection := newFakeDaemonConnection(session.RootSnapshot{RootID: "root"})
	client, err := NewClient(ClientOptions{ClientID: "manual-provider-route", RootID: "root", Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	client.Start()
	waitClientState(t, client, ClientLive)
	m := compactCmdModel()
	m.client, m.clientState, m.runContext = client, ClientLive, t.Context()
	m.modelName, m.provName = "fixture-coding", "custom"
	m.input.SetValue("preserve the unsent draft")
	m.providerSetup = s
	_, cmd = m.Update(setupKey("enter"))
	if cmd == nil {
		t.Fatal("using the saved model did not start selection")
	}
	_, cmd = m.Update(cmd())
	if cmd == nil {
		t.Fatal("using the saved model did not dispatch the chosen route")
	}
	m.Update(cmd())
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if len(connection.commands) != 1 || connection.commands[0].Operation != "session.model" || len(h.writes) != 0 || m.input.Value() != "preserve the unsent draft" || m.providerSetup != nil {
		t.Fatalf("expected only explicit model selection, got commands=%+v, defaults=%+v, draft=%q", connection.commands, h.writes, m.input.Value())
	}
	var selected protocol.ModelParams
	if err := json.Unmarshal(connection.commands[0].Payload, &selected); err != nil {
		t.Fatal(err)
	}
	if selected.Provider != "custom" || selected.Model != "custom/fixture-coding" {
		t.Fatalf("session selection lost the persisted alias: %+v", selected)
	}
}

func TestSetupProviderFailureRetainsDraftAndRefreshNeverReplaysSecret(t *testing.T) {
	for _, failure := range []struct {
		name string
		err  error
	}{
		{name: "conflict", err: &protocol.RPCError{Code: -32009, Message: "configuration changed"}},
		{name: "authentication", err: errors.New("credentials were rejected")},
		{name: "uncertain", err: context.DeadlineExceeded},
	} {
		t.Run(failure.name, func(t *testing.T) {
			s, h := newProviderFormTest(t, true)
			providerFormAdd(t, s)
			providerFormPaste(t, s, "name", "New connection")
			providerFormPaste(t, s, "url", "https://models.example/v1")
			providerFormPaste(t, s, "key", "one-time-secret")
			providerFormFocus(t, s, "save")
			h.err = failure.err
			_, cmd := s.Update(setupKey("enter"))
			providerFormDrain(t, s, cmd)
			if s.mode != "provider_form" || s.form.value("name") != "New connection" || s.form.value("url") != "https://models.example/v1" || s.form.value("key") != "" || !strings.Contains(s.message, "Re-enter") || len(h.creates) != 1 {
				t.Fatal("failed save discarded non-secret edits or retained a submitted secret")
			}
			h.err = nil
			h.view = protocol.ProviderConfiguration{Revision: "newer", Provider: "new-connection", Custom: true, Definition: protocol.ProviderDefinition{Name: "Saved elsewhere", BaseURL: "https://another.example/v1", API: "openai-completions"}}
			h.list.Revision = "newer"
			h.list.Providers = append(h.list.Providers, protocol.ProviderEntry{ID: "new-connection"})
			_, cmd = s.Update(providerFormKey("ctrl+r"))
			providerFormDrain(t, s, cmd)
			if len(h.creates) != 1 || len(h.updates) != 0 || s.form.isEditing || s.form.value("name") != "New connection" || s.form.value("url") != "https://models.example/v1" || s.form.view.Revision != "newer" || !strings.Contains(s.message, "already saved") {
				t.Fatal("refresh overwrote the user's draft or retried a mutation")
			}
			providerFormFocus(t, s, "save")
			if _, cmd := s.Update(setupKey("enter")); cmd != nil || len(h.updates) != 0 {
				t.Fatal("retry reused a submitted key")
			}
		})
	}
}

func TestSetupProviderCancelledSaveAndLateClipboardCannotCrossForms(t *testing.T) {
	s, _ := newProviderFormTest(t, true)
	providerFormAdd(t, s)
	providerFormPaste(t, s, "name", "A draft")
	providerFormPaste(t, s, "key", "hidden-secret")
	clipboard := s.inputCommand(func() tea.Msg { return tea.PasteMsg{Content: "late-secret"} })
	s.Update(providerFormKey("shift+tab"))
	s.Update(clipboard())
	if s.form.value("key") != "hidden-secret" || strings.Contains(s.form.value("name"), "late") {
		t.Fatal("late clipboard entered a different form field")
	}
	providerFormFocus(t, s, "save")
	_, save := s.Update(setupKey("enter"))
	if save == nil {
		t.Fatal("save did not start")
	}
	s.Update(setupKey("esc"))
	_, next := s.Update(save())
	if next != nil || s.mode != "providers" || s.done || s.form.value("name") != "A draft" || s.form.value("key") != "" {
		t.Fatal("cancelled reply changed the dialog or lost the draft")
	}
	s.Update(setupKey("esc"))
	s.Update(clipboard())
	if !s.done || s.form.value("key") != "" {
		t.Fatal("closed dialog accepted late credential input")
	}
}

func TestSetupProviderEscapePreservesEditUntilDialogCloses(t *testing.T) {
	s, h := newProviderFormTest(t, false)
	providerFormManage(t, s, h)
	providerFormAction(t, s, "edit")
	providerFormPaste(t, s, "name", " with draft")
	s.Update(setupKey("esc"))
	providerFormAction(t, s, "edit")
	if s.form.value("name") != "Existing endpoint with draft" {
		t.Fatal("returning to Edit discarded unsaved input")
	}
}

func TestSetupProviderManageBlocksReferencedRemovalAndPreservesOtherDisabledProviders(t *testing.T) {
	s, h := newProviderFormTest(t, false)
	providerFormManage(t, s, h)
	s.configuration.RemovalBlockers = []string{"models.shared", "default provider"}
	if cmd := providerFormAction(t, s, "remove-check"); cmd != nil || len(h.removes) != 0 || s.mode != "provider_manage" || !strings.Contains(s.message, "models.shared") || !strings.Contains(s.message, "Disable") {
		t.Fatal("referenced removal was allowed or lacked recovery")
	}
	cmd := providerFormAction(t, s, "disable")
	applySetupCommand(t, s, cmd)
	if len(h.writes) != 1 || h.writes[0].Revision != "original" || h.writes[0].DisabledProviders == nil || !slices.Equal(*h.writes[0].DisabledProviders, []string{"unrelated", "custom"}) {
		t.Fatalf("disable changed unrelated providers: %+v", h.writes)
	}
}

func TestSetupProviderRemoveRequiresConfirmation(t *testing.T) {
	s, h := newProviderFormTest(t, false)
	providerFormManage(t, s, h)
	providerFormAction(t, s, "remove-check")
	if s.mode != "provider_remove" || len(h.removes) != 0 {
		t.Fatal("remove was not confirmed")
	}
	s.Update(setupKey("enter")) // Keep connection is selected first.
	if s.mode != "provider_manage" || len(h.removes) != 0 {
		t.Fatal("confirmation did not default to keeping the connection")
	}
	providerFormAction(t, s, "remove-check")
	providerFormDrain(t, s, providerFormAction(t, s, "remove"))
	if len(h.removes) != 1 || h.removes[0].Provider != "custom" || h.removes[0].Revision != "original" || s.mode != "providers" || s.provider != "" {
		t.Fatalf("removal did not return to provider list: %+v", h.removes)
	}
}

func TestSetupProviderFormKeepsFocusedFieldVisibleOnSmallTerminals(t *testing.T) {
	t.Cleanup(func() { SetLightTheme(false) })
	for _, light := range []bool{false, true} {
		SetLightTheme(light)
		for _, width := range []int{32, 48, 80} {
			for _, height := range []int{12, 18, 30} {
				t.Run(fmt.Sprintf("light=%t/%dx%d", light, width, height), func(t *testing.T) {
					s, _ := newProviderFormTest(t, true)
					providerFormAdd(t, s)
					providerFormPaste(t, s, "name", "A fairly long endpoint display name")
					providerFormFocus(t, s, "manual")
					s.Update(setupKey("enter"))
					providerFormFocus(t, s, "advanced")
					s.Update(setupKey("enter"))
					m := compactCmdModel()
					m.Update(mkWinSize(width, height))
					for _, field := range []string{"name", "url", "key", "model", "id", "context", "output", "save"} {
						providerFormFocus(t, s, field)
						rows := s.rows(m)
						view := ansi.Strip(strings.Join(rows, "\n"))
						label := ansi.Truncate(s.form.fieldLabel(field), m.dialogWidth()-4, "…")
						if len(rows) > m.dialogHeight() || !strings.Contains(view, label) {
							t.Fatalf("focused %s hidden or height exceeded (%d > %d):\n%s", field, len(rows), m.dialogHeight(), view)
						}
						for _, row := range rows {
							if ansi.StringWidth(row) > m.dialogWidth() {
								t.Fatalf("row exceeds dialog width: %q", row)
							}
						}
					}
				})
			}
		}
	}
}

func TestSetupProviderReloadKeepsDraftAndDoesNotSendOrChangeModel(t *testing.T) {
	connection := newFakeDaemonConnection(session.RootSnapshot{RootID: "root"})
	client, err := NewClient(ClientOptions{ClientID: "reload-provider", RootID: "root", Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	client.Start()
	waitClientState(t, client, ClientLive)
	m := compactCmdModel()
	m.client, m.clientState, m.runContext = client, ClientLive, t.Context()
	m.input.SetValue("keep this draft")
	s, _ := newProviderFormTest(t, false)
	s.mode = "provider_saved"
	m.providerSetup = s
	_, cmd := m.Update(setupKey("enter"))
	if cmd == nil {
		t.Fatal("reload did not submit a command")
	}
	m.Update(cmd())
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if len(connection.commands) != 1 || connection.commands[0].Operation != "session.reload" || m.input.Value() != "keep this draft" || m.providerSetup != nil {
		t.Fatalf("reload sent a draft or changed the route: commands=%+v, draft=%q", connection.commands, m.input.Value())
	}
}

func TestSetupProviderBrowserReloginReloadsCurrentPairAfterSelection(t *testing.T) {
	s, h := newProviderFormTest(t, false)
	h.catalogs = protocol.ProviderCatalogsResult{
		Models:   map[string]protocol.ModelDescriptor{},
		Catalogs: map[string]config.Catalog{"inference-net": {Models: []config.ModelInfoLite{{ID: "coding-model"}}}},
	}
	s.provider, s.mode = "inference-net", "login"
	s.selectionProvider, s.selectionModel = "inference-net", "coding-model"
	h.list.Providers[0].Status.Available = new(true)
	h.list.Providers[0].SuggestedModel = "coding-model"
	_, cmd := s.Update(setupReply{
		owner: s, request: s.request, kind: "login",
		login: daemon.ProviderLoginStatus{Provider: "inference-net", FlowID: "replacement-login", State: "succeeded"},
	})
	providerFormDrain(t, s, cmd)
	if s.mode != "models" || s.provider != "inference-net" || s.model != "coding-model" || s.done || len(h.writes) != 0 {
		t.Fatal("browser login did not preserve the route in model choices")
	}
	connection := newFakeDaemonConnection(session.RootSnapshot{RootID: "root"})
	client, err := NewClient(ClientOptions{ClientID: "relogin-provider", RootID: "root", Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	client.Start()
	waitClientState(t, client, ClientLive)
	m := compactCmdModel()
	m.client, m.clientState, m.runContext = client, ClientLive, t.Context()
	m.modelName, m.provName = "coding-model", "inference-net"
	m.input.SetValue("draft after account reconnection")
	m.providerSetup = s
	_, cmd = m.Update(setupKey("enter"))
	if cmd == nil {
		t.Fatal("reconnected route selection did not start")
	}
	_, cmd = m.Update(cmd())
	if cmd == nil {
		t.Fatal("same-pair browser reconnection did not request a session update")
	}
	m.Update(cmd())
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if len(connection.commands) != 1 || connection.commands[0].Operation != "session.reload" || len(h.writes) != 0 || m.providerSetup != nil || m.input.Value() != "draft after account reconnection" {
		t.Fatalf("browser reconnection failed to reload credentials or sent the draft: commands=%+v, defaults=%+v, draft=%q", connection.commands, h.writes, m.input.Value())
	}
}
