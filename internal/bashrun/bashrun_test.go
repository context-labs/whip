package bashrun

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestNonInteractiveDoesNotHangOnTTYRead is the regression test for the bug that
// started this change: a program that tries to read a password from /dev/tty
// must not hang the agent. pre-fix the command would block until the 120s
// timeout; post-fix it exits immediately ("a terminal is required to read the
// password").
//
// We don't need real sudo to repro the failure: any program reading from
// /dev/tty reproduces it. We use a tiny inline script that mimics sudo's
// behaviour — open /dev/tty and read a line — and assert it returns *fast*.
func TestNonInteractiveDoesNotHangOnTTYRead(t *testing.T) {
	cmd := `exec 3< /dev/tty; read -r line <&3; echo "got: $line"`
	res := Run(context.Background(), Options{
		Command: cmd,
		// short cap so even if the fix regressed the test would fail quickly
		Timeout: 5 * time.Second,
	})

	if res.TimedOut {
		t.Fatalf("command hung and timed out — Setsid isolation regressed. output: %q", res.Output)
	}
	// We expect an immediate, non-zero exit (the read fails). Outlook that it
	// surfaces the failure text rather than nothing.
	if res.Output == "" && res.Exit == "" {
		t.Fatalf("expected a fast non-zero exit; got empty result %+v", res)
	}
}

// TestNonInteractiveCapture verifies basic stdout/stderr capture and clean exit.
func TestNonInteractiveCapture(t *testing.T) {
	res := Run(context.Background(), Options{
		Command: `echo hi; echo err >&2; exit 3`,
	})
	if !strings.Contains(res.Output, "hi") || !strings.Contains(res.Output, "err") {
		t.Fatalf("output missing: %q", res.Output)
	}
	if !strings.Contains(res.Exit, "exit") || !strings.Contains(res.Exit, "3") {
		t.Fatalf("exit status wrong: %q", res.Exit)
	}
	if res.TimedOut {
		t.Fatalf("should not time out: %+v", res)
	}
}

// TestNonInteractiveEmpty reports "(no output)" normally handled by the caller,
// here we just confirm output is empty and exit is clean.
func TestNonInteractiveCleanExit(t *testing.T) {
	res := Run(context.Background(), Options{Command: `true`})
	if res.Output != "" || res.Exit != "" {
		t.Fatalf("clean exit should be empty: %+v", res)
	}
}

// TestNonInteractiveTimeout confirms DeadlineExceeded is reported as TimedOut.
func TestNonInteractiveTimeout(t *testing.T) {
	res := Run(context.Background(), Options{
		Command: `sleep 5`,
		Timeout: 100 * time.Millisecond,
	})
	if !res.TimedOut || !res.Killed {
		t.Fatalf("expected Killed+TimedOut: %+v", res)
	}
	if !strings.Contains(res.Exit, "timed out") {
		t.Fatalf("exit text wrong: %q", res.Exit)
	}
}

func TestNonInteractiveTimeoutKillsDescendants(t *testing.T) {
	marker := t.TempDir() + "/survived"
	res := Run(context.Background(), Options{
		Command: `(sleep 0.4; touch ` + strconv.Quote(marker) + `) & wait`,
		Timeout: 100 * time.Millisecond,
	})
	if !res.TimedOut || !res.Killed {
		t.Fatalf("expected Killed+TimedOut: %+v", res)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant survived timeout: %v", err)
	}
}

// TestNonInteractiveCancellation exercises the ctx-cancel path.
func TestNonInteractiveCancellation(t *testing.T) {
	marker := t.TempDir() + "/survived"
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()
	res := Run(ctx, Options{Command: `(sleep 0.4; touch ` + strconv.Quote(marker) + `) & wait`, Timeout: 10 * time.Second})
	if !res.Killed {
		t.Fatalf("cancellation should kill: %+v", res)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant survived cancellation: %v", err)
	}
}

// TestInteractiveExitError simulates a short-lived interactive command and
// checks the runner waits for the child and reports its exit status. We use a
// command that prints then exits without waiting for input.
func TestInteractiveExit(t *testing.T) {
	res := Run(context.Background(), Options{
		Command:     `echo hello; exit 7`,
		Interactive: true,
		Timeout:     5 * time.Second,
		OnOutput:    func(s string) {}, // exercise the callback path
	})
	if !res.Interactive {
		t.Fatalf("expected Interactive result")
	}
	if !strings.Contains(res.Output, "hello") {
		t.Fatalf("interactive output missing: %q", res.Output)
	}
	// a non-zero exit should be reflected in Exit
	if res.Exit == "" {
		t.Fatalf("expected non-empty exit status for `exit 7`: %+v", res)
	}
}

