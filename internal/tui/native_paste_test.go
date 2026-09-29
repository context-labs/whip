package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativePastedOriginalIsJournaledBeforeSend(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.recovery = nativeJournal(t, t.TempDir(), m.connection.Identity())
	m.preferences.CollapsePaste = new(true)
	first := "  first\tcolumn\nsecond\r\n third  \n"
	m.Update(tea.PasteMsg{Content: first})
	chip := m.input.Value()
	if !strings.Contains(chip, "Pasted text") || strings.Contains(chip, "column") {
		t.Fatal(chip)
	}
	m.input.InsertString(" between ")
	// A literal chip in pasted data must not recursively expand.
	second := " literal " + chip + "\n fourth\n fifth "
	m.Update(tea.PasteMsg{Content: second})
	want := first + " between " + second
	send := m.submit()
	if send == nil || len(m.pastes) != 0 {
		t.Fatal("paste submission not prepared", m.status)
	}
	restored, err := m.recovery.restore(m.connection, m.owner.ID)
	if err != nil || restored == nil {
		t.Fatal("no immutable pre-send record", err)
	}
	var params protocol.SubmitParams
	if err := json.Unmarshal(restored.Record().Params, &params); err != nil || params.Parts[0].Text != want {
		t.Fatal("record altered original pasted text", params, err)
	}
	before, err := m.handle.History(t.Context(), protocol.HistoryPageParams{Direction: "forward", Limit: 10})
	if err != nil || len(before.Messages) != 0 {
		t.Fatal("preparing paste caused admission", before, err)
	}
	result := send().(nativeSubmission)
	m.Update(result)
	if result.err != nil || result.admission.Input == nil || result.admission.Input.Parts[0].Text != want {
		t.Fatal("host admitted changed paste", result)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := result.command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := m.handle.History(ctx, protocol.HistoryPageParams{Direction: "forward", Limit: 10})
	if err != nil || page.Messages[0].Parts[0].Text != want {
		t.Fatal("canonical history lost original whitespace", page, err)
	}
}

func TestNativePastePreferencesBoundsAndOwnerDrafts(t *testing.T) {
	m, _ := nativeUIFixture(t)
	text := "line one\nline two\nline three"
	m.Update(tea.PasteMsg{Content: text})
	if m.input.Value() != text || len(m.pastes) != 0 || m.input.Height() != 3 || m.input.ScrollYOffset() != 0 {
		t.Fatal("default paste altered or clipped", m.input.Value(), m.input.Height(), m.input.ScrollYOffset())
	}
	m.preferences.CollapsePaste = new(true)
	m.input.Reset()
	m.Update(tea.PasteMsg{Content: "only\ntwo"})
	if m.input.Value() != "only\ntwo" || len(m.pastes) != 0 {
		t.Fatal("short paste collapsed")
	}
	m.input.Reset()
	m.Update(tea.PasteMsg{Content: text})
	chip := m.input.Value()
	root := m.owner
	other := nativeNavigationRoot(t, m, "paste-other", "Other")
	if err := m.attachSession(other); err != nil || len(m.pastes) != 0 || m.input.Value() != "" {
		t.Fatal("paste leaked to another owner", err)
	}
	m.input.SetValue(chip)
	if value, err := m.expandPastes(chip); err != nil || value != chip {
		t.Fatal("unknown chip expanded", value, err)
	}
	if err := m.attachSession(root); err != nil || m.input.Value() != chip {
		t.Fatal("root draft lost", err)
	}
	if value, err := m.expandPastes(m.input.Value()); err != nil || value != text {
		t.Fatal("root paste registry lost", value, err)
	}
	for range 7 {
		m.Update(tea.PasteMsg{Content: text})
	}
	before := m.input.Value()
	m.Update(tea.PasteMsg{Content: text})
	if m.input.Value() != before || len(m.pastes) != 8 || !strings.Contains(m.status, "eight") {
		t.Fatal("chip bound discarded prior text", m.status)
	}
	m.input.Reset()
	m.Update(tea.PasteMsg{Content: strings.Repeat("x", nativeDraftLimit+1)})
	if m.input.Value() != "" || !strings.Contains(m.status, "256 KiB") {
		t.Fatal("oversized paste silently truncated", m.status)
	}
	m.Update(tea.PasteMsg{Content: text})
	if len(m.pastes) != 1 || m.input.Value() == chip {
		t.Fatal("deleted chips retained or identity reused", m.input.Value())
	}
	large := strings.Repeat("z", nativeDraftLimit/2)
	m.pastes = map[string]string{"[x]": large}
	if _, err := m.expandPastes("[x][x][x]"); err == nil {
		t.Fatal("repeated chip exceeded expanded bound")
	}
}

func TestNativePastePreservesEditorUnsafeWhitespaceAndGrowsWithinFrame(t *testing.T) {
	m, _ := nativeUIFixture(t)
	for _, text := range []string{"one\ttwo\r\n", strings.Repeat("\n", 10000)} {
		m.input.Reset()
		m.Update(tea.PasteMsg{Content: text})
		if value, err := m.expandPastes(m.input.Value()); err != nil || value != text {
			t.Fatal("pinned editor changed paste", err)
		}
	}
	m.input.Reset()
	m.pastes = nil
	m.Update(tea.PasteMsg{Content: strings.Repeat("visible\n", 40)})
	if strings.Count(m.input.Value(), "\n") != 40 {
		t.Fatal("visible paste truncated")
	}
	for _, size := range [][2]int{{8, 4}, {80, 24}, {150, 40}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		rows := strings.Split(m.View().Content, "\n")
		if len(rows) > size[1] || m.input.Height() > 24 {
			t.Fatal(size, len(rows), m.input.Height())
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > size[0] {
				t.Fatal("growing input exceeded frame", size)
			}
		}
	}
	m.input.Reset()
	m.Update(tea.PasteMsg{Content: "one\ntwo\nthree"})
	m.input.CursorUp()
	m.input.SetCursorColumn(1)
	line, column := m.input.Line(), m.input.Column()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.input.Line() != line || m.input.Column() != column || m.input.Height() != 3 {
		t.Fatal("resize moved editor cursor", m.input.Line(), m.input.Column())
	}
}

func TestNativeRejectedPasteRestoresExactOriginal(t *testing.T) {
	m, _ := nativeUIFixture(t)
	original := " \tone\r\ntwo\nthree "
	command, err := m.handle.Submission(protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "tui", RequestID: "rejected-paste"}, Source: "user", Parts: []protocol.Part{{Type: "text", Text: original}}})
	if err != nil {
		t.Fatal(err)
	}
	m.rejected = command
	if !m.restoreRejectedDraft() || m.rejected != nil {
		t.Fatal("rejected original could not be restored")
	}
	if value, err := m.expandPastes(m.input.Value()); err != nil || value != original {
		t.Fatal("rejected original lost whitespace", value, err)
	}
}
