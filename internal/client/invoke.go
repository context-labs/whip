package client

import (
	"context"

	"github.com/context-labs/whip/internal/protocol"
)

func (c *Client) Invoke(ctx context.Context, params protocol.QueryParams) (protocol.QueryResult, error) {
	var result protocol.QueryResult
	err := c.Call(ctx, "operation.invoke", params, &result)
	return result, err
}
