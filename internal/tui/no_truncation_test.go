package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/llm"
)

// Regression: at a narrow terminal width, a long tool-call command must wrap
// and stay fully visible — never truncated with "…".
func TestToolCallLineWrapsNotTruncates(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(40, 24))
	longCmd := `{"command":"cd /some/deeply/nested/directory && go test ./internal/... -run TestSomething -count=1 -v"}`
	tm, _ := m.Update(toolStartMsg{name: "bash", args: longCmd})
	m = tm.(*model)

	blk := m.blocks[len(m.blocks)-1]
	rendered := ansi.Strip(blk.render(m.width))
	for l := range strings.SplitSeq(rendered, "\n") {
		if strings.Contains(l, "…") {
			t.Fatalf("tool call line truncated: %q", rendered)
		}
		if w := ansi.StringWidth(l); w > 40 {
			t.Fatalf("line exceeds width 40 (%d): %q", w, l)
		}
	}
	// the full command survives: every chunk of the original is present
	joined := strings.Join(strings.Fields(rendered), " ")
	for _, frag := range []string{"go test", "count=1", "TestSomething", "nested/directory"} {
		if !strings.Contains(joined, frag) {
			t.Errorf("rendered command missing %q:\n%s", frag, joined)
		}
	}
}

// Regression: resumed sessions render full tool-call args too (no 120-char cut).
func TestResumedToolCallNotTruncated(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(50, 24))
	args := `{"command":"` + strings.Repeat("echo hello; ", 20) + `"}`
	msg := llm.Message{Role: "assistant"}
	var tc llm.ToolCall
	tc.Function.Name = "bash"
	tc.Function.Arguments = args
	msg.ToolCalls = []llm.ToolCall{tc}
	m.seedTranscript([]llm.Message{msg}, 1)

	var found bool
	for _, b := range m.blocks {
		rendered := ansi.Strip(b.render(m.width))
		if !strings.Contains(rendered, "$ Bash ") {
			continue
		}
		found = true
		if strings.Contains(rendered, "…") {
			t.Fatalf("resumed tool call truncated: %q", rendered)
		}
		for l := range strings.SplitSeq(rendered, "\n") {
			if w := ansi.StringWidth(l); w > 50 {
				t.Fatalf("line exceeds width 50 (%d): %q", w, l)
			}
		}
		joined := strings.Join(strings.Fields(rendered), " ")
		if c := strings.Count(joined, "echo hello;"); c != 20 {
			t.Fatalf("expected all 20 'echo hello;' chunks, got %d:\n%s", c, rendered)
		}
	}
	if !found {
		t.Fatal("no bash tool block rendered from the resumed transcript")
	}
}
