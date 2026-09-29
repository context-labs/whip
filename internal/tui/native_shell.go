package tui

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

type nativeShellView struct {
	owner, epoch, operation protocol.ID
	next                    protocol.Counter
	text                    string
	seconds                 int
}

type nativeShellFocus struct {
	owner, epoch, operation protocol.ID
	next                    protocol.Counter
	queued                  []byte
	inflight                bool
}

type nativeShellFocused struct {
	request    *nativeShellFocus
	generation uint64
	view       *nativeShellView
	err        error
}

type nativeShellSent struct {
	focus    *nativeShellFocus
	sequence protocol.Counter
	err      error
}

func readNativeShell(ctx context.Context, connection *client.Client, owner, epoch protocol.ID) (*nativeShellView, error) {
	var value protocol.ShellInteractionResult
	if err := connection.CallAtEpoch(ctx, epoch, "shell.interaction", protocol.ShellInteractionParams{SessionID: owner}, &value); err != nil {
		return nil, err
	}
	if value.Interaction == nil {
		return nil, nil //nolint:nilnil // Passive observation of an absent foreground shell is successful.
	}
	i := value.Interaction
	if len(i.DataBase64) > base64.StdEncoding.EncodedLen(64<<10) || i.NextInput <= 0 || i.NextInput == math.MaxInt64 || i.Through < i.From || i.Through-i.From > 64<<10 {
		return nil, errors.New("interactive shell preview exceeds its bounds")
	}
	data, err := base64.StdEncoding.DecodeString(i.DataBase64)
	if err != nil || protocol.Counter(len(data)) != i.Through-i.From {
		return nil, errors.New("interactive shell preview bytes do not match its cursor")
	}
	return &nativeShellView{owner: owner, epoch: epoch, operation: i.OperationID, next: i.NextInput, text: nativeDisplayText(strings.ToValidUTF8(string(data), "�")), seconds: i.SecondsLeft}, nil
}

func (m *nativeModel) releaseShellFocus() {
	if m.shellFocus != nil {
		clear(m.shellFocus.queued)
	}
	m.shellFocus = nil
	m.shellPending = nil
}

func (m *nativeModel) observeShell(view *nativeShellView, err error) {
	f := m.shellFocus
	if f != nil && (err != nil || view == nil || view.owner != f.owner || view.epoch != f.epoch || view.operation != f.operation || !f.inflight && view.next > f.next) {
		m.releaseShellFocus()
		m.status = "Shell focus ended: the process, operation, or input sequence changed. Buffered keys were discarded; refocus explicitly."
	}
	m.shell = view
}

func (m *nativeModel) shellCommand(args string) tea.Cmd {
	switch strings.TrimSpace(args) {
	case "", "status":
		m.input.Reset()
		m.shellHidden = false
		if m.shell == nil {
			m.status = "No observed foreground interactive shell. Observation never starts one."
		} else {
			m.status = "Interactive shell " + string(m.shell.operation) + "; /shell focus sends keys, /shell hide closes its preview."
		}
		m.refresh()
		return nil
	case "hide":
		m.input.Reset()
		m.releaseShellFocus()
		m.shellHidden = true
		m.status = "Interactive shell preview hidden. Host work continues."
		m.refresh()
		return nil
	case "focus":
		if !m.ready || m.history.epoch == "" {
			m.status = "Read the current host process before focusing shell input."
			return nil
		}
		m.input.Reset()
		m.releaseShellFocus()
		request := &nativeShellFocus{owner: m.owner.ID, epoch: m.history.epoch}
		m.shellPending = request
		generation := m.generation
		return func() tea.Msg {
			result := nativeShellFocused{request: request, generation: generation}
			ctx, done, err := m.work.begin()
			if err != nil {
				result.err = err
				return result
			}
			defer done()
			result.view, result.err = readNativeShell(ctx, m.connection, request.owner, request.epoch)
			return result
		}
	default:
		m.status = "usage: /shell [status|focus|hide]"
		return nil
	}
}

func (m *nativeModel) shellFocused(result nativeShellFocused) {
	if result.generation != m.generation || result.request != m.shellPending {
		return
	}
	m.shellPending = nil
	if result.err != nil || result.view == nil || m.owner.ID != result.request.owner || m.history.epoch != result.request.epoch {
		m.status = "Shell focus unavailable; no keys were sent."
		if result.err != nil {
			m.status += " " + result.err.Error()
		}
		return
	}
	v := result.view
	m.shell = v
	m.shellHidden = false
	m.shellFocus = &nativeShellFocus{owner: v.owner, epoch: v.epoch, operation: v.operation, next: v.next}
	m.status = "Shell input focused · Ctrl+] returns to the draft; Ctrl+C twice cancels the exact host turn."
	m.refresh()
}

