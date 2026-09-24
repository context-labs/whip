package mcp

import (
	"os"
	"testing"
)

// Smoke test against a real codex config. Opt-in: it reads the developer's
// ~/.codex/config.toml, so it only runs with WHIP_TEST_REAL_CODEX=1 and never
// prints header values. Reproduces the two startup-report failures from a
// francesco-shaped config: a bogus "incident_io.tools.ask_telemetry" server,
// and an Unauthorized incident_io with no way to express auth.
func TestRealCodexConfigSmoke(t *testing.T) {
	if os.Getenv("WHIP_TEST_REAL_CODEX") != "1" {
		t.Skip("set WHIP_TEST_REAL_CODEX=1 to parse the real ~/.codex/config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	path := home + "/.codex/config.toml"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skip("no codex config:", err)
	}
	cfgs, err := ParseCodex(data)
	if err != nil {
		t.Fatal(err)
	}
	for name := range cfgs {
		if name == "incident_io.tools.ask_telemetry" {
			t.Errorf("tool approval table leaked as server %q", name)
		}
	}
	if c, ok := cfgs["incident_io"]; ok {
		t.Logf("incident_io: url=%q headers=%d note=%q", c.URL, len(c.Headers), c.Note)
	}
}
