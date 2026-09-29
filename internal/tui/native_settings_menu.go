package tui

import (
	"errors"
	"image/color"
	"strings"

	"github.com/charmbracelet/colorprofile"

	"github.com/context-labs/whip/internal/tui/theme"
)

func (m *nativeMenu) Preferences() nativePreferences { return m.preferences }

func (m *nativeMenu) openLocalSettings() {
	value, err := readNativePreferences(m.options.PreferencesDirectory)
	if err != nil {
		m.mode, m.title, m.message = "settings", "Local settings", err.Error()
		m.choices = nil
		return
	}
	m.preferences = value
	if m.options.Kind == "theme" {
		m.openThemes()
		return
	}
	m.showLocalSettings()
}

func (m *nativeMenu) showLocalSettings() {
	m.mode, m.title = "settings", "Local terminal settings"
	m.resetInput()
	m.choices = []nativeMenuChoice{
		{id: "theme", label: "Theme · " + nativeThemeLabel(m.preferences.Theme), detail: "Preview themes without saving; Escape restores the previous appearance."},
		{id: "mouse", label: "Mouse capture · " + nativePreferenceLabel(m.preferences.Mouse, true), detail: "Client preference only. Disable to select terminal text normally."},
		{id: "thinking", label: "Thinking · " + nativePreferenceLabel(m.preferences.Thinking, true)},
		{id: "collapse_paste", label: "Collapse pasted text · " + nativePreferenceLabel(m.preferences.CollapsePaste, true)},
		{id: "sidebar", label: "Sidebar · " + nativePreferenceLabel(m.preferences.Sidebar, true)},
		{id: "repl", label: "REPL panel · " + nativePreferenceLabel(m.preferences.Repl, false)},
	}
	m.message = "Saved on this terminal client. Host credentials and session configuration are unchanged."
}

func nativePreferenceLabel(value *bool, fallback bool) string {
	if value != nil {
		fallback = *value
	}
	if fallback {
		return "on"
	}
	return "off"
}

func nativeThemeLabel(value string) string {
	if value == "" {
		return "auto"
	}
	return value
}

func (m *nativeMenu) openThemes() {
	specs, failures := theme.Load(m.options.PreferencesDirectory)
	themeMu.Lock()
	userThemes = specs
	themeMu.Unlock()
	if !m.previewing {
		mdMu.Lock()
		m.originalTheme = mdScheme
		mdMu.Unlock()
		m.previewing = true
	}
	m.mode, m.title = "theme", "Choose theme"
	m.resetInput()
	m.choices = nil
	for _, name := range themeNames() {
		m.choices = append(m.choices, nativeMenuChoice{id: name, label: name})
	}
	for i, choice := range m.choices {
		if choice.id == nativeThemeLabel(m.preferences.Theme) {
			m.selected = i
			break
		}
	}
	if err := errors.Join(failures...); err != nil {
		m.message = "Some custom themes could not be read: " + err.Error()
	}
}

func (m *nativeMenu) previewTheme() {
	if m.mode != "theme" {
		return
	}
	choices := m.visibleChoices()
	if len(choices) == 0 {
		return
	}
	pick := choices[min(m.selected, len(choices)-1)].id
	if pick == "auto" {
		pick = ""
	}
	setSchemeOverride(pick)
}

func (m *nativeMenu) chooseLocalSetting(choice nativeMenuChoice) {
	if m.mode == "settings" && choice.id == "theme" {
		m.openThemes()
		return
	}
	// Re-read before changing one field so another client preference edit is not
	// replaced with the stale snapshot displayed by this menu.
	value, err := readNativePreferences(m.options.PreferencesDirectory)
	if err != nil {
		m.message = err.Error()
		return
	}
	switch m.mode {
	case "theme":
		value.Theme = choice.id
		if value.Theme == "auto" {
			value.Theme = ""
		}
	case "settings":
		switch choice.id {
		case "mouse":
			value.Mouse = new(nativePreferenceLabel(value.Mouse, true) != "on")
		case "thinking":
			value.Thinking = new(nativePreferenceLabel(value.Thinking, true) != "on")
		case "collapse_paste":
			value.CollapsePaste = new(nativePreferenceLabel(value.CollapsePaste, true) != "on")
		case "sidebar":
			value.Sidebar = new(nativePreferenceLabel(value.Sidebar, true) != "on")
		case "repl":
			value.Repl = new(nativePreferenceLabel(value.Repl, false) != "on")
		}
	}
	if err := saveNativePreferences(m.options.PreferencesDirectory, value); err != nil {
		m.message = err.Error()
		return
	}
	m.preferences = value
	if m.mode == "theme" {
		setSchemeOverride(value.Theme)
		m.previewing = false
		if m.options.Kind == "theme" {
			m.Close()
			return
		}
	}
	m.showLocalSettings()
	m.message = "Saved " + strings.ReplaceAll(choice.id, "_", " ") + " on this client."
}

func nativeThemeSwatch(name string) []color.Color {
	spec := theme.Dark()
	if name == "auto" {
		if schemeIsLight() {
			spec = theme.Light()
		}
	} else {
		pick := pinnedSpec(name)
		if pick == nil {
			return nil
		}
		spec = *pick
	}
	resolved := theme.Resolve(spec, nil, colorprofile.TrueColor)
	return []color.Color{resolved.Primary, resolved.Accent, resolved.Success, resolved.Error}
}
