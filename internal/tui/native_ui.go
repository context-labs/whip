package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

// nativeWork joins terminal-owned readers on detach. InputCommand owns the
// admission send independently; cancelling this observation never cancels a
// host turn. The caller separately closes its borrowed native Client.
type nativeWork struct {
	ctx    context.Context
	stop   context.CancelFunc
	mu     sync.Mutex
	closed bool
	active int
	wg     sync.WaitGroup
}

func (w *nativeWork) begin() (context.Context, func(), error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, nil, errors.New("terminal detached")
	}
	if err := w.ctx.Err(); err != nil {
		return nil, nil, err
	}
	if w.active == 4 {
		return nil, nil, errors.New("terminal request capacity is busy")
	}
	w.active++
	w.wg.Add(1)
	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
	return ctx, func() { cancel(); w.mu.Lock(); w.active--; w.mu.Unlock(); w.wg.Done() }, nil
}

func (w *nativeWork) close() { w.mu.Lock(); w.closed = true; w.stop(); w.mu.Unlock(); w.wg.Wait() }

// nativeModel is the native chat composition. Commands and menus are added
// directly over typed host operations; it does not adapt retired RootActions.
type nativeModel struct {
	work                            nativeWork
	handle                          *client.Session
	owner                           protocol.Session
	observer                        *client.Observer
	history                         nativeTranscript
	activity                        protocol.SessionActivity
	usage                           protocol.Usage
	contextUsage                    protocol.ContextUsage
	input                           textarea.Model
	vp                              transcriptView
	rows                            []string
	width, height                   int
	ready, reading, sending, follow bool
	polls                           int
	status                          string
	uncertain                       *client.InputCommand
	quitArmed                       bool
	cancelling                      bool
}

type (
	nativePoll struct{}
	nativeRead struct {
		activity      protocol.SessionActivity
		page          *protocol.HistoryPageResult
		observation   *client.Observation
		observer      *client.Observer
		output        protocol.CellOutput
		usage         *protocol.Usage
		context       *protocol.ContextUsage
		err           error
		evidenceError error
	}
)

type nativeSubmission struct {
	command   *client.InputCommand
	admission protocol.Admission
	err       error
	uncertain bool
}
type nativeCancelled struct {
	turn protocol.ID
	err  error
}

func newNativeModel(ctx context.Context, c *client.Client, owner protocol.Session) (*nativeModel, error) {
	if c == nil {
		return nil, errors.New("native client is required")
	}
	handle, err := c.Session(owner.ID)
	if err != nil {
		return nil, err
	}
	lifecycle, cancel := context.WithCancel(ctx)
	return &nativeModel{
		work: nativeWork{ctx: lifecycle, stop: cancel}, handle: handle, owner: owner,
		history: nativeTranscript{owner: owner.ID}, input: newInput(), width: 80, height: 24, follow: true,
	}, nil
}

func (m *nativeModel) Init() tea.Cmd { return m.read() }

func (m *nativeModel) read() tea.Cmd {
	if m.reading {
		return nil
	}
	m.reading = true
	observer, handle := m.observer, m.handle
	evidence := m.polls%5 == 0
	m.polls++
	return func() tea.Msg {
		ctx, done, err := m.work.begin()
		if err != nil {
			return nativeRead{err: err}
		}
		defer done()
		result := nativeRead{observer: observer}
		result.activity, result.err = handle.Activity(ctx)
		if result.err != nil {
			return result
		}
		load := func() error {
			page, err := handle.History(ctx, protocol.HistoryPageParams{Direction: "backward", Limit: 64})
			if err != nil {
				return err
			}
			result.page = &page
			result.observer, err = handle.Observer(client.ObservationCursor{After: page.Snapshot.ThroughSequence, Revision: new(page.Snapshot.Revision)})
			return err
		}
		if observer == nil {
			result.err = load()
		} else {
			page, err := observer.Next(ctx)
			result.err = err
			if err == nil {
				if page.Reset {
					result.err = load()
				} else {
					result.observation = &page
				}
			}
		}
		if result.err != nil {
			return result
		}
		// Primary history is delivered even if a later auxiliary read fails.
		// Otherwise Observer's advanced cursor would silently skip messages.
		result.output, result.evidenceError = handle.CellOutput(ctx)
		if evidence {
			usage, err := handle.Usage(ctx)
			if err == nil {
				result.usage = &usage
			} else {
				result.evidenceError = errors.Join(result.evidenceError, err)
			}
			value, err := handle.ContextUsage(ctx)
			if err == nil {
				result.context = &value
			} else {
				result.evidenceError = errors.Join(result.evidenceError, err)
			}
		}
		return result
	}
}

func nativeTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return nativePoll{} })
}

func (m *nativeModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch value := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(value.Width, 8), max(value.Height, 4)
		m.input.SetWidth(max(m.width-2, 1))
		m.refresh()
	case nativePoll:
		return m, m.read()
	case nativeRead:
		m.reading = false
		if value.err != nil {
			m.ready = false
			m.status = "Connection read failed: " + value.err.Error()
			return m, nativeTick()
		}
		var err error
		renderChanged := value.page != nil || value.observation != nil && (len(value.observation.Messages) > 0 || value.observation.Preview != nil) || m.history.preview != nil || m.history.cellOutput != nil || value.output.Preview != nil
		if value.page != nil {
			err = m.history.replace(*value.page)
		}
		if err == nil && value.observation != nil {
			err = m.history.observe(*value.observation)
		}
		if err != nil {
			// Reload a bounded canonical page; never continue past a failed
			// projection with the Observer cursor that already advanced.
			m.ready, m.observer = false, nil
			m.status = "Transcript reload required: " + err.Error()
			return m, nativeTick()
		}
		m.ready, m.observer, m.activity = true, value.observer, value.activity
		m.history.output(value.output)
		if value.usage != nil {
			m.usage = *value.usage
		}
		if value.context != nil {
			m.contextUsage = *value.context
		}
		if value.evidenceError != nil {
			m.status = "Some live evidence is unavailable: " + value.evidenceError.Error()
		}
		if renderChanged {
			m.refresh()
		}
		return m, nativeTick()
	case nativeSubmission:
		m.sending = false
		if value.uncertain {
			m.uncertain = value.command
			m.status = "Input acceptance is uncertain; inspect the original request before submitting again. " + value.err.Error()
		} else if value.err != nil {
			m.status = "Input rejected: " + value.err.Error()
		} else {
			m.uncertain = nil
			if value.admission.Input == nil {
				m.status = "The original input was deleted; it has not been resubmitted."
			} else {
				m.status = "Accepted input " + string(value.admission.Input.ID)
			}
		}
	case nativeCancelled:
		m.cancelling = false
		if value.err != nil {
			m.status = "Cancel " + string(value.turn) + ": " + value.err.Error()
		} else {
			m.status = "Cancellation requested for turn " + string(value.turn)
		}
	case tea.KeyPressMsg:
		switch value.String() {
		case "ctrl+c":
			if m.quitArmed {
				return m, tea.Quit
			}
			m.quitArmed = true
			m.status = "Press Ctrl+C again to detach. Host work continues. Esc cancels the displayed active turn."
			return m, nil
		case "esc":
			if m.ready && !m.cancelling && m.activity.ActiveTurn != nil {
				return m, m.cancelTurn(m.activity.ActiveTurn.ID)
			}
		case "enter":
			return m, m.submit()
		case "pgup", "pgdown":
			m.vp, _ = m.vp.Update(value)
			m.follow = m.vp.AtBottom()
			return m, nil
		default:
			m.quitArmed = false
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(value)
		return m, cmd
	case tea.PasteMsg:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(value)
		return m, cmd
	}
	return m, nil
}

func (m *nativeModel) submit() tea.Cmd {
	text := m.input.Value()
	if strings.TrimSpace(text) == "" || !m.ready || m.sending {
		return nil
	}
	if m.uncertain != nil {
		m.status = "Inspect the original uncertain input before another submission."
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(text), "/") {
		m.status = "Native command controls are still being connected."
		return nil
	}
	params := protocol.SubmitParams{Source: "user", Identity: protocol.RequestIdentity{ClientID: "tui", RequestID: protocol.ID(uuid.NewString())}, Parts: []protocol.Part{{Type: "text", Text: text}}}
	if m.activity.ActiveTurn != nil && m.activity.ActiveTurn.Kind == "prompt" {
		params.Delivery, params.TargetTurnID = "steer", new(m.activity.ActiveTurn.ID)
	}
	command, err := m.handle.Submission(params)
	if err != nil {
		m.status = err.Error()
		return nil
	}
	m.sending = true
	m.input.Reset()
	return func() tea.Msg {
		ctx, done, err := m.work.begin()
		if err != nil {
			return nativeSubmission{command: command, err: err}
		}
		defer done()
		value, err := command.Send(ctx)
		if err == nil {
			return nativeSubmission{command: command, admission: value}
		}
		if _, rejected := errors.AsType[*client.Error](err); rejected {
			return nativeSubmission{command: command, err: err}
		}
		value, found, checkErr := command.Check(ctx)
		if checkErr == nil && found && value.Input != nil {
			return nativeSubmission{command: command, admission: value}
		}
		return nativeSubmission{command: command, err: errors.Join(err, checkErr), uncertain: true}
	}
}

