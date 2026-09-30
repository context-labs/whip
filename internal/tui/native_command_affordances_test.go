package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

func nativeAffordanceFixture(t *testing.T) (*nativeModel, *nativeMenuFixture) {
	t.Helper()
	f := newNativeMenuFixture(t)
	m, err := newNativeModel(t.Context(), f.connection, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.close)
	m.preferencesDirectory = t.TempDir()
	m.ready = true
	return m, f
}

func TestNativeCommandAliasesSelectProviderMaskKeyAndPersistOnlyLocalThemeMouse(t *testing.T) {
	m, f := nativeAffordanceFixture(t)
	for _, command := range []string{"/auth inference secret-inline-key", "/connect inference secret-inline-key", "/setup inference secret-inline-key"} {
		cmd := m.command(command)
		if cmd == nil || m.menu == nil {
			t.Fatal(command, m.status)
		}
		m.Update(cmd())
		if m.menu.mode != "setup-key" || m.menu.setup.id != "inference-net" || m.menu.input.EchoMode != textinput.EchoPassword || m.menu.input.Value() != "secret-inline-key" || m.menu.setupKey != "" || strings.Contains(m.View().Content, "secret-inline-key") {
			t.Fatal("provider alias failed to enter exact masked form", m.menu.mode, m.menu.setup.id)
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	}
	if f.requests.Load() != 0 {
		t.Fatal("opening provider aliases sent credentials or fetched network catalogs")
	}
	mdMu.Lock()
	before := mdScheme
	mdMu.Unlock()
	t.Cleanup(func() { setSchemeOverride(before) })
	if err := saveNativePreferences(m.preferencesDirectory, nativePreferences{Theme: "dark", Sidebar: new(false)}); err != nil {
		t.Fatal(err)
	}
	m.command("/theme light")
	if m.menu != nil || CurrentTheme() != "light" {
		t.Fatal("direct theme did not select the existing menu entry")
	}
	m.command("/mouse")
	prefs, err := readNativePreferences(m.preferencesDirectory)
	if err != nil || prefs.Mouse == nil || *prefs.Mouse || prefs.Theme != "light" || prefs.Sidebar == nil || *prefs.Sidebar {
		t.Fatal("mouse changed unrelated local preferences", prefs, err)
	}
	m.command("/theme definitely-missing")
	if m.menu == nil || !strings.Contains(m.menu.message, "Unknown theme") || CurrentTheme() != "light" {
		t.Fatal("unknown theme mutated preferences")
	}
}

func TestNativeDirectModelCommandsResolveCachedRoutesAndKeepScopes(t *testing.T) {
	m, f := nativeAffordanceFixture(t)
	if value := nativeUIControl(t, m, "/model-for-session config"); value.err != nil || m.owner.Configuration.Model.Name != "configured" || m.owner.Configuration.Model.Effort != "" || m.owner.Configuration.Model.Temperature == nil || *m.owner.Configuration.Model.Temperature != 0.25 {
		t.Fatal("single-name route did not preserve native sampling/reset effort", value.err, m.owner.Configuration.Model)
	}
	inventory := nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.list", protocol.EmptyParams{})
	if inventory.Defaults != nil || f.requests.Load() != 0 {
		t.Fatal("session-only cached selection changed defaults or called network")
	}
	if value := nativeUIControl(t, m, "/model arbitrary-exact-id fixture"); value.err != nil {
		t.Fatal(value.err)
	}
	inventory = nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.list", protocol.EmptyParams{})
	if inventory.Defaults == nil || inventory.Defaults.Name != "arbitrary-exact-id" || inventory.Defaults.Provider != "fixture" || f.requests.Load() != 0 {
		t.Fatal("explicit configured route did not save exact default", inventory.Defaults)
	}
	before := m.owner
	// Refresh includes the imported OpenRouter route's two catalog endpoints.
	if value := nativeUIControl(t, m, "/model refresh"); value.err != nil || f.requests.Load() != 3 || m.owner.ConfigRevision != before.ConfigRevision {
		t.Fatal("explicit refresh changed selection or failed to refresh", value.err, f.requests.Load())
	}
	if value := nativeUIControl(t, m, "/model-for-session discovered fixture"); value.err != nil || m.owner.Configuration.Model.Name != "discovered" || f.requests.Load() != 3 {
		t.Fatal("cached discovered model did not resolve", value.err, m.owner.Configuration.Model)
	}
	if value := nativeUIControl(t, m, "/model-for-session missing-provider-model absent"); value.err == nil {
		t.Fatal("unknown explicit provider accepted")
	}
}

func TestNativeModelConvenienceRejectsAmbiguityAndStaleSessionCAS(t *testing.T) {
	routes := []nativeModelRoute{{name: "alpha", provider: "one"}, {name: "alpha", provider: "two"}, {name: "alphabet", provider: "two"}}
	if _, err := resolveNativeModelRoute(routes, "alph", "one"); err == nil {
		t.Fatal("ambiguous model prefix resolved")
	}
	if value, err := resolveNativeModelRoute(routes, "alpha", "one"); err != nil || value.provider != "one" {
		t.Fatal("current provider preference lost", value, err)
	}
	if _, err := resolveNativeModelRoute(routes, "alpha", "other"); err == nil {
		t.Fatal("ambiguous provider resolved")
	}
	m, f := nativeAffordanceFixture(t)
	before := m.owner
	changed := nativeMenuRPC[protocol.Session](t, f.connection, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: before.ID, ExpectedRevision: before.ConfigRevision, Patch: protocol.ConfigPatch{Model: &protocol.ModelSelection{Provider: "fixture", Name: "newer-choice"}}})
	if value := nativeUIControl(t, m, "/model selected-default fixture"); value.err == nil || !strings.Contains(value.err.Error(), "host default was saved") {
		t.Fatal("partial host/session CAS outcome hidden", value.err)
	}
	got := nativeMenuRPC[protocol.Session](t, f.connection, "sessions.get", protocol.SessionParams{SessionID: before.ID})
	if got.ConfigRevision != changed.ConfigRevision || got.Configuration.Model.Name != "newer-choice" {
		t.Fatal("stale command overwrote session choice")
	}
}

