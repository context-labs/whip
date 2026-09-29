package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/localruntime"
)

func TestManagedOwnerProcess(t *testing.T) {
	path := os.Getenv("WHIP_TEST_MANAGED_OWNER")
	if path == "" {
		return
	}
	termination := make(chan os.Signal, 1)
	signal.Notify(termination, syscall.SIGTERM)
	defer signal.Stop(termination)
	owner, err := daemon.AcquireOwner(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	for range termination {
		if os.Getenv("WHIP_TEST_IGNORE_TERM") != "1" {
			return
		}
	}
}

func TestForcedDaemonStopOnlySignalsItsRecordedOwner(t *testing.T) {
	for _, ignoreTERM := range []bool{false, true} {
		t.Run(map[bool]string{false: "graceful", true: "kill fallback"}[ignoreTERM], func(t *testing.T) {
			paths, err := daemon.Paths(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestManagedOwnerProcess$")
			cmd.Env = append(os.Environ(), "WHIP_TEST_MANAGED_OWNER="+paths.Lock)
			if ignoreTERM {
				cmd.Env = append(cmd.Env, "WHIP_TEST_IGNORE_TERM=1")
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
			deadline := time.Now().Add(5 * time.Second)
			for {
				pid, owned, err := daemon.ActiveOwnerPID(paths.Lock)
				if err == nil && owned && pid == cmd.Process.Pid {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("fixture did not acquire its lock: %d %t %v", pid, owned, err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := forceStopManagedDaemon(paths, 200*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if pid, owned, err := daemon.ActiveOwnerPID(paths.Lock); err != nil || owned {
				t.Fatalf("owner remains after stop: %d %t %v", pid, owned, err)
			}
		})
	}
}

func TestForcedDaemonStopRefusesAmbiguousOwnership(t *testing.T) {
	paths, err := daemon.Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := forceStopManagedDaemon(paths, time.Second); err != nil {
		t.Fatalf("absent owner: %v", err)
	}
	owner, err := daemon.AcquireOwner(paths.Lock)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := forceStopManagedDaemon(paths, time.Millisecond); err == nil || !strings.Contains(err.Error(), "current process") {
		t.Fatalf("must refuse to signal this test process: %v", err)
	}
	if stopped, err := stopManagedDaemon(paths, time.Millisecond, false); stopped || err == nil || !strings.Contains(err.Error(), "unhealthy") {
		t.Fatalf("unresponsive owner was treated as stopped: %t %v", stopped, err)
	}
	if err := waitForDaemonStop(paths, time.Millisecond); err == nil {
		t.Fatal("live owner did not enforce wait timeout")
	}
}

func TestDaemonCommandsSurfaceUnavailableHomeAndLaunchErrors(t *testing.T) {
	file := filepath.Join(nativeDaemonHome(t), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIPCODE_HOME", filepath.Join(file, "home"))
	for _, command := range []string{"start", "stop", "restart", "logs"} {
		if err := daemonManageCLI([]string{command}); err == nil {
			t.Fatalf("%s accepted unavailable home", command)
		}
	}
	output := captureDaemonOutput(t, func() error { return daemonStatusCLI([]string{"--json"}) })
	var status nativeDaemonStatus
	if err := json.Unmarshal([]byte(output), &status); err != nil || status.State != "unhealthy" {
		t.Fatal(output, err)
	}
	nativeDaemonHome(t)
	previous := launchNativeRuntime
	t.Cleanup(func() { launchNativeRuntime = previous })
	launchErr := errors.New("fixture launch refused")
	launchNativeRuntime = func(context.Context, localruntime.Paths, localruntime.Launch) (localruntime.Status, error) {
		return localruntime.Status{}, launchErr
	}
	for _, command := range []string{"start", "restart"} {
		if err := daemonManageCLI([]string{command}); !errors.Is(err, launchErr) {
			t.Fatalf("%s lost launch error: %v", command, err)
		}
	}
	if err := daemonLogsCLI(nil); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing log was not reported: %v", err)
	}
}
