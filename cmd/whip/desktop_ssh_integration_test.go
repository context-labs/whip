//go:build integration && (darwin || linux)

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDesktopSSHSubprocess(t *testing.T) {
	if os.Getenv("WHIP_TEST_DESKTOP_SSH") != "1" {
		return
	}
	args := []string{}
	if err := json.Unmarshal([]byte(os.Getenv("WHIP_TEST_DESKTOP_ARGS")), &args); err != nil {
		os.Exit(90)
	}
	// main exits the subprocess directly. Its fresh TestMain home is empty and
	// unused by these helpers, so remove it before os.Exit bypasses that defer.
	if err := os.Remove(os.Getenv("HOME")); err != nil {
		os.Exit(91)
	}
	os.Args = append([]string{"whip", "_desktop-ssh"}, args...)
	main()
}

func TestDesktopSSHForwardsArgumentsAndExitStatus(t *testing.T) {
	process, _, output := desktopSSHFixture(t, []string{
		"-G", "-F", "/dev/null", "-p", "2299", "-l", "fixture-user",
		"-o", "HostName=fixture.example", "-o", "ProxyCommand=printf 'literal ; $text'", "fixture-alias",
	})
	if err := desktopWaitFixture(process, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"hostname fixture.example\n", "user fixture-user\n", "port 2299\n", "proxycommand printf 'literal ; $text'\n",
	} {
		if !strings.Contains(string(data), line) {
			t.Fatalf("SSH did not receive exact argument %q", line)
		}
	}

	process, _, _ = desktopSSHFixture(t, []string{"-F", "/dev/null", "-o", "UnknownFixtureOption=yes", "fixture"})
	err = desktopWaitFixture(process, 5*time.Second)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 255 {
		t.Fatalf("SSH exit status was not preserved: %v", err)
	}
}

func TestDesktopSSHParentLossAndSignalsCleanOwnedGroup(t *testing.T) {
	for _, tt := range []struct {
		name    string
		signal  syscall.Signal
		stopSSH bool
	}{
		{name: "parent EOF"},
		{name: "supervisor SIGTERM", signal: syscall.SIGTERM},
		{name: "supervisor SIGQUIT", signal: syscall.SIGQUIT},
		{name: "stopped SSH cleanup", stopSSH: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			pids := filepath.Join(directory, "pids")
			script := filepath.Join(directory, "proxy")
			content := `#!/bin/sh
trap '' TERM
echo "$$ $PPID" > ` + desktopShellQuote(pids) + `
exec /bin/sleep 30
`
			if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
				t.Fatal(err)
			}
			args := []string{"-F", "/dev/null", "-o", "BatchMode=yes", "-o", "ProxyCommand=exec " + desktopShellQuote(script), "fixture"}
			process, parent, _ := desktopSSHFixture(t, args)
			group := desktopReadPIDs(t, pids, 2)
			if pgid, err := syscall.Getpgid(group[0]); err != nil || pgid != group[1] {
				t.Fatalf("proxy is not owned by SSH's separate group: %d, %v", pgid, err)
			}
			if group[1] == syscall.Getpgrp() {
				t.Fatal("SSH shares the test runner's process group")
			}
			if tt.stopSSH {
				if err := syscall.Kill(group[1], syscall.SIGSTOP); err != nil {
					t.Fatal(err)
				}
			}
			// A separate process is deliberately left running while cleanup signals
			// are sent, so an overly broad kill would be visible.
			unrelated := exec.Command("/bin/sleep", "30")
			if err := unrelated.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() })
			started := time.Now()
			if tt.signal != 0 {
				if err := process.Process.Signal(tt.signal); err != nil {
					t.Fatal(err)
				}
			} else if err := parent.Close(); err != nil {
				t.Fatal(err)
			}
			if err := desktopWaitFixture(process, 5*time.Second); err == nil {
				t.Fatal("interrupted SSH succeeded")
			}
			if time.Since(started) > 4*time.Second {
				t.Fatal("SSH cleanup exceeded its bound")
			}
			for _, pid := range group {
				desktopAssertProcessGone(t, pid)
			}
			if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
				t.Fatalf("SSH cleanup affected unrelated process: %v", err)
			}
		})
	}
}

