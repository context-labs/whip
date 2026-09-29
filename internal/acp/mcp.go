package acp

import (
	"context"
	"errors"
	"fmt"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/protocol"
)

func (b *Bridge) attachMCP(ctx context.Context, owner protocol.ID, servers []acp.McpServer) error {
	if len(servers) == 0 {
		return nil
	}
	if len(servers) > 64 {
		return errors.New("ACP exceeds 64 MCP server attachments")
	}
	values := make(map[string]protocol.MCPServerInput, len(servers))
	for _, server := range servers {
		var name string
		value := protocol.MCPServerInput{Command: []string{}, Env: map[string]string{}, Headers: map[string]string{}}
		switch {
		case server.Stdio != nil:
			name = server.Stdio.Name
			value.Command = append([]string{server.Stdio.Command}, server.Stdio.Args...)
			for _, entry := range server.Stdio.Env {
				value.Env[entry.Name] = entry.Value
			}
		case server.Http != nil:
			name = server.Http.Name
			value.URL = server.Http.Url
			for _, entry := range server.Http.Headers {
				value.Headers[entry.Name] = entry.Value
			}
		default:
			return errors.New("ACP supports only stdio and HTTP MCP attachments")
		}
		if name == "" {
			return errors.New("MCP server name is required")
		}
		if _, found := values[name]; found {
			return fmt.Errorf("duplicate MCP attachment %q", name)
		}
		values[name] = value
	}
	var result protocol.MCPRefreshResult
	return b.client.Call(ctx, "mcp.attach", protocol.MCPAttachParams{SessionID: owner, Servers: values}, &result)
}
