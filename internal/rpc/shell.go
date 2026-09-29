package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/shell"
)

func dispatchShell(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "shell.interaction":
		return decode(raw, func(p protocol.ShellInteractionParams) (any, error) {
			value, err := r.ShellInteraction(ctx, session.SessionID(p.SessionID), int64(p.Cursor))
			result := protocol.ShellInteractionResult{}
			if value != nil {
				result.Interaction = &protocol.ShellInteraction{OperationID: protocol.ID(value.OperationID), StartedAt: value.Started.Format(time.RFC3339Nano), DataBase64: base64.StdEncoding.EncodeToString(value.Output), From: protocol.Counter(value.From), Through: protocol.Counter(value.Through), NextInput: protocol.Counter(value.NextInput), SecondsLeft: value.SecondsLeft}
			}
			return result, err
		})
	case "shell.input":
		return decode(raw, func(p protocol.ShellInputParams) (any, error) {
			if len(p.DataBase64) > base64.StdEncoding.EncodedLen(shell.MaxInputBytes) {
				return nil, session.ErrInvalid
			}
			data, err := base64.StdEncoding.Strict().DecodeString(p.DataBase64)
			if err != nil {
				return nil, session.ErrInvalid
			}
			err = r.ShellInput(ctx, session.SessionID(p.SessionID), session.OperationID(p.OperationID), int64(p.Sequence), data)
			return protocol.ShellInputResult{Sequence: p.Sequence}, err
		})
	default:
		return nil, ErrMethod
	}
}
