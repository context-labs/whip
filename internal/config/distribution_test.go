package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/buildinfo"
)

func TestDistributionConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WHIP_HOME", "")
	t.Setenv("WHIPCODE_HOME", "")
	want := filepath.Join(home, "."+buildinfo.Name)
	dir, err := Dir()
	if err != nil || dir != want {
		t.Fatalf("Dir = %q, %v; want %q", dir, err, want)
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(want, "config.json")); err != nil {
		t.Fatal(err)
	}
	other := "WHIPCODE_HOME"
	if buildinfo.Name == "whipcode" {
		other = "WHIP_HOME"
	}
	t.Setenv(other, filepath.Join(home, "foreign"))
	if got, err := Dir(); err != nil || got != want {
		t.Fatalf("foreign home override affected Dir: %q %v", got, err)
	}
	custom := filepath.Join(home, "custom")
	t.Setenv(buildinfo.Env("HOME"), custom)
	if got, err := Dir(); err != nil || got != custom {
		t.Fatalf("own home override ignored: %q %v", got, err)
	}
}
