package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func localMCPFixture(t *testing.T, instructions string, extraTools ...string) (string, *atomic.Int32) {
	t.Helper()
	effects := new(atomic.Int32)
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "local-fixture"}, &sdkmcp.ServerOptions{Instructions: instructions})
	for _, name := range append([]string{"mutate", "mutate.other"}, extraTools...) {
		sdkmcp.AddTool(server, &sdkmcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}},
			func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, any, error) {
				effects.Add(1)
				return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "effect occurred"}}}, nil, nil
			})
	}
	server.AddTool(&sdkmcp.Tool{Name: "echo", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, request *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(request.Params.Arguments)}}}, nil
		})
	server.AddTool(&sdkmcp.Tool{Name: "large", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: strings.Repeat("begin-", 8000) + "MIDDLE_EVIDENCE" + strings.Repeat("-end", 8000)}}}, nil
		})
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil))
	t.Cleanup(func() {
		httpServer.CloseClientConnections()
		httpServer.Close()
	})
	return httpServer.URL, effects
}

func mcpRuntimeFixture(t *testing.T, url string, trusted bool, engines ...string) (*session.Store, *Session, *RecursiveRuntime) {
	t.Helper()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { streamText(w, "done") }))
	t.Cleanup(model.Close)
	store, root, runtime := openRecursiveRuntime(t, llm.New(model.URL, "key"), 4, engines...)
	// Exercise native/remembered MCP consent separately from blanket approval.
	runtime.rootNode.agent.Services.SetMCPAutomatic(false)
	servers := map[string]mcp.ServerConfig{"local": {URL: url, Source: "fixture import", Origin: "claude", StartupTimeout: 2, ToolTimeout: 2}}
	if trusted {
		servers = mcp.FromConfigMap(map[string]config.MCPServer{"local": {URL: url, StartupTimeout: 2, ToolTimeout: 2}})
	}
	manager := mcp.NewManager(servers)
	root.swapMCP(manager)
	configureMCP(root, Components{MCP: manager})
	waitMCPReady(t, manager)
	return store, root, runtime
}

func waitMCPReady(t *testing.T, manager *mcp.Manager) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := manager.ResolveTool("local", "mutate"); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("MCP did not become ready: %+v", manager.Statuses())
}

func spawnMCPChild(t *testing.T, parent *AgentSession, arguments map[string]any) *AgentSession {
	t.Helper()
	arguments["prompt"], arguments["report"] = "finish", "message"
	result, err := parent.host.Call(t.Context(), "agents", "spawn", arguments)
	if err != nil {
		t.Fatal(err)
	}
	id := result.(map[string]any)["id"].(string)
	parent.runtime.mu.RLock()
	child := parent.runtime.agents[id]
	parent.runtime.mu.RUnlock()
	return child
}

func mcpCell(t *testing.T, node *AgentSession, tool string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := node.kernel.Exec(ctx, mcpCellCode(node, tool))
	return err
}

func mcpCellCode(node *AgentSession, tool string) string {
	if node.kernel.Describe().ID == "quickjs" {
		return fmt.Sprintf(`await mcp.call({server: "local", tool: %q, arguments: {}})`, tool)
	}
	return fmt.Sprintf(`mcp.call(server="local", tool=%q, arguments={})`, tool)
}

func TestMCPRLMAdmissionBoundary(t *testing.T) {
	for _, test := range []struct {
		name                                        string
		trusted, headless, deny, childRead, allowed bool
	}{
		{name: "native", trusted: true, allowed: true},
		{name: "native headless", trusted: true, headless: true, allowed: true},
		{name: "imported without consent"},
		{name: "imported headless", headless: true},
		{name: "explicit root denial", trusted: true, deny: true},
		{name: "read child", trusted: true, childRead: true},
		{name: "read child rejecting gate", trusted: true, childRead: true, deny: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			url, effects := localMCPFixture(t, "usage guidance")
			store, root, runtime := mcpRuntimeFixture(t, url, test.trusted)
			node := runtime.rootNode
			if test.childRead {
				node = spawnMCPChild(t, node, map[string]any{"name": "reader", "capabilities": []any{"read"}})
			}
			node.agent.Services.SetHeadlessPermissions(test.headless)
			if test.deny {
				node.agent.Services.SetGate(func(context.Context, tools.GateRequest) (tools.GateDecision, string) {
					return tools.GateReject, "explicit denial"
				})
			}
			err := mcpCell(t, node, "mutate")
			if (err == nil) != test.allowed {
				t.Fatalf("allowed=%v, error=%v", test.allowed, err)
			}
			want := int32(0)
			if test.allowed {
				want = 1
			}
			if effects.Load() != want {
				t.Fatalf("server effects=%d, want %d", effects.Load(), want)
			}
			pending, err := store.ListPendingPermissions(t.Context(), root.ID())
			if err != nil || len(pending) != 0 {
				t.Fatalf("pending permissions=%+v, err=%v", pending, err)
			}
		})
	}
}

