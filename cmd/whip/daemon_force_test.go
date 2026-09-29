package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/localruntime"
)

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
