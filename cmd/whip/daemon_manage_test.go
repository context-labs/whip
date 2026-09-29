package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/hostcmd"
	"github.com/context-labs/whip/internal/localruntime"
	"golang.org/x/sys/unix"
)

func nativeDaemonHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "whip-manage-") //nolint:usetesting // Unix socket paths must fit macOS.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("WHIPCODE_HOME", home)
	return home
}

func TestDaemonStatusDoesNotInitializeHome(t *testing.T) {
	base := nativeDaemonHome(t)
	home := filepath.Join(base, "absent")
	t.Setenv("WHIPCODE_HOME", home)
	output := invokeMain(t, "daemon", "status", "--json")
	var status nativeDaemonStatus
	if err := json.Unmarshal([]byte(output), &status); err != nil || status.State != "stopped" || status.Process != nil || status.Error != "" {
		t.Fatal(output, err)
	}
	if !strings.Contains(status.Socket, "/runtime-v4/") {
		t.Fatal(status)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("status initialized home", err)
	}
}

func TestDaemonStatusPreservesExistingRuntime(t *testing.T) {
	home := nativeDaemonHome(t)
	sentinel := filepath.Join(home, "config.json")
	if err := os.WriteFile(sentinel, []byte("retired configuration"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.Directory, 0o750); err != nil {
		t.Fatal(err)
	}
	output := captureDaemonOutput(t, func() error { return daemonStatusCLI([]string{"--json"}) })
	var status nativeDaemonStatus
	if err := json.Unmarshal([]byte(output), &status); err != nil || status.State != "unhealthy" || status.Process != nil {
		t.Fatal(output, err)
	}
	info, err := os.Stat(paths.Directory)
	if err != nil || info.Mode().Perm() != 0o750 {
		t.Fatal(info, err)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "retired configuration" {
		t.Fatal(string(data), err)
	}
	if entries, err := os.ReadDir(paths.Directory); err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

func TestDaemonStatusIdentifiesOnlyUnownedStaleSocket(t *testing.T) {
	nativeDaemonHome(t)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.Directory, 0o700); err != nil {
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
	if err := os.Chmod(paths.Socket, 0o600); err != nil {
		t.Fatal(err)
	}
	if status := localruntime.Inspect(t.Context(), paths); status.State != "stopped" || status.Process != nil {
		t.Fatal(status)
	}
	if _, err := os.Stat(paths.Lock); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection created lock", err)
	}
	lock, err := os.OpenFile(paths.Lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	if status := localruntime.Inspect(t.Context(), paths); status.State != "unhealthy" || status.Process != nil {
		t.Fatal(status)
	}
	if err := daemonStopCLI([]string{"--force", "--timeout", "100ms"}); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatal("unverified owner force stop accepted", err)
	}
}

func TestDaemonManagementLifecycle(t *testing.T) {
	nativeDaemonHome(t)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	previous := launchNativeRuntime
	var runs []chan error
	var stops []context.CancelFunc
	launchNativeRuntime = func(ctx context.Context, p localruntime.Paths, l localruntime.Launch) (localruntime.Status, error) {
		if status := localruntime.Inspect(ctx, p); status.Process != nil {
			return status, nil
		}
		owned, cancel := context.WithCancel(context.Background())
		stops = append(stops, cancel)
		done := make(chan error, 1)
		runs = append(runs, done)
		go func() {
			done <- hostcmd.Run(owned, []string{"-directory", p.Directory, "-scripted", "-build", l.Build}, io.Discard, io.Discard)
		}()
		wait, cancelWait := context.WithTimeout(ctx, 5*time.Second)
		defer cancelWait()
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			status := localruntime.Inspect(wait, p)
			if status.Process != nil {
				return status, nil
			}
			select {
			case <-wait.Done():
				return status, wait.Err()
			case <-ticker.C:
			}
		}
	}
	t.Cleanup(func() {
		launchNativeRuntime = previous
		for _, stop := range stops {
			stop()
		}
		for _, done := range runs {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(5 * time.Second):
				t.Error("host did not join")
			}
		}
	})
	if output := captureDaemonOutput(t, func() error { return daemonStartCLI(nil) }); !strings.Contains(output, "daemon started") {
		t.Fatal(output)
	}
	first := localruntime.Inspect(t.Context(), paths)
	output := captureDaemonOutput(t, func() error { return daemonStatusCLI([]string{"--json"}) })
	var status nativeDaemonStatus
	if err := json.Unmarshal([]byte(output), &status); err != nil || status.Process == nil || status.Process.RuntimeID != first.Process.RuntimeID || status.Process.ProcessEpoch != first.Process.ProcessEpoch || status.ClientBuild != version {
		t.Fatal(output, err)
	}
	if strings.Contains(output, `"generation"`) {
		t.Fatal("fabricated generation", output)
	}
	output = captureDaemonOutput(t, func() error { return daemonStatusCLI(nil) })
	for _, part := range []string{"state:         running", "epoch:", "runtime:", "build match:   true", "uptime:", "directory:"} {
		if !strings.Contains(output, part) {
			t.Fatal(part, output)
		}
	}
	if output := captureDaemonOutput(t, func() error { return daemonStartCLI(nil) }); !strings.Contains(output, "already running") || len(runs) != 1 {
		t.Fatal(output, len(runs))
	}
	if output := captureDaemonOutput(t, func() error { return daemonRestartCLI([]string{"--timeout", "5s"}) }); !strings.Contains(output, "daemon restarted") || len(runs) != 2 {
		t.Fatal(output, len(runs))
	}
	next := localruntime.Inspect(t.Context(), paths)
	if next.Process == nil || next.Process.RuntimeID != first.Process.RuntimeID || next.Process.ProcessEpoch == first.Process.ProcessEpoch {
		t.Fatal("restart identity", first, next)
	}
	if output := captureDaemonOutput(t, func() error { return daemonStopCLI([]string{"--force"}) }); strings.TrimSpace(output) != "daemon stopped" {
		t.Fatal(output)
	}
	if output := captureDaemonOutput(t, func() error { return daemonStopCLI(nil) }); strings.TrimSpace(output) != "daemon already stopped" {
		t.Fatal(output)
	}
}

