//go:build unix

package daemonconn

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLaunchDaemonProcessUsesOwnerOnlyLog(t *testing.T) {
	paths, err := Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := launchDaemonProcess(paths, "/usr/bin/true"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(paths.Home, "daemon.log"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("daemon log mode = %v, %v", info, err)
	}
}

func TestSelfLaunchAndRestartUseCurrentExecutable(t *testing.T) {
	previousExecutable, previousReplace := selfExecutable, replaceProcess
	selfExecutable = func() (string, error) { return "/usr/bin/true", nil }
	var replaced bool

	replaceProcess = func(path string, args, _ []string) error {
		replaced = path == "/usr/bin/true" && len(args) == 2 && args[1] == "_daemon"
		return errors.New("exec stopped for test")
	}
	t.Cleanup(func() { selfExecutable, replaceProcess = previousExecutable, previousReplace })
	paths, err := Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := LaunchSelfDaemon(paths); err != nil {
		t.Fatal(err)
	}
	if err := RestartSelfDaemon(); err == nil || !replaced {
		t.Fatalf("restart replacement = %v, called=%t", err, replaced)
	}
}

func TestSelfLaunchAndRestartReportExecutableFailures(t *testing.T) {
	previousExecutable := selfExecutable
	want := errors.New("executable unavailable")
	selfExecutable = func() (string, error) { return "", want }
	t.Cleanup(func() { selfExecutable = previousExecutable })
	paths, err := Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := LaunchSelfDaemon(paths); !errors.Is(err, want) {
		t.Fatalf("launch executable error = %v", err)
	}
	if err := RestartSelfDaemon(); !errors.Is(err, want) {
		t.Fatalf("restart executable error = %v", err)
	}
	if err := launchDaemonProcess(paths, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing daemon executable launched")
	}
}
