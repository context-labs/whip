package tui

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/protocol"
)

const nativeCopyLimit = 1 << 20

// A foreground X11/Wayland owner keeps its clipboard offer alive until replaced
// or detached. It never daemonizes outside the terminal's process ownership.
type nativeClipboardOwner struct {
	ctx     context.Context
	stop    context.CancelFunc
	mu      sync.Mutex
	manager *capability.ProcessManager
	process *capability.Process
	done    <-chan error
	closed  bool
}

func newNativeClipboardOwner(ctx context.Context) *nativeClipboardOwner {
	ctx, stop := context.WithCancel(ctx)
	return &nativeClipboardOwner{ctx: ctx, stop: stop, manager: capability.NewProcessManager()}
}

func (o *nativeClipboardOwner) close() {
	o.stop() // Interrupt an in-flight offer before waiting for its slot lock.
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	o.join()
	_ = o.manager.Close()
}

func (o *nativeClipboardOwner) join() {
	if o.process != nil {
		o.process.Stop()
		<-o.done
		o.process, o.done = nil, nil
	}
}

// Read's EOF proves the bounded body was written to the helper's stdin pipe,
// not that a window manager accepted it. Do not embed Reader/WriterTo: io.Copy
// must pass through Read so this observation cannot be bypassed.
type nativeCopyReader struct {
	text     *strings.Reader
	accepted chan struct{}
	once     sync.Once
}

func (r *nativeCopyReader) Read(p []byte) (int, error) {
	n, err := r.text.Read(p)
	if err == io.EOF {
		r.once.Do(func() { close(r.accepted) })
	}
	return n, err
}

func (o *nativeClipboardOwner) offer(ctx context.Context, directory, text string) (string, error) {
	if text == "" || len(text) > nativeCopyLimit {
		return "", errors.New("clipboard text must contain 1 byte to 1 MiB")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.ctx.Err() != nil {
		return "", errors.New("terminal clipboard is closed")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	program, name := "", ""
	for _, candidate := range []string{"pbcopy", "wl-copy", "xclip"} {
		if path, err := exec.LookPath(candidate); err == nil {
			program, name = path, candidate
			break
		}
	}
	if program == "" {
		return "", errors.New("no supported local text clipboard helper is available")
	}
	var args []string
	switch name {
	case "wl-copy":
		args = []string{"--foreground", "--type", "text/plain"}
	case "xclip":
		args = []string{"-selection", "clipboard", "-quiet"}
	}
	o.join()
	lifetime, cancel := context.WithCancel(o.ctx)
	input := nativeCopyReader{text: strings.NewReader(text), accepted: make(chan struct{})}
	stdout := nativeClipboardBuffer{limit: 16 << 10, stop: cancel}
	stderr := nativeClipboardBuffer{limit: 16 << 10, stop: cancel}
	process, err := o.manager.Start(lifetime, "terminal-text-clipboard", program, args, capability.ProcessOptions{
		Cwd: directory, Env: nativeClipboardEnvironment(), Stdin: &input, Stdout: &stdout, Stderr: &stderr,
	})
	if err != nil {
		cancel()
		return name, err
	}
	done := make(chan error, 1)
	o.process, o.done = process, done
	go func() {
		err := process.Wait()
		process.Stop()
		if stdout.full || stderr.full {
			err = errors.New("clipboard helper exceeded its output limit")
		}
		done <- err
		close(done)
		cancel()
	}()
	ctx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	select {
	case err = <-done:
		if err != nil {
			return name, fmt.Errorf("clipboard helper failed: %w", err)
		}
		select {
		case <-input.accepted:
			return name, nil
		default:
			return name, errors.New("clipboard helper exited before reading the complete text")
		}
	case <-input.accepted:
		if name != "pbcopy" {
			return name, nil
		}
	case <-ctx.Done():
		o.join()
		return name, ctx.Err()
	case <-o.ctx.Done():
		o.join()
		return name, o.ctx.Err()
	}
	// pbcopy owns no continuing selection process; its exit is observable.
	select {
	case err = <-done:
		return name, err
	case <-ctx.Done():
		o.join()
		return name, ctx.Err()
	case <-o.ctx.Done():
		o.join()
		return name, o.ctx.Err()
	}
}

type nativeCopyResult struct {
	owner      protocol.ID
	generation uint64
	helper     string
	err        error
}

func (m *nativeModel) copyCommand(args string) tea.Cmd {
	var text string
	switch args {
	case "", "last":
		messages := m.history.messages
		if m.browse != nil {
			messages = m.browse.transcript.messages
		}
		for _, message := range slices.Backward(messages) {
			if message.Role != "assistant" {
				continue
			}
			var parts []string
			for _, part := range message.Parts {
				if part.Type == "text" {
					parts = append(parts, part.Text)
				}
			}
			text = strings.Join(parts, "\n")
			if text != "" {
				break
			}
		}
		if text == "" {
			m.status = "No assistant text in this loaded history window. Use /older or /latest to choose another window."
			return nil
		}
	case "repl":
		text = ansi.Strip(strings.Join(m.replDisplay, "\n"))
	default:
		m.status = "usage: /copy [last|repl]"
		return nil
	}
	command := m.copyText(text)
	if command != nil {
		m.input.Reset()
		m.sizeInput()
	}
	return command
}

func (m *nativeModel) copyText(text string) tea.Cmd {
	if m.copyBusy {
		m.status = "The previous clipboard offer is still being observed."
		return nil
	}
	if text == "" || len(text) > nativeCopyLimit {
		m.status = "Copy requires nonempty text at most 1 MiB; nothing was copied."
		return nil
	}
	if m.clipboard == nil {
		m.clipboard = newNativeClipboardOwner(m.work.ctx)
	}
	m.copyBusy = true
	clipboard, directory := m.clipboard, m.clientDirectory
	owner, generation := m.owner.ID, m.generation
	m.status = "Offering the selected text to the terminal and local clipboard helper."
	sequence := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
	if os.Getenv("TMUX") != "" {
		sequence = "\x1bPtmux;" + strings.ReplaceAll(sequence, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return tea.Batch(tea.Raw(sequence), func() tea.Msg {
		ctx, done, err := m.work.beginFor(5 * time.Second)
		if err != nil {
			return nativeCopyResult{owner: owner, generation: generation, err: err}
		}
		defer done()
		helper, err := clipboard.offer(ctx, directory, text)
		return nativeCopyResult{owner: owner, generation: generation, helper: helper, err: err}
	})
}

func (m *nativeModel) copied(value nativeCopyResult) {
	m.copyBusy = false
	if value.owner != m.owner.ID || value.generation != m.generation {
		return
	}
	m.status = "Clipboard text offered; the terminal/OS does not acknowledge paste availability."
	if value.err != nil {
		m.status = "Terminal clipboard sequence sent; local helper unavailable: " + value.err.Error()
	}
}
