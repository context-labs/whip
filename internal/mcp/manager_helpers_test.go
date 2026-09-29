package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

func testCall(ctx context.Context, m *Manager, server, tool string, args json.RawMessage) (capability.MCPResult, error) {
	call, err := m.ResolveTool(server, tool)
	if err != nil {
		return capability.MCPResult{}, err
	}
	call.Arguments = args
	return m.CallChecked(ctx, call, nil)
}

func callTestTool(t *testing.T, m *Manager, server, tool string, args json.RawMessage) string {
	t.Helper()
	result, err := testCall(t.Context(), m, server, tool, args)
	if err != nil {
		t.Errorf("MCP tool %s.%s: %v", server, tool, err)
	}
	return result.Text
}

func listTestTools(t *testing.T, m *Manager, server string) []Tool {
	t.Helper()
	tools, err := m.ListTools(server)
	if err != nil {
		t.Fatal(err)
	}
	return tools
}