func (m *nativeModel) cancelTurn(id protocol.ID) tea.Cmd {
	m.cancelling = true
	handle := m.handle
	return func() tea.Msg {
		ctx, done, err := m.work.begin()
		if err != nil {
			return nativeCancelled{turn: id, err: err}
		}
		defer done()
		_, err = handle.CancelTurn(ctx, id)
		return nativeCancelled{turn: id, err: err}
	}
}

func (m *nativeModel) refresh() {
	var rows []string
	truncated := false
	appendText := func(text string) {
		for line := range strings.Lines(ansi.Hardwrap(nativeDisplayText(text), max(m.width-2, 1), true)) {
			if len(rows) >= 65536 {
				truncated = true
				break
			}
			rows = append(rows, strings.TrimSuffix(line, "\n"))
		}
	}
	if m.history.earlier {
		rows = append(rows, "Older messages are available outside this display window.", "")
	}
	for _, message := range m.history.messages {
		if len(rows) >= 65536 {
			truncated = true
			break
		}
		label := message.Role
		if message.Source != nil {
			label += " · imported"
		}
		rows = append(rows, label)
		appendText(nativeMessageText(message))
		rows = append(rows, "")
	}
	if p := m.history.preview; p != nil {
		rows = append(rows, "assistant · provisional")
		appendText(p.Text)
		if p.Truncated {
			rows = append(rows, "Preview truncated; committed content will replace it.")
		}
	}
	if p := m.history.cellOutput; p != nil {
		rows = append(rows, "REPL output · provisional")
		appendText(p.Text)
		if p.Truncated {
			rows = append(rows, "Output preview truncated.")
		}
	}
	if truncated {
		rows = append(rows[:min(len(rows), 65536)], "Display row limit reached; complete message bodies remain in canonical history.")
	}
	m.rows = rows
	m.vp.rows = func(y int) string { return m.rows[y] }
	m.vp.SetWidth(m.width)
	m.vp.SetHeight(max(m.height-m.input.Height()-4, 1))
	m.vp.setTotal(len(rows))
	if m.follow {
		m.vp.GotoBottom()
	}
}

func (m *nativeModel) View() tea.View {
	state := m.activity.Lifecycle
	if m.activity.ActiveTurn != nil {
		state = m.activity.ActiveTurn.State
	}
	footer := fmt.Sprintf("%s · queued %d · permissions %d · questions %d", state, m.activity.QueuedInputCount, m.activity.PendingPermissionCount, m.activity.PendingQuestionCount)
	view := tea.NewView(m.vp.View() + "\n" + ansi.Truncate(nativeDisplayText(m.status), m.width, "…") + "\n" + m.input.View() + "\n" + ansi.Truncate(nativeContextLabel(m.contextUsage), m.width, "…") + "\n" + ansi.Truncate(footer, m.width, "…"))
	view.AltScreen = true
	return view
}

func nativeDisplayText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return unicode.ReplacementChar
		}
		return r
	}, ansi.Strip(text))
}

func nativeContextLabel(value protocol.ContextUsage) string {
	if value.Prefill == nil {
		return "Latest prefill: unavailable"
	}
	p := value.Prefill
	label := fmt.Sprintf("Latest prefill: %d tokens (%s)", p.InputTokens, p.InputSource)
	if p.ContextWindowTokens == nil {
		label += " · capacity unknown"
	} else {
		label += fmt.Sprintf(" · capacity %d", *p.ContextWindowTokens)
	}
	if p.Stale {
		label += " · earlier history tail"
	}
	return label
}
