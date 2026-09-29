package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/context-labs/whip/internal/protocol"
)

type nativeRedraft struct {
	owner   protocol.ID
	message protocol.ID
	parts   []protocol.Part
	design  *protocol.DesignContext
}

type nativeHistoryChoice struct {
	message protocol.Message
	keep    protocol.Counter
	known   bool
}

type nativeHistoryDialog struct {
	owner      protocol.Session
	snapshot   protocol.HistorySnapshot
	generation uint64
	entries    []nativeHistoryChoice
	selected   int
	fork       bool
	title      string
	keep       protocol.Counter
	redraft    *nativeRedraft
	status     string
}

func (m *nativeModel) openHistoryDialog(fork bool, keep protocol.Counter) tea.Cmd {
	if !m.navigationAllowed() {
		return nil
	}
	if !m.ready || m.history.snapshot.Revision < 1 {
		m.status = "Read current history before selecting an edit or fork."
		return nil
	}
	view := &m.history
	if m.browse != nil {
		view = &m.browse.transcript
	}
	if view.snapshot.Revision != m.history.snapshot.Revision {
		m.status = "History changed; read the current page before selecting a boundary."
		return nil
	}
	d := &nativeHistoryDialog{owner: m.owner, snapshot: m.history.snapshot, generation: m.generation, fork: fork, keep: keep, title: "Fork of " + string(m.owner.ID)}
	var previous protocol.Counter
	earlier := view.earlier
	if m.browse != nil {
		earlier = m.browse.earlier
	}
	known := !earlier
	for _, message := range view.messages {
		if message.Role == "user" && message.OpeningInput {
			d.entries = append(d.entries, nativeHistoryChoice{message: message, keep: previous, known: known})
		}
		previous, known = message.Sequence, true
	}
	if !fork && len(d.entries) == 0 {
		m.status = "No opening inputs in this loaded history window. Use /older or /latest; /rewind 0 explicitly clears history."
		return nil
	}
	d.selected = max(len(d.entries)-1, 0)
	m.closeCompletion(false)
	m.selection = nil
	m.historyDialog = d
	if strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/") {
		m.input.Reset()
		m.sizeInput()
	}
	return nil
}

func nativeHistoryRedraft(owner protocol.ID, message protocol.Message) (*nativeRedraft, error) {
	// Test representation before changing host history; unsupported original
	// parts remain available through history/export, never silently truncated.
	probe := nativeModel{owner: protocol.Session{ID: owner}, input: newInput(), width: 80, height: 24}
	if !probe.restoreParts(message.Parts) {
		return nil, errors.New("original input exceeds this composer's bounds or has unsupported parts; use explicit history controls/export")
	}
	value := &nativeRedraft{owner: owner, message: message.ID, parts: slices.Clone(message.Parts)}
	if message.DesignContext != nil {
		design := message.DesignContext.DesignContext
		design.Elements = slices.Clone(design.Elements)
		value.design = &design
	}
	return value, nil
}

func (m *nativeModel) historyDialogKey(key tea.KeyPressMsg) tea.Cmd {
	d := m.historyDialog
	if d == nil {
		return nil
	}
	if d.owner.ID != m.owner.ID || d.generation != m.generation {
		m.historyDialog = nil
		m.status = "The selected history owner changed; open the picker again."
		return nil
	}
	switch key.String() {
	case "esc":
		m.historyDialog = nil
		return nil
	case "up":
		if !d.fork {
			d.selected = max(d.selected-1, 0)
		}
	case "down":
		if !d.fork {
			d.selected = min(d.selected+1, len(d.entries)-1)
		}
	case "pgup", "pgdown":
		if !d.fork {
			m.historyDialog = nil
			direction := "backward"
			if key.String() == "pgdown" {
				direction = "forward"
			}
			return m.browseHistory(direction)
		}
	case "ctrl+u":
		if d.fork {
			d.title = ""
		}
	case "backspace":
		if d.fork && d.title != "" {
			_, size := utf8.DecodeLastRuneInString(d.title)
			d.title = d.title[:len(d.title)-size]
		}
	case "enter":
		if d.fork {
			params := protocol.ForkParams{ForkID: protocol.ID(uuid.NewString()), SessionID: d.owner.ID, ExpectedHistoryRevision: d.snapshot.Revision, ExpectedConfigRevision: d.owner.ConfigRevision, ObservedThrough: d.snapshot.ThroughSequence, KeepThrough: d.keep, Title: new(d.title)}
			raw, err := json.Marshal(params)
			if err == nil {
				err = protocol.Validate("ForkParams", raw)
			}
			if err != nil || strings.TrimSpace(d.title) == "" || utf8.RuneCountInString(d.title) > 256 {
				d.status = "Enter a nonempty title of at most 256 characters."
				return nil
			}
			m.historyDialog = nil
			return m.forkPrepared(params, d.redraft)
		}
		if d.owner.Lifecycle != "stopped" {
			d.status = "Stop this owner explicitly first: Esc, /stop, then reopen /rewind. No files are restored."
			return nil
		}
		choice := d.entries[d.selected]
		if !choice.known {
			d.status = "Read the preceding page with Page Up before selecting this boundary."
			return nil
		}
		redraft, err := nativeHistoryRedraft(d.owner.ID, choice.message)
		if err != nil {
			d.status = err.Error()
			return nil
		}
		params := protocol.RewindParams{EditID: protocol.ID(uuid.NewString()), SessionID: d.owner.ID, ExpectedRevision: d.snapshot.Revision, ObservedThrough: d.snapshot.ThroughSequence, KeepThrough: choice.keep}
		m.historyDialog = nil
		return m.rewindPrepared(params, redraft)
	default:
		if d.fork {
			m.historyDialogPaste(key.Text)
		} else if key.String() == "f" {
			choice := d.entries[d.selected]
			if !choice.known {
				d.status = "Read the preceding page before forking this prefix."
				return nil
			}
			redraft, err := nativeHistoryRedraft(d.owner.ID, choice.message)
			if err != nil {
				d.status = err.Error()
				return nil
			}
			d.fork, d.keep, d.redraft, d.status = true, choice.keep, redraft, ""
		}
	}
	return nil
}

