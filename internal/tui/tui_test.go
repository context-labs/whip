package tui

import (
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestOpenPickerForCWDScopesSessions(t *testing.T) {
	st, err := session.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	save := func(cwd, content string) string {
		t.Helper()
		id, err := st.Create(cwd, "model", "provider")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Save(id, 1, []llm.Message{{Role: "system"}, {Role: "user", Content: content}}, "model", "provider"); err != nil {
			t.Fatal(err)
		}
		return id
	}

	currentID := save("/projects/whip", "current project")
	otherID := save("/projects/other", "another project")
	m := &model{store: st}

	m.openPickerForCWD("/projects/whip")
	if m.picker == nil || len(m.picker.metas) != 1 || m.picker.metas[0].ID != currentID {
		t.Fatalf("scoped picker = %+v, want only %s", m.picker, currentID)
	}

	m.picker = nil
	m.openPicker()
	if m.picker == nil || len(m.picker.metas) != 2 {
		t.Fatalf("/resume picker = %+v, want both sessions", m.picker)
	}
	ids := map[string]bool{}
	for _, meta := range m.picker.metas {
		ids[meta.ID] = true
	}
	if !ids[currentID] || !ids[otherID] {
		t.Fatalf("/resume picker omitted a session: %+v", m.picker.metas)
	}
}
