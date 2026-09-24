package daemon

import (
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
)

func TestRecursiveHostMCPRecoveryValidatesArguments(t *testing.T) {
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	_, _, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	for _, tc := range []struct {
		name, operation, want string
		args                  map[string]any
	}{
		{"refresh unknown", "refresh", "unexpected argument", map[string]any{"server": "local"}},
		{"reconnect missing", "reconnect", "non-empty string", nil},
		{"reconnect null", "reconnect", "non-empty string", map[string]any{"server": nil}},
		{"reconnect number", "reconnect", "non-empty string", map[string]any{"server": 1}},
		{"reconnect empty", "reconnect", "non-empty string", map[string]any{"server": ""}},
		{"reconnect blank", "reconnect", "non-empty string", map[string]any{"server": "  "}},
		{"reconnect unknown argument", "reconnect", "unexpected argument", map[string]any{"server": "local", "force": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runtime.rootNode.host.Call(t.Context(), "mcp", tc.operation, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestRecursiveHostMCPRecoveryRequiresRootAndCapability(t *testing.T) {
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	url, effects := localMCPFixture(t, "guidance")
	_, root, runtime := mcpRuntimeFixture(t, url, true)
	child := spawnMCPChild(t, runtime.rootNode, map[string]any{"name": "child"})
	for _, operation := range []string{"refresh", "reconnect"} {
		_, err := child.host.Call(t.Context(), "mcp", operation, map[string]any{"server": "local"})
		if err == nil || !strings.Contains(err.Error(), "root agent") {
			t.Fatalf("child %s error=%v", operation, err)
		}
	}
	reference := runtime.rootNode.authority.MCP
	runtime.rootNode.authority.MCP = capability.Reference{}
	for _, operation := range []string{"refresh", "reconnect"} {
		_, err := runtime.rootNode.host.Call(t.Context(), "mcp", operation, map[string]any{"server": "local"})
		if err == nil || !strings.Contains(err.Error(), "active mcp capability") {
			t.Fatalf("missing capability %s error=%v", operation, err)
		}
	}
	runtime.rootNode.authority.MCP = reference
	if _, err := root.RevokeCapability(t.Context(), root.AgentID(), reference.ID); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"refresh", "reconnect"} {
		_, err := runtime.rootNode.host.Call(t.Context(), "mcp", operation, map[string]any{"server": "local"})
		if err == nil || !strings.Contains(err.Error(), "active mcp capability") {
			t.Fatalf("revoked capability %s error=%v", operation, err)
		}
	}
	if effects.Load() != 0 {
		t.Fatalf("recovery executed %d tools", effects.Load())
	}
}