func (m *nativeModel) historyDialogPaste(text string) {
	d := m.historyDialog
	if d == nil || !d.fork {
		return
	}
	if utf8.RuneCountInString(d.title)+utf8.RuneCountInString(text) > 256 || strings.IndexFunc(text, unicode.IsControl) >= 0 {
		d.status = "Title input refused: use at most 256 characters without control characters."
		return
	}
	d.title += text
}

func (d *nativeHistoryDialog) view(width, height int) string {
	rows := []string{fmt.Sprintf("History · %s · captured revision %d / configuration %d", d.owner.ID, d.snapshot.Revision, d.owner.ConfigRevision)}
	if d.fork {
		rows = append(rows, fmt.Sprintf("Fork through #%d · fresh root/REPL · same working directory", d.keep), "", "Title: "+d.title, "", "Enter creates this exact fork; Ctrl+U clears its title; Esc closes. No workspace files are restored.")
		if d.redraft != nil {
			rows = append(rows, "The selected original input becomes an unsent draft after the matching receipt.")
		}
	} else {
		rows = append(rows, "Enter rewinds before an input · F forks that prefix · Esc closes", "Page Up/Down reads another bounded page, then reopen /rewind", "Host validates terminal whole-group boundaries; Stop is explicit. Files are unchanged.", "")
		count := max(height-8, 1)
		start := max(min(d.selected-count/2, len(d.entries)-count), 0)
		for i := start; i < len(d.entries) && i < start+count; i++ {
			choice := d.entries[i]
			mark := strings.Repeat(" ", ansi.StringWidth("› "))
			if i == d.selected {
				mark = "› "
			}
			preview, _ := nativeTextPrefix(strings.Join(strings.Fields(nativeMessageText(choice.message)), " "), 200)
			boundary := fmt.Sprintf("keep #%d", choice.keep)
			if !choice.known {
				boundary = "preceding boundary outside page"
			}
			rows = append(rows, fmt.Sprintf("%s#%d · %s · %s", mark, choice.message.Sequence, boundary, preview))
		}
	}
	rows = append(rows, "", d.status)
	for i := range rows {
		rows[i] = ansi.Truncate(nativeDisplayText(rows[i]), width, "…")
	}
	return strings.Join(rows[:min(len(rows), height)], "\n")
}

func (m *nativeModel) stageRedraft(value *nativeRedraft) {
	if value == nil || value.owner != m.owner.ID {
		return
	}
	m.redraft = value
	if m.input.Value() == "" && m.uncertain == nil && m.restoreParts(value.parts) {
		m.draftDesign = value.design
		m.redraft = nil
		m.status += " Original input restored as an unsent draft."
	} else {
		m.status += " Original input staged; current draft kept. /redraft restore|replace|discard."
	}
}

func (m *nativeModel) redraftCommand(args string) tea.Cmd {
	if args == "clear-context" {
		m.draftDesign = nil
		m.status = "Display-only design metadata cleared from this draft; its text and attachments were kept."
		return nil
	}
	if m.redraft == nil || m.redraft.owner != m.owner.ID {
		m.status = "No original input is staged for this owner."
		return nil
	}
	if args == "discard" {
		m.redraft = nil
		m.status = "Staged original discarded locally; host history is unchanged."
		return nil
	}
	if args != "restore" && args != "replace" {
		m.status = "Original input staged: /redraft restore (empty composer), /redraft replace, or /redraft discard."
		return nil
	}
	if args == "restore" && m.input.Value() != "" && !strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/redraft ") {
		m.status = "The composer is not empty. Keep it, or explicitly use /redraft replace."
		return nil
	}
	if !m.restoreParts(m.redraft.parts) {
		m.status = "Original input cannot fit this composer; the staged original was retained."
		return nil
	}
	m.draftDesign, m.redraft = m.redraft.design, nil
	m.status = "Original input restored as an unsent draft; no input was submitted."
	return nil
}

func nativeDesignPartsPresent(design *protocol.DesignContext, parts []protocol.Part) bool {
	if design == nil {
		return true
	}
	contextFound, screenshotFound := false, design.ScreenshotAttachmentID == nil
	for _, part := range parts {
		if part.Type == "content" {
			contextFound = contextFound || part.ReferenceID == design.ContextAttachmentID
			screenshotFound = screenshotFound || design.ScreenshotAttachmentID != nil && part.ReferenceID == *design.ScreenshotAttachmentID
		}
	}
	return contextFound && screenshotFound
}