func (m *nativeModel) queueShellInput(data string) tea.Cmd {
	f := m.shellFocus
	if f == nil || data == "" {
		return nil
	}
	if len(data) > 16<<10-len(f.queued) || !utf8.ValidString(data) {
		m.status = "Shell input refused: at most 16 KiB of valid text may wait behind one in-flight write."
		return nil
	}
	f.queued = append(f.queued, data...)
	return m.sendShellInput()
}

func (m *nativeModel) sendShellInput() tea.Cmd {
	f := m.shellFocus
	if f == nil || f.inflight || len(f.queued) == 0 {
		return nil
	}
	params := protocol.ShellInputParams{SessionID: f.owner, OperationID: f.operation, Sequence: f.next, DataBase64: base64.StdEncoding.EncodeToString(f.queued)}
	clear(f.queued)
	f.queued = nil
	f.inflight = true
	return func() tea.Msg {
		result := nativeShellSent{focus: f, sequence: params.Sequence}
		ctx, done, err := m.work.beginFor(5 * time.Second)
		if err != nil {
			result.err = err
			return result
		}
		defer done()
		var acknowledgement protocol.ShellInputResult
		result.err = m.connection.CallAtEpoch(ctx, f.epoch, "shell.input", params, &acknowledgement)
		if result.err == nil && acknowledgement.Sequence != params.Sequence {
			result.err = errors.New("shell input acknowledgement sequence mismatch")
		}
		return result
	}
}

func (m *nativeModel) shellSent(result nativeShellSent) tea.Cmd {
	f := m.shellFocus
	if f != result.focus || f == nil || result.sequence != f.next {
		return nil
	}
	f.inflight = false
	if result.err != nil {
		m.releaseShellFocus()
		m.status = "Shell input delivery is uncertain or rejected: " + result.err.Error() + ". Buffered keys were discarded. Inspect the shell and refocus; keys are never replayed."
		return nil
	}
	f.next++
	m.status = "Shell input acknowledged by its bounded queue; terminal consumption is not confirmed. Ctrl+] returns to the draft."
	return m.sendShellInput()
}

func (m *nativeModel) shellKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.shellFocus == nil {
		return nil, false
	}
	if key.String() == "ctrl+]" {
		m.releaseShellFocus()
		m.status = "Returned to the draft. Host shell work continues."
		return nil, true
	}
	if key.String() == "ctrl+c" {
		return m.interruptKey(), true
	}
	if key.Mod.Contains(tea.ModCtrl) && key.Code >= 'a' && key.Code <= 'z' {
		return m.queueShellInput(string(key.Code - 'a' + 1)), true
	}
	keys := map[rune]string{tea.KeyUp: "\x1b[A", tea.KeyDown: "\x1b[B", tea.KeyRight: "\x1b[C", tea.KeyLeft: "\x1b[D", tea.KeyEnter: "\r", tea.KeyTab: "\t", tea.KeyBackspace: "\x7f", tea.KeyEscape: "\x1b", tea.KeyDelete: "\x1b[3~", tea.KeyHome: "\x1b[H", tea.KeyEnd: "\x1b[F"}
	text := keys[key.Code]
	if text == "" {
		text = key.Text
		if key.Mod.Contains(tea.ModAlt) && text != "" {
			text = "\x1b" + text
		}
	}
	return m.queueShellInput(text), true
}

func (m *nativeModel) shellHeight() int {
	if m.shell == nil || m.shellHidden {
		return 0
	}
	return min(13, max(m.height/3, 1))
}

func (m *nativeModel) shellView() string {
	v, height, width := m.shell, m.shellHeight(), m.transcriptWidth()
	if v == nil || height == 0 {
		return ""
	}
	label := "Interactive shell · preview · /shell focus"
	if m.shellFocus != nil {
		label = "Interactive shell · keys focused · Ctrl+] returns to draft"
	}
	if v.seconds > 0 {
		label += fmt.Sprintf(" · input timeout %ds", v.seconds)
	}
	rows := strings.Split(strings.TrimRight(v.text, "\n"), "\n")
	rows = rows[max(0, len(rows)-height+1):]
	for i := range rows {
		rows[i] = ansi.Truncate(rows[i], width, "…")
	}
	return nativeFixedRows(ansi.Truncate(label, width, "…")+"\n"+strings.Join(rows, "\n"), width, height)
}
