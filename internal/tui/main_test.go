package tui

import (
	"fmt"
	"os"
	"testing"
)

// Every terminal test owns a disposable client home. Native tests additionally
// supply their own host; no test may read or persist the developer's setup.
func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "whip-test-home")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("WHIPCODE_HOME", directory); err != nil {
		_ = os.RemoveAll(directory)
		panic(err)
	}
	// Pure ANSI tests need a deterministic initial scheme without a real tty.
	SetLightTheme(false)
	code := m.Run()
	if err := os.RemoveAll(directory); err != nil {
		fmt.Fprintln(os.Stderr, "remove terminal test home:", err)
		code = 1
	}
	os.Exit(code)
}
