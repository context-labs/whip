package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

func (m *nativeModel) setupCommand(args string) tea.Cmd {
	fields := strings.Fields(args)
	provider, key := "", ""
	if len(fields) > 0 {
		provider = fields[0]
		if provider == "inference" {
			provider = "inference-net"
		}
		key = strings.Join(fields[1:], "")
	}
	if len(provider) > 128 || len(key) > 64<<10 {
		m.status = "Provider or key exceeds the setup form's bounds."
		return nil
	}
	command := m.openMenu("setup")
	if m.menu != nil && m.menu.options.Kind == "setup" {
		m.menu.setupProvider, m.menu.setupKey = provider, key
	}
	return command
}

func (m *nativeMenu) selectInitialSetup() tea.Cmd {
	provider, key := m.setupProvider, m.setupKey
	m.setupProvider, m.setupKey = "", ""
	if provider == "" {
		return nil
	}
	for _, choice := range m.choices {
		if choice.id != "preset:"+provider && choice.id != "route:"+provider {
			continue
		}
		command := m.chooseSetup(choice)
		if key != "" {
			m.chooseSetup(nativeMenuChoice{id: "key"})
			m.input.SetValue(key)
		}
		return command
	}
	m.message = "Unknown provider " + nativeDisplayText(provider) + "; select a listed provider or add a custom route."
	return nil
}

func (m *nativeModel) themeCommand(args string) tea.Cmd {
	command := m.openMenu("theme")
	if args == "" || m.menu == nil || m.menu.options.Kind != "theme" {
		return command
	}
	for _, choice := range m.menu.choices {
		if choice.id == args {
			m.menu.chooseLocalSetting(choice)
			m.preferences = m.menu.Preferences()
			if m.menu.Done() {
				m.menu = nil
			}
			m.refresh()
			return command
		}
	}
	m.menu.message = "Unknown theme " + nativeDisplayText(args) + "; choose a listed theme."
	return command
}

func (m *nativeModel) mouseCommand(args string) tea.Cmd {
	if args != "" {
		m.status = "usage: /mouse"
		return nil
	}
	value, err := updateNativePreferences(m.preferencesDirectory, func(value *nativePreferences) {
		value.Mouse = new(nativePreferenceLabel(value.Mouse, true) != "on")
	})
	if err != nil {
		m.status = err.Error()
		return nil
	}
	m.preferences = value
	m.selection, m.selectionClick = nil, nativeSelectionClick{}
	m.status = "Mouse capture: " + nativePreferenceLabel(value.Mouse, true)
	return nil
}

type nativeModelRoute struct {
	name     string
	provider protocol.ID
}

// Cached discovery is used only to resolve the convenience form. Explicit
// model+provider accepts a valid exact ID without claiming availability.
func readNativeModelRoutes(ctx context.Context, reader nativeAgentReader, inventory protocol.ProviderInventory) ([]nativeModelRoute, error) {
	if len(inventory.Routes) > 128 {
		return nil, errors.New("provider inventory exceeds 128 routes")
	}
	var routes []nativeModelRoute
	bytes := 0
	for _, provider := range inventory.Routes {
		var catalog protocol.ProviderCatalog
		if err := reader.Call(ctx, "providers.catalog", protocol.ProviderParams{Provider: provider.ID}, &catalog); err != nil {
			return nil, err
		}
		if catalog.Provider != provider.ID {
			return nil, errors.New("provider catalog owner mismatch")
		}
		names := map[string]bool{}
		for name := range provider.Models {
			names[name] = true
		}
		for _, row := range catalog.Models {
			names[row.ID] = true
		}
		for name := range names {
			bytes += len(name) + len(provider.ID)
			if len(routes) >= 16384 || bytes > 4<<20 {
				return nil, errors.New("cached model routes exceed 16384 rows or 4 MiB; specify exact model and provider")
			}
			routes = append(routes, nativeModelRoute{name: name, provider: provider.ID})
		}
	}
	slices.SortFunc(routes, func(a, b nativeModelRoute) int {
		if order := strings.Compare(a.name, b.name); order != 0 {
			return order
		}
		return strings.Compare(string(a.provider), string(b.provider))
	})
	return routes, nil
}

