package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type failedOutputProvider struct{}

func (failedOutputProvider) ToolDefinitions(context.Context) ([]Definition, error) {
	return []Definition{newDefinition("fixture", "fixture", `{"type":"object"}`)}, nil
}

func (failedOutputProvider) CallTool(context.Context, string, json.RawMessage) (string, error) {
	return `{"state":"failed","value":"bounded settled evidence"}`, errors.New("confirmed failure")
}

func TestServeTransportPreservesConfirmedFailureEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- ServeTransport(ctx, "fixture", failedOutputProvider{}, serverTransport) }()
	c, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "fixture", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("MCP transport did not close")
		}
	})
	result, err := c.CallTool(ctx, &sdkmcp.CallToolParams{Name: "fixture", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || len(result.Content) != 1 {
		t.Fatal(result)
	}
	text := result.Content[0].(*sdkmcp.TextContent).Text
	if !strings.Contains(text, "bounded settled evidence") || !strings.Contains(text, "confirmed failure") {
		t.Fatal(text)
	}
}
