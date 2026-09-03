package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/context-labs/whip/internal/llm"
)

// ToolProvider is the dispatcher-backed surface exposed over MCP. Production
// uses a daemon adapter; tests may use bound in-process services.
type ToolProvider interface {
	ToolDefinitions(context.Context) ([]llm.Tool, error)
	CallTool(context.Context, string, json.RawMessage) (string, error)
}

// Serve runs whip's built-in tools as an MCP server over stdio — the other
// direction of the integration: any MCP-capable harness (claude-code, codex,
// another whip) can drive whip's read/bash/edit/write with
//
//	whip mcp serve
//
// registered as a stdio server. The model-facing `rlm_exec` tool is not part
// of this restricted protocol endpoint. Callers use the raw definitions.
func Serve(ctx context.Context, version string, provider ToolProvider) error {
	if provider == nil {
		return errors.New("mcp serve requires a tool provider")
	}
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "whip", Version: version}, nil)
	definitions, err := provider.ToolDefinitions(ctx)
	if err != nil {
		return fmt.Errorf("mcp serve schemas: %w", err)
	}
	for _, definition := range definitions {
		srv.AddTool(&sdkmcp.Tool{
			Name:        definition.Function.Name,
			Description: definition.Function.Description,
			// The defs carry a JSON-schema string; the SDK wants any value
			// that marshals to a schema, and json.RawMessage marshals verbatim.
			InputSchema: definition.Function.Parameters,
		}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			out, err := provider.CallTool(ctx, definition.Function.Name, req.Params.Arguments)
			isError := err != nil
			if err != nil {
				out = "Error: " + err.Error() // errors are tool output, not protocol failures
			}
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: out}}, IsError: isError,
			}, nil
		})
	}
	if err := srv.Run(ctx, &sdkmcp.StdioTransport{}); err != nil {
		return fmt.Errorf("mcp serve: %w", err)
	}
	return nil
}
