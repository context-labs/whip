package tui

import (
	"os"
	"testing"
)

// TestMain is a safety net: several TUI code paths persist through
// config.Save() (setEffort, switchModel, compactCommand, /mouse). Without
// isolation those writes land in the REAL ~/.whip/config.json — this exact
// bug corrupted the config twice. Point the whole test binary at a scratch
// WHIP_HOME so even a future test that forgets t.Setenv cannot clobber the
// user's setup. Per-test overrides still apply on top.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "whip-test-home")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	os.Setenv("WHIP_HOME", dir)
	// Hook discovery reads the platform home directory, not WHIP_HOME. Keep
	// tests from loading or executing a developer's real ~/.agents plugins.
	os.Setenv("HOME", dir)
	// Style tests assert ANSI-dark markdown output; pin a known dark theme so a
	// test-binary with no tty (where detection reports unknown → neutral) still
	// renders the dark style they expect. Per-test overrides (SetLightTheme,
	// SetUnknownTheme) apply on top.
	SetLightTheme(false)
	os.Exit(m.Run())
}
