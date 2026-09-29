package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeLaunchCapturesHostDefaultsAndImmutableResumeSelection(t *testing.T) {
	f := newNativeMenuFixture(t)
	options := NativeOptions{WorkingDirectory: t.TempDir(), ClientHome: t.TempDir(), Agent: "junior-developer", Engine: "quickjs", Cautious: true}
	owner, err := configureNativeSession(t.Context(), f.connection, options)
	if err != nil || owner.Definition.ID != "junior-developer" || owner.Configuration.Model.Provider != "" {
		t.Fatal(owner, err)
	}
	policy := nativeMenuRPC[protocol.PermissionPolicy](t, f.connection, "permissions.policy", protocol.SessionParams{SessionID: owner.ID})
	if policy.Mode != "prompt" {
		t.Fatal(policy)
	}
	m, err := newNativeModel(t.Context(), f.connection, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	m.input.SetValue("keep this draft")
	if cmd := m.prompt(m.input.Value(), "auto"); cmd != nil || m.input.Value() != "keep this draft" || !strings.Contains(m.status, "/model") {
		t.Fatal("unconfigured session submitted or lost draft", m.status)
	}
	options.Resume, options.Cautious = string(owner.ID), false
	options.Engine = "starlark"
	if _, err := configureNativeSession(t.Context(), f.connection, options); err == nil || !strings.Contains(err.Error(), "engine") {
		t.Fatal(err)
	}
	options.Engine, options.Agent = "quickjs", "coding"
	if _, err := configureNativeSession(t.Context(), f.connection, options); err == nil || !strings.Contains(err.Error(), "definition") {
		t.Fatal(err)
	}
	options.Agent, options.Provider, options.Model, options.Automatic = "junior-developer", "fixture", "exact-model", true
	updated, err := configureNativeSession(t.Context(), f.connection, options)
	if err != nil || updated.ID != owner.ID || updated.ConfigRevision <= owner.ConfigRevision || updated.Configuration.Model.Name != "exact-model" {
		t.Fatal(updated, err)
	}
	policy = nativeMenuRPC[protocol.PermissionPolicy](t, f.connection, "permissions.policy", protocol.SessionParams{SessionID: owner.ID})
	if policy.Mode != "automatic" {
		t.Fatal(policy)
	}
	options.Resume, options.Agent, options.Model = "", "coding", ""
	if _, err := configureNativeSession(t.Context(), f.connection, options); err == nil {
		t.Fatal("partial model accepted without a host default")
	}
	before := nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.list", protocol.EmptyParams{})
	nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.defaults", protocol.ProviderDefaultsParams{Revision: before.Revision, Defaults: protocol.ProviderDefaults{Selection: &f.owner.Configuration.Model}})
	options.Provider, options.Model = "", "exact-remote-name"
	captured, err := configureNativeSession(t.Context(), f.connection, options)
	if err != nil || captured.Configuration.Model.Provider != "fixture" || captured.Configuration.Model.Name != "exact-remote-name" || captured.Configuration.Model.Effort != f.owner.Configuration.Model.Effort || captured.Configuration.Model.Temperature == nil || *captured.Configuration.Model.Temperature != 0.25 {
		t.Fatal("explicit name lost native default selection details", captured, err)
	}
}

func TestNativeMenusAreMountedAndDoNotInterceptLiveEvidence(t *testing.T) {
	f := newNativeMenuFixture(t)
	m, err := newNativeModel(t.Context(), f.connection, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	m.preferencesDirectory = t.TempDir()
	m.Update(m.Init()())
	command := m.command("/model-for-session")
	if command == nil || m.menu == nil {
		t.Fatal("model menu not mounted")
	}
	m.Update(command())
	m.Update(nativeRead{generation: m.generation, activity: protocol.SessionActivity{SessionID: f.owner.ID, Lifecycle: "stopped", QueuedInputCount: 7}})
	if m.activity.QueuedInputCount != 7 || m.menu == nil {
		t.Fatal("menu swallowed canonical live evidence")
	}
	// The real menu's acknowledged configuration returns through the shared UI.
	m.menu.provider = "fixture"
	m.menu.selectModel("mounted-choice")
	m.Update(m.menu.saveModel(false)())
	if m.owner.Configuration.Model.Name != "mounted-choice" {
		t.Fatal("saved model did not reach mounted owner", m.owner)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.menu != nil {
		t.Fatal("menu did not close")
	}
	m.command("/settings")
	m.menu.chooseLocalSetting(nativeMenuChoice{id: "mouse"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("saved mouse preference was not mounted")
	}
	for _, name := range []string{"/setup", "/theme", "/model"} {
		command := m.command(name)
		if m.menu == nil {
			t.Fatal("menu missing", name)
		}
		if command != nil {
			m.Update(command())
		}
		menu := m.menu
		if err := m.attachSession(f.owner); err != nil || !menu.Done() || m.menu != nil {
			t.Fatal("owner switch retained old menu", err)
		}
	}
}

func TestNativeClientDirectoryRejectsRetiredNamespaceLinks(t *testing.T) {
	home, retired := t.TempDir(), t.TempDir()
	if err := os.Symlink(retired, filepath.Join(home, "client-v4")); err != nil {
		t.Fatal(err)
	}
	if _, err := nativeClientDirectory(home); err == nil {
		t.Fatal("followed linked client namespace")
	}
	entries, err := os.ReadDir(retired)
	if err != nil || len(entries) != 0 {
		t.Fatal("changed linked target", entries, err)
	}
}
