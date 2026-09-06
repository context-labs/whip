package daemon

import (
	"context"

	"github.com/context-labs/whip/internal/protocol"
)

// invoke never persists ephemeral credentials or terminal input. A lost reply
// leaves the caller uncertain and must not cause automatic re-delivery.
func (s *Server) invoke(ctx context.Context, params protocol.QueryParams) (protocol.QueryResult, error) {
	operation, ok := protocol.LookupRuntime(params.Operation)
	if !ok || operation.Execution != protocol.Ephemeral {
		return protocol.QueryResult{}, rpcFailure(-32601, "operation is not ephemeral")
	}
	if err := protocol.ValidateRuntime(params.Operation, params.Payload); err != nil {
		return protocol.QueryResult{}, rpcFailure(-32602, err.Error())
	}
	root, err := s.daemon.Open(params.RootID)
	if err != nil {
		return protocol.QueryResult{}, err
	}
	output, err := routeControlValue(root, ctx, func(actorCtx context.Context) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return root.applyClientCommand(ctx, params.Operation, params.Payload)
	})
	if err != nil {
		return protocol.QueryResult{}, err
	}
	return protocol.QueryResult{Result: encodeCommandOutcome(params.Operation, output, nil)}, nil
}

func (c *Client) Invoke(ctx context.Context, params protocol.QueryParams) (protocol.QueryResult, error) {
	var result protocol.QueryResult
	err := c.Call(ctx, "operation.invoke", params, &result)
	return result, err
}
