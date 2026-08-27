package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A tool call streaming from the model renders a queued row before execution;
// when execution starts, the same tool-call id's running row replaces it
// rather than appending a duplicate.
func TestToolCallQueuedRowReplacedOnStart(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 24))

	m.Update(toolCallMsg{id: "c1", name: "bash", args: `{"command":"make"}`})
	if len(m.blocks) == 0 || m.blocks[len(m.blocks)-1].kind != blockToolQueued {
		t.Fatal("toolCallMsg should append a queued row")
	}
	row := m.blocks[len(m.blocks)-1]
	if !strings.Contains(ansi.Strip(row.render(m.width)), "bash") {
		t.Fatalf("queued row should name the tool, got %q", ansi.Strip(row.render(m.width)))
	}

	before := len(m.blocks)
	m.Update(toolStartMsg{id: "c1", name: "bash", args: `{"command":"make"}`})
	if len(m.blocks) != before {
		t.Fatalf("toolStart should replace the queued row, not append (before=%d after=%d)", before, len(m.blocks))
	}
	last := m.blocks[len(m.blocks)-1]
	if last.kind != blockToolRun || !last.toolRunning {
		t.Fatalf("after toolStart the row should be a running row, got kind=%v running=%v", last.kind, last.toolRunning)
	}
}
