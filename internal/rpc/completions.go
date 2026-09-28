package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchCompletion(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "completions.list":
		return decode(raw, func(p protocol.ListCompletionsParams) (any, error) {
			var after session.SessionID
			if p.After != nil {
				after = session.SessionID(*p.After)
			}
			values, err := r.ListPendingCompletions(ctx, session.SessionID(p.ParentID), after, p.Limit)
			result := protocol.ListCompletionsResult{Items: []protocol.CompletionMetadata{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.CompletionFromDomain(value))
			}
			return result, err
		})
	case "completions.read":
		return decode(raw, func(p protocol.ReadCompletionParams) (any, error) {
			value, err := r.ReadPendingCompletion(ctx, session.SessionID(p.ParentID), session.SessionID(p.ChildID), session.TurnID(p.TurnID), int64(p.Offset), p.Length)
			result := protocol.ReadCompletionResult{
				Completion: protocol.CompletionFromDomain(value.Completion), Offset: protocol.Counter(value.Offset),
				TotalBytes: protocol.Counter(value.TotalBytes), DataBase64: base64.StdEncoding.EncodeToString(value.Data),
			}
			if value.NextOffset != nil {
				result.NextOffset = new(protocol.Counter(*value.NextOffset))
			}
			return result, err
		})
	default:
		return nil, ErrMethod
	}
}
