package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestServeSelfHost builds `whipcode mcp serve` and connects to it as a real
// stdio MCP server — the full loop: config → manager → CommandTransport →
// subprocess → served tools. Gated on WHIP_TEST_SELFHOST since it shells
// out to `go build`.
func TestServeSelfHost(t *testing.T) {
	if os.Getenv("WHIP_TEST_SELFHOST") == "" {
		t.Skip("builds the whipcode binary; set WHIP_TEST_SELFHOST=1 to run")
	}
	bin := filepath.Join(t.TempDir(), "whip")
	if out, err := exec.CommandContext(context.Background(), "go", "build", "-o", bin, "../../cmd/whip").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	m := NewManager(map[string]ServerConfig{"self": {Command: []string{bin, "mcp", "serve"}, StartupTimeout: 15, ToolTimeout: 15}})
	m.Start(context.Background())
	t.Cleanup(m.Close)
	s := m.servers["self"]
	select {
	case <-s.ready:
	case <-time.After(20 * time.Second):
		t.Fatal("never settled")
	}
	st := m.Statuses()[0]
	if st.Status != StatusReady {
		t.Fatalf("self-serve status: %+v", st)
	}
	if st.Tools != 4 {
		t.Fatalf("expected whip's 4 tools, got %d", st.Tools)
	}
	out, err := s.call(context.Background(), "bash", json.RawMessage(`{"command":"echo selfhost-ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "selfhost-ok") {
		t.Fatalf("bash via MCP: %q", out)
	}
}
