package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/rlm"
)

func TestRecursiveHostMCPRefreshDiscoversNewToolsWithoutExpandingChildren(t *testing.T) {
	for _, engine := range []string{rlm.EngineStarlark, rlm.EngineQuickJS} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("WHIP_HOME", t.TempDir())
			url, effects := localMCPFixture(t, "guidance")
			_, root, runtime := mcpRuntimeFixture(t, url, true, engine)
			child := spawnMCPChild(t, runtime.rootNode, map[string]any{"name": "before-refresh"})
			// Spawn has already committed the child's grant snapshot; its model
			// turn need not finish before the catalog changes.
			root.mcpMu.Lock()
			root.loadMCP = func(context.Context) (mcp.Filtered, error) {
				return mcp.Filtered{Merged: mcp.FromConfigMap(map[string]config.MCPServer{
					"local": {URL: url, StartupTimeout: 2, ToolTimeout: 2},
					"fresh": {URL: url, StartupTimeout: 2, ToolTimeout: 2},
				})}, nil
			}
			root.mcpMu.Unlock()
			refresh, reconnect := `mcp.refresh()`, `mcp.reconnect(server="fresh")`
			if engine == rlm.EngineQuickJS {
				refresh, reconnect = `await mcp.refresh({})`, `await mcp.reconnect({server: "fresh"})`
			}
			result, err := runtime.rootNode.kernel.Exec(t.Context(), refresh)
			if err != nil {
				t.Fatal(err)
			}
			added := result.Value.(map[string]any)["added"].([]any)
			if len(added) != 1 || added[0] != "fresh" {
				t.Fatalf("refresh=%+v", result.Value)
			}
			waitMCPRecoveryTool(t, root.mcpManager(), "fresh")
			for _, node := range []*AgentSession{runtime.rootNode, child} {
				matches := hostBehaviorCall(t, node.host, "mcp", "search", map[string]any{"query": "mutate", "server": "fresh"}).([]map[string]any)
				if len(matches) == 0 {
					t.Fatal("fresh tools not discoverable")
				}
				for _, match := range matches {
					if match["authorized"] != (node == runtime.rootNode) {
						t.Fatalf("incorrect fresh-tool authority for %s: %+v", node.id, match)
					}
				}
				described := hostBehaviorCall(t, node.host, "mcp", "describe", map[string]any{"server": "fresh", "tool": "mutate"}).(map[string]any)
				if described["authorized"] != (node == runtime.rootNode) {
					t.Fatalf("describe authority=%+v", described)
				}
			}
			args := map[string]any{"server": "fresh", "tool": "mutate", "arguments": map[string]any{}}
			if _, err := child.host.Call(t.Context(), "mcp", "call", args); err == nil {
				t.Fatal("refresh broadened the existing child's grant")
			}
			hostBehaviorCall(t, runtime.rootNode.host, "mcp", "call", args)
			if effects.Load() != 1 {
				t.Fatalf("fresh tool effects=%d", effects.Load())
			}
			result, err = runtime.rootNode.kernel.Exec(t.Context(), reconnect)
			if err != nil {
				t.Fatal(err)
			}
			status := result.Value.(map[string]any)
			if status["name"] != "fresh" || (status["status"] != "connecting" && status["status"] != "ready") {
				t.Fatalf("reconnect=%+v", status)
			}
			waitMCPRecoveryTool(t, root.mcpManager(), "fresh")
			if effects.Load() != 1 {
				t.Fatalf("reconnect replayed a tool call: effects=%d", effects.Load())
			}
			hostBehaviorCall(t, runtime.rootNode.host, "mcp", "call", args)
			if effects.Load() != 2 {
				t.Fatalf("reconnected tool effects=%d", effects.Load())
			}
		})
	}
}

func waitMCPRecoveryTool(t *testing.T, manager *mcp.Manager, server string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := manager.ResolveTool(server, "mutate"); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("MCP did not become ready: %+v", manager.Statuses())
}

func TestRecursiveHostMCPRefreshCreatesFirstManager(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	url, _ := localMCPFixture(t, "guidance")
	_, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	root.mcpMu.Lock()
	root.loadMCP = func(context.Context) (mcp.Filtered, error) {
		return mcp.Filtered{Merged: mcp.FromConfigMap(map[string]config.MCPServer{
			"local": {URL: url, StartupTimeout: 2, ToolTimeout: 2},
		})}, nil
	}
	root.mcpMu.Unlock()
	hostBehaviorCall(t, runtime.rootNode.host, "mcp", "refresh", nil)
	waitMCPReady(t, root.mcpManager())
	described := hostBehaviorCall(t, runtime.rootNode.host, "mcp", "describe", map[string]any{"server": "local", "tool": "mutate"}).(map[string]any)
	if described["authorized"] != true {
		t.Fatalf("first server's tools not authorized: %+v", described)
	}
}