func TestNativeBareRenameUsesCapturedMetadataRevisionAndKeepsComposer(t *testing.T) {
	m, f := nativeAffordanceFixture(t)
	m.input.SetValue("unsent composer")
	command := m.commandKeepingDraft("/rename")
	m.Update(command())
	if m.menu == nil || m.menu.mode != "rename" || m.input.Value() != "unsent composer" {
		t.Fatal("bare rename prompt missing or erased composer")
	}
	tree := *m.menu.renameTree
	metadata := tree.Metadata
	metadata.Title = new("another terminal's title")
	nativeMenuRPC[protocol.Tree](t, f.connection, "trees.update", protocol.UpdateTreeParams{TreeID: tree.ID, ExpectedRevision: tree.Revision, Metadata: metadata})
	m.menu.input.SetValue("my stale title")
	_, command = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(command())
	if m.menu == nil || !strings.Contains(strings.ToLower(m.menu.message), "conflict") {
		t.Fatal("stale rename did not keep dialog for inspection", m.status)
	}
	got := nativeMenuRPC[protocol.Tree](t, f.connection, "trees.get", protocol.TreeParams{TreeID: tree.ID})
	if got.Metadata.Title == nil || *got.Metadata.Title != *metadata.Title {
		t.Fatal("rename rebased across metadata change")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	command = m.commandKeepingDraft("/rename")
	m.Update(command())
	m.menu.input.SetValue("explicit fresh title")
	_, command = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(command())
	got = nativeMenuRPC[protocol.Tree](t, f.connection, "trees.get", protocol.TreeParams{TreeID: tree.ID})
	if m.menu != nil || got.Metadata.Title == nil || *got.Metadata.Title != "explicit fresh title" || m.input.Value() != "unsent composer" {
		t.Fatal("fresh rename did not commit independently of draft", got, m.status)
	}
}
