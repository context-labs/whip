package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var engineWorkerDirectory string

func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "whip-runtime-worker-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	engineWorkerDirectory = directory
	code := m.Run()
	if err := os.RemoveAll(directory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

// The host suite remains race-instrumented. Its workers use the shipping binary:
// ThreadSanitizer's RSS overhead can exceed the worker's production memory cap.
// Engine/process tests separately exercise instrumented workers and exhaustion.
var buildEngineWorker = sync.OnceValues(func() (string, error) {
	executable := filepath.Join(engineWorkerDirectory, "whip-runtime")
	command := exec.CommandContext(context.Background(), "go", "build", "-race=false", "-o", executable, "./cmd/whip-runtime")
	command.Dir = "../.."
	if output, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build runtime worker: %w\n%s", err, output)
	}
	return executable, nil
})
