package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

// resumeBrowseModel builds an idle model wired to a temp session store, the
// minimal setup for exercising the startup resume paths (continueRecent and
// openPicker) without driving Run (trust gate + TTY).
func resumeBrowseModel(t *testing.T) *model {
	t.Helper()
	st, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m := compactCmdModel()
	m.store = st
	return m
}

// seedSession persists an ordinary session (with messages so it's not skipped)
// in cwd. LatestInDir filters cwd = ? before ordering, so a single session per
// cwd is deterministic without stamping updated_at (now() has only second
// precision and same-second saves would tie on ORDER BY).
func seedSession(t *testing.T, st *session.Store, cwd, content string) string {
	t.Helper()
	id, err := st.Create(cwd, "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(id, 0, []llm.Message{{Role: "user", Content: content, Authored: true}}, "m", "p"); err != nil {
		t.Fatal(err)
	}
	return id
}

// continueRecent resumes the newest ordinary session in the current directory.
// Seed one in this cwd and an older one elsewhere; the cwd one must win.
func TestContinueRecentResumesNewestInCwd(t *testing.T) {
	m := resumeBrowseModel(t)

	// Sessions in other dirs must never be the pick (cwd filter), regardless of
	// age. Seed two foreign-dir sessions and one in THIS cwd; the local one wins.
	seedSession(t, m.store, "/elsewhere", "other")
	seedSession(t, m.store, "/other-new", "newest-overall")
	want := seedSession(t, m.store, cwd(), "local work")

	if err := m.continueRecent(); err != nil {
		t.Fatalf("continueRecent: %v", err)
	}
	if m.sessionID != want {
		t.Fatalf("continueRecent resumed %q, want newest-in-cwd %q", m.sessionID, want)
	}
}

// continueRecent with no session in the current dir falls through to a fresh
// session and prints a notice — it must not error or resume a foreign dir.
func TestContinueRecentNoSessionInDirStartsFresh(t *testing.T) {
	m := resumeBrowseModel(t)
	// Sessions exist, but none in this cwd.
	seedSession(t, m.store, "/somewhere-else", "x")
	before := len(m.blocks)
	hadSession := m.sessionID

	if err := m.continueRecent(); err != nil {
		t.Fatalf("continueRecent on empty dir should not error: %v", err)
	}
	if m.sessionID != hadSession {
		t.Fatalf("continueRecent resumed a foreign-dir session: sessionID=%q", m.sessionID)
	}
	if len(m.blocks) != before+1 {
		t.Fatalf("expected one notice block, got %d blocks (was %d)", len(m.blocks), before)
	}
	if !strings.Contains(ansi.Strip(m.blocks[before].render(m.width)), "no previous session in this directory") {
		t.Errorf("unexpected notice: %q", m.blocks[before].render(m.width))
	}
}

// --browse opens the interactive picker at startup (the same picker /resume
// uses). Seed a session so the picker has a row, call openPicker, then assert
// the model is in picker mode and enter on the highlighted row resumes it.
func TestBrowseOpensPickerAndEnterResumes(t *testing.T) {
	m := resumeBrowseModel(t)
	want := seedSession(t, m.store, cwd(), "browse me")

	m.openPicker()
	if m.picker == nil {
		t.Fatal("openPicker should set m.picker")
	}
	if len(m.picker.metas) != 1 || m.picker.metas[0].ID != want {
		t.Fatalf("picker row = %+v, want [%s]", m.picker.metas, want)
	}

	// Enter on the highlighted row resumes the session, same as /resume.
	mm, _ := m.pickerKey(tea.KeyMsg{Type: tea.KeyEnter})
	if mm != m {
		t.Fatal("pickerKey should return the same model")
	}
	if m.picker != nil {
		t.Fatal("enter should dismiss the picker")
	}
	if m.sessionID != want {
		t.Fatalf("picker enter resumed %q, want %q", m.sessionID, want)
	}
}

// --browse with no sessions anywhere prints the empty-state notice and leaves
// the model picker-less, instead of erroring.
func TestBrowseNoSessionsPrintsEmptyState(t *testing.T) {
	m := resumeBrowseModel(t)
	before := len(m.blocks)

	m.openPicker()
	if m.picker != nil {
		t.Fatal("openPicker with no sessions should not set a picker")
	}
	if len(m.blocks) != before+1 {
		t.Fatalf("expected one empty-state notice, got %d blocks (was %d)", len(m.blocks), before)
	}
	if !strings.Contains(ansi.Strip(m.blocks[before].render(m.width)), "no previous sessions") {
		t.Errorf("unexpected notice: %q", m.blocks[before].render(m.width))
	}
}
