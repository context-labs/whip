package tui

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
)

func nativeTerminalRestore(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		bgCache = bgResult{}
		setThemeProfile(colorprofile.TrueColor)
		setSchemeOverride("")
		SetLightTheme(false)
	})
}

func TestNativeTerminalThemeAutoUsesOwnedRepliesAndKeepsExplicitChoice(t *testing.T) {
	nativeTerminalRestore(t)
	m := nativeSelectionFixture(t)
	setSchemeOverride("")
	m.terminal = newNativeTerminal(t.TempDir(), "xterm", "", "", "")
	if CurrentTheme() != "auto" || !strings.Contains(m.terminal.notice(), "unknown") {
		t.Fatal("missing terminal evidence guessed a theme")
	}
	m.Update(uv.LightColorSchemeEvent{})
	if !schemeIsLight() || CurrentTheme() != "light" || m.terminal.notice() != "" {
		t.Fatal("996 theme report was not applied")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 20, G: 30, B: 40, A: 255}})
	if schemeIsLight() || !bgCache.hasRGB || bgCache.b != 40 {
		t.Fatal("OSC RGB did not supersede report", bgCache)
	}
	m.Update(uv.LightColorSchemeEvent{})
	if schemeIsLight() {
		t.Fatal("less precise report replaced actual RGB")
	}
	setSchemeOverride("dark")
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if CurrentTheme() != "dark" || !schemeIsLight() {
		t.Fatal("terminal reply overrode explicit choice or lost detected auto value")
	}
	setSchemeOverride("")
	if CurrentTheme() != "light" {
		t.Fatal("auto did not reuse latest observed terminal color")
	}
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.ANSI})
	themeMu.Lock()
	profile := themeProfile
	themeMu.Unlock()
	if profile != colorprofile.ANSI {
		t.Fatal("terminal color depth ignored")
	}
}

func TestNativeTerminalEnvironmentOverridesAndQueriesAreBounded(t *testing.T) {
	nativeTerminalRestore(t)
	m := nativeSelectionFixture(t)
	setSchemeOverride("")
	m.terminal = newNativeTerminal(t.TempDir(), "tmux", "socket,1,0", "dark", "0;15")
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if CurrentTheme() != "dark" || !m.terminal.forced {
		t.Fatal("WHIP_THEME precedence changed")
	}
	m.terminal = newNativeTerminal(t.TempDir(), "xterm", "", "", "0;15")
	if !schemeIsLight() || !m.terminal.known {
		t.Fatal("COLORFGBG fallback missing")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	if schemeIsLight() {
		t.Fatal("environment fallback overrode actual reply")
	}
	m.terminal = newNativeTerminal(t.TempDir(), "xterm", "", "", strings.Repeat("1", 257))
	if m.terminal.known {
		t.Fatal("unbounded environment parsed")
	}
	if nativeTerminalQuery(false) != "\x1b]11;?\x1b\\\x1b[?996n" || nativeTerminalQuery(true) != "\x1b]11;?\x1b\\\x1bPtmux;\x1b\x1b]11;?\x1b\x1b\\\x1b\\\x1b[?996n" {
		t.Fatal("terminal query changed")
	}
	before := m.terminal.hints
	m.Update(nativeTerminalHints{terminal: &nativeTerminal{}, text: "stale"})
	if m.terminal.hints != before {
		t.Fatal("stale probe altered a new terminal")
	}
}

func TestNativeTerminalHintsReadOnlyHelperAndProcessAncestry(t *testing.T) {
	dir, proc := t.TempDir(), t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("INFERENCE_API_KEY", "must-not-reach-local-probe")
	script := "#!/bin/sh\n[ -z \"$INFERENCE_API_KEY\" ] || exit 9\nprintf '%s' \"$*\" > args\nprintf '999|off\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	terminal := &nativeTerminal{pid: 123, directory: dir, proc: proc, tmux: "captured-socket,1,0", multiplexed: true}
	if hint := terminal.readHints(t.Context()); !strings.Contains(hint, "extended-keys on") {
		t.Fatal(hint)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil || string(args) != "display-message -p #{client_pid}|#{extended-keys}" {
		t.Fatal("probe mutated multiplexer configuration", string(args), err)
	}
	writeNativeProc(t, proc, 999, 500, "tmux client")
	writeNativeProc(t, proc, 500, 1, "mosh-server")
	if hint := terminal.readHints(t.Context()); !strings.Contains(hint, "over mosh") || strings.Contains(hint, "extended-keys on") {
		t.Fatal("client ancestry hidden behind tmux", hint)
	}
	if err := os.WriteFile(filepath.Join(proc, "999", "stat"), []byte(strings.Repeat("x", 4097)), 0o600); err != nil {
		t.Fatal(err)
	}
	if nativeAncestor(proc, 999, "mosh-server") {
		t.Fatal("oversize process metadata accepted")
	}
}

func writeNativeProc(t *testing.T, dir string, pid, parent int, name string) {
	t.Helper()
	path := filepath.Join(dir, strconv.Itoa(pid))
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	for file, data := range map[string]string{"comm": name + "\n", "stat": fmt.Sprintf("%d (name with ) and spaces) S %d 0", pid, parent)} {
		if err := os.WriteFile(filepath.Join(path, file), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNativeTerminalHintCancellationJoinsDescendantsAndOutputIsBounded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	program := filepath.Join(dir, "tmux")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n/bin/sleep 30 &\nprintf '%s' \"$!\" > child.tmp\n/bin/mv child.tmp child\nwait\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	terminal := &nativeTerminal{directory: dir, proc: t.TempDir(), tmux: "fixture", multiplexed: true}
	ctx, cancel := context.WithCancel(t.Context())
	joined := make(chan error, 1)
	go func() { _, err := terminal.tmuxInfo(ctx); joined <- err }()
	nativeCopyWaitFile(t, filepath.Join(dir, "child"), "")
	raw, err := os.ReadFile(filepath.Join(dir, "child"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-joined:
		if err == nil || syscall.Kill(pid, 0) == nil {
			t.Fatal("probe returned before descendant was joined", pid, err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled terminal probe did not join")
	}
	if err := os.WriteFile(program, []byte("#!/bin/sh\n/bin/dd if=/dev/zero bs=8192 count=1 2>/dev/null\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := terminal.tmuxInfo(t.Context()); err == nil {
		t.Fatal("oversize helper output accepted")
	}
}
