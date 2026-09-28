package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchResource(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "resources.list":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			values, err := r.Resources(ctx, session.SessionID(p.SessionID))
			if err != nil {
				return nil, err
			}
			result := protocol.ResourcesResult{Items: []protocol.ResourceUsage{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.ResourceUsageFromDomain(value))
			}
			return result, nil
		})
	case "resources.set":
		return decode(raw, func(p protocol.SetResourceParams) (any, error) {
			value, err := r.SetResource(ctx, session.SessionID(p.SessionID), int64(p.ExpectedRevision), p.Resource.Domain())
			return protocol.ResourceUsageFromDomain(value), err
		})
	default:
		return nil, ErrMethod
	}
}
