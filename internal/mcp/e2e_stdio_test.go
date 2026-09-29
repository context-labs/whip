package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestStdioServerEndToEnd runs the full production path against a REAL stdio
// subprocess (the self-served whip), exercising CommandTransport, the
// owned process spawn, explicit disposable environment, and stderr on failure.
// Gated on WHIP_TEST_SELFHOST since it builds the binary.
func TestStdioServerEndToEnd(t *testing.T) {
	ctx, m, workspace := newSelfHostManager(t)
	out, err := testCall(ctx, m, "self", "read", json.RawMessage(`{"path":"fixture.txt","limit":3}`))
	if err != nil || !strings.Contains(out.Text, "selfhost-ok") {
		t.Fatalf("read via MCP = %+v, %v", out, err)
	}

	m.Close() // must return promptly and reap the child
	if st := m.Statuses()[0]; st.Status == StatusReady {
		t.Error("post-Close status should not be ready")
	}

	// Failure path: a command that dies instantly surfaces stderr in /mcp.
	m2 := NewManager(map[string]ServerConfig{"bad": {Command: []string{"sh", "-c", "echo dying-loudly >&2; exit 1"}, StartupTimeout: 5, Cwd: workspace}})
	m2.Start(context.Background())
	defer m2.Close()
	s2 := m2.servers["bad"]
	select {
	case <-s2.ready:
	case <-time.After(10 * time.Second):
		t.Fatal("never settled")
	}
	st := m2.Statuses()[0]
	if st.Status != StatusFailed {
		t.Fatalf("bad server status = %+v", st)
	}
	if !strings.Contains(st.Err, "dying-loudly") {
		t.Errorf("stderr tail should be in the failure message: %q", st.Err)
	}
}
