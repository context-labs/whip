package buildinfo

import (
	"path/filepath"
	"testing"
)

func TestCanonicalIdentity(t *testing.T) {
	if Name != "whipcode" || Env("HOME") != "WHIPCODE_HOME" {
		t.Fatal("unexpected application identity")
	}
	t.Setenv("WHIPCODE_HOME", "")
	t.Setenv("WHIP_HOME", t.TempDir())
	home := t.TempDir()
	if got := Home(home); got != filepath.Join(home, ".whipcode") {
		t.Fatalf("default home = %q", got)
	}
	custom := filepath.Join(home, "custom")
	t.Setenv("WHIPCODE_HOME", custom)
	if got := Home(home); got != custom {
		t.Fatalf("override home = %q", got)
	}
}
