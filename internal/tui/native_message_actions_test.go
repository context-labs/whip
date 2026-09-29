package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

func nativeMessageActionClick(t *testing.T, m *nativeModel, id protocol.ID) tea.Cmd {
	t.Helper()
	m.follow = false
	for _, row := range m.messageRows {
		if row.id != id {
			continue
		}
		m.vp.SetYOffset(row.start)
		area := m.selectionArea(nativeSelectTranscript)
		y := row.start - m.vp.YOffset()
		m.Update(tea.MouseClickMsg{X: area.x + 1, Y: y, Button: tea.MouseLeft})
		_, command := m.Update(tea.MouseReleaseMsg{X: area.x + 1, Y: y, Button: tea.MouseLeft})
		if command == nil || m.messageActions != nil {
			t.Fatal("single click did not defer its message actions")
		}
		return command
	}
	t.Fatal("message not rendered", id)
	return nil
}

func nativeMessageActionFixture(t *testing.T) *nativeModel {
	t.Helper()
	m := nativeSelectionFixture(t)
	m.ready = true
	m.history.snapshot = protocol.HistorySnapshot{Revision: 1, ThroughSequence: 2}
	opening := nativeMessage(1, "user", "original text")
	opening.OpeningInput = true
	opening.Parts = append(opening.Parts, protocol.Part{Type: "content", ReferenceID: "opaque_attachment"})
	m.history.messages = []protocol.Message{opening, nativeMessage(2, "assistant", "answer")}
	m.refresh()
	return m
}

func TestNativeMessageActionsWaitForSingleClickAndCopyOnlyText(t *testing.T) {
	m := nativeMessageActionFixture(t)
	m.input.SetValue("keep composer")
	command := nativeMessageActionClick(t, m, m.history.messages[0].ID)
	m.Update(command())
	if m.messageActions == nil || m.messageActions.choice.message.ID != m.history.messages[0].ID {
		t.Fatal("single-click actions missing")
	}
	for row := range strings.SplitSeq(m.View().Content, "\n") {
		if ansi.StringWidth(row) > m.width {
			t.Fatal("message menu exceeded terminal width")
		}
	}
	_, command = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if got := nativeSelectionCopied(t, m, command); got != "original text" || m.input.Value() != "keep composer" || m.messageActions != nil {
		t.Fatal("copy read attachment content or changed draft", got, m.input.Value())
	}
}

func TestNativeMessageActionsDiscardInterruptedClick(t *testing.T) {
	for _, change := range []string{"double", "key", "wheel", "resize", "owner", "history", "rows", "modal"} {
		t.Run(change, func(t *testing.T) {
			m := nativeMessageActionFixture(t)
			command := nativeMessageActionClick(t, m, m.history.messages[0].ID)
			switch change {
			case "double":
				click := m.selectionClick
				_, copied := m.Update(tea.MouseClickMsg{X: click.x, Y: click.y, Button: tea.MouseLeft})
				if copied == nil {
					t.Fatal("word-selection gesture was swallowed")
				}
			case "key":
				m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
			case "wheel":
				m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
			case "resize":
				m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			case "owner":
				m.owner.ID = "new-owner"
			case "history":
				m.history.snapshot.Revision++
			case "rows":
				m.messageRows[0].id = "replacement"
			case "modal":
				m.openNativePalette()
			}
			m.Update(command())
			if m.messageActions != nil {
				t.Fatal("interrupted gesture opened stale actions", change)
			}
		})
	}
}

func TestNativeMessageActionsRequireKnownOpeningBoundaryAndCaptureExactFork(t *testing.T) {
	m := nativeMessageActionFixture(t)
	m.history.earlier = true
	m.Update(nativeMessageActionClick(t, m, m.history.messages[0].ID)())
	m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if m.messageActions == nil || m.historyDialog != nil || !strings.Contains(m.messageActions.status, "preceding") {
		t.Fatal("unobserved preceding boundary was guessed")
	}
	m.messageActions = nil
	m.selectionClick = nativeSelectionClick{}
	m.history.earlier = false
	m.Update(nativeMessageActionClick(t, m, m.history.messages[0].ID)())
	captured := m.messageActions
	m.owner.ConfigRevision++
	m.history.snapshot.Revision++
	m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if d := m.historyDialog; d == nil || d.owner.ConfigRevision != captured.owner.ConfigRevision || d.snapshot != captured.snapshot || d.keep != 0 || d.redraft == nil || !reflect.DeepEqual(d.redraft.parts, captured.choice.message.Parts) {
		t.Fatal("menu rebased its exact selection", d)
	}
}

func TestNativeMessageActionsUseCanonicalRewindAndRedraft(t *testing.T) {
	m, _ := nativeUIFixture(t)
	params, _ := nativeHistoryInputFixture(t, m)
	id := m.history.messages[0].ID
	m.Update(nativeMessageActionClick(t, m, id)())
	_, command := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if command != nil || m.historyDialog == nil || !strings.Contains(m.historyDialog.status, "explicitly") {
		t.Fatal("message action silently stopped owner")
	}
	owner, err := m.handle.Get(t.Context())
	if err != nil || owner.Lifecycle != "active" {
		t.Fatal(owner, err)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	nativeUIControl(t, m, "/stop")
	m.input.SetValue("unrelated draft")
	m.Update(nativeMessageActionClick(t, m, id)())
	_, command = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if command == nil {
		t.Fatal(m.status)
	}
	result := command().(nativeControlResult)
	if result.err != nil || result.redraft == nil {
		t.Fatal(result.err, m.status)
	}
	m.Update(result)
	if m.redraft == nil || m.input.Value() != "unrelated draft" || !reflect.DeepEqual(m.redraft.parts, params.Parts) || !reflect.DeepEqual(m.redraft.design, params.DesignContext) {
		t.Fatal("rewind action lost original metadata or replaced destination draft")
	}
	activity, err := m.handle.Activity(t.Context())
	if err != nil || activity.Lifecycle != "stopped" || activity.ActiveTurn != nil || activity.QueuedInputCount != 0 {
		t.Fatal("message action automatically submitted or restarted work", activity, err)
	}
}
