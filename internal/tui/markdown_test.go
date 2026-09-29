package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

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
	plain := strings.Join(strings.Fields(ansi.Strip(out)), "")
	if plain != "plaintext" { // a degenerate width may break a word across rows
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
