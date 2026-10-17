package client

import (
	"context"

	"github.com/context-labs/whip/internal/protocol"
)

func (c *Client) CompleteWorkspace(ctx context.Context, p protocol.CompletionParams) (protocol.CompletionResult, error) {
	var result protocol.CompletionResult
	err := c.Call(ctx, "workspace.complete", p, &result)
	return result, err
}

func (c *RootClient) CompleteWorkspace(ctx context.Context, p protocol.CompletionParams) (protocol.CompletionResult, error) {
	var result protocol.CompletionResult
	err := c.providerCall(ctx, "workspace.complete", p, &result)
	return result, err
}
