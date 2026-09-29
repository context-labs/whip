package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
)

// UI styles stay legible on both dark and light terminal backgrounds. Lip
// Gloss v2 has no global background state, so refreshBaseStyles rebuilds
// them from whip's own scheme detection whenever it changes.
var youStyle, botStyle, toolStyle, dimStyle, errStyle, thinkingStyle lipgloss.Style

// diff bands (set by refreshBaseStyles): colored background across the
// full row, terminal-default foreground on top (legible on both themes).
var diffAddStyle, diffDelStyle lipgloss.Style

func init() { refreshBaseStyles() }

// refreshBaseStyles picks the light or dark variant of every package-level
// style for the current scheme (see SetLightTheme / SetUnknownTheme).
func refreshBaseStyles() {
	rebuildTheme()
	th := currentTheme()
	youStyle = th.On(th.Info, nil).Bold(true)
	botStyle = th.On(th.Accent, nil).Bold(true)
	toolStyle = th.On(th.Warning, nil)
	dimStyle = th.On(th.Muted, nil)
	errStyle = th.On(th.Error, nil)
	thinkingStyle = th.On(th.Muted, nil).Italic(true)
	diffAddStyle = th.On(nil, th.DiffAdd)
	diffDelStyle = th.On(nil, th.DiffDel)
}

// newInput builds the prompt textarea with whip's keybindings and styling.
// Newlines come from ctrl+j / shift+enter / alt+enter; plain enter submits.
func newInput() textarea.Model {
	ti := textarea.New()
	ti.Placeholder = inputPlaceholder
	ti.Prompt = "" // the enclosing client draws its prompt marker
	ti.SetHeight(1)
	ti.MaxHeight = 24 // input grows with content up to this many lines
	ti.ShowLineNumbers = false
	ti.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("ctrl+j", "shift+enter", "alt+enter"),
		key.WithHelp("ctrl+j", "newline"),
	)
	// The enclosing client owns ctrl+k; do not let the textarea delete text first.
	ti.KeyMap.DeleteAfterCursor = key.NewBinding()
	ti.SetStyles(currentTheme().Textarea)
	ti.SetVirtualCursor(false) // the terminal draws the cursor where View places it
	ti.Focus()
	return ti
}

// inputPlaceholder is the shared idle input hint.
var inputPlaceholder = "Ask whipcode anything… (/ commands, tab completes)"

// bgResult retains the terminal background observation used by theme resolution.
type bgResult struct {
	light, valid bool
	r, g, b      int
	hasRGB       bool
}

var bgCache bgResult
