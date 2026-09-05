package tui

import (
	"strings"
	"testing"
)

// A `!` shell escape whose output is binary must land in the conversation as
// the placeholder, not raw bytes — same gate as the bash tool.
func TestShellEscapeBinaryOutputGated(t *testing.T) {
	// Generate binary output portably (printf emits raw NUL bytes) — no
	// dependence on a platform-specific binary path.
	out := shellExec("printf '\\x00\\x01\\x02\\x03\\x04\\x05'")
	if !strings.Contains(out, "[binary output:") {
		t.Errorf("binary shell output was not gated: %q", out[:min(len(out), 80)])
	}
}

// A normal `!` command still passes through unchanged.
func TestShellEscapeTextOutputPasses(t *testing.T) {
	out := shellExec("echo hello")
	if !strings.Contains(out, "hello") {
		t.Errorf("text shell output was gated: %q", out)
	}
}
