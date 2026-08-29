package bashrun

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunReportsUnstartableShell: a $SHELL that doesn't exist must come back as
// a failed run, not a hang and not a panic — on both the piped and the
// interactive path (the latter falls back to the piped one when pty.Start
// fails).
func TestRunReportsUnstartableShell(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		t.Setenv("SHELL", "/nonexistent/definitely-not-a-shell")
		res := Run(context.Background(), Options{
			Command:           "echo hi",
			Interactive:       interactive,
			Timeout:           5 * time.Second,
			InactivityTimeout: time.Second,
		})
		if res.Exit == "" {
			t.Fatalf("interactive=%v: a shell that can't start must report an exit status: %+v", interactive, res)
		}
		// the piped path names the missing binary; the interactive path first
		// fails in pty.Start and reports the failed retry of the same cmd
		if !strings.Contains(res.Exit, "exit:") {
			t.Fatalf("interactive=%v: exit should name the start failure, got %q", interactive, res.Exit)
		}
		if res.Output != "" {
			t.Fatalf("interactive=%v: nothing ran, so there is no output: %q", interactive, res.Output)
		}
	}
}

func TestRunStructuredIO(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("HOOK_TEST", "ambient")
	dir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	res := Run(t.Context(), Options{
		Command:        `read input; printf 'out:%s:%s:%s' "$input" "$HOOK_TEST" "$PWD"; printf 'diagnostic' >&2; exit 7`,
		Stdin:          []byte("payload\n"),
		Env:            []string{"HOOK_TEST=value"},
		Dir:            dir,
		SeparateOutput: true,
	})
	if res.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7 (%+v)", res.ExitCode, res)
	}
	if res.Stdout != "out:payload:value:"+realDir {
		t.Fatalf("stdout = %q", res.Stdout)
	}
	if res.Stderr != "diagnostic" {
		t.Fatalf("stderr = %q", res.Stderr)
	}
	if !strings.Contains(res.Output, "out:payload:value:") || !strings.Contains(res.Output, "diagnostic") {
		t.Fatalf("combined output lost a stream: %q", res.Output)
	}
}

func TestRunCapsStructuredOutput(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	res := Run(t.Context(), Options{
		Command:        `printf 'abcdefghij'`,
		SeparateOutput: true,
		MaxOutputBytes: 4,
	})
	if !res.Truncated {
		t.Fatal("expected output cap to report truncation")
	}
	if res.Output != "abcd" || res.Stdout != "abcd" {
		t.Fatalf("capped output = %q stdout = %q", res.Output, res.Stdout)
	}
}

func TestRunStructuredOutputLimitAppliesPerStream(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	res := Run(t.Context(), Options{
		Command:        `printf 'abcd'; printf 'wxyz' >&2`,
		SeparateOutput: true,
		MaxOutputBytes: 4,
	})
	if res.Truncated {
		t.Fatalf("individually bounded streams reported truncation: %+v", res)
	}
	if res.Stdout != "abcd" || res.Stderr != "wxyz" || len(res.Output) != 8 {
		t.Fatalf("structured output = stdout %q stderr %q combined %q", res.Stdout, res.Stderr, res.Output)
	}
}

func TestMergeEnvReplacesDuplicateKeys(t *testing.T) {
	got := mergeEnv(
		[]string{"A=old", "B=base", "A=stale", "loose"},
		[]string{"A=marker", "C=marker"},
		[]string{"A=hook", "C=hook"},
	)
	want := []string{"A=hook", "B=base", "loose", "C=hook"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("merged environment = %v, want %v", got, want)
	}
}

