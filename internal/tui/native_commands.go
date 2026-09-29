package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

type nativeControlResult struct {
	recoveryCleared *client.InputCommand
	redraft         *nativeRedraft
	input           *client.InputCommand
	generation      uint64
	attach          *protocol.Session
	picker          *nativeSessionPicker
	label           string
	mutation        bool
	owner           *protocol.Session
	policy          *protocol.PermissionPolicy
	reset           bool
	err             error
	retry           tea.Cmd
	inspectOnError  bool
	notice          string
	decisionID      protocol.ID
}

// control owns only this UI request. Its retry closure captures the original
// typed CAS payload; a transport failure never rebuilds it from newer state.
func (m *nativeModel) control(label string, mutate bool, call func(context.Context) nativeControlResult) tea.Cmd {
	m.controlling = true
	m.input.Reset()
	generation := m.generation
	var command tea.Cmd
	command = func() tea.Msg {
		ctx, done, err := m.work.begin()
		if err != nil {
			return nativeControlResult{label: label, generation: generation, err: err}
		}
		defer done()
		value := call(ctx)
		value.generation = generation
		if value.label == "" || value.err != nil {
			value.label = label
		}
		if mutate {
			value.mutation = true
			value.retry = command
		}
		return value
	}
	return command
}

func (m *nativeModel) command(text string) tea.Cmd {
	defer func() {
		if m.input.Value() == "" {
			m.draftDesign = nil
		}
	}()
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil
	}
	name := fields[0]
	args := strings.TrimSpace(strings.TrimPrefix(text, name))
	if name != "/attach" && name != "/quit" && (m.attachmentBusy || m.attachment != nil) {
		m.status = "Resolve the pending image upload with /attach check, /attach retry, or /attach discard first."
		return nil
	}
	switch name {
	case "/shell":
		return m.shellCommand(args)
	case "/pending":
		return m.pendingCommand(args)
	case "/panel":
		return m.panelCommand(args)
	case "/redraft":
		return m.redraftCommand(args)
	case "/copy":
		return m.copyCommand(args)
	case "/attach":
		return m.attachCommand(args)
	case "/help":
		m.input.Reset()
		m.notice = nativeHelp
		m.refresh()
		return nil
	case "/report":
		m.input.Reset()
		m.notice = m.nativeReport()
		m.refresh()
		return nil
	case "/context", "/context-doctor":
		return m.contextDoctor(args)
	case "/export":
		return m.exportNative(args)
	case "/effort":
		return m.effortCommand(args)
	case "/quit", "/exit", "/q":
		return tea.Quit
	case "/rejected":
		if args == "restore" && m.restoreRejectedDraft() {
			m.status = "Rejected input restored as a draft; nothing was sent."
			return nil
		}
		if args == "discard" {
			m.rejected = nil
			m.input.Reset()
			m.status = "Rejected draft discarded."
			return nil
		}
		m.status = "usage: /rejected restore|discard"
		return nil
	case "/check":
		if m.uncertain != nil {
			m.input.Reset()
			return m.sendInput(m.uncertain, "check")
		}
		m.status = "No uncertain input is retained in this terminal."
		return nil
	case "/retry":
		if m.uncertain != nil {
			m.input.Reset()
			return m.sendInput(m.uncertain, "retry")
		}
		if m.retryControl != nil {
			m.controlling = true
			m.input.Reset()
			return m.retryControl
		}
		m.status = "No uncertain action is retained in this terminal."
		return nil
	case "/older", "/newer", "/latest":
		if args != "" {
			m.status = "usage: " + name
			return nil
		}
		m.input.Reset()
		if name == "/latest" {
			m.latest()
			return nil
		}
		direction := "backward"
		if name == "/newer" {
			direction = "forward"
		}
		return m.browseHistory(direction)
	case "/tools":
		if args != "expand" && args != "collapse" {
			m.status = "usage: /tools expand|collapse (display only)"
			return nil
		}
		m.input.Reset()
		m.expandTools = args == "expand"
		m.toolExpansion = nil
		m.status = "Tool output display: " + args
		m.refresh()
		return nil
	case "/reasoning":
		if args != "on" && args != "off" {
			m.status = "usage: /reasoning on|off (live preview only; unavailable after reload)"
			return nil
		}
		m.input.Reset()
		m.showReasoning = args == "on"
		m.status = "Live reasoning display: " + args + ". Reasoning is not retained in history."
		m.refresh()
		return nil
	case "/me":
		return m.standing(args)
	case "/memory":
		return m.memory(args)
	case "/repl":
		return m.replCommand(args)
	case "/agents":
		return m.agentsCommand(args)
	case "/dock", "/sidebar":
		if args != "" {
			m.status = "usage: " + name
			return nil
		}
		return m.layoutCommand(strings.TrimPrefix(name, "/"))
	case "/mcp":
		return m.mcpCommand(args)
	case "/lsp":
		return m.lspCommand(args)
	case "/browser":
		return m.browserCommand(args)
	case "/computer", "/computer-use":
		return m.computerCommand(args)
	case "/compact":
		return m.compactionCommand(args)
	case "/goal", "/goal-from-context":
		return m.goalCommand(name, args)
	case "/schedule":
		return m.scheduleCommand(args)
	case "/permissions":
		return m.permissionsCommand(args)
	case "/status":
		return m.control("Session status", false, func(ctx context.Context) nativeControlResult {
			owner, err := m.handle.Get(ctx)
			return nativeControlResult{label: "Session is " + owner.Lifecycle, owner: &owner, err: err}
		})
	case "/pwd":
		return m.control("Working directory", false, func(ctx context.Context) nativeControlResult {
			owner, err := m.handle.Get(ctx)
			return nativeControlResult{label: owner.WorkingDirectory, owner: &owner, err: err}
		})
	}
	if m.uncertain != nil || m.retryControl != nil {
		m.status = "Inspect or explicitly retry the original uncertain action first."
		return nil
	}
	switch name {
	case "/model", "/model-for-session":
		return m.modelCommand(name, args)
	case "/auth", "/connect", "/setup":
		return m.setupCommand(args)
	case "/theme":
		return m.themeCommand(args)
	case "/mouse":
		return m.mouseCommand(args)
	case "/settings":
		if args != "" {
			m.status = "usage: " + name
			return nil
		}
		return m.openMenu(strings.TrimPrefix(name, "/"))
	case "/rewind":
		return m.rewindHistory(args)
	case "/fork", "/fork-at":
		return m.forkHistory(args, name == "/fork-at")
	case "/resume", "/sessions":
		return m.resumeSession(args)
	case "/stop", "/start":
		if args != "" {
			m.status = "usage: " + name
			return nil
		}
		state := "stopped"
		if name == "/start" {
			state = "active"
		}
		params := protocol.LifecycleParams{SessionID: m.handle.ID(), Lifecycle: state}
		// Lifecycle changes have no immutable receipt. A later explicit command
		// is fresh intent; never retain one as a retry against future work.
		return m.control("Set session "+state, false, func(ctx context.Context) nativeControlResult {
			var owner protocol.Session
			err := m.connection.Call(ctx, "sessions.lifecycle", params, &owner)
			if err == nil && (owner.ID != params.SessionID || owner.Lifecycle != state) {
				err = errors.New("lifecycle control ownership mismatch")
			}
			return nativeControlResult{label: "Session is " + state, mutation: true, owner: &owner, err: err, inspectOnError: true}
		})
	case "/steer", "/queue":
		if args == "" {
			m.status = name + " <message>"
			return nil
		}
		return m.prompt(args, strings.TrimPrefix(name, "/"))
	case "/cd":
		if args == "" {
			return m.command("/pwd")
		}
		params := protocol.WorkspaceSetParams{ID: protocol.ID(uuid.NewString()), SessionID: m.handle.ID(), ExpectedRevision: m.owner.ConfigRevision, Path: args}
		return m.control("Change working directory", true, func(ctx context.Context) nativeControlResult {
			var value protocol.ControlEdit
			err := m.connection.Call(ctx, "workspace.set", params, &value)
			if err == nil && (value.ID != params.ID || value.SessionID != params.SessionID || value.Session != nil && value.Session.ID != params.SessionID) {
				err = errors.New("workspace control ownership mismatch")
			}
			label := "Working directory edit recorded"
			if value.Deleted {
				label = "The edited session was deleted; no session was recreated"
			} else if value.Session != nil {
				label = "Working directory: " + value.Session.WorkingDirectory
			}
			return nativeControlResult{label: label, owner: value.Session, err: err}
		})
	case "/clear":
		if args != "" {
			m.status = "usage: /clear"
			return nil
		}
		if m.owner.Lifecycle != "stopped" {
			m.status = "Clear requires a stopped session with no queued work. Use /stop first; it also cancels active work."
			return nil
		}
		params := protocol.RewindParams{EditID: protocol.ID(uuid.NewString()), SessionID: m.handle.ID(), ExpectedRevision: m.history.snapshot.Revision, ObservedThrough: m.history.snapshot.ThroughSequence}
		return m.control("Clear conversation", true, func(ctx context.Context) nativeControlResult {
			var value protocol.HistoryEdit
			err := m.connection.Call(ctx, "sessions.rewind", params, &value)
			if err == nil && (value.ID != params.EditID || value.SessionID != params.SessionID || value.KeepThrough != 0) {
				err = errors.New("history edit ownership mismatch")
			}
			return nativeControlResult{label: "Conversation cleared and REPL reset. Workspace snapshots and files are unchanged. Use /start to accept execution again.", reset: err == nil, err: err}
		})
	case "/rename":
		if args == "" {
			return m.openMenu("rename")
		}
		owner := m.owner
		// The metadata revision is read once for this human command, then frozen
		// for explicit retry. A later conflict never overwrites a newer title.
		var params *protocol.UpdateTreeParams
		return m.control("Rename session", true, func(ctx context.Context) nativeControlResult {
			if params == nil {
				var tree protocol.Tree
				if err := m.connection.Call(ctx, "trees.get", protocol.TreeParams{TreeID: owner.TreeID}, &tree); err != nil {
					return nativeControlResult{err: err}
				}
				if tree.ID != owner.TreeID {
					return nativeControlResult{err: errors.New("tree ownership mismatch")}
				}
				metadata := tree.Metadata
				metadata.Title = new(args)
				params = &protocol.UpdateTreeParams{TreeID: tree.ID, ExpectedRevision: tree.Revision, Metadata: metadata}
			}
			var value protocol.Tree
			err := m.connection.Call(ctx, "trees.update", *params, &value)
			if err == nil && value.ID != owner.TreeID {
				err = errors.New("renamed tree ownership mismatch")
			}
			return nativeControlResult{label: fmt.Sprintf("Session named %q", args), err: err}
		})
	default:
		m.status = "Unknown or unavailable terminal command: " + name
		return nil
	}
}
