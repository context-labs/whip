package mcp

import (
	"os"
	"testing"
)

// TestMain keeps discovery tests away from the developer's real OpenCode
// files. Codex and Claude paths are stubbed per test because their fixtures
// differ; OpenCode tests that want fixtures override OpenCodePaths themselves.
func TestMain(m *testing.M) {
	OpenCodePaths = func() []string { return nil }
	os.Exit(m.Run())
}