func TestDaemonLogsSelectNativeOwnedDescriptor(t *testing.T) {
	home := nativeDaemonHome(t)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.Directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Log, []byte("native log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "web.log"), []byte("retired gateway"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := tailDaemonLog
	t.Cleanup(func() { tailDaemonLog = previous })
	calls := 0
	tailDaemonLog = func(file *os.File, lines int, follow bool) error {
		calls++
		if file.Name() != paths.Log || lines != 12 || !follow {
			t.Fatal(file.Name(), lines, follow)
		}
		data, err := io.ReadAll(file)
		if err != nil || string(data) != "native log\n" {
			t.Fatal(string(data), err)
		}
		return nil
	}
	if err := daemonLogsCLI([]string{"--web", "-f", "-n", "12"}); err != nil || calls != 1 {
		t.Fatal(calls, err)
	}
	if err := os.Remove(paths.Log); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "web.log"), paths.Log); err != nil {
		t.Fatal(err)
	}
	if err := daemonLogsCLI(nil); err == nil || calls != 1 {
		t.Fatal("log symlink accepted", err)
	}
	if err := os.Remove(paths.Log); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Log, []byte("native log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(paths.Directory, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := daemonLogsCLI(nil); err == nil || calls != 1 {
		t.Fatal("nonprivate log directory accepted", err)
	}
	if err := os.Chmod(paths.Directory, 0o700); err != nil {
		t.Fatal(err)
	}
	relocated := filepath.Join(home, "relocated")
	if err := os.Rename(paths.Directory, relocated); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relocated, paths.Directory); err != nil {
		t.Fatal(err)
	}
	if err := daemonLogsCLI(nil); err == nil || calls != 1 {
		t.Fatal("log directory symlink accepted", err)
	}
}

func TestDaemonManagementRejectsInvalidCommands(t *testing.T) {
	nativeDaemonHome(t)
	for _, args := range [][]string{nil, {"unknown"}, {"status", "extra"}, {"start", "extra"}, {"stop", "--timeout", "0s"}, {"restart", "--timeout", "1m"}, {"restart", "extra"}, {"logs", "-n", "0"}, {"logs", "-n", "10001"}} {
		if err := daemonManageCLI(args); err == nil {
			t.Fatal(args)
		}
	}
}

func TestNativeRuntimeLaunchRetainsExplicitNetworkPolicy(t *testing.T) {
	for name, value := range map[string]string{"WHIPCODE_NETWORK": "true", "WHIPCODE_NETWORK_TERMINALS": "true", "WHIPCODE_LISTEN": "127.0.0.1:0", "WHIPCODE_ALLOWED_HOSTS": "localhost,127.0.0.1", "WHIPCODE_ALLOWED_ORIGINS": "http://localhost"} {
		t.Setenv(name, value)
	}
	launch, err := nativeRuntimeLaunch()
	if err != nil || !launch.WaitForWeb {
		t.Fatal(launch, err)
	}
	if args := strings.Join(launch.Arguments, " "); args != "_native-runtime -web -web-listen 127.0.0.1:0 -web-hosts localhost,127.0.0.1 -web-origins http://localhost -web-terminals" {
		t.Fatal(args)
	}
	t.Setenv("WHIPCODE_NETWORK", "false")
	launch, err = nativeRuntimeLaunch()
	if err != nil || len(launch.Arguments) != 1 || launch.WaitForWeb {
		t.Fatal(launch, err)
	}
	t.Setenv("WHIPCODE_NETWORK_TERMINALS", "invalid")
	if _, err := nativeRuntimeLaunch(); err == nil {
		t.Fatal("invalid network flag accepted")
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
