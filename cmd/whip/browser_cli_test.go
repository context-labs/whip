package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/localruntime"
)

func TestBrowserCLIDispatch(t *testing.T) {
	if err := browserCLI(nil); err == nil {
		t.Error("bare `whipcode browser` should print usage")
	}
	if err := browserCLI([]string{"bogus"}); err == nil {
		t.Error("unknown subcommand should error")
	}
}

// install writes only unpacked extension assets into an
// isolated HOME. PATH is emptied so the best-effort open of
// chrome://extensions can never launch anything on the test machine.
func TestBrowserInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIPCODE_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir()) // xdg-open/open cannot launch a browser

	var err error
	out := captureStdout(t, func() { err = browserCLI([]string{"install"}) })
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	dir := filepath.Join(buildinfo.Home(home), localruntime.Namespace, "browser", "extension")
	entries, rerr := os.ReadDir(dir)
	if rerr != nil || len(entries) == 0 {
		t.Fatalf("extension dir not written: %v", rerr)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Errorf("manifest.json missing: %v", err)
	}

	// Installation is inert: the native operation owner publishes relay credentials
	// only after durable dispatch and permission, never a closed placeholder relay.
	if _, err := os.Stat(filepath.Join(dir, "relay.json")); !os.IsNotExist(err) {
		t.Fatal("install minted relay state", err)
	}
	if !strings.Contains(out, "host.json") || !strings.Contains(out, "external_browser.mode") || strings.Contains(out, "config.json") {
		t.Fatal(out)
	}

	// the instructions name the folder the user must load
	if !strings.Contains(out, dir) || !strings.Contains(out, "Load unpacked") {
		t.Errorf("install output should walk through the manual load:\n%s", out)
	}
}

// install can't proceed without a home directory, and reports the write
// failure (rather than a partial install) when the whipcode dir can't be made.
func TestBrowserInstallHomeErrors(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	t.Setenv("WHIPCODE_HOME", "")
	t.Setenv("HOME", "")
	if err := browserCLI([]string{"install"}); err == nil {
		t.Error("install without a home directory should error")
	}

	file := filepath.Join(t.TempDir(), "home-is-a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIPCODE_HOME", "")
	t.Setenv("HOME", file)
	err := browserCLI([]string{"install"})
	if err == nil || !strings.Contains(err.Error(), "write extension") {
		t.Errorf("an unwritable home should fail on the extension write, got %v", err)
	}
}

func TestBrowserInstallPreservesRunningOwnerState(t *testing.T) {
	directory := t.TempDir()
	dir := filepath.Join(directory, "browser", "extension")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "relay.json")
	if err := os.WriteFile(state, []byte("owned active record"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	var err error
	_ = captureStdout(t, func() { err = browserCLI([]string{"install", "--directory", directory}) })
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(state)
	if err != nil || string(data) != "owned active record" {
		t.Fatal(string(data), err)
	}
	if err := browserCLI([]string{"install", "--directory", "relative"}); err == nil {
		t.Fatal("relative runtime install")
	}
}
