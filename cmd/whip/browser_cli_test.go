package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/browser/extrelay"
)

func TestBrowserCLIDispatch(t *testing.T) {
	if err := browserCLI(nil); err == nil {
		t.Error("bare `whip browser` should print usage")
	}
	if err := browserCLI([]string{"bogus"}); err == nil {
		t.Error("unknown subcommand should error")
	}
}

// install writes the unpacked extension and the relay state file into an
// isolated HOME. PATH is emptied so the best-effort open of
// chrome://extensions can never launch anything on the test machine.
func TestBrowserInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir()) // xdg-open/open not found: Start fails silently

	var err error
	out := captureStdout(t, func() { err = browserCLI([]string{"install"}) })
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	dir := extrelay.ExtensionDir(home)
	entries, rerr := os.ReadDir(dir)
	if rerr != nil || len(entries) == 0 {
		t.Fatalf("extension dir not written: %v", rerr)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Errorf("manifest.json missing: %v", err)
	}

	// relay state: valid JSON with a non-empty token, private perms
	statePath := extrelay.RelayStatePath(home)
	info, serr := os.Stat(statePath)
	if serr != nil {
		t.Fatalf("relay state missing: %v", serr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("relay state should be 0600, got %v", info.Mode().Perm())
	}
	data, _ := os.ReadFile(statePath)
	var state struct{ Addr, Token string }
	if err := json.Unmarshal(data, &state); err != nil || state.Token == "" || state.Addr == "" {
		t.Errorf("relay state should carry addr+token: %v %q", err, data)
	}

	// the instructions name the folder the user must load
	if !strings.Contains(out, dir) || !strings.Contains(out, "Load unpacked") {
		t.Errorf("install output should walk through the manual load:\n%s", out)
	}
}

// install can't proceed without a home directory, and reports the write
// failure (rather than a partial install) when the whip dir can't be made.
func TestBrowserInstallHomeErrors(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	t.Setenv("WHIP_HOME", "")
	t.Setenv("HOME", "")
	if err := browserCLI([]string{"install"}); err == nil {
		t.Error("install without a home directory should error")
	}

	file := filepath.Join(t.TempDir(), "home-is-a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIP_HOME", "")
	t.Setenv("HOME", file)
	err := browserCLI([]string{"install"})
	if err == nil || !strings.Contains(err.Error(), "write extension") {
		t.Errorf("an unwritable home should fail on the extension write, got %v", err)
	}
}

// The relay state file is part of the install: if it can't be written, the
// install fails loudly rather than leaving an extension with no token.
func TestBrowserInstallRelayStateError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	// occupy the state path with a directory, which os.WriteFile can't replace
	state := extrelay.RelayStatePath(home)
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}

	var err error
	_ = captureStdout(t, func() { err = browserCLI([]string{"install"}) })
	if err == nil || !strings.Contains(err.Error(), "write relay state") {
		t.Errorf("an unwritable relay state should fail the install, got %v", err)
	}
}
