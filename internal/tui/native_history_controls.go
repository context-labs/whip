package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/context-labs/whip/internal/protocol"
)

func (m *nativeModel) rewindHistory(args string) tea.Cmd {
	if args == "" {
		var text strings.Builder
		text.WriteString("/rewind <keep-through sequence> keeps that prefix and resets the REPL.\nUse 0 to clear. Stop the session first. Only complete terminal groups are accepted.\n\nDisplayed canonical messages (not guaranteed valid group boundaries):\n")
		view := &m.history
		if m.browse != nil {
			view = &m.browse.transcript
		}
		for _, message := range view.messages {
			preview, _ := nativeTextPrefix(strings.Join(strings.Fields(nativeMessageText(message)), " "), 160)
			fmt.Fprintf(&text, "#%d %s · %s\n", message.Sequence, message.Role, preview)
		}
		m.input.Reset()
		m.notice = nativeBoundedNotice(text.String())
		m.refresh()
		return nil
	}
	if m.owner.Lifecycle != "stopped" {
		m.status = "Rewind requires a stopped session with no queued work. Use /stop first; it also cancels active work."
		return nil
	}
	keep, err := nativeHistoryBoundary(args, m.history.snapshot.ThroughSequence)
	if err != nil {
		m.status = err.Error()
		return nil
	}
	params := protocol.RewindParams{EditID: protocol.ID(uuid.NewString()), SessionID: m.owner.ID, ExpectedRevision: m.history.snapshot.Revision, ObservedThrough: m.history.snapshot.ThroughSequence, KeepThrough: keep}
	return m.control("Rewind conversation", true, func(ctx context.Context) nativeControlResult {
		var value protocol.HistoryEdit
		err := m.connection.Call(ctx, "sessions.rewind", params, &value)
		if err == nil && (value.ID != params.EditID || value.SessionID != params.SessionID || value.ExpectedRevision != params.ExpectedRevision || value.ObservedThrough != params.ObservedThrough || value.KeepThrough != params.KeepThrough) {
			err = errors.New("history edit receipt mismatch")
		}
		return nativeControlResult{label: fmt.Sprintf("Kept history through #%d and reset the REPL. Workspace files are unchanged. Use /start to accept execution again.", keep), reset: err == nil, err: err}
	})
}

func nativeHistoryBoundary(text string, through protocol.Counter) (protocol.Counter, error) {
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < 0 || protocol.Counter(value) > through || strconv.FormatInt(value, 10) != text {
		return 0, errors.New("use an exact displayed message sequence from 0 through the observed history tail")
	}
	return protocol.Counter(value), nil
}

func (m *nativeModel) forkHistory(args string, explicitBoundary bool) tea.Cmd {
	if !m.navigationAllowed() {
		return nil
	}
	keep := m.history.snapshot.ThroughSequence
	if explicitBoundary {
		fields := strings.Fields(args)
		if len(fields) == 0 {
			m.status = "usage: /fork-at <keep-through sequence> <title>"
			return nil
		}
		sequence := fields[0]
		value, err := nativeHistoryBoundary(sequence, keep)
		if err != nil {
			m.status = err.Error()
			return nil
		}
		keep, args = value, strings.TrimSpace(strings.TrimPrefix(args, sequence))
	}
	if args == "" {
		m.status = "usage: /fork <title> or /fork-at <keep-through sequence> <title>; creates a fresh root/REPL sharing this working directory"
		return nil
	}
	params := protocol.ForkParams{ForkID: protocol.ID(uuid.NewString()), SessionID: m.owner.ID, ExpectedHistoryRevision: m.history.snapshot.Revision, ExpectedConfigRevision: m.owner.ConfigRevision, ObservedThrough: m.history.snapshot.ThroughSequence, KeepThrough: keep, Title: new(args)}
	raw, err := json.Marshal(params)
	if err == nil {
		err = protocol.Validate("ForkParams", raw)
	}
	if err != nil {
		m.status = "Invalid fork: " + err.Error()
		return nil
	}
	return m.control("Fork conversation", true, func(ctx context.Context) nativeControlResult {
		var value protocol.ForkResult
		if err := m.connection.Call(ctx, "sessions.fork", params, &value); err != nil {
			return nativeControlResult{err: err}
		}
		if err := checkedNativeFork(params, value); err != nil {
			return nativeControlResult{err: err}
		}
		if value.Deleted {
			return nativeControlResult{label: "The original fork destination was deleted; no session was recreated."}
		}
		return nativeControlResult{label: "Fork created with imported history, fresh REPL and the same working directory.", attach: value.Root}
	})
}

func checkedNativeFork(params protocol.ForkParams, value protocol.ForkResult) error {
	f := value.Fork
	if f.ID != params.ForkID || f.SessionID != params.SessionID || f.ExpectedHistoryRevision != params.ExpectedHistoryRevision || f.ExpectedConfigRevision != params.ExpectedConfigRevision || f.ObservedThrough != params.ObservedThrough || f.KeepThrough != params.KeepThrough || f.RootID == "" || f.TreeID == "" || (f.Title == nil) != (params.Title == nil) || f.Title != nil && *f.Title != *params.Title {
		return errors.New("fork receipt mismatch")
	}
	if value.Deleted {
		if value.Root != nil || value.Tree != nil {
			return errors.New("deleted fork returned a live destination")
		}
	} else if value.Root == nil || value.Tree == nil || value.Root.ID != f.RootID || value.Root.TreeID != f.TreeID || value.Tree.ID != f.TreeID || value.Root.ParentID != nil {
		return errors.New("fork destination identity mismatch")
	}
	return nil
}
