package acp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBridgeMCPPermissionKeepsConcreteArguments(t *testing.T) {
	backend := newFakeBackend(t)
	client := &fakeACPClient{answer: optAllowAlways}
	fixture := newACPFixture(t, backend, client)
	fixture.initialize(t)
	id := fixture.newSession(t)
	detail := "MCP server \"billing\", tool \"delete.invoice\", source \"ACP attachment\"\nArguments: {\"invoice\":\"TARGET42\"}"
	payload, _ := json.Marshal(pendingPermission{PermissionID: "mcp-permission", OperationID: "operation", Operation: "mcp.call", Command: detail, Rule: "opaque-definition-rule"})
	fixture.bridge.mu.Lock()
	current := fixture.bridge.sessions[id]
	fixture.bridge.mu.Unlock()
	fixture.bridge.handlePermission(current, payload)
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.perms) != 1 {
		t.Fatalf("prompts=%d", len(client.perms))
	}
	request := client.perms[0]
	if request.ToolCall.Title == nil || !strings.Contains(*request.ToolCall.Title, "delete.invoice") || strings.Contains(*request.ToolCall.Title, "Arguments:") {
		t.Fatalf("title=%v", request.ToolCall.Title)
	}
	if len(request.ToolCall.Content) != 1 || request.ToolCall.Content[0].Content == nil || request.ToolCall.Content[0].Content.Content.Text == nil || request.ToolCall.Content[0].Content.Content.Text.Text != detail {
		t.Fatalf("concrete request content=%+v", request.ToolCall.Content)
	}
	if len(request.Options) != 3 || request.Options[2].Name != "Always allow this MCP tool and server definition in this tree" {
		t.Fatalf("options=%+v", request.Options)
	}
	backend.mu.Lock()
	root := backend.roots[string(id)]
	backend.mu.Unlock()
	root.mu.Lock()
	defer root.mu.Unlock()
	if root.decisions != 1 || root.remember != "tree" {
		t.Fatalf("decisions=%d remember=%q", root.decisions, root.remember)
	}
}