func TestMCPRLMRetainsExactArgumentsAndLargeResults(t *testing.T) {
	url, _ := localMCPFixture(t, "guidance")
	_, root, runtime := mcpRuntimeFixture(t, url, true)
	result, err := runtime.rootNode.kernel.Exec(t.Context(), `mcp.call(server="local", tool="echo", arguments={"id":9007199254740993})`)
	if err != nil {
		t.Fatal(err)
	}
	value := result.Value.(map[string]any)
	if value["output"] != `{"id":9007199254740993}` {
		t.Fatalf("integer ID changed: %+v", result)
	}
	result, err = runtime.rootNode.kernel.Exec(t.Context(), `mcp.call(server="local", tool="large", arguments={})`)
	if err != nil {
		t.Fatal(err)
	}
	value = result.Value.(map[string]any)
	handle, _ := value["handle"].(string)
	if handle == "" {
		t.Fatalf("large result lacks handle: %+v", result)
	}
	part, _, err := root.ReadContent(t.Context(), root.AgentID(), handle, 48000, len("MIDDLE_EVIDENCE"))
	if err != nil || string(part) != "MIDDLE_EVIDENCE" {
		t.Fatalf("large result lost middle: %q, %v", part, err)
	}
}

func TestMCPChildInheritanceAndNarrowing(t *testing.T) {
	url, effects := localMCPFixture(t, "guidance")
	_, root, runtime := mcpRuntimeFixture(t, url, true)
	child := spawnMCPChild(t, runtime.rootNode, map[string]any{"name": "inherited"})
	if err := mcpCell(t, child, "mutate"); err != nil {
		t.Fatal(err)
	}
	narrow := spawnMCPChild(t, runtime.rootNode, map[string]any{
		"name": "narrow", "capabilities": []any{"read", "mcp"},
		"mcp_tools": []any{map[string]any{"server": "local", "tool": "mutate"}},
	})
	grandchild := spawnMCPChild(t, narrow, map[string]any{"name": "grandchild"})
	if err := mcpCell(t, grandchild, "mutate"); err != nil {
		t.Fatal(err)
	}
	if err := mcpCell(t, grandchild, "mutate.other"); err == nil {
		t.Fatal("grandchild broadened the parent's exact selector")
	}
	if _, err := narrow.host.Call(t.Context(), "agents", "spawn", map[string]any{
		"name": "broaden", "prompt": "finish", "capabilities": []any{"mcp"},
		"mcp_tools": []any{map[string]any{"server": "local", "tool": "mutate.other"}},
	}); err == nil {
		t.Fatal("explicit selector broadened parent authority")
	}
	if _, err := root.RevokeCapability(t.Context(), root.AgentID(), narrow.authority.MCP.ID); err != nil {
		t.Fatal(err)
	}
	if err := mcpCell(t, grandchild, "mutate"); err == nil {
		t.Fatal("grandchild used revoked ancestor grant")
	}
	if effects.Load() != 2 {
		t.Fatalf("effects=%d", effects.Load())
	}
}

