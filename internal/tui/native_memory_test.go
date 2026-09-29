package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/clientnotes"
)

func nativeUIMemory(t *testing.T, m *nativeModel, text string) nativeNotesResult {
	t.Helper()
	m.input.SetValue(text)
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal("memory command not prepared", m.status)
	}
	value, ok := command().(nativeNotesResult)
	if !ok {
		t.Fatal("wrong memory result")
	}
	m.Update(value)
	return value
}

func TestNativeMemoryListsAndMarksOnlyClientOwnedNotes(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.notesHome = t.TempDir()
	store, err := clientnotes.Open(m.notesHome)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope, err := store.Session(string(m.connection.Identity()), string(m.handle.ID()))
	if err != nil {
		t.Fatal(err)
	}
	installation, err := store.Installation().Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	local, err := scope.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(filepath.Base(local.Path), string(m.handle.ID())) || !strings.Contains(local.Path, "client-v4") {
		t.Fatal("raw owner used as path", local.Path)
	}
	if err := os.WriteFile(installation.Path, []byte("# Notes\n- [ ] keep global\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local.Path, []byte("# Scoped\r\n- [ ] keep source bytes\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(m.notesHome, "memory.md")
	if err := os.WriteFile(old, []byte("untouched retired note\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("/memory 1 session")
	if command := m.submit(); command != nil || !strings.Contains(m.status, "list current") {
		t.Fatal("edit proceeded without a listed revision", m.status)
	}
	if result := nativeUIMemory(t, m, "/memory"); result.err != nil || !strings.Contains(m.notice, "keep global") || !strings.Contains(m.notice, "never sent") {
		t.Fatal(result.err, m.notice)
	}
	if result := nativeUIMemory(t, m, "/memory 1 session"); result.err != nil || !result.values[1].Entries[0].Done {
		t.Fatal(result.err)
	}
	raw, err := os.ReadFile(local.Path)
	if err != nil || string(raw) != "# Scoped\r\n- [x] keep source bytes\r\n" {
		t.Fatal(string(raw), err)
	}
	if raw, err := os.ReadFile(old); err != nil || string(raw) != "untouched retired note\n" {
		t.Fatal(string(raw), err)
	}
	if len(m.history.messages) != 0 || !strings.Contains(strings.Join(m.rows, "\n"), "not conversation history") {
		t.Fatal("local note presentation became canonical history")
	}
	page, err := m.handle.Inputs(t.Context(), "all", nil, 20)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("memory manufactured a host input", page, err)
	}
}

func TestNativeMemoryRejectsAChangedListedRevision(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.notesHome = t.TempDir()
	listed := nativeUIMemory(t, m, "/memory")
	if listed.err != nil {
		t.Fatal(listed.err)
	}
	path := listed.values[0].Path
	if err := os.WriteFile(path, []byte("- [ ] newly written\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if result := nativeUIMemory(t, m, "/memory 1"); result.err == nil || m.noteRevisions[0] != "" || m.retryControl != nil {
		t.Fatal("stale local edit accepted or offered blind retry", result.err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "- [ ] newly written\n" {
		t.Fatal(string(raw), err)
	}
	if result := nativeUIMemory(t, m, "/memory"); result.err != nil {
		t.Fatal(result.err)
	}
	if result := nativeUIMemory(t, m, "/memory 1 global"); result.err != nil {
		t.Fatal(result.err)
	}
}

func TestNativeMemoryRequiresExplicitLocalHomeAndBoundsDisplay(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.owner.WorkingDirectory = "/remote/host/private/path"
	m.input.SetValue("/memory")
	if command := m.submit(); command != nil || !strings.Contains(m.status, "no client home") {
		t.Fatal("remote path substituted for local notes home", m.status)
	}
	value := [2]*clientnotes.Snapshot{{Path: "/local/notes.md", Entries: []clientnotes.Entry{{Number: 1, Text: strings.Repeat("界", nativeNoticeLimit)}}}, nil}
	text := nativeNotesText(value)
	if len(text) > nativeNoticeLimit+100 || !utf8.ValidString(text) || !strings.Contains(text, "Display truncated") {
		t.Fatal("unbounded or damaged notice", len(text))
	}
}
