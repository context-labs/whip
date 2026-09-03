package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func mkWinSize(w, h int) tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: w, Height: h} }

func TestRenderMarkdownBasics(t *testing.T) {
	out := renderMarkdown("# Title\n\nsome **bold** text\n\n- a\n- b\n\n```go\nfmt.Println()\n```", 80)
	plain := ansi.Strip(out)
	for _, want := range []string{"Title", "bold", "• a", "• b", "fmt.Println()"} {
		if !strings.Contains(plain, want) {
			t.Errorf("rendered output missing %q:\n%s", want, plain)
		}
	}
	// bold is styled, not literal asterisks
	if strings.Contains(plain, "**") {
		t.Errorf("markdown markers should be consumed:\n%s", plain)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Error("expected ANSI styling in rendered output")
	}
}

func TestRenderMarkdownStripsRightPadding(t *testing.T) {
	out := renderMarkdown("short line", 80)
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 12 {
			t.Errorf("line %d padded to width %d (should be unpadded): %q", i, w, l)
		}
	}
}

func TestRenderMarkdownFallback(t *testing.T) {
	if got := renderMarkdown("", 80); got != "" {
		t.Errorf("empty input should pass through, got %q", got)
	}
	// width<=0 is clamped to the minimum render width, never passed through
	// unwrapped (that was the overflow bug)
	out := renderMarkdown("plain text", 0)
	plain := strings.Join(strings.Fields(ansi.Strip(out)), " ")
	if plain != "plain text" {
		t.Errorf("content must survive the clamp, got %q", out)
	}
	for l := range strings.SplitSeq(out, "\n") {
		if ansi.StringWidth(l) > 8 {
			t.Errorf("clamped render must respect width 8: %q", l)
		}
	}
}

func TestRenderMarkdownWrapsToWidth(t *testing.T) {
	long := strings.Repeat("word ", 40)
	out := renderMarkdown(long, 40)
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 40 {
			t.Errorf("line %d exceeds width 40 (%d): %q", i, w, l)
		}
	}
}

func TestIndentLines(t *testing.T) {
	// relative shift: glamour's 2-cell document margin becomes n; deeper
	// lines keep their relative indent (nested bullets, code blocks)
	in := "  first\n\n    second" // margin + 2 extra
	want := "  first\n\n    second"
	if got := indentLines(in, 2); got != want {
		t.Errorf("indentLines:\ngot  %q\nwant %q", got, want)
	}
	// whitespace-only lines become truly empty (no stray styled cells)
	if got := indentLines("  \n  x", 2); got != "\n  x" {
		t.Errorf("blank line should be empty: %q", got)
	}
}

// Assistant segments land in the transcript as raw markdown; rendering (with
// the ● marker and body indent) happens per-width in block.render.
func TestAppendAssistantRendersMarkdown(t *testing.T) {
	m := compactCmdModel()
	m.width = 80
	m.appendAssistant("results:\n\n- **one**\n- two")
	if len(m.blocks) == 0 {
		t.Fatal("no transcript block")
	}
	if m.blocks[0].kind != blockAssistant {
		t.Fatalf("assistant text should be stored raw (blockAssistant), got %v", m.blocks[0].kind)
	}
	rendered := ansi.Strip(m.blocks[0].render(80))
	if !strings.HasPrefix(rendered, "● ") {
		t.Errorf("first line should carry the marker: %q", rendered)
	}
	if !strings.Contains(rendered, "• one") || !strings.Contains(rendered, "• two") {
		t.Errorf("list should be rendered: %q", rendered)
	}
	if strings.Contains(rendered, "**") {
		t.Errorf("markdown markers should be consumed: %q", rendered)
	}
	// continuation segment: merges into the same block (one marker, one doc)
	m.appendAssistant("more text")
	if len(m.blocks) != 1 {
		t.Fatalf("continuation should merge into the open block, got %d blocks", len(m.blocks))
	}
	full := ansi.Strip(m.blocks[0].render(80))
	if strings.Count(full, "● ") != 1 {
		t.Errorf("continuation segment must not add a second marker:\n%s", full)
	}
	if !strings.Contains(full, "more text") {
		t.Errorf("merged content missing: %q", full)
	}
}

