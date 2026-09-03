package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/daemon"
)

func TestDaemonManagementLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	paths, err := daemon.Paths(home)
	if err != nil {
		t.Fatal(err)
	}

	previousLaunch := launchManagedDaemon
	previousTail := tailDaemonLog
	var daemonRuns []chan error
	launchManagedDaemon = func(daemon.RuntimePaths) error {
		done := make(chan error, 1)
		daemonRuns = append(daemonRuns, done)
		go func() { done <- runDaemon(t.Context(), nil) }()
		return nil
	}
	tailDaemonLog = func(path string, lines int, follow bool) error {
		if path != filepath.Join(paths.Home, "daemon.log") || lines != 12 || !follow {
			t.Errorf("tail args = %q, %d, %t", path, lines, follow)
		}
		return nil
	}
	t.Cleanup(func() {
		launchManagedDaemon = previousLaunch
		tailDaemonLog = previousTail
		_, _ = stopManagedDaemon(paths, time.Second, false)
	})

	output := captureDaemonOutput(t, func() error { return daemonStatusCLI([]string{"--json"}) })
	var status daemonStatus
	if err := json.Unmarshal([]byte(output), &status); err != nil || status.State != "stopped" {
		t.Fatalf("initial status = %q, %+v, %v", output, status, err)
	}

	output = captureDaemonOutput(t, func() error { return daemonStartCLI(nil) })
	if !strings.Contains(output, "daemon started") || len(daemonRuns) != 1 {
		t.Fatalf("start output = %q, launches = %d", output, len(daemonRuns))
	}
	output = captureDaemonOutput(t, func() error { return daemonStatusCLI([]string{"--json"}) })
	if err := json.Unmarshal([]byte(output), &status); err != nil || status.State != "running" || status.PID != os.Getpid() || !status.BuildMatch {
		t.Fatalf("running status = %q, %+v, %v", output, status, err)
	}

	output = captureDaemonOutput(t, func() error { return daemonRestartCLI([]string{"--timeout", "5s"}) })
	if !strings.Contains(output, "daemon restarted") || len(daemonRuns) != 2 {
		t.Fatalf("restart output = %q, launches = %d", output, len(daemonRuns))
	}
	output = captureDaemonOutput(t, func() error { return daemonStopCLI([]string{"--timeout", "5s"}) })
	if strings.TrimSpace(output) != "daemon stopped" {
		t.Fatalf("stop output = %q", output)
	}
	for index, done := range daemonRuns {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("daemon run %d = %v", index, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("daemon run %d did not finish", index)
		}
	}

	if err := os.WriteFile(filepath.Join(paths.Home, "daemon.log"), []byte("test log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := daemonLogsCLI([]string{"-f", "-n", "12"}); err != nil {
		t.Fatal(err)
	}
	output = captureDaemonOutput(t, func() error { return daemonStopCLI(nil) })
	if strings.TrimSpace(output) != "daemon already stopped" {
		t.Fatalf("second stop output = %q", output)
	}
}

func TestDaemonManagementRejectsInvalidCommands(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	for _, args := range [][]string{
		nil,
		{"unknown"},
		{"status", "extra"},
		{"start", "extra"},
		{"stop", "--timeout", "0s"},
		{"restart", "extra"},
		{"logs", "-n", "0"},
	} {
		if err := daemonManageCLI(args); err == nil {
			t.Fatalf("daemon command accepted %#v", args)
		}
	}
}

func captureDaemonOutput(t *testing.T, action func() error) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = write
	var output bytes.Buffer
	copied := make(chan error, 1)
	go func() {
		_, err := io.Copy(&output, read)
		copied <- err
	}()
	actionErr := action()
	os.Stdout = previous
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-copied; err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	if actionErr != nil {
		t.Fatal(actionErr)
	}
	return output.String()
}
