package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchObservation(ctx context.Context, r *runtime.Runtime, raw json.RawMessage) (any, error) {
	return decode(raw, func(p protocol.HistoryParams) (any, error) {
		value, err := r.Observe(ctx, session.SessionID(p.SessionID), int64(p.After), p.Limit)
		if err != nil {
			return nil, err
		}
		result := protocol.SessionObservation{Epoch: protocol.ID(value.Epoch), Messages: []protocol.Message{}}
		for _, message := range value.Messages {
			result.Messages = append(result.Messages, protocol.MessageFromDomain(message))
		}
		if preview := value.Preview; preview != nil {
			result.Preview = &protocol.MessagePreview{AttemptID: protocol.ID(preview.AttemptID), TurnID: protocol.ID(preview.TurnID), MessageID: protocol.ID(preview.MessageID), Revision: protocol.Counter(preview.Revision), Text: preview.Text, Calls: []protocol.CallPreview{}, Truncated: preview.Truncated}
			for _, call := range preview.Calls {
				result.Preview.Calls = append(result.Preview.Calls, protocol.CallPreview{Index: call.Index, ID: call.ID, Name: call.Name, Arguments: call.Arguments})
			}
		}
		return result, nil
	})
}
