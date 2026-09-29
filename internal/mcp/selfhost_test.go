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

	"github.com/context-labs/whip/internal/localruntime"
)

// TestServeSelfHost builds `whipcode mcp serve` and connects to it as a real
// stdio MCP server — the full loop: config → manager → CommandTransport →
// subprocess → served tools. Gated on WHIP_TEST_SELFHOST since it shells
// out to `go build`.
func TestServeSelfHost(t *testing.T) {
	if os.Getenv("WHIP_TEST_SELFHOST") == "" {
		t.Skip("builds the whipcode binary; set WHIP_TEST_SELFHOST=1 to run")
	}
	directory := t.TempDir()
	bin := filepath.Join(directory, "whipcode")
	buildContext, cancelBuild := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancelBuild()
	build := exec.CommandContext(buildContext, "go", "build", "-o", bin, "../../cmd/whip")
	build.WaitDelay = 2 * time.Second
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	workspace := filepath.Join(directory, "workspace")
	home := filepath.Join(directory, "home")
	for _, path := range []string{workspace, home} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, workspace, "fixture.txt", "selfhost-ok")
	paths, err := localruntime.Resolve(filepath.Join(home, ".whipcode"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	m := NewManager(map[string]ServerConfig{"self": {
		Command: []string{bin, "mcp", "serve"}, Cwd: workspace, StartupTimeout: 15, ToolTimeout: 15,
		Env: map[string]string{"HOME": home, "WHIPCODE_HOME": filepath.Join(home, ".whipcode"), "WHIPCODE_NETWORK": "false", "WHIPCODE_NETWORK_TERMINALS": "false"},
	}})
	t.Cleanup(func() {
		m.Close()
		stopContext, stopCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer stopCancel()
		if _, err := localruntime.Stop(stopContext, paths); err != nil {
			t.Errorf("stop disposable native host: %v", err)
		}
		if status := localruntime.Inspect(stopContext, paths); status.State != "stopped" {
			t.Errorf("disposable native host survived cleanup: %+v", status)
		}
	})
	m.Start(ctx)
	s := m.servers["self"]
	select {
	case <-s.ready:
	case <-ctx.Done():
		t.Fatal("never settled: ", ctx.Err())
	}
	st := m.Statuses()[0]
	if st.Status != StatusReady {
		t.Fatalf("self-serve status: %+v", st)
	}
	if st.Tools != 10 {
		t.Fatalf("expected the native endpoint's 10 tools, got %d", st.Tools)
	}
	out, err := s.call(ctx, "read", json.RawMessage(`{"path":"fixture.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "selfhost-ok") {
		t.Fatalf("read via MCP: %q", out)
	}
	for _, call := range []struct{ name, arguments string }{
		{"write", `{"path":"fixture.txt","content":"changed"}`},
		{"bash", `{"command":"touch denied-marker"}`},
	} {
		if out, err := s.call(ctx, call.name, json.RawMessage(call.arguments)); err == nil || !strings.Contains(err.Error(), "tree policy denies interactive tool permissions") {
			t.Fatalf("native endpoint did not deny unapproved %s through policy: %q, %v", call.name, out, err)
		}
	}
	if body, err := os.ReadFile(filepath.Join(workspace, "fixture.txt")); err != nil || string(body) != "selfhost-ok" {
		t.Fatalf("denied write changed workspace: %q, %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "denied-marker")); !os.IsNotExist(err) {
		t.Fatalf("denied shell changed workspace: %v", err)
	}
}
