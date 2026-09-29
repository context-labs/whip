package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func mcpChildCell(t *testing.T, r *Runtime, owner session.SessionID) session.Cell {
	t.Helper()
	admitted, err := r.store.Claim(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	message, err := r.store.AppendMessage(t.Context(), admitted.Turn.ID, session.MessageDraft{ID: session.MessageID("message_" + string(owner)), Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "execute", Name: "execute", Arguments: json.RawMessage(`{"code":"1"}`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	cell, accepted, err := r.store.BeginCell(t.Context(), session.CellSpec{ID: session.CellID("cell_" + string(owner)), TurnID: admitted.Turn.ID, CallMessageID: message.ID, CallID: "execute"})
	if err != nil || !accepted {
		t.Fatal(accepted, err)
	}
	return cell
}

func TestMCPDefaultChildCatalogMatchesCallableScopeAcrossReconnect(t *testing.T) {
	isolateMCP(t)
	url, server, effects := mcpHTTPFixture(t)
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	owner := createTest(t, r)
	configureMCPFixture(t, r, url)
	if _, err := r.MCPRefresh(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	manager := awaitMCPReady(t, r, owner)
	identity := session.RequestIdentity{ClientID: "test", RequestID: "child"}
	request := store.ChildRequest{ParentID: owner.ID, Parts: []session.Part{{Type: "text", Text: "work"}}}
	admitted, err := r.SpawnChild(t.Context(), identity, request)
	if err != nil {
		t.Fatal(err)
	}
	child := *admitted.Session
	cell := mcpChildCell(t, r, child.ID)
	entry, err := r.mcpRoot(t.Context(), child, false)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := r.store.MCPInheritedTools(t.Context(), child.ID)
	if err != nil || len(captured) != 2 {
		t.Fatal("Ask spawn did not capture bounded tools", captured, err)
	}
	visible, _, err := r.mcpVisibleCatalog(t.Context(), child, entry)
	if err != nil || len(visible) != 0 {
		t.Fatal("Ask child exposed unapproved tools", visible, err)
	}
	if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "automatic", SessionID: owner.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
		t.Fatal(err)
	}
	visible, _, err = r.mcpVisibleCatalog(t.Context(), child, entry)
	if err != nil || len(visible["fixture"]) != 2 {
		t.Fatal("Full Access child catalog omitted callable tools", visible, err)
	}
	if r.mcpManager(entry) != manager || effects.Load() != 0 {
		t.Fatal("discovery started another connection or invoked a tool")
	}
	call := tool.Invocation{SessionID: child.ID, CellID: cell.ID, RequestID: "visible", Module: "mcp", Name: "call", Arguments: map[string]any{"server": "fixture", "tool": "visible", "arguments": map[string]any{}}}
	if _, _, err := r.tools.Call(t.Context(), call); err != nil {
		t.Fatal("discovered tool not callable", err)
	}
	if effects.Load() != 1 {
		t.Fatal("tool effect missing", effects.Load())
	}
	if _, err := r.mcpCatalog(t.Context(), child, session.MCPCatalogRequest{Action: "instructions", Server: "fixture"}); !errors.Is(err, store.ErrConflict) {
		t.Fatal("Full Access leaked separately scoped server instructions", err)
	}
	if _, err := r.MCPRefresh(t.Context(), child.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatal("child refreshed root connection", err)
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "future"}, func(context.Context, *sdkmcp.CallToolRequest, map[string]any) (*sdkmcp.CallToolResult, any, error) {
		effects.Add(1)
		return &sdkmcp.CallToolResult{}, nil, nil
	})
	if _, err := r.MCPReconnect(t.Context(), owner.ID, "fixture"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		visible, _, err = r.mcpVisibleCatalog(t.Context(), child, entry)
		_, futureErr := manager.ResolveTool("fixture", "future")
		if err != nil || len(visible["fixture"]) > 2 {
			t.Fatal("reconnect widened child", visible, err)
		}
		if futureErr == nil && len(visible["fixture"]) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reconnected catalog did not settle", visible)
		}
		time.Sleep(5 * time.Millisecond)
	}
	call.RequestID, call.Arguments["tool"] = "future", "future"
	if _, _, err := r.tools.Call(t.Context(), call); err == nil {
		t.Fatal("known but uncaptured tool bypassed discovery restriction")
	}
	if effects.Load() != 1 {
		t.Fatal("uncaptured call reached server")
	}
	again, err := r.SpawnChild(t.Context(), identity, request)
	if err != nil || again.Session.ID != child.ID {
		t.Fatal("catalog change broke exact spawn retry", again, err)
	}
	after, err := r.store.MCPInheritedTools(t.Context(), child.ID)
	if err != nil || !slices.Equal(captured, after) {
		t.Fatal("retry replaced child scope", after, err)
	}
	nested, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "grandchild"}, store.ChildRequest{ParentID: child.ID, Parts: request.Parts})
	if err != nil {
		t.Fatal(err)
	}
	nestedScope, err := r.store.MCPInheritedTools(t.Context(), nested.Session.ID)
	if err != nil || !slices.Equal(captured, nestedScope) {
		t.Fatal("descendant inherited new root tools", nestedScope, err)
	}
	request.GrantIDs = []session.GrantID{}
	restricted, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "restricted"}, request)
	if err != nil {
		t.Fatal(err)
	}
	visible, _, err = r.mcpVisibleCatalog(t.Context(), *restricted.Session, entry)
	if err != nil || len(visible) != 0 {
		t.Fatal("explicit empty grants gained full access catalog", visible, err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, directory, model.Scripted{})
	child, err = reopened.Session(t.Context(), child.ID)
	if err != nil {
		t.Fatal(err)
	}
	entry, err = reopened.mcpRoot(t.Context(), child, false)
	if err != nil {
		t.Fatal(err)
	}
	visible, _, err = reopened.mcpVisibleCatalog(t.Context(), child, entry)
	if err != nil || len(visible) != 0 || reopened.mcpManager(entry) != nil {
		t.Fatal("child discovery reconnected after restart", visible, err)
	}
	if _, err := reopened.MCPRefresh(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	awaitMCPReady(t, reopened, owner)
	entry, err = reopened.mcpRoot(t.Context(), child, false)
	if err != nil {
		t.Fatal(err)
	}
	visible, _, err = reopened.mcpVisibleCatalog(t.Context(), child, entry)
	if err != nil || len(visible["fixture"]) != 2 {
		t.Fatal("restart lost captured scope", visible, err)
	}
	// The name alone is not a delegation: replacing a tool's schema changes
	// its stable identity even when the server and tool names stay the same.
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "visible"}, func(context.Context, *sdkmcp.CallToolRequest, struct {
		Query string `json:"query"`
	},
	) (*sdkmcp.CallToolResult, any, error) {
		effects.Add(1)
		return &sdkmcp.CallToolResult{}, nil, nil
	})
	if _, err := reopened.MCPReconnect(t.Context(), owner.ID, "fixture"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		visible, _, err = reopened.mcpVisibleCatalog(t.Context(), child, entry)
		changed, resolveErr := reopened.mcpManager(entry).ResolveTool("fixture", "visible")
		if err != nil {
			t.Fatal(err)
		}
		changedScope := store.MCPToolScope{Capability: mcpCallCapability(changed), Resource: mcpCallResource(owner.ID, changed)}
		if resolveErr == nil && !slices.Contains(captured, changedScope) && len(visible["fixture"]) == 1 && visible["fixture"][0].Name == "hidden" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("changed tool schema retained child authority", visible, resolveErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMCPBothEnginesDefaultChildDiscoversAndCallsWithoutGrants(t *testing.T) {
	isolateMCP(t)
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			url, _, effects := mcpHTTPFixture(t)
			code := `print(mcp.list_tools(server="fixture"))
print(mcp.call(server="fixture",tool="visible",arguments={}))`
			if engine == session.QuickJS {
				code = `console.log(await mcp.list_tools({server:"fixture"})); console.log(await mcp.call({server:"fixture",tool:"visible",arguments:{}}));`
			}
			spawnCode := `child = agents.spawn(prompt="child_call", overrides={"report_mode":"notice"})
print(agents.wait_after_cell(input_ids=[child["input_id"]]))`
			if engine == session.QuickJS {
				spawnCode = `var child = await agents.spawn({prompt:"child_call",overrides:{report_mode:"notice"}}); console.log(await agents.wait_after_cell({input_ids:[child.input_id]}));`
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"child_call": code, "spawn": spawnCode}))
			owner := createEngineSession(t, r, engine)
			configureMCPFixture(t, r, url)
			if _, err := r.MCPRefresh(t.Context(), owner.ID); err != nil {
				t.Fatal(err)
			}
			awaitMCPReady(t, r, owner)
			if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "automatic", SessionID: owner.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, owner.ID, "spawn")
			parent := waitTestWithin(t, r, "spawn", terminal, 30*time.Second)
			if parent.Turn.State != session.Succeeded {
				t.Fatal(parent.Turn)
			}
			parentOps, err := r.Operations(t.Context(), parent.Turn.ID, "", 20)
			if err != nil || len(parentOps) != 2 {
				t.Fatal(parentOps, err)
			}
			var spawn session.Operation
			for _, op := range parentOps {
				if op.Capability == "agents.spawn" {
					spawn = op
				}
			}
			admission, err := r.Admission(t.Context(), session.RequestIdentity{ClientID: "operation", RequestID: string(spawn.ID)})
			if err != nil || admission.Turn.State != session.Succeeded {
				t.Fatal(admission, err)
			}
			operations, err := r.Operations(t.Context(), admission.Turn.ID, "", 20)
			if err != nil || len(operations) != 2 {
				t.Fatal(operations, err)
			}
			for _, op := range operations {
				if op.SessionID != admission.Input.SessionID || op.State != session.OperationSucceeded || op.GrantID != nil {
					t.Fatal("default MCP child used wrong authority", op)
				}
				if op.Capability == "mcp.catalog" && !strings.Contains(string(op.Result.Value), "visible") {
					t.Fatal("callable tool missing from discovery", op)
				}
				if op.Capability == "mcp.call.trusted" && op.PermissionRevision == nil {
					t.Fatal("call lost live policy evidence", op)
				}
			}
			if effects.Load() != 1 {
				t.Fatal("incorrect child effects", effects.Load())
			}
		})
	}
}
