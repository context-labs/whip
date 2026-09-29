package tui

import (
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeSetupSuggestedDefaultIsExactVerifiedAndExplicit(t *testing.T) {
	f := newNativeMenuFixture(t)
	f.catalog.Store(`{"data":[{"id":"z-ai/glm-5.3","context_length":64000,"reasoning_efforts":["max"]}]}`)
	m := newNativeMenu(nativeMenuWork(t), f.connection, nativeMenuOptions{Kind: "setup"})
	defer m.Close()
	nativeMenuRun(t, m, m.Init())
	nativeMenuSelect(t, m, "preset:openrouter")
	next := m.Update(m.saveProvider("fixture-synthetic-key", false)())
	nativeMenuRun(t, m, next)
	if m.mode != "setup-suggestions" || m.inventory.Defaults != nil {
		t.Fatal("connect skipped explicit default confirmation", m.mode, m.inventory.Defaults, m.message)
	}
	nativeMenuSelect(t, m, "suggest:z-ai/glm-5.3")
	if m.mode != "model-scope" || m.selection.Effort != "max" {
		t.Fatal("suggested model/effort did not match live catalog", m.mode, m.selection)
	}
	inventory := nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.list", protocol.EmptyParams{})
	if inventory.Defaults != nil {
		t.Fatal("choosing suggestion secretly saved default")
	}
	nativeMenuRun(t, m, nativeMenuSelect(t, m, "default"))
	if m.inventory.Defaults == nil || m.inventory.Defaults.Name != "z-ai/glm-5.3" || m.inventory.Defaults.Provider != "openrouter" {
		t.Fatal("explicit atomic default selection missing", m.inventory.Defaults)
	}
	owner := nativeMenuRPC[protocol.Session](t, f.connection, "sessions.get", protocol.SessionParams{SessionID: f.owner.ID})
	if owner.Configuration.Model.Name != "configured" {
		t.Fatal("first-run host default changed an existing session")
	}
	if command := m.readSetupSuggestions(); command != nil {
		t.Fatal("already configured default offered replacement on setup")
	}
}

func TestNativeSetupSuggestionsRejectStaleMissingForeignAndUnadvertisedEffort(t *testing.T) {
	m := newNativeMenu(nativeMenuWork(t), nil, nativeMenuOptions{Kind: "setup"})
	defer m.Close()
	m.setup.preset = &protocol.ProviderPreset{ID: "openrouter", SuggestedModels: []string{"exact"}, SuggestedEffort: "max"}
	fresh := protocol.ProviderCatalog{Provider: "openrouter", State: "cached", ScopeState: "current", Discovery: "authenticated_catalog", Models: []protocol.ProviderModel{{ID: "exact", ReasoningEfforts: []string{"low"}}}}
	for _, kind := range []string{"stale", "foreign", "scope", "not-checked", "no-match"} {
		value := fresh
		switch kind {
		case "stale":
			value.Stale = true
		case "foreign":
			value.Provider = "foreign"
		case "scope":
			value.ScopeState = "unverified"
		case "not-checked":
			value.Discovery = "not_checked"
		case "no-match":
			value.Models = []protocol.ProviderModel{{ID: "exact-preview"}}
		}
		m.showSetupSuggestions(value)
		if m.mode == "setup-suggestions" {
			t.Fatal("invalid catalog offered suggested default", kind)
		}
	}
	m.showSetupSuggestions(fresh)
	nativeMenuSelect(t, m, "suggest:exact")
	if m.selection.Effort != "" {
		t.Fatal("unadvertised recommended effort selected", m.selection)
	}
	m.setupForm("setup-key", "Key", "", true)
	if m.input.CharLimit != 64<<10 {
		t.Fatal("key input silently imposed a smaller limit than provider contract")
	}
	m.input.SetValue(strings.Repeat("x", 5000))
	if strings.Contains(m.View(80, 20), strings.Repeat("x", 10)) {
		t.Fatal("long key displayed")
	}
}

func TestNativeSetupVisibleFlowTimerClosesWithWorkOwner(t *testing.T) {
	work := nativeMenuWork(t)
	m := newNativeMenu(work, nil, nativeMenuOptions{Kind: "setup"})
	command := m.accountPoll()
	done := make(chan struct{})
	go func() { defer close(done); command() }()
	m.Close()
	work.close()
	<-done
}
