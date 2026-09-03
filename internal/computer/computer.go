// Package computer is whip's computer-use subsystem: drive the user's
// actual desktop (apps, windows, mouse, keyboard, screen) — with browsers
// as the flagship case. macOS-first; the design and borrow rationale live
// in .ai-docs/plans/computer-use/README.md and
// docs/learnings/other-harnesses/codex-computer-use-plugin.md.
//
// v1 is pure osascript (AppleScript via subprocess): Chrome control needs no
// CDP setup and no compilation — the user's already-open Chrome is
// scriptable through its AppleScript dictionary. AX-tree reads and CGEvent
// input are v2 (System Events UI scripting for v1.5, a signed embedded
// helper for the full tier).
package computer

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ErrUnsupportedPlatform gates every backend call on non-macOS.
var ErrUnsupportedPlatform = errors.New("computer-use is macOS-only for now (linux/windows backends are follow-ups)")

// Available reports whether this platform can drive the desktop.
func Available() bool { return runtime.GOOS == "darwin" }

type CommandRunner func(context.Context, string, ...string) ([]byte, error)

type Automation struct {
	Context context.Context
	Run     CommandRunner
}

func (a Automation) run(name string, args ...string) ([]byte, error) {
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if a.Run != nil {
		return a.Run(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (a Automation) AppleScript(script string) (string, error) {
	if !Available() {
		return "", ErrUnsupportedPlatform
	}
	out, err := a.run("osascript", "-e", script)
	s := strings.TrimRight(string(out), "\n")
	if err != nil {
		if strings.Contains(s, "User canceled") {
			return s, nil // user dismissed a dialog — not a failure
		}
		return s, fmt.Errorf("osascript: %w: %s", err, s)
	}
	return s, nil
}

// quote escapes a Go string for embedding inside an AppleScript "…" literal.
// mack doesn't do this; a `"` in user input would break the script.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// Tell runs `tell application <app>` with the given commands — the general
// escape hatch (mack's Tell, fixed). app is an app name ("Google Chrome") or
// bundle id; commands are AppleScript statements.
func Tell(app string, commands ...string) (string, error) {
	return Automation{}.Tell(app, commands...)
}

func (a Automation) Tell(app string, commands ...string) (string, error) {
	var b strings.Builder
	b.WriteString("tell application " + quote(app) + "\n")
	for _, c := range commands {
		b.WriteString(c + "\n")
	}
	b.WriteString("end tell")
	return a.AppleScript(b.String())
}