// TestInteractiveNeverOutlivesItsCaps: the safety property of the PTY path —
// a silent long-running child dies promptly whether the hard wall-clock cap or
// the user's interrupt gets there first, and the result says so.
//
// The reported reason comes from the context, not from whichever arm of the
// run loop's select happened to win the race with the output pump, so each
// case has one correct exit text.
func TestInteractiveNeverOutlivesItsCaps(t *testing.T) {
	cases := map[string]struct {
		setup    func() (context.Context, time.Duration)
		wantExit string
		wantTO   bool
	}{
		"hard timeout": {
			setup: func() (context.Context, time.Duration) {
				return context.Background(), 200 * time.Millisecond
			},
			wantExit: "timed out",
			wantTO:   true,
		},
		"cancellation": {
			setup: func() (context.Context, time.Duration) {
				ctx, cancel := context.WithCancel(context.Background())
				time.AfterFunc(150*time.Millisecond, cancel)
				t.Cleanup(cancel)
				return ctx, 30 * time.Second
			},
			wantExit: "cancelled",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, timeout := tc.setup()
			start := time.Now()
			res := Run(ctx, Options{
				Command:           "sleep 30",
				Interactive:       true,
				Timeout:           timeout,
				InactivityTimeout: 500 * time.Millisecond,
			})
			elapsed := time.Since(start)
			if !res.Killed || !res.Interactive {
				t.Fatalf("expected a killed interactive result: %+v", res)
			}
			if res.Exit != tc.wantExit {
				t.Fatalf("exit reason: got %q, want %q (%+v)", res.Exit, tc.wantExit, res)
			}
			if res.TimedOut != tc.wantTO {
				t.Fatalf("TimedOut: got %v, want %v (%+v)", res.TimedOut, tc.wantTO, res)
			}
			if elapsed > 10*time.Second {
				t.Fatalf("the child outlived its caps by %s", elapsed)
			}
		})
	}
}

// TestExitString covers the three renderings the model sees.
func TestExitString(t *testing.T) {
	if got := exitString(nil); got != "" {
		t.Fatalf("a clean exit must render empty, got %q", got)
	}
	if got := exitString(errors.New("fork/exec: no such file")); got != "(exit: fork/exec: no such file)" {
		t.Fatalf("non-exit errors: %q", got)
	}
	err := exec.CommandContext(t.Context(), "sh", "-c", "exit 5").Run()
	if got := exitString(err); got != "(exit: exit status 5)" {
		t.Fatalf("exit status: %q", got)
	}
}

// TestIsKilledBySignal distinguishes a signalled death from a plain non-zero
// exit and from a non-exec error.
func TestIsKilledBySignal(t *testing.T) {
	if isKilledBySignal(errors.New("boom")) {
		t.Fatal("a non-exec error is not a signal kill")
	}
	if isKilledBySignal(exec.CommandContext(t.Context(), "sh", "-c", "exit 2").Run()) {
		t.Fatal("a plain non-zero exit is not a signal kill")
	}
	err := exec.CommandContext(t.Context(), "sh", "-c", "kill -9 $$").Run()
	if err == nil {
		t.Fatal("self-SIGKILL should fail the command")
	}
	if !isKilledBySignal(err) {
		t.Fatalf("SIGKILL should be reported as a signal kill: %v", err)
	}
}

// TestTrackIgnoresUnstartedCommands: a command that never started has no pid,
// so the registry must ignore it rather than record a zero key (which KillAll
// would then signal).
func TestTrackIgnoresUnstartedCommands(t *testing.T) {
	before := trackedCount()
	unstarted := exec.CommandContext(t.Context(), "true")
	track(unstarted)
	if got := trackedCount(); got != before {
		t.Fatalf("tracking an unstarted command changed the registry: %d -> %d", before, got)
	}
	untrack(unstarted)
	if got := trackedCount(); got != before {
		t.Fatalf("untracking an unstarted command changed the registry: %d -> %d", before, got)
	}
}

// TestKillAllSkipsProcesslessEntries: KillAll must not panic on a registry
// entry whose process is gone.
func TestKillAllSkipsProcesslessEntries(t *testing.T) {
	trackMu.Lock()
	tracked[-1] = exec.CommandContext(t.Context(), "true") // never started: Process is nil
	trackMu.Unlock()
	t.Cleanup(func() {
		trackMu.Lock()
		delete(tracked, -1)
		trackMu.Unlock()
	})

	KillAll()

	trackMu.Lock()
	_, still := tracked[-1]
	trackMu.Unlock()
	if !still {
		t.Fatal("KillAll should not mutate the registry")
	}
}
