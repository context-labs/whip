package tui

import (
	"context"
	"strconv"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/tools"
)

// Question prompts: the model's `question` tool pauses the turn and a modal
// lists the options (opencode-style: numbered rows, dim description, a
// "type your own answer" row). ↑/↓ or j/k move, 1-9 jump, enter picks, space
// toggles in multi-select, esc dismisses. Same goroutine hand-off as the
// permission gate: the tool goroutine blocks on reply until the UI answers.

type askRequest struct {
	req   tools.AskRequest
	reply chan askAnswer
}

type askAnswer struct {
	answers []string
	ok      bool
}

// askDialog is the UI-thread modal state while a question is open.
type askDialog struct {
	req     tools.AskRequest
	reply   chan askAnswer
	sel     int          // row index; len(options) is the custom row
	picked  map[int]bool // multi-select toggles
	editing bool         // typing the custom answer
	custom  string
}

// askClose drops the dialog for a request whose tool call was cancelled.
type askClose struct{ reply chan askAnswer }

// installAskHook wires tools.Ask to the modal. Called once at startup.
// One question at a time: parallel question calls in one tool batch queue on
// the mutex instead of overwriting each other's dialog. ctx cancel (ctrl+c,
// esc interrupt) releases the tool goroutine and closes the dialog.
func (m *model) installAskHook() {
	var mu sync.Mutex
	tools.Ask = func(ctx context.Context, req tools.AskRequest) ([]string, bool) {
		if m.prog == nil {
			return nil, false // headless: no one to ask
		}
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil {
			return nil, false
		}
		reply := make(chan askAnswer, 1)
		m.prog.Send(askRequest{req: req, reply: reply}) //nolint:uilock // background: the calling tool goroutine, which blocks on reply — never the event loop
		select {
		case ans := <-reply:
			return ans.answers, ans.ok
		case <-ctx.Done():
			m.prog.Send(askClose{reply: reply}) //nolint:uilock // background: same tool goroutine
			return nil, false
		}
	}
}

// askKey handles keys while the dialog is open. Returns (handled).
func (m *model) askKey(msg tea.KeyMsg) bool {
	d := m.askDialog
	if d == nil {
		return false
	}
	n := len(d.req.Options)
	rows := n + 1 // + custom row
	answer := func(a askAnswer) {
		d.reply <- a
		m.askDialog = nil
	}
	submit := func() bool {
		var out []string
		for i, o := range d.req.Options {
			if d.picked[i] {
				out = append(out, o.Label)
			}
		}
		if c := strings.TrimSpace(d.custom); c != "" {
			out = append(out, c)
		}
		if len(out) == 0 {
			return false // nothing chosen yet
		}
		answer(askAnswer{answers: out, ok: true})
		return true
	}
	choose := func(i int) {
		if i == n { // custom row
			d.editing = true
			return
		}
		if d.req.Multiple {
			d.picked[i] = !d.picked[i]
			return
		}
		answer(askAnswer{answers: []string{d.req.Options[i].Label}, ok: true})
	}

	if d.editing {
		switch msg.Type {
		case tea.KeyEnter:
			d.editing = false
			if d.req.Multiple {
				submit() // custom text and/or toggles; no-op when both empty
			} else if c := strings.TrimSpace(d.custom); c != "" {
				answer(askAnswer{answers: []string{c}, ok: true})
			}
		case tea.KeyEsc:
			d.editing = false
		case tea.KeyBackspace:
			if len(d.custom) > 0 {
				d.custom = d.custom[:len(d.custom)-1]
			}
		case tea.KeyRunes, tea.KeySpace: // KeySpace arrives with Runes == " "
			d.custom += string(msg.Runes)
		}
		return true
	}
	switch msg.Type {
	case tea.KeyUp:
		d.sel = (d.sel + rows - 1) % rows
	case tea.KeyDown, tea.KeyTab:
		d.sel = (d.sel + 1) % rows
	case tea.KeyEnter:
		if d.req.Multiple && d.sel != n {
			submit() // on the custom row enter edits; enter while editing submits
			return true
		}
		choose(d.sel)
	case tea.KeySpace:
		choose(d.sel)
	case tea.KeyEsc:
		answer(askAnswer{ok: false})
	case tea.KeyCtrlC:
		answer(askAnswer{ok: false})
		if m.busy && m.cancel != nil {
			m.cancel() // interrupt the turn, not just the question
		}
	case tea.KeyRunes:
		s := string(msg.Runes)
		switch s {
		case "k":
			d.sel = (d.sel + rows - 1) % rows
		case "j":
			d.sel = (d.sel + 1) % rows
		default:
			if i, err := strconv.Atoi(s); err == nil && i >= 1 && i <= rows {
				d.sel = i - 1
				choose(d.sel)
			}
		}
	}
	return true
}

// askView renders the modal: the question, numbered options with dim
// descriptions, and the custom-answer row.
func (m *model) askView() string {
	d := m.askDialog
	if d == nil {
		return ""
	}
	var b strings.Builder
	title := d.req.Question
	if d.req.Multiple {
		title += " (select all that apply)"
	}
	b.WriteString(youStyle.Render("? " + title))
	for i, o := range d.req.Options {
		mark := ""
		if d.req.Multiple {
			mark = "[ ] "
			if d.picked[i] {
				mark = "[✓] "
			}
		}
		row := strconv.Itoa(i+1) + ". " + mark + o.Label
		if i == d.sel && !d.editing {
			b.WriteString("\n" + youStyle.Render(glyphUser+row))
		} else {
			b.WriteString("\n  " + row)
		}
		if o.Description != "" {
			b.WriteString("\n" + dimStyle.Render("     "+o.Description))
		}
	}
	custom := strconv.Itoa(len(d.req.Options)+1) + ". type your own answer"
	switch {
	case d.editing:
		b.WriteString("\n" + youStyle.Render(glyphUser+custom+": ") + d.custom + "█")
		b.WriteString(dimStyle.Render("\n  enter confirms · esc back"))
		return b.String()
	case d.sel == len(d.req.Options):
		b.WriteString("\n" + youStyle.Render(glyphUser+custom))
	default:
		b.WriteString("\n  " + custom)
	}
	hint := "  ↑/↓ or 1-9 select · enter picks · esc dismisses"
	if d.req.Multiple {
		hint = "  ↑/↓ or 1-9 toggle · enter submits · esc dismisses"
	}
	b.WriteString("\n" + dimStyle.Render(hint))
	return b.String()
}
