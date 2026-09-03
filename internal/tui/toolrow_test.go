package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/llm"
)

func TestToolHeaderRowSubjects(t *testing.T) {
	cases := []struct{ name, args, want string }{
		{"edit", `{"path":"internal/tui/tui.go"}`, "Update(internal/tui/tui.go)"},
		{"write", `{"path":"a.go","content":"x"}`, "Write(a.go)"},
		{"read", `{"path":"a.go"}`, "Read(a.go)"},
		{"bash", `{"command":"git  status"}`, "Bash(git status)"},
	}
	for _, c := range cases {
		if got := ansi.Strip(toolHeaderRow(c.name, c.args, false)); got != "● "+c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, "● "+c.want)
		}
	}
	if got := ansi.Strip(toolHeaderRow("bash", `{"command":"false"}`, true)); got != "● Bash(false)" {
		t.Errorf("failed header: %q", got)
	}
}

func TestExtractDiffAndCounts(t *testing.T) {
	result := "Replaced 1 occurrence(s) in a.go\n```diff\n11   ctx\n12 - old\n12 + new\n13 + more\n14   ctx\n```\na.go:12: some diagnostic"
	diff, rest := extractDiff(result)
	if diff == "" || !strings.Contains(rest, "diagnostic") || strings.Contains(rest, "```") {
		t.Fatalf("extract: diff=%q rest=%q", diff, rest)
	}
	add, del := diffCounts(diff)
	if add != 2 || del != 1 {
		t.Fatalf("counts: +%d -%d", add, del)
	}
	// unnumbered rows classify too, and the cap marker is context
	if diffLineKind("- x") != '-' || diffLineKind("+ x") != '+' || diffLineKind("… +3 more lines") != ' ' {
		t.Fatal("diffLineKind misclassifies")
	}
}

// An edit result renders claude-style: header row keeps the path, the result
// block shows the ⎿ summary, the colored diff, and the trailing diagnostics.
func TestEditResultRendersDiff(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 24))
	m.Update(toolStartMsg{id: "e1", name: "edit", args: `{"path":"a.go","old_string":"x","new_string":"y"}`})
	m.Update(toolEndMsg{
		id: "e1", name: "edit",
		result: "Replaced 1 occurrence(s) in a.go\n```diff\n3   ctx\n4 - x\n4 + y\n```\na.go:4: unused variable",
	})

	run := m.blocks[len(m.blocks)-2]
	if got := ansi.Strip(run.render(m.width)); got != "● Update(a.go)" {
		t.Fatalf("run row: %q", got)
	}
	res := ansi.Strip(m.blocks[len(m.blocks)-1].render(m.width))
	for _, want := range []string{"⎿ Added 1 line, removed 1 line", "4 - x", "4 + y", "unused variable"} {
		if !strings.Contains(res, want) {
			t.Fatalf("result block missing %q:\n%s", want, res)
		}
	}
}

// A long diff caps collapsed and expands in full.
func TestDiffPreviewCapAndExpand(t *testing.T) {
	var rows []string
	for range diffPreviewRows + 5 {
		rows = append(rows, "+ added")
	}
	b := block{kind: blockTool, text: "Wrote 1 bytes to a.go\n```diff\n" + strings.Join(rows, "\n") + "\n```"}
	out := ansi.Strip(b.render(80))
	if !strings.Contains(out, "… +5 more") {
		t.Fatalf("collapsed diff should cap:\n%s", out)
	}
	b.expanded, b.stale = true, true
	if out := ansi.Strip(b.render(80)); strings.Contains(out, "more (ctrl+e") {
		t.Fatal("expanded diff should show everything")
	}
}

// Resumed sessions re-render write/edit diffs from the stored tool results.
func TestSeedTranscriptShowsDiffs(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 24))
	call := llm.Message{Role: "assistant"}
	var tc llm.ToolCall
	tc.Function.Name = "edit"
	tc.Function.Arguments = `{"path":"a.go"}`
	call.ToolCalls = []llm.ToolCall{tc}
	res := llm.Message{Role: "tool", Name: "edit", Content: "Replaced 1 occurrence(s) in a.go\n```diff\n4 - x\n4 + y\n```"}
	m.seedTranscript([]llm.Message{call, res}, 1)

	var joined strings.Builder
	for i := range m.blocks {
		joined.WriteString(ansi.Strip(m.blocks[i].render(m.width)) + "\n")
	}
	for _, want := range []string{"● Update(a.go)", "Added 1 line, removed 1 line", "4 + y"} {
		if !strings.Contains(joined.String(), want) {
			t.Fatalf("resumed transcript missing %q:\n%s", want, joined.String())
		}
	}
}
