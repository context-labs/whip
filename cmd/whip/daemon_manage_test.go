package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/daemon"
)

func TestDaemonStatusDoesNotInitializeHome(t *testing.T) {
	for _, distribution := range []string{"whip", "whipcode"} {
		for _, override := range []bool{false, true} {
			name := distribution + "/default home"
			if override {
				name = distribution + "/home override"
			}
			t.Run(name, func(t *testing.T) {
				previous := buildinfo.Name
				buildinfo.Name = distribution
				t.Cleanup(func() { buildinfo.Name = previous })
				userHome := filepath.Join(t.TempDir(), "absent-user-home")
				t.Setenv("HOME", userHome)
				t.Setenv("WHIP_HOME", "")
				t.Setenv("WHIPCODE_HOME", "")
				home := filepath.Join(userHome, "."+distribution)
				if override {
					home = filepath.Join(userHome, "custom-home")
					t.Setenv(buildinfo.Env("HOME"), home)
				}
				paths, err := daemon.ResolvePaths(home)
				if err != nil {
					t.Fatal(err)
				}
				output := invokeMain(t, "daemon", "status", "--json")
				var status daemonStatus
				if err := json.Unmarshal([]byte(output), &status); err != nil || status.State != "stopped" || status.Error != "" || status.Socket != paths.Socket {
					t.Fatalf("absent home status = %q, %v", output, err)
				}
				for _, path := range []string{userHome, home, paths.Home, paths.Runtime, paths.Lock} {
					if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("status touched %s: %v", path, err)
					}
				}
			})
		}
	}
}

func TestDaemonStatusPreservesExistingRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv(buildinfo.Env("HOME"), home)
	paths, err := daemon.Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Runtime != paths.Home {
		t.Cleanup(func() { _ = os.RemoveAll(paths.Runtime) })
	}
	for _, dir := range []string{home, paths.Home, paths.Runtime} {
		if err := os.Chmod(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	output := captureDaemonOutput(t, func() error { return daemonStatusCLI([]string{"--json"}) })
	var status daemonStatus
	if err := json.Unmarshal([]byte(output), &status); err != nil || status.State != "stopped" {
		t.Fatalf("empty runtime status = %q, %v", output, err)
	}
	for _, dir := range []string{home, paths.Home, paths.Runtime} {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0o750 {
			t.Fatalf("status changed permissions on %s: %v, %v", dir, info, err)
		}
	}
	for _, dir := range []string{paths.Home, paths.Runtime} {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("status wrote runtime files in %s: %v, %v", dir, entries, err)
		}
	}
}

func TestDaemonStatusIdentifiesOnlyUnownedStaleSocket(t *testing.T) {
	paths, err := daemon.Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: paths.Socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(paths.Socket) })
	if err := os.Chmod(paths.Socket, 0o600); err != nil {
		t.Fatal(err)
	}
	status, client := probeDaemon(paths, time.Second)
	if client != nil || status.State != "unhealthy" || !status.StaleSocket {
		t.Fatalf("unowned stale socket = %+v", status)
	}
	if _, err := os.Stat(paths.Lock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale socket inspection created an owner lock: %v", err)
	}
	owner, err := daemon.AcquireOwner(paths.Lock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close() }()
	status, client = probeDaemon(paths, time.Second)
	if client != nil || status.State != "unhealthy" || status.StaleSocket || status.PID != os.Getpid() {
		t.Fatalf("owned unhealthy socket must not be recovered: %+v", status)
	}
}

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
	for _, dir := range []string{paths.Home, paths.Runtime} {
		if err := os.Chmod(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	output = captureDaemonOutput(t, func() error { return daemonStatusCLI([]string{"--json"}) })
	if err := json.Unmarshal([]byte(output), &status); err != nil || status.State != "running" || status.PID != os.Getpid() || !status.BuildMatch {
		t.Fatalf("running status = %q, %+v, %v", output, status, err)
	}
	for _, dir := range []string{paths.Home, paths.Runtime} {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0o750 {
			t.Fatalf("running status changed permissions on %s: %v, %v", dir, info, err)
		}
	}
	output = captureDaemonOutput(t, func() error { return daemonStatusCLI(nil) })
	for _, want := range []string{"state:         running", "build match:   true", "uptime:", "socket:", "database:"} {
		if !strings.Contains(output, want) {
			t.Fatalf("human status omits %q: %s", want, output)
		}
	}
	output = captureDaemonOutput(t, func() error { return daemonStartCLI(nil) })
	if !strings.Contains(output, "already running") || len(daemonRuns) != 1 {
		t.Fatalf("repeated start launched a second daemon: %q, %d", output, len(daemonRuns))
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
