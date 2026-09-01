package tui

// shell.go — the `!` shell escape and the /cd & /pwd directory commands.
//
// `!cmd` runs cmd locally with the same runner the agent's bash tool uses and
// lands the output in the transcript AND in the conversation (the model sees
// it at the next request) — opencode's `!` shell submit
// (packages/tui/.../prompt/index.tsx:1059 → session.shell), minus the
// shell-mode chrome (ponytail: border/placeholder swap, esc/backspace-at-0
// exit; the prefix at submit time covers the common case).
//
// Concurrency: the command runs on its own goroutine so a slow command never
// freezes the UI, and the conversation append happens on the TUI goroutine
// inside the shellDoneMsg handler — the turn goroutine owns Agent.Messages
// while busy, so mid-turn output goes through Agent.Steer (mutex-guarded,
// injected at the next loop boundary) instead of a raw append. Esc does NOT
// cancel a running `!` command (esc stays bound to interrupting the turn;
// the 120s cap bounds it). ponytail: a second cancel path if long-running
// escapes become a pattern.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/tools"
	"github.com/context-labs/whip/internal/tools/bashrun"
)

// shellDoneMsg reports a finished `!` command: the transcript block and the
// conversation message are applied on the TUI goroutine.
type shellDoneMsg struct {
	cmd string
	out string // formatted like bash-tool output (truncated, exit markers)
}

// runShell starts a `!`-prefixed input line: echo to the transcript now, run
// the command on a goroutine, land the result on shellDoneMsg. Safe while a
// turn is running; queued `!` lines also execute when the queue drains rather
// than being submitted to the model.
func (m *model) runShell(text string) {
	m.startShell(text, true)
}

// runShellQueued runs a `!` line drained from the queue without re-echoing it
// (the queue view already rendered it, like submit for queued messages).
func (m *model) runShellQueued(text string) {
	m.startShell(text, false)
}

func (m *model) startShell(text string, echo bool) {
	cmdLine := strings.TrimSpace(strings.TrimPrefix(text, "!"))
	if cmdLine == "" {
		m.append(dimStyle.Render("(! <command> — run a shell command, output shared with the model)"))
		return
	}
	if echo {
		m.flushThink()
		m.flushCurrent() // don't split the in-flight assistant line with the echo
		m.append(youStyle.Render("❯ ") + text)
	}
	if m.store != nil {
		if err := m.ensureSession(); err != nil {
			m.append(errStyle.Render("shell start failed: " + err.Error()))
			return
		}
	}

	if m.prog == nil {
		// headless (tests): run inline and apply the result directly
		out := shellExecWithOptions(cmdLine, m.shellOptions())
		m.applyShellDone(shellDoneMsg{cmd: cmdLine, out: out})
		return
	}
	p := m.prog
	services := m.agent.Services
	workingDir := m.agent.WorkingDir
	m.shellRunning++
	go func() {
		ctx := tools.WithWorkingDirectory(tools.WithLocalHuman(context.Background()), workingDir)
		res, err := services.RunBash(ctx, cmdLine, 120*time.Second)
		p.Send(shellDoneMsg{cmd: cmdLine, out: formatShellResult(res, err)})
	}()
}

func (m *model) shellOptions() bashrun.Options {
	if m.agent == nil || m.agent.Services == nil {
		return bashrun.Options{}
	}
	opts := m.agent.Services.ProcessOptions()
	if m.agent.WorkingDir != "" {
		opts.Cwd = m.agent.WorkingDir
	}
	return opts
}

func shellExecWithOptions(cmdLine string, opts bashrun.Options) string {
	// context.Background is deliberate: the command is independent of any
	// turn, and esc stays bound to turn interruption. The 120s cap bounds it.
	opts.Command = cmdLine
	res := bashrun.Run(context.Background(), opts)
	return formatShellResult(res, nil)
}

func formatShellResult(res bashrun.Result, err error) string {
	out := tools.TruncateTail(res.Output)
	if err != nil {
		out = strings.TrimSpace(out + "\n" + err.Error())
	}
	if res.TimedOut {
		out += "\n(command timed out)"
	} else if res.Exit != "" {
		out = fmt.Sprintf("%s\n(%s)", out, res.Exit)
	}
	if strings.TrimSpace(out) == "" {
		out = "(no output)"
	}
	return out
}

// applyShellDone lands a finished command in the transcript and conversation.
func (m *model) applyShellDone(msg shellDoneMsg) {
	if m.shellRunning > 0 {
		m.shellRunning--
	}
	// transcript: a tool-style block (collapsed preview, ctrl+e/click expand)
	m.appendRaw(blockTool, msg.out)

	content := "$ " + msg.cmd + "\n" + msg.out
	if m.busy {
		// mid-turn: the turn goroutine owns Messages; steer injects the output
		// at the next loop boundary, where OnSteer echoes it to the transcript.
		m.agent.Steer(content)
		return
	}
	// idle: non-authored so input-history recall skips it; the "$ " prefix
	// keeps resumed transcripts self-explanatory.
	m.agent.AppendUser(content)
	m.persist()
}

// cdCommand changes whip's working directory for everything (bash tool,
// relative read/write/edit paths, @ file index). Bare prints it. A command
// already running under the old cwd keeps it (POSIX); whip's next spawns —
// and the next session record — use the new one.
func (m *model) cdCommand(arg string) {
	if m.shellRunning > 0 {
		m.append(errStyle.Render("/cd: a shell command is running"))
		return
	}
	if arg == "" {
		m.append(dimStyle.Render(cwd()))
		return
	}
	if arg == "~" || strings.HasPrefix(arg, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			m.append(errStyle.Render("/cd: " + err.Error()))
			return
		}
		arg = home + arg[1:]
	}
	target, err := filepath.Abs(arg)
	if err != nil {
		m.append(errStyle.Render("/cd: " + err.Error()))
		return
	}
	if m.agent != nil && m.agent.Services != nil {
		target, err = m.agent.Services.ResolveWorkingDirectory(target)
		if err != nil {
			m.append(errStyle.Render("/cd: " + err.Error()))
			return
		}
	}
	if err := os.Chdir(target); err != nil {
		m.append(errStyle.Render("/cd: " + err.Error()))
		return
	}
	if m.agent != nil {
		m.agent.WorkingDir = target
	}
	m.append(dimStyle.Render("→ " + cwd()))
}
