package acp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBridgeMCPPermissionKeepsConcreteArguments(t *testing.T) {
	var calls atomic.Int32
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "billing", Version: "1"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "delete_invoice", Description: "delete invoice"}, func(context.Context, *sdkmcp.CallToolRequest, map[string]any) (*sdkmcp.CallToolResult, any, error) {
		calls.Add(1)
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "deleted"}}}, nil, nil
	})
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil))
	t.Cleanup(httpServer.Close)
	f := nativeFixture(t, codeProvider(`mcp.list_tools(server="billing")`+"\n"+`print(mcp.call(server="billing",tool="delete_invoice",arguments={"invoice":"TARGET42"}))`), &fakeACPClient{answer: optAllowAlways})
	created, err := f.conn.NewSession(t.Context(), acpsdk.NewSessionRequest{Cwd: f.cwd, McpServers: []acpsdk.McpServer{{Http: &acpsdk.McpServerHttpInline{Type: "http", Name: "billing", Url: httpServer.URL, Headers: []acpsdk.HttpHeader{}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("attachment executed a tool")
	}
	if _, err := f.conn.SetSessionMode(t.Context(), acpsdk.SetSessionModeRequest{SessionId: created.SessionId, ModeId: ModeAuto}); err != nil {
		t.Fatal(err)
	}
	f.prompt(t, created.SessionId, "MCP call")
	if calls.Load() != 1 {
		t.Fatalf("MCP calls=%d", calls.Load())
	}
	f.editor.mu.Lock()
	defer f.editor.mu.Unlock()
	sawCall := false
	for _, request := range f.editor.perms {
		if strings.Contains(*request.ToolCall.Title, "mcp.call") {
			sawCall = true
			if len(request.ToolCall.Content) != 1 || !strings.Contains(request.ToolCall.Content[0].Content.Content.Text.Text, "TARGET42") || len(request.Options) != 3 {
				t.Fatalf("lost concrete intent: %+v", request)
			}
		}
	}
	if !sawCall {
		t.Fatalf("Full Access bypassed untrusted MCP approvals: %+v", f.editor.perms)
	}
}
