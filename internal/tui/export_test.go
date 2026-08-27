package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

// TestExportTranscript: /export (via exportTranscript) flattens a fake session
// into a readable markdown log — headings per role, tool-call names under the
// issuing assistant message, tool results under a sub-heading.
func TestExportTranscript(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi", ToolCalls: []llm.ToolCall{{Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "bash", Arguments: `{"cmd":"ls"}`}}}},
		{Role: "tool", Name: "bash", Content: "file.go"},
	}
	path := filepath.Join(t.TempDir(), "out.md")
	if err := exportTranscript(path, msgs); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"# Session transcript", "## User", "hello", "## Assistant", "`bash`", "#### Tool result", "file.go"} {
		if !strings.Contains(s, want) {
			t.Fatalf("export missing %q:\n%s", want, s)
		}
	}
}

// TestExportNothingToExport: an empty transcript export command reports empty
// without writing a file.
func TestExportNothingToExport(t *testing.T) {
	m := &model{width: 80, height: 24}
	before := len(m.blocks)
	if _, cmd := m.command("/export"); cmd != nil {
		t.Error("/export should not return a tea.Cmd")
	}
	if len(m.blocks) != before+1 {
		t.Fatalf("blocks grew by %d, want 1", len(m.blocks)-before)
	}
	if !strings.Contains(m.blocks[before].text, "nothing to export") {
		t.Errorf("unexpected empty-transcript notice: %q", m.blocks[before].text)
	}
}

// TestDisplayRole: known roles map to title-case labels; unknown roles are
// title-cased by first letter (no strings.Title, which is deprecated), and the
// empty string is returned unchanged rather than panicking on role[:1].
func TestDisplayRole(t *testing.T) {
	for in, want := range map[string]string{
		"user":      "User",
		"assistant": "Assistant",
		"system":    "System",
		"tool":      "Tool",
		"custom":    "Custom",
		"":          "",
	} {
		if got := displayRole(in); got != want {
			t.Errorf("displayRole(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestExportFilePerms: the transcript can hold secrets the user pasted, so it
// is written owner-only (0o600), not world-readable.
func TestExportFilePerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.md")
	if err := exportTranscript(path, []llm.Message{{Role: "user", Content: "x"}}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("transcript perms = %o, want 600", perm)
	}
}
