//go:build integration

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
)

func TestRecursiveAgentReceiptsWithLargeMCPGrants(t *testing.T) {
	const toolCount = 780
	extraTools := make([]string, toolCount-4)
	for index := range extraTools {
		extraTools[index] = fmt.Sprintf("workspace_documentation_search_%04d", index)
	}
	url, effects := localMCPFixture(t, "guidance", extraTools...)
	store, root, runtime := mcpRuntimeFixture(t, url, true)
	runs := &sync.Map{}
	runtime.setRunTurnHook(observeRunTurn(runs))
	parent := runtime.rootNode
	result, err := parent.kernel.Exec(t.Context(), `child = agents.spawn(name="large", prompt="finish", report="message")
print(child)
child`)
	if err != nil || result.Scratch != nil {
		t.Fatalf("spawn receipt failed: err=%v, scratch=%+v, output bytes=%d", err, result.Scratch, len(result.Output))
	}
	receipt := result.Value.(map[string]any)
	childID := receipt["id"].(string)
	if len(receipt) != 5 || receipt["name"] != "large" || receipt["parent_id"] != parent.id || receipt["status"] != "queued" || receipt["report"] != "message" {
		t.Fatalf("unexpected admission receipt: %+v", receipt)
	}
	waitRunTurn(t, runs, childID, 1)
	runtime.mu.RLock()
	child := runtime.agents[childID]
	runtime.mu.RUnlock()
	waitAgentIdle(t, child)
	relatives, err := root.ListAgentRelatives(t.Context(), parent.id)
	if err != nil || len(relatives.Children) != 1 || relatives.Children[0].ID != childID {
		t.Fatalf("spawn did not admit exactly one child: %+v, %v", relatives, err)
	}
	selectors, all, err := store.MCPSelectors(t.Context(), root.ID(), childID, child.authority.MCP)
	if err != nil || all || len(selectors) != toolCount {
		t.Fatalf("inherited grants: all=%v, count=%d, err=%v", all, len(selectors), err)
	}
	encoded, err := json.Marshal(selectors)
	if err != nil || len(encoded) <= rlm.DefaultLimits().OutputBytes {
		t.Fatalf("fixture does not reproduce the oversized grant response: bytes=%d, err=%v", len(encoded), err)
	}
	t.Logf("spawn output: %d bytes; persisted selectors: %d bytes", len(result.Output), len(encoded))
	if err := mcpCell(t, child, extraTools[len(extraTools)-1]); err != nil || effects.Load() != 1 {
		t.Fatalf("last inherited tool was not callable: err=%v, effects=%d", err, effects.Load())
	}
	if err := parent.kernel.Suspend(); err != nil {
		t.Fatal(err)
	}
	result, err = parent.kernel.Exec(t.Context(), `child["id"]`)
	if err != nil || result.Value != childID || result.Restored == nil || !slices.Contains(result.Restored.Restored, "child") {
		t.Fatalf("receipt did not survive worker replacement: %+v, %v", result, err)
	}
	result, err = parent.kernel.Exec(t.Context(), `info = agents.inspect(id=child["id"])
print(info)
info`)
	if err != nil || result.Scratch != nil || len(result.Output) >= session.InlineValueLimit {
		t.Fatalf("default inspection is not compact: err=%v, scratch=%+v, output bytes=%d", err, result.Scratch, len(result.Output))
	}
	info := result.Value.(map[string]any)
	if info["id"] != childID || info["effective_capabilities"] == nil || info["budgets"] == nil || info["effective_mcp_tools"] != nil || info["mcp_grants"] != nil {
		t.Fatalf("default inspection lost state or included grants: %+v", info)
	}
	result, err = parent.kernel.Exec(t.Context(), `details = agents.inspect(id=child["id"], include_grants=True)
print(details)
details["mcp_grants"]`)
	if err != nil || result.Scratch != nil {
		t.Fatalf("explicit inspection overflowed: err=%v, scratch=%+v", err, result.Scratch)
	}
	grants := result.Value.(map[string]any)
	handle, _ := grants["handle"].(string)
	if handle == "" || grants["preview"] == nil || grants["output"] != nil {
		t.Fatalf("large grant details lack a bounded content reference: %+v", grants)
	}
	var body strings.Builder
	for offset, size := 0, int(grants["size"].(float64)); offset < size; {
		part, err := parent.host.Call(t.Context(), "context", "read", map[string]any{"handle": handle, "offset": float64(offset)})
		if err != nil {
			t.Fatal(err)
		}
		text := part.(map[string]any)["text"].(string)
		if len(text) == 0 || len(text) > session.InlineValueLimit {
			t.Fatalf("invalid retrieval chunk length: %d", len(text))
		}
		body.WriteString(text)
		offset += len(text)
	}
	var recovered struct {
		All       bool                     `json:"all"`
		Selectors []capability.MCPSelector `json:"selectors"`
	}
	if err := json.Unmarshal([]byte(body.String()), &recovered); err != nil || recovered.All || !slices.Equal(recovered.Selectors, selectors) {
		t.Fatalf("grant retrieval changed selectors: count=%d, err=%v", len(recovered.Selectors), err)
	}
	middle := extraTools[len(extraTools)/2]
	search, err := parent.host.Call(t.Context(), "context", "search", map[string]any{"handle": handle, "query": middle})
	if err != nil {
		t.Fatal(err)
	}
	searchJSON, err := json.Marshal(search)
	if err != nil || !strings.Contains(string(searchJSON), middle) {
		t.Fatalf("middle selector was not searchable: %s, %v", searchJSON, err)
	}
	if _, _, err := root.ReadContent(t.Context(), childID, handle, 0, 1); !errors.Is(err, session.ErrContentAccess) {
		t.Fatalf("another agent read the caller's grant handle: %v", err)
	}
}

