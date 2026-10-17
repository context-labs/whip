package client

import (
	"context"
	"errors"

	"github.com/context-labs/whip/internal/protocol"
)

func (c *Client) Query(ctx context.Context, params protocol.QueryParams) (protocol.QueryResult, error) {
	var result protocol.QueryResult
	err := c.Call(ctx, "query", params, &result)
	if err == nil && result.Content != nil {
		if result.RootID != params.RootID {
			return protocol.QueryResult{}, errors.New("query content root does not match request")
		}
		command := protocol.CommandResult{Content: result.Content, Operation: params.Operation}
		if err := c.commandContent(ctx, result.RootID, &command); err != nil {
			return protocol.QueryResult{}, err
		}
		result.Result = command.Result
	}
	return result, err
}
