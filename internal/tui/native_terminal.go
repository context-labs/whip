package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/context-labs/whip/internal/capability"
)

type nativeTerminal struct {
	pid                   int
	directory, proc, tmux string
	multiplexed, forced   bool
	rgb, known            bool
	hints                 string
}

type nativeTerminalHints struct {
	terminal *nativeTerminal
	text     string
}

func newNativeTerminal(directory, term, tmux, scheme, colorfgbg string) *nativeTerminal {
	t := &nativeTerminal{pid: os.Getpid(), directory: directory, proc: "/proc", tmux: tmux, multiplexed: tmux != "" || strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux")}
	bgCache = bgResult{}
	SetUnknownTheme()
	switch strings.ToLower(scheme) {
	case "light", "dark":
		t.forced, t.known = true, true
		SetLightTheme(strings.EqualFold(scheme, "light"))
	default:
		if len(colorfgbg) <= 256 {
			if i := strings.LastIndexByte(colorfgbg, ';'); i >= 0 {
				if bg, err := strconv.Atoi(colorfgbg[i+1:]); err == nil && bg >= 0 && bg <= 255 {
					t.known = true
					SetLightTheme(bg >= 7)
				}
			}
		}
	}
	return t
}

// Bubble Tea owns both query writes and input decoding. No second reader opens
// the tty, changes termios, or competes with the composer's input stream.
func nativeTerminalQuery(multiplexed bool) string {
	const osc = "\x1b]11;?\x1b\\"
	query := osc
	if multiplexed {
		query += "\x1bPtmux;" + strings.ReplaceAll(osc, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return query + "\x1b[?996n"
}

func (m *nativeModel) terminalCommands() tea.Cmd {
	t := m.terminal
	if t == nil {
		return nil
	}
	probe := func() tea.Msg {
		ctx, done, err := m.work.beginFor(3 * time.Second)
		if err != nil {
			return nativeTerminalHints{terminal: t}
		}
		defer done()
		return nativeTerminalHints{terminal: t, text: t.readHints(ctx)}
	}
	return tea.Batch(tea.Raw(nativeTerminalQuery(t.multiplexed)), probe)
}

func (m *nativeModel) terminalMessage(message tea.Msg) bool {
	t := m.terminal
	if t == nil {
		return false
	}
	switch value := message.(type) {
	case nativeTerminalHints:
		if value.terminal == t {
			t.hints = value.text
		}
	case tea.BackgroundColorMsg:
		if value.Color == nil || t.forced {
			return true
		}
		r, g, b, _ := value.RGBA()
		t.rgb, t.known = true, true
		bgCache = bgResult{light: !value.IsDark(), valid: true, hasRGB: true, r: int(r >> 8), g: int(g >> 8), b: int(b >> 8)}
		SetLightTheme(!value.IsDark())
	case uv.LightColorSchemeEvent, uv.DarkColorSchemeEvent:
		if t.rgb || t.forced {
			return true
		}
		_, light := message.(uv.LightColorSchemeEvent)
		t.known = true
		bgCache = bgResult{light: light, valid: true}
		SetLightTheme(light)
	case tea.ColorProfileMsg:
		setThemeProfile(value.Profile)
	default:
		return false
	}
	m.refresh()
	return true
}

func (t *nativeTerminal) notice() string {
	if t == nil {
		return ""
	}
	hints := t.hints
	mdMu.Lock()
	override := mdScheme
	mdMu.Unlock()
	if !t.known && override == "" {
		if hints != "" {
			hints += "\n"
		}
		hints += "Terminal background unknown; using neutral colors. /theme light or /theme dark sets an explicit preference."
	}
	return hints
}

func (t *nativeTerminal) readHints(ctx context.Context) string {
	mosh := nativeAncestor(t.proc, t.pid, "mosh-server")
	keys, known := "", false
	if !mosh && t.multiplexed {
		if result, err := t.tmuxInfo(ctx); err == nil {
			pid, setting, ok := strings.Cut(strings.TrimSpace(result), "|")
			if ok {
				keys, known = setting, true
				if clientPID, err := strconv.Atoi(pid); err == nil && clientPID > 1 {
					mosh = nativeAncestor(t.proc, clientPID, "mosh-server")
				}
			}
		}
	}
	switch {
	case mosh:
		return "Shift+Enter is unavailable over mosh; use Ctrl+J or Alt+Enter for a newline."
	case known && keys != "on" && keys != "always":
		return "Shift+Enter needs tmux extended-keys on. Add `set -s extended-keys on` to your tmux configuration; Ctrl+J or Alt+Enter also insert newlines."
	case t.multiplexed && !known:
		return "Terminal key forwarding could not be inspected; Ctrl+J inserts a newline if modified Enter is unavailable."
	default:
		return ""
	}
}

func (t *nativeTerminal) tmuxInfo(ctx context.Context) (string, error) {
	if len(t.tmux) > 4096 || !t.multiplexed {
		return "", errors.New("terminal multiplexer declaration is unavailable")
	}
	program, err := exec.LookPath("tmux")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	manager := capability.NewProcessManager()
	defer func() { _ = manager.Close() }()
	stdout, stderr := nativeClipboardBuffer{limit: 4096, stop: cancel}, nativeClipboardBuffer{limit: 4096, stop: cancel}
	process, err := manager.Start(ctx, "terminal-hints", program, []string{"display-message", "-p", "#{client_pid}|#{extended-keys}"}, capability.ProcessOptions{Cwd: t.directory, Stdin: strings.NewReader(""), Env: map[string]string{"TMUX": t.tmux}, Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return "", err
	}
	err = process.Wait()
	process.Stop()
	if err != nil || stdout.full || stderr.full {
		return "", errors.New("terminal key capability probe did not complete within its bounds")
	}
	return stdout.buffer.String(), nil
}

// Linux /proc inspection is optional and bounded. Other systems simply have
// no matching evidence; neither environment secrets nor process argv are read.
func nativeAncestor(directory string, pid int, name string) bool {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	read := func(path string) ([]byte, error) {
		file, err := root.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = file.Close() }()
		data, err := io.ReadAll(io.LimitReader(file, 4097))
		if len(data) > 4096 {
			return nil, errors.New("process metadata exceeds bound")
		}
		return data, err
	}
	for range 64 {
		if pid <= 1 {
			return false
		}
		comm, err := read(fmt.Sprintf("%d/comm", pid))
		if err != nil {
			return false
		}
		if strings.TrimSpace(string(comm)) == name {
			return true
		}
		stat, err := read(fmt.Sprintf("%d/stat", pid))
		if err != nil {
			return false
		}
		end := bytes.LastIndexByte(stat, ')')
		if end < 0 {
			return false
		}
		fields := strings.Fields(string(stat[end+1:]))
		if len(fields) < 2 {
			return false
		}
		parent, err := strconv.Atoi(fields[1])
		if err != nil || parent == pid {
			return false
		}
		pid = parent
	}
	return false
}