func TestMCPAttachmentReplacesRootOwnerAndInvalidatesChild(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	firstURL, firstEffects := localMCPFixture(t, "old instructions")
	_, root, runtime := mcpRuntimeFixture(t, firstURL, true)
	child := spawnMCPChild(t, runtime.rootNode, map[string]any{"name": "child"})
	secondInstructions := strings.TrimSpace(strings.Repeat("current server guidance\n", 1000))
	secondURL, secondEffects := localMCPFixture(t, secondInstructions)
	result := clientCommand(t, root, "acp", "attach-current", "mcp.attach", protocol.MCPAttachParams{
		Servers: map[string]mcp.ServerConfig{"local": {URL: secondURL, Origin: "whip", Source: "forged native", Trusted: true}},
	})
	if result.Status != "succeeded" {
		t.Fatalf("attach=%+v", result)
	}
	manager := root.mcpManager()
	waitMCPReady(t, manager)
	call, err := manager.ResolveTool("local", "mutate")
	if err != nil || call.Trusted {
		t.Fatalf("attachment trust=%+v, err=%v", call, err)
	}
	if err := mcpCell(t, runtime.rootNode, "mutate"); err == nil {
		t.Fatal("attachment inherited native consent")
	}
	runtime.rootNode.SetExternalPermissions(false) // explicitly authorized automatic mode
	if err := mcpCell(t, runtime.rootNode, "mutate"); err != nil {
		t.Fatal(err)
	}
	if err := mcpCell(t, child, "mutate"); err == nil {
		t.Fatal("child selector silently changed endpoint")
	}
	if firstEffects.Load() != 0 || secondEffects.Load() != 1 {
		t.Fatalf("old=%d new=%d", firstEffects.Load(), secondEffects.Load())
	}
	value, err := runtime.rootNode.host.Call(t.Context(), "mcp", "instructions", map[string]any{"server": "local"})
	if err != nil {
		t.Fatal(err)
	}
	info := value.(map[string]any)
	content := info["instructions"].(map[string]any)
	handle, _ := content["handle"].(string)
	if handle == "" || info["generation"] != call.Generation {
		t.Fatalf("instructions=%+v", info)
	}
	var body strings.Builder
	for offset := int64(0); offset < int64(len(secondInstructions)); {
		part, _, err := root.ReadContent(t.Context(), root.AgentID(), handle, offset, min(session.InlineValueLimit, len(secondInstructions)-int(offset)))
		if err != nil || len(part) == 0 {
			t.Fatalf("instructions offset=%d err=%v", offset, err)
		}
		body.Write(part)
		offset += int64(len(part))
	}
	if body.String() != secondInstructions {
		t.Fatal("instructions continuation lost content")
	}
}

