package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A running tool renders a verb line; when the result lands, the same block
// collapses to one line — red on failure — and ctrl+e expands it back.
func TestToolRowCollapsesOnCompletion(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 24))

	m.Update(toolStartMsg{id: "c1", name: "read", args: `{"path":"foo.go"}`})
	if len(m.blocks) == 0 || m.blocks[len(m.blocks)-1].kind != blockToolRun {
		t.Fatal("toolStart should append a running row")
	}
	row := m.blocks[len(m.blocks)-1]
	if !row.toolRunning {
		t.Fatal("row should be running")
	}
	if got := ansi.Strip(row.render(m.width)); !strings.Contains(got, "Reading") {
		t.Fatalf("running row should show the verb, got %q", got)
	}

	m.Update(toolEndMsg{id: "c1", name: "read", result: "file body\nline2\nline3\nline4\nline5\nline6"})
	row = m.blocks[len(m.blocks)-2] // the run row; the result block follows it
	if row.toolRunning {
		t.Fatal("completion should stop the run state")
	}
	got := ansi.Strip(row.render(m.width))
	if strings.Count(got, "\n") > 0 {
		t.Fatalf("completed row should collapse to one line, got %q", got)
	}
	// the collapse keeps the call visible: "icon Verb subject"
	if !strings.Contains(got, "Read foo.go") {
		t.Fatalf("completed row should keep the call header, got %q", got)
	}
	// the result renders in the blockTool below, tied by the ⎿ marker
	res := m.blocks[len(m.blocks)-1]
	if res.kind != blockTool {
		t.Fatal("the result block should follow the run row")
	}
	if got := ansi.Strip(res.render(m.width)); !strings.Contains(got, "↳ 6 lines") {
		t.Fatalf("collapsed result block should show the lines hint, got %q", got)
	}
	if !row.toggle() {
		t.Fatal("ctrl+e should still toggle the collapsed row")
	}
}

// A failed tool collapses to a red one-liner.
func TestToolRowFailureIsRed(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 24))
	m.Update(toolStartMsg{id: "c1", name: "bash", args: `{"command":"false"}`})
	m.Update(toolEndMsg{id: "c1", name: "bash", result: "Error: exit status 1"})
	var run *block
	for i := range m.blocks {
		if m.blocks[i].kind == blockToolRun {
			run = &m.blocks[i]
		}
	}
	if run == nil || !run.toolFailed {
		t.Fatal("a failed tool should mark the collapsed row")
	}
	if got := ansi.Strip(run.render(m.width)); !strings.Contains(got, "Bash false") {
		t.Fatalf("failed row should keep the call header, got %q", got)
	}
	// the error text renders red in the result block below
	res := m.blocks[len(m.blocks)-1]
	if got := ansi.Strip(res.render(m.width)); !strings.Contains(got, "Error: exit status 1") {
		t.Fatalf("result block should carry the error text, got %q", got)
	}
}
