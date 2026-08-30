package mcp

import (
	"context"
	"errors"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/context-labs/whip/internal/tools"
)

// Serve runs whip's built-in tools as an MCP server over stdio — the other
// direction of the integration: any MCP-capable harness (claude-code, codex,
// another whip) can drive whip's read/bash/edit/write with
//
//	whip mcp serve
//
// registered as a stdio server. The `task` tool is excluded (no subagent
// recursion over MCP). Callers use the raw llm definitions verbatim.
func Serve(ctx context.Context, version string, services *tools.Services) error {
	if services == nil || services.ProcessOptions().Processes == nil {
		return errors.New("mcp serve requires scoped tool services")
	}
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "whip", Version: version}, nil)
	for _, t := range tools.AllWithServices(services) {
		srv.AddTool(&sdkmcp.Tool{
			Name:        t.Def.Function.Name,
			Description: t.Def.Function.Description,
			// The defs carry a JSON-schema string; the SDK wants any value
			// that marshals to a schema, and json.RawMessage marshals verbatim.
			InputSchema: t.Def.Function.Parameters,
		}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			out, err := t.Run(ctx, req.Params.Arguments)
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
