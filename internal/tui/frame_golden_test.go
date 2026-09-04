package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/context-labs/whip/internal/session"
)

// TestFrameGolden pins the full-screen frame for a small scripted session:
// a user turn, a completed tool row, assistant markdown with a list, a code
// block and a link, and the sidebar with an agent tree. It guards layout
// drift through the Bubble Tea v2 migration and the design-system work.
// Regenerate deliberately with: go test ./internal/tui -run TestFrameGolden -update
func TestFrameGolden(t *testing.T) {
	pinDarkTheme(t)
	golden.RequireEqual(t, []byte(ansi.Strip(viewStr(goldenModel(140, 40)))))
}

// goldenModel builds the scripted session at a terminal size.
func goldenModel(w, h int) *model {
	m := compactCmdModel()
	fixed := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixed }
	m.sessTitle = "Golden session"
	m.clientView.workingDir = "/work/whip" // the status line shows it: never the checkout path
	m.clientView.agents = []session.RuntimeAgent{
		{ID: "root-agent", LifecyclePhase: "running"},
		{ID: "root-agent:ba06cc4c6983c16d", ParentID: "root-agent", Name: "file-reader", LifecyclePhase: "idle"},
	}
	m.Update(mkWinSize(w, h))
	m.append(" ❯ find the config loader")
	m.Update(toolStartMsg{id: "t1", name: "read", args: `{"path":"internal/config/config.go"}`})
	m.Update(toolEndMsg{id: "t1", name: "read", result: "package config\n\nfunc Load() (*Config, error) {\n\treturn load(Dir())\n}\n"})
	m.appendAssistant("Found it. **Load** reads the JSON file:\n\n1. resolve the directory\n2. decode the file\n\n```go\ncfg, err := config.Load()\n```\n\nSee [the docs](https://example.com/config).")
	m.input.SetValue("")
	m.layout()
	return m
}

// TestFrameGoldenVariants pins every layout branch the compositor work must
// preserve: the narrow frame with the agents dock, the REPL panel, a dialog
// over the dimmed frame, the toast, the light theme (stripped and styled, so
// token changes show up), and the neutral theme on a 16-color terminal.
// Regenerate deliberately with: go test ./internal/tui -run TestFrameGoldenVariants -update
func TestFrameGoldenVariants(t *testing.T) {
	pinDarkTheme(t)
	plain := func(t *testing.T, m *model) { golden.RequireEqual(t, []byte(ansi.Strip(viewStr(m)))) }
	t.Run("79x24-dock", func(t *testing.T) { plain(t, goldenModel(79, 24)) })
	t.Run("160x40-repl", func(t *testing.T) {
		m := goldenModel(160, 40)
		m.replPanel = true
		m.recalcWidth()
		m.layout()
		plain(t, m)
	})
	t.Run("palette", func(t *testing.T) {
		m := goldenModel(140, 40)
		m.openThinThemePalette()
		plain(t, m)
	})
	t.Run("toast", func(t *testing.T) {
		m := goldenModel(140, 40)
		m.toast = "Copied to clipboard"
		plain(t, m)
	})
	t.Run("light", func(t *testing.T) {
		SetLightTheme(true)
		defer SetLightTheme(false)
		m := goldenModel(140, 40)
		t.Run("plain", func(t *testing.T) { plain(t, m) })
		t.Run("styled", func(t *testing.T) { golden.RequireEqual(t, []byte(viewStr(m))) })
	})
	t.Run("neutral-ansi16", func(t *testing.T) {
		SetUnknownTheme()
		setThemeProfile(colorprofile.ANSI)
		defer func() {
			setThemeProfile(colorprofile.TrueColor)
			SetLightTheme(false)
		}()
		plain(t, goldenModel(140, 40))
	})
}

// pinDarkTheme puts the process-global scheme in the state the goldens were
// recorded under and restores it afterwards, so frame comparisons never
// depend on which test ran before.
func pinDarkTheme(t *testing.T) {
	t.Helper()
	SetLightTheme(false)
	t.Cleanup(func() { SetLightTheme(false) })
}