// TestInteractiveInactivityTimeout is the core safety property: an interactive
// command that waits for input and never receives any must be killed after
// the inactivity window — not the full 120s timeout.
func TestInteractiveInactivityTimeout(t *testing.T) {
	start := time.Now()
	res := Run(context.Background(), Options{
		Command:           `cat`, // waits for input forever
		Interactive:       true,
		Timeout:           60 * time.Second, // well beyond the inactivity cap
		InactivityTimeout: 400 * time.Millisecond,
		OnAwaitInput:      func(int) {}, // exercise the callback path
	})
	elapsed := time.Since(start)

	if !res.Killed {
		t.Fatalf("inactivity should kill the command: %+v", res)
	}
	if !strings.Contains(res.Exit, "waiting for input") {
		t.Fatalf("exit text wrong: %q", res.Exit)
	}
	// must return near the inactivity cap, not the wall-clock timeout
	if elapsed > 3*time.Second {
		t.Fatalf("took too long (%s); inactivity timeout not honoured", elapsed)
	}
}

// A ready, non-echoing PTY must stay alive while input arrives, then expire.
// The child records received lines outside the PTY, so output cannot mask a
// regression in the keystroke activity clock.
func TestInteractiveKeyForwardingDelaysInactivity(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	directory := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	keys := make(chan []byte)
	ready := make(chan struct{})
	done := make(chan struct{})
	var sent []time.Duration
	start := time.Now()
	go func() {
		defer close(done)
		defer close(keys)
		select {
		case <-ready:
		case <-ctx.Done():
			return
		}
		for range 6 {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
			select {
			case keys <- []byte("x\n"):
				sent = append(sent, time.Since(start))
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() { cancel(); <-done })

	var output strings.Builder
	announced := false
	res := Run(ctx, Options{
		Command:           `stty -echo || exit 97; printf 'ready\n'; while IFS= read -r line; do printf '%s\n' "$line" >> forwarded; done`,
		Cwd:               directory,
		Interactive:       true,
		Timeout:           10 * time.Second,
		InactivityTimeout: 250 * time.Millisecond,
		Keys:              keys,
		OnOutput: func(chunk string) {
			output.WriteString(chunk)
			if !announced && strings.Contains(strings.ReplaceAll(output.String(), "\r", ""), "ready\n") {
				announced = true
				close(ready)
			}
		},
	})
	elapsed := time.Since(start)
	cancel()
	<-done
	received, err := os.ReadFile(filepath.Join(directory, "forwarded"))
	if !res.Interactive || !res.Killed || res.TimedOut || res.Exit != "timed out waiting for input" {
		t.Fatalf("expected PTY inactivity after input: result=%+v elapsed=%s sent=%v received=%q read_error=%v", res, elapsed, sent, received, err)
	}
	if len(sent) != 6 || err != nil || string(received) != strings.Repeat("x\n", 6) {
		t.Fatalf("child did not receive six lines: result=%+v elapsed=%s sent=%v received=%q read_error=%v", res, elapsed, sent, received, err)
	}
	if strings.TrimSpace(res.Output) != "ready" {
		t.Fatalf("child output masked keystroke activity: %q", res.Output)
	}
	if elapsed < 550*time.Millisecond {
		t.Fatalf("forwarded keys did not reset inactivity: elapsed=%s sent=%v result=%+v", elapsed, sent, res)
	}
}

// TestUserShellResolution: $SHELL wins, an empty $SHELL falls back to the
// passwd entry (or bash), and the runner actually executes through the
// resolved shell — the `!` escape regression ("should use the user's shell").
func TestUserShellResolution(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	if sh := userShell(); sh != "/bin/zsh" {
		t.Fatalf("$SHELL should win, got %q", sh)
	}

	t.Setenv("SHELL", "")
	if sh := userShell(); sh == "" {
		t.Fatal("empty $SHELL must fall back to the passwd entry or bash")
	}

	// end-to-end: run through a "shell" that proves it was the interpreter.
	// A real shell is required for -c, so point $SHELL at /bin/sh and check
	// the command ran through it.
	t.Setenv("SHELL", "/bin/sh")
	res := Run(context.Background(), Options{Command: "echo shell-ok"})
	if !strings.Contains(res.Output, "shell-ok") || res.Exit != "" {
		t.Fatalf("run via user shell: %+v", res)
	}
}

func TestKeyBytes(t *testing.T) {
	cases := map[string]string{
		"enter":     KeyEnter,
		"esc":       KeyEsc,
		"tab":       KeyTab,
		"backspace": KeyBS,
		"delete":    KeyBS,
		"up":        KeyUp,
		"down":      KeyDown,
		"right":     KeyRight,
		"left":      KeyLeft,
		"bogus":     "",
		"":          "",
	}
	for name, want := range cases {
		if got := KeyBytes(name); got != want {
			t.Errorf("KeyBytes(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestOpenDevNullIsReadOnlyStdin(t *testing.T) {
	f := openDevNull()
	if f == nil {
		t.Fatal("openDevNull failed")
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	var b [1]byte
	if n, err := f.Read(b[:]); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("stdin read = %d, %v; want EOF", n, err)
	}
	if _, err := f.WriteString("not writable"); err == nil {
		t.Fatal("stdin descriptor is writable")
	}
}
