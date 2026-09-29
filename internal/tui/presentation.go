package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
)

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