func TestRecursiveAgentGrantInspectionAccess(t *testing.T) {
	url, _ := localMCPFixture(t, "guidance")
	_, root, runtime := mcpRuntimeFixture(t, url, true)
	parent := runtime.rootNode
	child := spawnMCPChild(t, parent, map[string]any{
		"name": "narrow", "capabilities": []any{"read", "mcp"},
		"mcp_tools": []any{map[string]any{"server": "local", "tool": "mutate"}},
	})
	sibling := spawnMCPChild(t, parent, map[string]any{"name": "reader", "capabilities": []any{"read"}})
	grandchild := spawnMCPChild(t, child, map[string]any{"name": "grandchild"})
	call, err := root.mcpManager().ResolveTool("local", "mutate")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		caller    *AgentSession
		target    *AgentSession
		all       bool
		selectors []capability.MCPSelector
		denied    bool
	}{
		{name: "finite child", caller: parent, target: child, selectors: []capability.MCPSelector{call.MCPSelector}},
		{name: "unrestricted parent", caller: child, target: parent, all: true},
		{name: "no MCP capability", caller: child, target: sibling},
		{name: "sibling", caller: sibling, target: child, selectors: []capability.MCPSelector{call.MCPSelector}},
		{name: "grandchild is not a direct relative", caller: parent, target: grandchild, denied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.caller.host.Call(t.Context(), "agents", "inspect", map[string]any{"id": test.target.id, "include_grants": true})
			if test.denied {
				if !errors.Is(err, session.ErrAgentAccess) {
					t.Fatalf("unrelated inspection: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			grants := result.(map[string]any)["mcp_grants"].(map[string]any)
			output, ok := grants["output"].(string)
			if !ok || grants["handle"] != nil {
				t.Fatalf("small grant details are not inline: %+v", grants)
			}
			var decoded struct {
				All       bool                     `json:"all"`
				Selectors []capability.MCPSelector `json:"selectors"`
			}
			if err := json.Unmarshal([]byte(output), &decoded); err != nil || decoded.All != test.all || !slices.Equal(decoded.Selectors, test.selectors) {
				t.Fatalf("incorrect grant details: %s, %v", output, err)
			}
		})
	}
	if _, err := parent.host.Call(t.Context(), "agents", "inspect", map[string]any{"id": child.id, "include_grants": "true"}); err == nil {
		t.Fatal("non-boolean include_grants was accepted")
	}
	if _, err := root.RevokeCapability(t.Context(), parent.id, child.authority.MCP.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := child.host.Call(t.Context(), "agents", "inspect", map[string]any{"id": grandchild.id}); err != nil {
		t.Fatalf("default inspection unnecessarily read MCP selectors: %v", err)
	}
	if _, err := child.host.Call(t.Context(), "agents", "inspect", map[string]any{"id": grandchild.id, "include_grants": true}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("requested grant read failure was hidden: %v", err)
	}
}
