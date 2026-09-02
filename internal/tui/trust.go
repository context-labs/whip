package tui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/context-labs/whip/internal/config"
)

// trustOutcome is the tri-state result of the startup folder-trust gate.
type trustOutcome int

const (
	// trustGranted: the cwd is already trusted, or the user just approved.
	trustGranted trustOutcome = iota
	// trustDenied: the user (or a safe default) said no. Startup aborts.
	trustDenied
	// trustDeferred: there's no terminal anywhere to ask on (stdin piped and
	// no /dev/tty — an editor spawning whip, a daemon), so the question moves
	// into the TUI (see model.trustPending) instead of aborting before the UI
	// exists.
	trustDeferred
)

// Package vars so tests can substitute a buffer for the real terminal.
var (
	trustStdin  = func() *os.File { return os.Stdin }
	trustDevTTY = func() (*os.File, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) }
)

// checkTrust gates startup on the folder-trust dialog. If the cwd is already
// trusted (~/.whip/trusted.json), it returns trustGranted immediately.
// Otherwise it asks on the controlling terminal — stdin when it's a terminal,
// else /dev/tty (so `git diff | whip up` still prompts even though stdin is
// the pipe). Enter or "y" records the path; anything else declines. When no
// terminal is available at all, it returns trustDeferred so Run can route the
// question into the TUI rather than dying before the UI exists.
//
// r is the caller's shared stdin reader: a bufio.Reader reads ahead, so a
// fresh one here would swallow the first-run wizard's answers when the user
// (or a paste) supplies more than one line. It's used only when stdin is the
// terminal; a /dev/tty prompt reads its own file.
func checkTrust(r *bufio.Reader) (trustOutcome, error) {
	wd, err := os.Getwd()
	if err != nil {
		return trustDenied, err
	}
	if config.Trusted(wd) {
		return trustGranted, nil
	}

	// Pick the terminal to ask on: stdin if it's a char device, else /dev/tty.
	// Neither → defer to the in-TUI prompt.
	in := r
	out := io.Writer(os.Stderr)
	// tty, when non-nil, is the writable /dev/tty we fell back to; its Close
	// error is checked (CodeQL: a writable handle's failed close can mean lost
	// output — here, the prompt the user is supposed to read).
	var tty *os.File
	if st, err := trustStdin().Stat(); err != nil || st.Mode()&os.ModeCharDevice == 0 {
		var terr error
		tty, terr = trustDevTTY()
		if terr != nil {
			// No controlling terminal: that's the signal to ask in the TUI, not
			// an error to report. The terr is deliberately swallowed.
			return trustDeferred, nil //nolint:nilerr // a missing /dev/tty means "defer to the in-TUI gate", by design
		}
		in = bufio.NewReader(tty)
		out = tty
	}
	outcome, err := askTrust(in, out, wd)
	if tty != nil {
		if cerr := tty.Close(); cerr != nil && err == nil {
			err = cerr // surface a failed close; the prompt may not have flushed
		}
	}
	return outcome, err
}

// askTrust renders the folder-trust dialog on out, reads one line from in, and
// maps the answer to an outcome. Enter/"y"/"yes" records trust; anything else
// (or a read error) declines.
func askTrust(in *bufio.Reader, out io.Writer, wd string) (trustOutcome, error) {
	fmt.Fprintf(out, "\nDo you trust the files in this folder?\n%s\n\n", wd)
	fmt.Fprintln(out, "whip may read files in this folder. Reading untrusted files may lead whip to behave in unexpected ways.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "With your permission whip may execute files in this folder. Executing untrusted code is unsafe.")
	fmt.Fprintln(out, "")
	fmt.Fprint(out, "Proceed? [Y/n] ")
	ans, err := in.ReadString('\n')
	if err != nil {
		return trustDenied, err
	}
	if a := strings.ToLower(strings.TrimSpace(ans)); a == "" || a == "y" || a == "yes" {
		if err := config.Trust(wd); err != nil {
			return trustDenied, err
		}
		return trustGranted, nil
	}
	return trustDenied, nil
}

// openTrustPrompt asks the folder-trust question inside the TUI, for the
// checkTrust-deferred case (no terminal was available). It reuses the inline
// namePrompt: Enter commits, Esc cancels. Either answer routes back through
// Update as a trustAnswerMsg so the loop can return tea.Quit on decline —
// these callbacks can't.
func (m *model) openTrustPrompt() {
	dir := m.trustPending
	send := func(approved bool) {
		if m.prog == nil {
			return // headless tests
		}
		// Detached: the namePrompt commit runs this on the UI thread inside
		// Update, where a synchronous Send would deadlock the event loop.
		go m.prog.Send(trustAnswerMsg{approved: approved})
	}
	m.append(dimStyle.Render("◎ trust " + dir + "? whip may read its files and, with approval, run code here. (y/n — esc declines)"))
	// The label says [y/N]: a bare Enter — the textarea's default key — must
	// decline, fail-closed. (The terminal prompt above is the opposite default,
	// [Y/n]: Enter approves. Different prompts, honest labels.)
	m.openNamePrompt("trust this folder? [y/N]:", "", func(value string) {
		a := strings.ToLower(strings.TrimSpace(value))
		send(a == "y" || a == "yes")
	})
	m.namePrompt.mask = false
	m.namePrompt.onCancel = func() { send(false) } // esc = decline
}
