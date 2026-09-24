package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestClientPreferencesCannotOverwriteFreshHostCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	stale, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := UpdateVersioned("", func(c *Config) error { c.UpsertOpenRouter("new-host-secret", false); return nil })
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	stale.Theme = "light"
	if err := stale.SavePreferences(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("client preference save changed runtime configuration")
	}
	current, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if current.Theme != "light" || current.Providers["openrouter"].APIKey != "new-host-secret" {
		t.Fatal("preferences or host credentials were lost")
	}
	_, next, err := ReadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	if revision != next {
		t.Fatal("client preference changed runtime revision")
	}
	preferences, err := os.ReadFile(filepath.Join(home, ClientPreferencesFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(preferences, []byte("secret")) {
		t.Fatal("credentials leaked into client preferences")
	}
}
