package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps discovery tests away from the developer's real OpenCode
// files. Codex and Claude paths are stubbed per test because their fixtures
// differ; OpenCode tests that want fixtures override OpenCodePaths themselves.
func TestMain(m *testing.M) {
	OpenCodePaths = func() []string { return nil }
	os.Exit(m.Run())
}

// stubSources points every discovery source at fixture paths for one test.
func stubSources(t *testing.T, codex, claude string, opencode ...string) {
	t.Helper()
	origC, origG, origOC := CodexPath, ClaudeGlobalPath, OpenCodePaths
	CodexPath = func() string { return codex }
	ClaudeGlobalPath = func() string { return claude }
	OpenCodePaths = func() []string { return opencode }
	t.Cleanup(func() { CodexPath, ClaudeGlobalPath, OpenCodePaths = origC, origG, origOC })
}

// writeFile writes one fixture file and returns its path.
func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
