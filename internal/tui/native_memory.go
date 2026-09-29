package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/clientnotes"
)

const nativeNoticeLimit = 64 << 10

type nativeNotesResult struct {
	values [2]*clientnotes.Snapshot
	err    error
}

func (m *nativeModel) memory(args string) tea.Cmd {
	fields := strings.Fields(args)
	which, number := 0, 0
	if len(fields) > 0 {
		var err error
		number, err = strconv.Atoi(fields[0])
		if err != nil || number < 1 || len(fields) > 2 {
			m.status = "usage: /memory [<number> [installation|session]]"
			return nil
		}
		if len(fields) == 2 {
			switch fields[1] {
			case "installation", "install", "global":
			case "session":
				which = 1
			default:
				m.status = "Local note scope must be installation or session."
				return nil
			}
		}
		if m.noteRevisions[which] == "" {
			m.status = "Use /memory to list current local notes before marking one done."
			return nil
		}
	}
	// The CLI supplies its own explicit local home. A remote host's working
	// directory and runtime storage are never interpreted as client paths.
	if m.notesHome == "" {
		m.status = "Local notes are unavailable: no client home was configured."
		return nil
	}
	home, runtimeID, owner, expected := m.notesHome, string(m.connection.Identity()), string(m.handle.ID()), m.noteRevisions[which]
	m.controlling = true
	m.input.Reset()
	return func() tea.Msg {
		ctx, done, err := m.work.begin()
		if err != nil {
			return nativeNotesResult{err: err}
		}
		defer done()
		store, err := clientnotes.Open(home)
		if err != nil {
			return nativeNotesResult{err: err}
		}
		result := nativeNotesResult{}
		session, err := store.Session(runtimeID, owner)
		if err == nil {
			scopes := [2]clientnotes.Scope{store.Installation(), session}
			if number != 0 {
				value, writeErr := scopes[which].Forget(ctx, number, expected)
				err = writeErr
				if err == nil {
					result.values[which] = &value
				}
			} else {
				for i, scope := range scopes {
					value, readErr := scope.Read(ctx)
					if readErr != nil {
						err = readErr
						break
					}
					result.values[i] = &value
				}
			}
		}
		result.err = errors.Join(err, store.Close())
		return result
	}
}

func nativeNotesText(values [2]*clientnotes.Snapshot) string {
	var text strings.Builder
	text.WriteString("Local notes · never sent to the host or model.\n/memory <number> [session] marks done. Edit these client-owned files directly:\n")
	for i, value := range values {
		if value == nil {
			continue
		}
		scope := "installation"
		if i == 1 {
			scope = "session"
		}
		fmt.Fprintf(&text, "\n%s (%s)\n", scope, value.Path)
		if len(value.Entries) == 0 {
			text.WriteString("  Empty. Add a markdown checkbox such as: - [ ] prefers pnpm\n")
		}
		for _, entry := range value.Entries {
			mark := " "
			if entry.Done {
				mark = "x"
			}
			fmt.Fprintf(&text, "  %d. [%s] %s\n", entry.Number, mark, entry.Text)
			if text.Len() > nativeNoticeLimit {
				return nativeBoundedNotice(text.String())
			}
		}
	}
	return nativeBoundedNotice(text.String())
}

func nativeBoundedNotice(value string) string {
	if len(value) <= nativeNoticeLimit {
		return value
	}
	end := nativeNoticeLimit
	for end > 0 && value[end]&0xc0 == 0x80 {
		end--
	}
	return value[:end] + "\nDisplay truncated; the underlying source remains complete."
}
