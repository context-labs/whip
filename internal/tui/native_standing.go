package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

// Matches the native standing-publication byte bound. This is a local editor
// copy, never a host path or a source publication created by the terminal.
const nativeStandingLimit = 64 << 10

type nativeStandingResult struct {
	draft *protocol.WriteHostStandingInstructionsParams
	err   error
}

func (m *nativeModel) standing(args string) tea.Cmd {
	switch args {
	case "draft":
		m.input.Reset()
		if m.standingDraft == nil {
			m.status = "No unsaved standing-instruction draft."
		} else {
			m.notice = nativeBoundedNotice("Standing-instruction draft · publication may be unresolved:\n" + m.standingDraft.Text)
			m.refresh()
		}
		return nil
	case "discard":
		m.input.Reset()
		m.standingDraft = nil
		m.notice = ""
		m.status = "Local draft discarded; this discard did not modify host instructions."
		m.refresh()
		return nil
	case "read":
		return m.control("Published standing instructions", false, func(ctx context.Context) nativeControlResult {
			var value protocol.HostStandingInstructions
			err := m.connection.Call(ctx, "host.standing.read", protocol.EmptyParams{}, &value)
			if err == nil && !value.Published {
				return nativeControlResult{label: "No standing-instruction file is published by this host."}
			}
			if err == nil && value.Text == nil {
				err = errors.New("published instructions have no text")
			}
			result := nativeControlResult{err: err}
			if err == nil {
				result.notice = "Host-published standing instructions · granted sessions only:\n" + *value.Text
			}
			return result
		})
	case "", "retry":
	default:
		m.status = "usage: /me [read|draft|retry|discard]"
		return nil
	}
	if m.uncertain != nil || m.retryControl != nil {
		m.status = "Resolve the original uncertain action before editing standing instructions."
		return nil
	}
	if args == "retry" {
		if m.standingDraft == nil {
			m.status = "No standing-instruction draft to retry."
			return nil
		}
		draft := *m.standingDraft
		m.controlling = true
		m.input.Reset()
		return func() tea.Msg {
			ctx, done, err := m.work.begin()
			if err != nil {
				return nativeStandingResult{draft: &draft, err: err}
			}
			defer done()
			return nativeStandingResult{draft: &draft, err: writeNativeStanding(ctx, m.connection, draft)}
		}
	}
	if m.standingDraft != nil {
		m.status = "An unsaved draft is retained: /me draft, /me retry (same revision), or /me discard."
		return nil
	}
	m.controlling = true
	m.input.Reset()
	editor := newNativeStandingEditor(&m.work, m.connection)
	return tea.Exec(editor, func(err error) tea.Msg { return nativeStandingResult{draft: editor.draft, err: err} })
}

func writeNativeStanding(ctx context.Context, connection *client.Client, draft protocol.WriteHostStandingInstructionsParams) error {
	var value protocol.HostStandingInstructions
	if err := connection.Call(ctx, "host.standing.write", draft, &value); err != nil {
		return err
	}
	if !value.Published || value.Text == nil || *value.Text != draft.Text {
		return errors.New("standing publication response did not match the edited text")
	}
	return nil
}

// The editor is explicitly chosen by the local human. It inherits the local
// terminal like the retained editor workflow; it is not a host capability.
// Run owns and joins the direct editor process before releasing its UI work.
type nativeStandingEditor struct {
	work       *nativeWork
	connection *client.Client
	editor     string
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	draft      *protocol.WriteHostStandingInstructionsParams
}

func newNativeStandingEditor(work *nativeWork, connection *client.Client) *nativeStandingEditor {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	return &nativeStandingEditor{work: work, connection: connection, editor: editor}
}

func (e *nativeStandingEditor) SetStdin(value io.Reader)  { e.stdin = value }
func (e *nativeStandingEditor) SetStdout(value io.Writer) { e.stdout = value }
func (e *nativeStandingEditor) SetStderr(value io.Writer) { e.stderr = value }

func (e *nativeStandingEditor) Run() error {
	ctx, done, err := e.work.beginFor(30 * time.Minute)
	if err != nil {
		return err
	}
	defer done()
	var value protocol.HostStandingInstructions
	if err := e.connection.Call(ctx, "host.standing.read", protocol.EmptyParams{}, &value); err != nil {
		return err
	}
	if !value.Published {
		return errors.New("no standing-instruction file is published by this host; nothing was created")
	}
	if value.Text == nil || value.Revision == nil || len(*value.Text) > nativeStandingLimit {
		return errors.New("invalid standing-instruction publication")
	}
	directory, err := os.MkdirTemp("", "whip-standing-edit-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	path := filepath.Join(directory, "standing.md")
	if err := os.WriteFile(path, []byte(*value.Text), 0o600); err != nil {
		return err
	}
	// The local human chooses this executable; no shell parses the value.
	command := exec.CommandContext(ctx, e.editor, path)
	command.Stdin, command.Stdout, command.Stderr = e.stdin, e.stdout, e.stderr
	command.WaitDelay = time.Second
	runErr := command.Run()
	text, err := readNativeStandingDraft(directory)
	if err == nil {
		e.draft = &protocol.WriteHostStandingInstructionsParams{ExpectedRevision: *value.Revision, Text: text}
	}
	if err := errors.Join(runErr, err); err != nil {
		return err
	}
	return writeNativeStanding(ctx, e.connection, *e.draft)
}

func readNativeStandingDraft(directory string) (string, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat("standing.md")
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("edited instructions must be a regular file, not a symbolic link")
	}
	file, err := root.OpenFile("standing.md", os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(info, actual) {
		return "", errors.New("edited instruction file changed while opening")
	}
	text, err := io.ReadAll(io.LimitReader(file, nativeStandingLimit+1))
	if err != nil {
		return "", err
	}
	if len(text) > nativeStandingLimit || !utf8.Valid(text) || bytes.IndexByte(text, 0) >= 0 {
		return "", fmt.Errorf("edited instructions must be valid UTF-8 without NUL and at most %d bytes", nativeStandingLimit)
	}
	return string(text), nil
}