func TestDesktopSSHNormalExitCleansProxyDescendants(t *testing.T) {
	directory := t.TempDir()
	pids := filepath.Join(directory, "descendant")
	script := filepath.Join(directory, "proxy")
	content := `#!/bin/sh
/bin/sh -c 'trap "" TERM; echo $$ > "$1"; exec /bin/sleep 30' fixture ` + desktopShellQuote(pids) + ` </dev/null >/dev/null 2>&1 &
while [ ! -s ` + desktopShellQuote(pids) + ` ]; do /bin/sleep 0.01; done
exit 0
`
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	process, _, _ := desktopSSHFixture(t, []string{
		"-F", "/dev/null", "-o", "BatchMode=yes", "-o", "ProxyCommand=exec " + desktopShellQuote(script), "fixture",
	})
	pid := desktopReadPIDs(t, pids, 1)[0]
	if err := desktopWaitFixture(process, 5*time.Second); err == nil {
		t.Fatal("SSH with a closed handshake unexpectedly succeeded")
	}
	desktopAssertProcessGone(t, pid)
}

func TestDesktopSSHAskpassEnvironmentIsScoped(t *testing.T) {
	directory := t.TempDir()
	record := filepath.Join(directory, "record")
	script := filepath.Join(directory, "proxy")
	content := `#!/bin/sh
printf '%s\n%s\n%s\n' "$WHIP_DESKTOP_ASKPASS" "$SSH_ASKPASS" "$WHIP_TEST_PRESERVED" > ` + desktopShellQuote(record) + `
exit 0
`
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIP_TEST_PRESERVED", "preserved")
	t.Setenv("SSH_ASKPASS", "/invalid/caller/override")
	process, parent, _ := desktopSSHFixture(t, []string{
		"-F", "/dev/null", "-o", "ProxyCommand=exec " + desktopShellQuote(script), "fixture",
	})
	if _, err := io.WriteString(parent, "not an SSH input stream\n"); err != nil {
		t.Fatal(err)
	}
	if err := desktopWaitFixture(process, 5*time.Second); err == nil {
		t.Fatal("SSH with a closed handshake unexpectedly succeeded")
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "1\n"+executable+"\npreserved\n" {
		t.Fatal("SSH askpass path, marker, or caller environment was changed")
	}
	if os.Getenv("SSH_ASKPASS") != "/invalid/caller/override" {
		t.Fatal("supervisor changed parent environment")
	}
}

func TestDesktopSSHRejectsClosedParentBeforeStarting(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	_ = writer.Close()
	output, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	err = desktopSSH(context.Background(), []string{"-V"}, desktopSSHStreams{
		parent: reader, stdout: output, stderr: output,
	})
	if err == nil {
		t.Fatal("started SSH after parent EOF")
	}
	info, err := output.Stat()
	if err != nil || info.Size() != 0 {
		t.Fatal("SSH wrote output after parent EOF")
	}
}

func desktopSSHFixture(t *testing.T, args []string) (*exec.Cmd, *os.File, string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	output, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = output.Close() })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	process := exec.Command(executable, "-test.run=^TestDesktopSSHSubprocess$")
	process.Env = append(os.Environ(),
		"WHIP_TEST_DESKTOP_SSH=1",
		"WHIP_TEST_DESKTOP_ARGS="+string(encoded),
		// Hidden dispatch must take priority over an inherited askpass marker.
		"WHIP_DESKTOP_ASKPASS=1",
	)
	process.Stdin, process.Stdout, process.Stderr = reader, output, output
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	t.Cleanup(func() { _ = writer.Close(); _ = process.Process.Kill() })
	return process, writer, output.Name()
}

func desktopWaitFixture(command *exec.Cmd, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = command.Process.Kill()
		<-done
		return errors.New("SSH supervisor did not finish")
	}
}

func desktopReadPIDs(t *testing.T, path string, count int) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		file, err := os.Open(path)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Split(bufio.ScanWords)
		pids := make([]int, 0, count)
		for scanner.Scan() {
			pid, err := strconv.Atoi(scanner.Text())
			if err == nil && pid > 1 {
				pids = append(pids, pid)
			}
		}
		_ = file.Close()
		if len(pids) == count {
			return pids
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("SSH proxy fixture did not report its process IDs")
	return nil
}

func desktopAssertProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("SSH left owned process %d behind", pid)
}

func desktopShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