func resolveNativeModelRoute(routes []nativeModelRoute, name string, current protocol.ID) (nativeModelRoute, error) {
	matches := map[string]bool{}
	best := 5
	for _, route := range routes {
		if route.name == name {
			matches = map[string]bool{name: true}
			break
		}
		for _, candidate := range []string{route.name, string(route.provider)} {
			if tier := matchTier(candidate, strings.ToLower(name)); tier >= 0 && tier <= best {
				if tier < best {
					best, matches = tier, map[string]bool{}
				}
				matches[route.name] = true
			}
		}
	}
	if len(matches) != 1 {
		return nativeModelRoute{}, fmt.Errorf("model %q is unknown or ambiguous; use /model to inspect routes or specify exact model and provider", name)
	}
	var selected nativeModelRoute
	count := 0
	for _, route := range routes {
		if matches[route.name] {
			if route.provider == current {
				return route, nil
			}
			selected, count = route, count+1
		}
	}
	if count != 1 {
		return nativeModelRoute{}, errors.New("model exists on multiple providers; specify an exact provider")
	}
	return selected, nil
}

func (m *nativeModel) modelCommand(name, args string) tea.Cmd {
	if args == "" {
		return m.openMenu(strings.TrimPrefix(name, "/"))
	}
	if !m.navigationAllowed() {
		return nil
	}
	fields := strings.Fields(args)
	if len(fields) > 2 {
		m.status = "usage: " + name + " [<model> [provider]|refresh]"
		return nil
	}
	owner := m.owner
	if args == "refresh" {
		return m.control("Refresh provider catalogs", false, func(ctx context.Context) nativeControlResult {
			var inventory protocol.ProviderInventory
			if err := m.connection.Call(ctx, "providers.list", protocol.EmptyParams{}, &inventory); err != nil {
				return nativeControlResult{err: err}
			}
			if len(inventory.Routes) > 128 {
				return nativeControlResult{err: errors.New("provider inventory exceeds 128 routes")}
			}
			var rows []string
			for _, route := range inventory.Routes {
				var catalog protocol.ProviderCatalog
				if err := m.connection.Call(ctx, "providers.refresh", protocol.ProviderParams{Provider: route.ID}, &catalog); err != nil {
					return nativeControlResult{notice: strings.Join(rows, "\n"), err: err}
				}
				if catalog.Provider != route.ID {
					return nativeControlResult{err: errors.New("refreshed catalog owner mismatch")}
				}
				rows = append(rows, fmt.Sprintf("%s: %s · %d models · stale %t", route.ID, catalog.State, len(catalog.Models), catalog.Stale))
			}
			return nativeControlResult{notice: strings.Join(rows, "\n") + "\nCatalog discovery does not test inference. No model selection was changed."}
		})
	}
	var inventory *protocol.ProviderInventory
	var selection protocol.ModelSelection
	prepared, hostSaved := false, false
	return m.control("Select model", true, func(ctx context.Context) nativeControlResult {
		if !prepared {
			inventory = new(protocol.ProviderInventory)
			if err := m.connection.Call(ctx, "providers.list", protocol.EmptyParams{}, inventory); err != nil {
				return nativeControlResult{err: err}
			}
			route := nativeModelRoute{name: fields[0]}
			if len(fields) == 2 {
				route.provider = protocol.ID(fields[1])
				if !slices.ContainsFunc(inventory.Routes, func(p protocol.ProviderRoute) bool { return p.ID == route.provider }) {
					return nativeControlResult{err: errors.New("explicit provider is not configured")}
				}
			} else {
				routes, err := readNativeModelRoutes(ctx, m.connection, *inventory)
				if err != nil {
					return nativeControlResult{err: err}
				}
				route, err = resolveNativeModelRoute(routes, fields[0], owner.Configuration.Model.Provider)
				if err != nil {
					return nativeControlResult{err: err}
				}
			}
			selection = owner.Configuration.Model
			selection.Provider, selection.Name, selection.Effort = route.provider, route.name, ""
			prepared = true
		}
		if name == "/model" && !hostSaved {
			var updated protocol.ProviderInventory
			if err := m.connection.Call(ctx, "providers.defaults", protocol.ProviderDefaultsParams{Revision: inventory.Revision, Defaults: protocol.ProviderDefaults{Selection: &selection}}, &updated); err != nil {
				return nativeControlResult{err: fmt.Errorf("default update did not acknowledge success; inspect /model before a new choice: %w", err)}
			}
			hostSaved = true
		}
		var updated protocol.Session
		err := m.connection.Call(ctx, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: owner.ID, ExpectedRevision: owner.ConfigRevision, Patch: protocol.ConfigPatch{Model: &selection}}, &updated)
		if err != nil && hostSaved {
			err = fmt.Errorf("host default was saved; session update did not acknowledge success: %w", err)
		}
		if err == nil && (updated.ID != owner.ID || updated.ConfigRevision <= owner.ConfigRevision || updated.Configuration.Model.Provider != selection.Provider || updated.Configuration.Model.Name != selection.Name) {
			err = errors.New("model selection acknowledgment mismatch")
		}
		return nativeControlResult{owner: &updated, err: err, label: "Model selected; inference availability remains untested"}
	})
}
