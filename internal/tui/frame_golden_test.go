package tui

import (
	"testing"
	"time"

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
	m := compactCmdModel()
	fixed := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixed }
	m.sessTitle = "Golden session"
	m.clientView.agents = []session.RuntimeAgent{
		{ID: "root-agent", LifecyclePhase: "running"},
		{ID: "root-agent:ba06cc4c6983c16d", ParentID: "root-agent", Name: "file-reader", LifecyclePhase: "idle"},
	}
	m.Update(mkWinSize(140, 40))
	m.append(" ❯ find the config loader")
	m.Update(toolStartMsg{id: "t1", name: "read", args: `{"path":"internal/config/config.go"}`})
	m.Update(toolEndMsg{id: "t1", name: "read", result: "package config\n\nfunc Load() (*Config, error) {\n\treturn load(Dir())\n}\n"})
	m.appendAssistant("Found it. **Load** reads the JSON file:\n\n1. resolve the directory\n2. decode the file\n\n```go\ncfg, err := config.Load()\n```\n\nSee [the docs](https://example.com/config).")
	m.input.SetValue("")
	m.layout()
	golden.RequireEqual(t, []byte(ansi.Strip(viewStr(m))))
}
