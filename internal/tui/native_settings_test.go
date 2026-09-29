package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestNativePreferencesPreservePresentationWithoutHostConfiguration(t *testing.T) {
	directory := t.TempDir()
	host := []byte(`{"fixture_host_credential":"host-only-value"}`)
	if err := os.WriteFile(filepath.Join(directory, "host.json"), host, 0o600); err != nil {
		t.Fatal(err)
	}
	initial := nativePreferences{Theme: "dark", Sidebar: new(false), Mouse: new(true), Panel: "repl"}
	if err := saveNativePreferences(directory, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := readNativePreferences(directory)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Theme = "light"
	if err := saveNativePreferences(directory, loaded); err != nil {
		t.Fatal(err)
	}
	reopened, err := readNativePreferences(directory)
	if err != nil || reopened.Theme != "light" || reopened.Sidebar == nil || *reopened.Sidebar ||
		reopened.Mouse == nil || !*reopened.Mouse || reopened.Panel != "repl" {
		t.Fatalf("reopened presentation: %+v, %v", reopened, err)
	}
	actual, err := os.ReadFile(filepath.Join(directory, "host.json"))
	if err != nil || !bytes.Equal(actual, host) {
		t.Fatalf("client changed host settings: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(directory, nativePreferencesFile))
	if err != nil || bytes.Contains(data, []byte("host-only-value")) {
		t.Fatalf("host credentials entered client settings: %v", err)
	}
	info, err := os.Stat(filepath.Join(directory, nativePreferencesFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("preference permissions: %v, %v", info, err)
	}
}

func TestNativePreferencesRequireExplicitScopeAndBoundedRegularFile(t *testing.T) {
	if _, err := readNativePreferences(""); err == nil {
		t.Fatal("discovered a client directory implicitly")
	}
	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "oversized", data: bytes.Repeat([]byte(" "), (16<<10)+1)},
		{name: "unknown fields", data: []byte(`{"credential":"not-a-client-preference"}`)},
		{name: "trailing record", data: []byte(`{} {}`)},
		{name: "invalid theme", data: []byte(`{"theme":"\u001b[31m"}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, nativePreferencesFile), test.data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readNativePreferences(directory); err == nil {
				t.Fatal("accepted invalid client preferences")
			}
		})
	}
	directory, outside := t.TempDir(), filepath.Join(t.TempDir(), "unrelated.json")
	if err := os.WriteFile(outside, []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, nativePreferencesFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativePreferences(directory); err == nil {
		t.Fatal("read a linked preferences file")
	}
	if err := saveNativePreferences(directory, nativePreferences{Theme: "light"}); err == nil {
		t.Fatal("replaced a linked preferences file")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != `{"theme":"dark"}` {
		t.Fatalf("changed unrelated preferences: %s, %v", data, err)
	}
}
