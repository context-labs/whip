package daemon

import "context"

func (c *Client) CompleteWorkspace(ctx context.Context, p CompletionParams) (CompletionResult, error) {
	var result CompletionResult
	err := c.Call(ctx, "workspace.complete", p, &result)
	return result, err
}

func (c *RootClient) CompleteWorkspace(ctx context.Context, p CompletionParams) (CompletionResult, error) {
	var result CompletionResult
	err := c.providerCall(ctx, "workspace.complete", p, &result)
	return result, err
}
