package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/buildinfo"
)

func TestDistributionUpdateDownloadFailureKeepsNoticeAndDaemon(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv(buildinfo.Env("HOME"), home)
	notice := filepath.Join(home, "update.json")
	original := []byte(`{"latest":"whipcode-v0.0.99","acknowledged":false}`)
	if err := os.WriteFile(notice, original, 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "installer-executed")
	t.Setenv("WHIP_TEST_INSTALLER_MARKER", marker)
	// Even if curl writes executable content before failing, it must never run.
	script := `#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then
    shift
    printf 'touch "$WHIP_TEST_INSTALLER_MARKER"\n' > "$1"
    exit 22
  fi
  shift
done
exit 23
`
	if err := os.WriteFile(filepath.Join(bin, "curl"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	previous := restartDaemonAfterUpdate
	restarts := 0
	restartDaemonAfterUpdate = func() error { restarts++; return nil }
	t.Cleanup(func() { restartDaemonAfterUpdate = previous })
	var updateErr error
	output := captureStdout(t, func() { updateErr = updateCLI() })
	if updateErr == nil || strings.Contains(output, "updated —") {
		t.Fatalf("download failure reported success: %q, %v", output, updateErr)
	}
	if restarts != 0 {
		t.Fatalf("failed installation restarted daemon %d times", restarts)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("failed download executed installer: %v", err)
	}
	if after, err := os.ReadFile(notice); err != nil || string(after) != string(original) {
		t.Fatalf("failed update acknowledged notice: %q, %v", after, err)
	}
}