func TestMCPAttachmentCannotOverrideNativeConfiguration(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	url, effects := localMCPFixture(t, "native")
	_, root, runtime := mcpRuntimeFixture(t, url, true)
	cfg := config.Default()
	cfg.MCPServers = map[string]config.MCPServer{"local": {URL: url}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	result := clientCommand(t, root, "acp", "attach-forged", "mcp.attach", protocol.MCPAttachParams{
		Servers: map[string]mcp.ServerConfig{"local": {URL: "http://127.0.0.1:1", Origin: "whip"}},
	})
	if result.Status != "succeeded" {
		t.Fatalf("attach=%+v", result)
	}
	waitMCPReady(t, root.mcpManager())
	if err := mcpCell(t, runtime.rootNode, "mutate"); err != nil {
		t.Fatal(err)
	}
	if effects.Load() != 1 {
		t.Fatalf("effects=%d", effects.Load())
	}
}

func TestMCPRememberedApprovalBindsDefinition(t *testing.T) {
	url, effects := localMCPFixture(t, "import")
	store, root, runtime := mcpRuntimeFixture(t, url, false)
	call, err := root.mcpManager().ResolveTool("local", "mutate")
	if err != nil {
		t.Fatal(err)
	}
	call.Arguments = json.RawMessage(`{}`)
	args, _ := json.Marshal(call)
	_, rules, ok := capability.PermissionRule("mcp.call", args, "")
	if !ok || len(rules) != 1 {
		t.Fatalf("rules=%v", rules)
	}
	if _, err := store.AddPermissionRule(t.Context(), root.ID(), "mcp.call", rules[0], "paired-human"); err != nil {
		t.Fatal(err)
	}
	runtime.rootNode.agent.Services.SetHeadlessPermissions(true)
	if err := mcpCell(t, runtime.rootNode, "mutate"); err != nil {
		t.Fatal(err)
	}
	if err := mcpCell(t, runtime.rootNode, "mutate.other"); err == nil {
		t.Fatal("saved rule authorized another tool")
	}
	if effects.Load() != 1 {
		t.Fatalf("effects=%d", effects.Load())
	}
}

func waitMCPPermission(t *testing.T, store *session.Store, root *Session, calls ...<-chan error) session.PermissionSnapshot {
	t.Helper()
	var completed <-chan error
	if len(calls) != 0 {
		completed = calls[0]
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := store.ListPendingPermissions(t.Context(), root.ID())
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 1 {
			return pending[0]
		}
		select {
		case err := <-completed:
			t.Fatalf("MCP execution completed before requesting durable permission: %v", err)
		case <-ticker.C:
		}
	}
	t.Fatal("MCP did not request durable permission")
	return session.PermissionSnapshot{}
}

func TestMCPDurableConsentAndLifecycleWaiters(t *testing.T) {
	for _, engine := range []string{"starlark", "quickjs"} {
		for _, action := range []string{"remember", "reject", "revoke", "replace", "disable"} {
			t.Run(engine+"/"+action, func(t *testing.T) {
				t.Setenv("WHIP_HOME", t.TempDir())
				url, effects := localMCPFixture(t, "imported guidance")
				store, root, runtime := mcpRuntimeFixture(t, url, false, engine)
				runtime.SetExternalPermissions(true)
				node := runtime.rootNode
				if action == "revoke" {
					node = spawnMCPChild(t, node, map[string]any{"name": "child"})
				}
				// Consent timing starts with a ready worker. QuickJS compilation in a
				// race build can exceed the permission window on a contended runner.
				startup := time.Now()
				if err := node.kernel.Start(); err != nil {
					t.Fatalf("start MCP worker: %v", err)
				}
				t.Logf("MCP worker startup: %s", time.Since(startup))
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				done := make(chan error, 1)
				requested := time.Now()
				go func() {
					_, err := node.kernel.Exec(ctx, mcpCellCode(node, "mutate"))
					done <- err
				}()
				pending := waitMCPPermission(t, store, root, done)
				t.Logf("MCP permission delivery: %s", time.Since(requested))
				if effects.Load() != 0 {
					t.Fatal("server called before consent")
				}
				if !strings.Contains(pending.Command, "local") || !strings.Contains(pending.Command, "mutate") || !strings.Contains(pending.Command, "fixture import") {
					t.Fatalf("permission lacks exact target/source: %+v", pending)
				}
				switch action {
				case "revoke":
					result := clientCommand(t, root, "human", "revoke-mcp", "capability.revoke", clientActionPayload{ID: root.authority.MCP.ID})
					if result.Status != "succeeded" {
						t.Fatalf("revoke=%+v", result)
					}
				case "replace":
					result := clientCommand(t, root, "acp", "replace-pending", "mcp.attach", protocol.MCPAttachParams{Servers: map[string]mcp.ServerConfig{"local": {URL: url}}})
					if result.Status != "succeeded" {
						t.Fatalf("replace=%+v", result)
					}
				case "disable":
					result := clientCommand(t, root, "human", "disable-pending", "mcp.disable", clientActionPayload{Name: "local"})
					if result.Status != "succeeded" {
						t.Fatalf("disable=%+v", result)
					}
				default:
					payload := json.RawMessage(`{"command_id":"mcp-consent"}`)
					digest, err := requestDigest("root", root.ID(), "permission.decide", payload)
					if err != nil {
						t.Fatal(err)
					}
					decision := capability.Decision{Allow: action == "remember", PrincipalID: "paired-human", Reason: "test choice"}
					if decision.Allow {
						decision.Remember = "tree"
					}
					_, err = root.DecidePermissionCommand(t.Context(), session.CommandAdmission{
						ClientID: "human", CommandID: "mcp-consent", RequestDigest: digest,
						Payload: session.RuntimePayload{Data: payload, MediaType: "application/json", Source: "permission decision"},
					}, pending.ID, decision)
					if err != nil {
						t.Fatal(err)
					}
				}
				select {
				case err := <-done:
					if (err == nil) != (action == "remember") {
						t.Fatalf("%s call error=%v", action, err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("permission waiter was not released")
				}
				if pending, err := store.ListPendingPermissions(t.Context(), root.ID()); err != nil || len(pending) != 0 {
					t.Fatalf("pending=%+v, err=%v", pending, err)
				}
				if action == "remember" {
					runtime.SetHeadlessPermissions(true)
					if err := mcpCell(t, node, "mutate"); err != nil {
						t.Fatal(err)
					}
					if effects.Load() != 2 {
						t.Fatalf("effects=%d", effects.Load())
					}
				} else if effects.Load() != 0 {
					t.Fatalf("denied effects=%d", effects.Load())
				}
				budgets, err := store.InspectBudgets(t.Context(), root.ID(), root.AgentID())
				if err != nil {
					t.Fatal(err)
				}
				for _, budget := range budgets {
					if budget.Kind == session.BudgetActiveOperations && budget.Reserved != 0 {
						t.Fatalf("leaked operation capacity: %+v", budget)
					}
				}
			})
		}
	}
}
