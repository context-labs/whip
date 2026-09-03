// interactive.go: PTY-backed interactive command runner for the bash tool.
//
// When the agent calls bash with interactive:true, the tool delegates to the
// interactiveRunner is injected into the active agent's tool services. It spawns
// the command in a PTY (via bashrun), streams its output into the transcript,
// shows a countdown when the command goes quiet (likely awaiting input), and
// forwards the user's keystrokes to the PTY. After 15s of no input the command
// is killed so whip never hangs — the property that motivated this change.
//
// Run executes on the agent goroutine; it talks to the TUI by sending tea
// messages (interactiveStartMsg / interactiveOutMsg / interactiveAwaitMsg /
// interactiveDoneMsg) and by publishing a chan []byte the TUI pushes keys into.
package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/tools"
	"github.com/context-labs/whip/internal/tools/bashrun"
)

// interactive is the UI-thread state for one in-flight interactive command.
// Only the keys channel is written to off the UI thread (by key()).
type interactive struct {
	keys    chan []byte // TUI pushes keystrokes here for forwarding to the PTY
	output  string      // streamed PTY output rendered in the live pane
	await   bool        // command is quiet and likely waiting for input
	awaitcd int         // last reported seconds-left before inactivity timeout
}

// messages sent from the runner goroutine to the UI thread.
// interactiveStartMsg carries the keys channel the TUI should push user
// keystrokes into; processing it on the UI thread starts passthrough mode.
type (
	interactiveStartMsg struct{ keys chan []byte }
	interactiveOutMsg   struct{ chunk string }
	interactiveAwaitMsg struct{ secsLeft int }
	interactiveDoneMsg  struct {
		output string
		exit   string
	}
)

// interactiveRunner implements tools.InteractiveRunner and is installed on the
// agent's bash tool at startup. It owns the tea.Program so it can Send messages
// into the UI loop, and it publishes a chan []byte the TUI pushes keys into.
type interactiveRunner struct {
	prog *tea.Program

	mu   sync.Mutex
	keys chan []byte
}

// Run implements tools.InteractiveRunner. It blocks the agent goroutine until
// the command finishes, the inactivity timeout fires, or ctx is cancelled.
func (r *interactiveRunner) Run(ctx context.Context, opts bashrun.Options) string {
	keys := make(chan []byte, 16)
	r.mu.Lock()
	r.keys = keys
	r.mu.Unlock()
	r.prog.Send(interactiveStartMsg{keys: keys}) //nolint:uilock // background: the interactive bash tool goroutine

	opts.Interactive = true
	opts.InactivityTimeout = 15 * time.Second
	opts.OnOutput = func(chunk string) { r.prog.Send(interactiveOutMsg{chunk}) }      //nolint:uilock // background: bashrun's PTY reader goroutine
	opts.OnAwaitInput = func(s int) { r.prog.Send(interactiveAwaitMsg{secsLeft: s}) } //nolint:uilock // background: bashrun's PTY goroutine
	opts.Keys = keys
	res := bashrun.Run(ctx, opts)

	r.mu.Lock()
	r.keys = nil
	r.mu.Unlock()

	r.prog.Send(interactiveDoneMsg{output: res.Output, exit: res.Exit}) //nolint:uilock // background: the interactive bash tool goroutine

	return interactiveOutput(res)
}

// interactiveOutput renders the final string the tool returns to the model:
// the captured PTY output plus the exit status line if any.
func interactiveOutput(res bashrun.Result) string {
	switch {
	case res.Output == "" && res.Exit == "":
		return "(no output)"
	case res.Exit == "":
		return res.Output
	default:
		return res.Output + "\n(" + res.Exit + ")"
	}
}

// iactiveKey translates a bubbletea keystroke into PTY bytes and forwards it,
// returning a no-op. ctrl+c ctrl+c breaks out and cancels the whole turn so a
// stuck user can escape; the first ctrl+c arms like the busy path.
func (m *model) iactiveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		// two-press escape hatch identical to the normal busy flow
		if !m.interrupt1 {
			m.interrupt1 = true
			return m, nil
		}
		if m.cancel != nil {
			m.cancel()
		}
		return m, nil
	case tea.KeyEsc:
		// forward a single ESC byte; many prompts (less, top, fzf) use ESC to
		// quit, and forwarding is less surprising than dropping the key.
		m.sendKeys([]byte{0x1b})
		return m, nil
	case tea.KeyEnter:
		m.sendKeys([]byte("\r"))
		return m, nil
	case tea.KeyTab:
		m.sendKeys([]byte("\t"))
		return m, nil
	case tea.KeyBackspace, tea.KeyDelete:
		m.sendKeys([]byte{0x7f})
		return m, nil
	case tea.KeyUp, tea.KeyDown, tea.KeyLeft, tea.KeyRight:
		// bubbletea maps these to named types; reconstruct the ANSI sequence.
		m.sendKeys([]byte(arrowBytes(msg.Type)))
		return m, nil
	case tea.KeyCtrlJ:
		// ctrl+j is whip's newline key; in passthrough, behave like enter so
		// the user's muscle memory still "submits" a prompt answer.
		m.sendKeys([]byte("\r"))
		return m, nil
	}

	// KeyRunes: forward the raw UTF-8 bytes, honouring Alt as a leading ESC.
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		var buf []byte
		if msg.Alt {
			buf = append(buf, 0x1b)
		}
		for _, r := range msg.Runes {
			var rb [4]byte
			n := utf8.EncodeRune(rb[:], r)
			buf = append(buf, rb[:n]...)
		}
		if len(buf) > 0 {
			m.sendKeys(buf)
		}
		return m, nil
	}

	// Anything else (pgup/down, f-keys, etc.) we drop to avoid surprising the
	// child with partial escape sequences we might mis-encode.
	return m, nil
}

// sendKeys pushes bytes into the in-flight interactive command's PTY. Safe to
// call only from the UI thread; the keys channel is set by interactiveStartMsg.
func (m *model) sendKeys(b []byte) {
	if m.iactive == nil {
		return
	}
	select {
	case m.iactive.keys <- b:
	default:
		// channel full (user typing faster than the child reads): drop. The
		// backpressure here protects the UI thread from blocking.
	}
}

// arrowBytes returns the ANSI cursor sequence for an arrow key type.
func arrowBytes(t tea.KeyType) string {
	switch t {
	case tea.KeyUp:
		return bashrun.KeyUp
	case tea.KeyDown:
		return bashrun.KeyDown
	case tea.KeyLeft:
		return bashrun.KeyLeft
	case tea.KeyRight:
		return bashrun.KeyRight
	}
	return ""
}

// interactiveView renders the live PTY output pane (its last few lines, since
// the full transcript is for programs that scroll) and, when the command is
// quiet, a countdown to the inactivity timeout.
func (m *model) interactiveView() string {
	if m.iactive == nil {
		return ""
	}
	const maxLines = 12
	out := m.iactive.output
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	rendered := dimStyle.Render("  " + strings.Join(lines, "\n  "))

	header := toolStyle.Render("⚒ bash (interactive)")
	if m.iactive.await {
		header += errStyle.Render(fmt.Sprintf(
			"  ⏳ waiting for input — cancels in %ds", m.iactive.awaitcd,
		))
	} else {
		header += dimStyle.Render("  (type to respond; ctrl+c ctrl+c to cancel)")
	}
	return header + "\n" + rendered
}

var _ tools.InteractiveRunner = (*interactiveRunner)(nil)