// A width change re-renders the whole transcript: assistant markdown reflows
// and status/tool lines re-wrap.
func TestResizeRewrapsTranscript(t *testing.T) {
	m := compactCmdModel()
	m.width = 80
	m.appendAssistant("a paragraph of assistant text that should reflow when the terminal gets narrower")
	m.append(dimStyle.Render("status line with enough words to need rewrapping at a narrow width ok"))
	// narrow the terminal via a WindowSizeMsg (the real resize path)
	tm, _ := m.Update(mkWinSize(40, 24))
	m = tm.(*model)
	for _, b := range m.blocks {
		for i, l := range strings.Split(ansi.Strip(b.render(m.width)), "\n") {
			if w := ansi.StringWidth(l); w > 40 {
				t.Errorf("after resize to 40: block line %d is %d wide: %q", i, w, l)
			}
		}
	}
	// and back wide again
	tm, _ = m.Update(mkWinSize(120, 24))
	m = tm.(*model)
	for _, b := range m.blocks {
		for i, l := range strings.Split(ansi.Strip(b.render(m.width)), "\n") {
			if w := ansi.StringWidth(l); w > 120 {
				t.Errorf("after resize to 120: block line %d is %d wide", i, w)
			}
		}
	}
}

// Table rendering: pipes separate columns, a header rule with box-drawing
// joints, cell content wraps within width, and alignment markers hold. Pins
// the explicit Table style (stock Dark/Light leave separators to lipgloss
// defaults — a dependency bump must not silently unformat tables).
func TestRenderMarkdownTable(t *testing.T) {
	md := "| Name | Age | City |\n|:---|---:|---|\n| Alice | 30 | New York |\n| Bob | 25 | London |"
	out := renderMarkdown(md, 50)
	plain := ansi.Strip(out)
	for _, want := range []string{"│", "─", "Alice", "New York"} {
		if !strings.Contains(plain, want) {
			t.Errorf("table render missing %q:\n%s", want, plain)
		}
	}
	// every rendered line respects width
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 50 {
			t.Errorf("line %d exceeds width 50 (%d): %q", i, w, l)
		}
	}
	// markdown pipes consumed, not literal
	if strings.Contains(plain, "|---|") {
		t.Errorf("table markers should be consumed:\n%s", plain)
	}
}

// A wide table at narrow width wraps cell content instead of overflowing or
// mangling columns.
func TestRenderMarkdownTableNarrow(t *testing.T) {
	md := "| Package | Purpose |\n|---|---|\n| internal/agent | the agent loop with a long description that must wrap around |"
	out := renderMarkdown(md, 40)
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 40 {
			t.Errorf("line %d exceeds width 40 (%d): %q", i, w, l)
		}
	}
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "internal/agent") || !strings.Contains(plain, "wrap") {
		t.Errorf("wrapped table lost content:\n%s", plain)
	}
}

// sanitizeInputView closes every styled line (the input box is written raw
// into the frame — an un-closed SGR piece from bubbles' cursor-line render
// bleeds its style into the status line below, seen as "colors go wrong"
// after a large paste + ctrl+j). Unlike sanitizeView it never trims trailing
// padding: opencode's prompt box and the drag-selection highlight live there.
func TestSanitizeInputView(t *testing.T) {
	// an unterminated styled line gets a reset appended
	in := "\x1b[31mred text"
	got := sanitizeInputView(in)
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("unterminated line must gain a reset, got %q", got)
	}
	// already-closed lines are untouched
	in = "\x1b[31mred\x1b[0m"
	if got := sanitizeInputView(in); got != in {
		t.Fatalf("closed line changed: %q", got)
	}
	// multi-line: every line handled independently
	in = "\x1b[7mrow1\x1b[0m\n\x1b[32mrow2"
	got = sanitizeInputView(in)
	want := "\x1b[7mrow1\x1b[0m\n\x1b[32mrow2\x1b[0m"
	if got != want {
		t.Fatalf("multi-line sanitize = %q, want %q", got, want)
	}
	// trailing styled padding survives (sanitizeView would strip it — that
	// breaks opencode's panel fill)
	in = "\x1b[48;5;236mtext   \x1b[0m"
	if got := sanitizeInputView(in); got != in {
		t.Fatalf("styled padding must survive input sanitize, got %q", got)
	}
	// plain text passes through unchanged
	if got := sanitizeInputView("hello"); got != "hello" {
		t.Fatalf("plain text changed: %q", got)
	}
}
