package buildinfo

import (
	"path/filepath"
	"testing"
)

func TestDistributionIdentity(t *testing.T) {
	original := Name
	t.Cleanup(func() { Name = original })
	for _, name := range []string{"whip", "whipcode"} {
		t.Run(name, func(t *testing.T) {
			Name = name
			t.Setenv("WHIP_HOME", "")
			t.Setenv("WHIPCODE_HOME", "")
			home := t.TempDir()
			if got := Home(home); got != filepath.Join(home, "."+name) {
				t.Fatalf("default home = %q", got)
			}
			override := filepath.Join(home, "custom")
			t.Setenv(Env("HOME"), override)
			if got := Home(home); got != override {
				t.Fatalf("override home = %q", got)
			}
			if got := Text("whip --resume id: ~/.whip/config.json WHIP_HOME WHIP_NETWORK"); got != name+" --resume id: ~/."+name+"/config.json "+Env("HOME")+" "+Env("NETWORK") {
				t.Fatalf("instructions = %q", got)
			}
			if got := Text("whip-computer github.com/context-labs/whip whip.method"); got != "whip-computer github.com/context-labs/whip whip.method" {
				t.Fatalf("internal identity changed: %q", got)
			}
			if got := Text("run `whip`, then /model"); got != "run `"+name+"`, then /model" {
				t.Fatalf("standalone invocation = %q", got)
			}
		})
	}
}

func TestDistributionVersion(t *testing.T) {
	for _, tag := range []string{"dev", "v1.2.3", "whipcode-v0.0.12"} {
		want := tag
		if tag == "whipcode-v0.0.12" {
			want = "v0.0.12"
		}
		if got := Version(tag); got != want {
			t.Fatalf("Version(%q) = %q", tag, got)
		}
	}
}
