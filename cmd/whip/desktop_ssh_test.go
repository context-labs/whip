//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDesktopSSHLocalExitAndParentLifetime(t *testing.T) {
	for _, kind := range []string{"normal", "invalid-option", "cancelled", "parent-closed", "non-pipe", "closed-file", "deadline", "parent-disconnect"} {
		t.Run(kind, func(t *testing.T) {
			parent, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = parent.Close(); _ = writer.Close() }()
			originalParent := parent
			defer func() { _ = originalParent.Close() }()
			output, err := os.CreateTemp(t.TempDir(), "ssh-output")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = output.Close() }()
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			args := []string{"-V"} // Print local version; never contact an external host.
			switch kind {
			case "invalid-option":
				args = []string{"-invalid-fixture-option"}
			case "cancelled":
				cancel()
			case "parent-closed":
				_ = writer.Close()
			case "non-pipe":
				parent = output
			case "closed-file":
				_ = parent.Close()
			case "deadline", "parent-disconnect":
				// The local proxy keeps SSH alive without DNS or network traffic.
				args = []string{"-F", "/dev/null", "-o", "BatchMode=yes", "-o", "ProxyCommand=/bin/sleep 20", "fixture.invalid"}
				if kind == "parent-disconnect" {
					cancel()
					ctx = t.Context()
					timer := time.AfterFunc(75*time.Millisecond, func() { _ = writer.Close() })
					defer timer.Stop()
				}
			}
			start := time.Now()
			err = desktopSSH(ctx, args, desktopSSHStreams{parent: parent, stdout: output, stderr: output})
			if kind == "normal" && err != nil {
				t.Fatal(err)
			}
			if kind != "normal" && err == nil {
				t.Fatal("invalid or interrupted SSH supervision succeeded")
			}
			if kind == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
			if time.Since(start) > 4*time.Second {
				t.Fatal("SSH cleanup exceeded its bounded grace period")
			}
		})
	}
}

func TestDesktopSSHCLIExitStatus(t *testing.T) {
	parent, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = parent.Close(); _ = writer.Close() }()
	output, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()
	stdin, stdout, stderr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = parent, output, output
	defer func() { os.Stdin, os.Stdout, os.Stderr = stdin, stdout, stderr }()
	if code := desktopSSHCLI([]string{"-V"}); code != 0 {
		t.Fatalf("local SSH version exit code: %d", code)
	}
	if code := desktopSSHCLI([]string{"-invalid-fixture-option"}); code <= 1 {
		t.Fatalf("SSH usage exit status was lost: %d", code)
	}
	os.Stdin = output
	if code := desktopSSHCLI([]string{"-V"}); code != 1 {
		t.Fatalf("supervisor error exit code: %d", code)
	}
}

func TestDesktopSSHWatcherTimeoutAndFallbackReaping(t *testing.T) {
	command := exec.CommandContext(t.Context(), "/bin/sleep", "20")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	watcher, err := desktopWatchProcess(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.close()
	if err := desktopAwaitExit(watcher, time.Millisecond); err == nil {
		t.Fatal("live process was reported exited")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := desktopAwaitExit(watcher, time.Second); err != nil {
		t.Fatal(err)
	}
	pid := command.Process.Pid
	desktopReapUnobserved(command.Process)
	if _, err := unix.Wait4(pid, nil, unix.WNOHANG, nil); !errors.Is(err, unix.ECHILD) {
		t.Fatalf("fallback did not reap the owned process: %v", err)
	}
	parent, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = parent.Close(); _ = writer.Close() }()
	if _, err := writer.WriteString("parent lifetime bytes"); err != nil {
		t.Fatal(err)
	}
	if ended, err := desktopParentEnded(int(parent.Fd()), time.Millisecond); ended || err != nil {
		t.Fatalf("parent data was mistaken for EOF: %v, %v", ended, err)
	}
}

func TestDesktopSSHFallbackWaitsForOwnedChild(t *testing.T) {
	command := exec.CommandContext(t.Context(), "/bin/sleep", "0.1")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	pid := command.Process.Pid
	desktopReapUnobserved(command.Process)
	if _, err := unix.Wait4(pid, nil, unix.WNOHANG, nil); !errors.Is(err, unix.ECHILD) {
		t.Fatalf("owned child was not reaped: %v", err)
	}
	for _, fd := range []int{-1, 1 << 32} {
		if _, err := desktopParentEnded(fd, 0); err == nil {
			t.Fatal("out-of-range parent descriptor accepted")
		}
	}
}

func TestDesktopSSHWatcherFailsClosedWhenInvalidated(t *testing.T) {
	command := exec.CommandContext(t.Context(), "/bin/sleep", "20")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { command.Process.Kill(); command.Wait() }()
	watcher, err := desktopWatchProcess(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	watcher.close()
	if runtime.GOOS == "darwin" {
		if _, err := watcher.exited(); err == nil {
			t.Fatal("closed native watcher reported a reliable lifetime")
		}
		if invalid, err := desktopWatchProcess(-1); err == nil {
			invalid.close()
			t.Fatal("invalid process ID accepted")
		}
	}
}
