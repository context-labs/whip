package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

type nativeMessageActions struct {
	owner      protocol.Session
	generation uint64
	snapshot   protocol.HistorySnapshot
	choice     nativeHistoryChoice
	click      nativeSelectionClick
	status     string
}

type nativeMessageClick struct{ actions *nativeMessageActions }

func (m *nativeModel) messageClick(row int) tea.Cmd {
	var id protocol.ID
	for _, block := range m.messageRows {
		if row >= block.start && row < block.end && !block.tool {
			id = block.id
			break
		}
	}
	if id == "" {
		return nil
	}
	view, earlier := &m.history, m.history.earlier
	if m.browse != nil {
		view, earlier = &m.browse.transcript, m.browse.earlier
	}
	if view.snapshot.Revision != m.history.snapshot.Revision {
		return nil
	}
	var keep protocol.Counter
	known := !earlier
	for _, message := range view.messages {
		if message.ID == id {
			if message.Role != "user" && message.Role != "assistant" {
				return nil
			}
			value := &nativeMessageActions{owner: m.owner, generation: m.generation, snapshot: m.history.snapshot, choice: nativeHistoryChoice{message: message, keep: keep, known: known}, click: m.selectionClick}
			return tea.Tick(multiClickWindow, func(time.Time) tea.Msg { return nativeMessageClick{actions: value} })
		}
		keep, known = message.Sequence, true
	}
	return nil
}

func (m *nativeModel) showMessageActions(value nativeMessageClick) {
	a := value.actions
	if a == nil || a.generation != m.generation || a.owner.ID != m.owner.ID || a.click != m.selectionClick || a.click.n != 1 || m.selection != nil || m.historyDialog != nil || m.messageActions != nil || m.menu != nil || m.picker != nil || m.decision != nil || m.palette != nil || m.completion != nil {
		return
	}
	if a.snapshot.Revision != m.selectionRevision() {
		return
	}
	point, visible := m.selectionArea(nativeSelectTranscript).point(a.click.x, a.click.y, false)
	if !visible {
		return
	}
	for _, block := range m.messageRows {
		if point.row >= block.start && point.row < block.end && block.id == a.choice.message.ID && !block.tool {
			m.messageActions = a
			return
		}
	}
}

func (m *nativeModel) messageActionKey(key tea.KeyPressMsg) tea.Cmd {
	a := m.messageActions
	if a == nil {
		return nil
	}
	if a.owner.ID != m.owner.ID || a.generation != m.generation {
		m.messageActions = nil
		return nil
	}
	switch key.String() {
	case "esc":
		m.messageActions = nil
	case "c":
		var text strings.Builder
		for _, part := range a.choice.message.Parts {
			if part.Type != "text" {
				continue
			}
			if len(part.Text)+text.Len()+1 > nativeCopyLimit {
				a.status = "Message text exceeds the 1 MiB clipboard bound; nothing copied."
				return nil
			}
			if text.Len() > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(part.Text)
		}
		command := m.copyText(text.String())
		if command != nil {
			m.messageActions = nil
		} else {
			a.status = m.status
		}
		return command
	case "r", "f":
		if !m.ready || !m.navigationAllowed() {
			a.status = "Resolve pending work and read the current owner before a history control."
			return nil
		}
		opening := a.choice.message.Role == "user" && a.choice.message.OpeningInput
		if key.String() == "r" && !opening {
			a.status = "Rewind/redraft selects an opening input. This message is not an opening input."
			return nil
		}
		d := &nativeHistoryDialog{owner: a.owner, generation: a.generation, snapshot: a.snapshot, entries: []nativeHistoryChoice{a.choice}, title: "Fork of " + string(a.owner.ID)}
		if key.String() == "f" {
			d.fork, d.keep = true, a.choice.message.Sequence
			if opening {
				if !a.choice.known {
					a.status = "Read the preceding history page before selecting this prefix."
					return nil
				}
				var err error
				d.redraft, err = nativeHistoryRedraft(a.owner.ID, a.choice.message)
				if err != nil {
					a.status = err.Error()
					return nil
				}
				d.keep = a.choice.keep
			}
		}
		m.messageActions, m.historyDialog = nil, d
		if !d.fork {
			return m.historyDialogKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
	}
	return nil
}

func (a *nativeMessageActions) view(width, height int) string {
	message := a.choice.message
	preview, _ := nativeTextPrefix(nativeMessageText(message), 512)
	rows := []string{fmt.Sprintf("Message %s · %s · #%d", message.ID, message.Role, message.Sequence), fmt.Sprintf("Captured history %d · configuration %d · owner %s", a.snapshot.Revision, a.owner.ConfigRevision, a.owner.ID), "", "C copies text parts only · F names a fork of this prefix · Esc closes"}
	if message.Role == "user" && message.OpeningInput {
		rows = append(rows, "R rewinds before this input and retains its original parts as an unsent draft.", "Rewind requires an explicitly stopped owner. Fork also redrafts this input.")
	} else {
		rows = append(rows, "Fork keeps through this message; the host requires a terminal whole-group boundary.")
	}
	rows = append(rows, "No workspace files are restored. No automatic Stop, upload, or submission.", "", preview, "", a.status)
	return nativeFixedRows(strings.Join(nativePlainRows(strings.Join(rows, "\n"), max(width, 1), false), "\n"), width, height)
}
