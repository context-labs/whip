package tui

import (
	"bufio"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
)

// The startup gate: a trusted cwd passes without a prompt.
func TestTrustGate(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	wd, _ := os.Getwd()
	if err := config.Trust(wd); err != nil {
		t.Fatal(err)
	}
	outcome, err := checkTrust(bufio.NewReader(strings.NewReader("")))
	if err != nil || outcome != trustGranted {
		t.Fatalf("trusted cwd should pass: %v %v", outcome, err)
	}
}

// An untrusted cwd with no terminal anywhere — stdin piped and /dev/tty
// unavailable — defers to the in-TUI prompt rather than aborting, and never
// consumes the piped answer.
func TestTrustGateDefersWhenNoTerminal(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir()) // fresh home: cwd untrusted

	// stdin is a pipe (not a char device) and /dev/tty can't be opened.
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	origStdin, origTTY := trustStdin, trustDevTTY
	t.Cleanup(func() { trustStdin, trustDevTTY = origStdin, origTTY })
	trustStdin = func() *os.File { return r }
	trustDevTTY = func() (*os.File, error) { return nil, errors.New("no controlling terminal") }

	outcome, err := checkTrust(bufio.NewReader(strings.NewReader("y\n")))
	if err != nil {
		t.Fatalf("no-terminal defer should not error, got %v", err)
	}
	if outcome != trustDeferred {
		t.Fatalf("untrusted cwd with no terminal should defer, got %v", outcome)
	}
	// The deferred path must not have read (or recorded) the piped "y".
	wd, _ := os.Getwd()
	if config.Trusted(wd) {
		t.Fatal("a deferred gate must not trust the folder")
	}
}

// When stdin is piped but /dev/tty answers, the gate prompts there instead of
// deferring — `git diff | whip up` still asks.
func TestTrustGateAsksOnDevTTY(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	wd, _ := os.Getwd()

	pr, pw, _ := os.Pipe() // stdin: not a char device
	defer pr.Close()
	defer pw.Close()
	origStdin, origTTY := trustStdin, trustDevTTY
	t.Cleanup(func() { trustStdin, trustDevTTY = origStdin, origTTY })
	trustStdin = func() *os.File { return pr }

	// A stand-in for /dev/tty: one end is handed to checkTrust, the other feeds
	// it a "y".
	ttyR, ttyW, _ := os.Pipe()
	defer ttyR.Close()
	defer ttyW.Close()
	trustDevTTY = func() (*os.File, error) { return ttyR, nil }
	go func() { ttyW.WriteString("y\n") }()

	outcome, err := checkTrust(bufio.NewReader(strings.NewReader("")))
	if err != nil || outcome != trustGranted {
		t.Fatalf("a 'y' on /dev/tty should grant: %v %v", outcome, err)
	}
	if !config.Trusted(wd) {
		t.Fatal("approving on /dev/tty should record trust")
	}
}
