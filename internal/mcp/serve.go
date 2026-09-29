package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolProvider is the dispatcher-backed surface exposed over MCP. Production
// uses a native host client; tests may use bounded protocol fixtures.
type ToolProvider interface {
	ToolDefinitions(context.Context) ([]Definition, error)
	CallTool(context.Context, string, json.RawMessage) (string, error)
}

// Serve runs whip's built-in tools as an MCP server over stdio — the other
// direction of the integration: any MCP-capable harness (claude-code, codex,
// another whip) can drive whip's read/bash/edit/write with
//
//	whipcode mcp serve
//
// registered as a stdio server. The model-facing `rlm_exec` tool is not part
// of this restricted protocol endpoint. Callers use the raw definitions.
func Serve(ctx context.Context, version string, provider ToolProvider) error {
	return ServeTransport(ctx, version, provider, &sdkmcp.StdioTransport{})
}

// ServeTransport borrows one transport. The caller owns bounded I/O and shutdown.
func ServeTransport(ctx context.Context, version string, provider ToolProvider, transport sdkmcp.Transport) error {
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
				if out != "" {
					out += "\n"
				}
				out += "Error: " + err.Error() // errors are tool output, not protocol failures
			}
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: out}}, IsError: isError,
			}, nil
		})
	}
	if err := srv.Run(ctx, transport); err != nil {
		return fmt.Errorf("mcp serve: %w", err)
	}
	return nil
}
