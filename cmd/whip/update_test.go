package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubShell puts a fake `sh` first (and only) in PATH so updateCLI can never
// reach the network or run the real installer. The stub records its args and
// exits with the given code.
func stubShell(t *testing.T, exitCode string) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\nexit " + exitCode + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return argsFile
}

func TestUpdateCLIRunsInstaller(t *testing.T) {
	argsFile := stubShell(t, "0")
	previousRestart := restartDaemonAfterUpdate
	restarted := false
	restartDaemonAfterUpdate = func() error { restarted = true; return nil }
	t.Cleanup(func() { restartDaemonAfterUpdate = previousRestart })

	var err error
	out := captureStdout(t, func() { err = updateCLI() })
	if err != nil {
		t.Fatalf("update with a succeeding installer: %v", err)
	}
	if !strings.Contains(out, "whip updated") {
		t.Errorf("success message missing:\n%s", out)
	}
	if !restarted {
		t.Fatal("successful update did not request daemon restart")
	}
	args, rerr := os.ReadFile(argsFile)
	if rerr != nil {
		t.Fatalf("stub sh never ran: %v", rerr)
	}
	if !strings.Contains(string(args), installURL) {
		t.Errorf("installer command should pipe %s, got %q", installURL, args)
	}
}

func TestUpdateCLIInstallerFails(t *testing.T) {
	stubShell(t, "3")

	var err error
	out := captureStdout(t, func() { err = updateCLI() })
	if err == nil || !strings.Contains(err.Error(), "update failed") {
		t.Fatalf("a failing installer should surface as an update error, got %v", err)
	}
	if strings.Contains(out, "whip updated") {
		t.Errorf("failure must not claim success:\n%s", out)
	}
}
