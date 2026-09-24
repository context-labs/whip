package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Model requests in tests must not depend on, or expose, the developer's
// standing instructions or installed skills. Tests may override these fixtures.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "whip-daemon-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err = os.Setenv("HOME", home); err == nil {
		err = os.Setenv("WHIPCODE_HOME", filepath.Join(home, ".whipcode"))
	}
	if err == nil {
		err = os.Unsetenv("XDG_CONFIG_HOME") // OpenCode discovery would read it before HOME
	}
	if err != nil {
		os.RemoveAll(home)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
