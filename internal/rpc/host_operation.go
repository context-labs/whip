package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchHostOperation(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "tool.schemas":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			values, err := r.HostToolSchemas(ctx, session.SessionID(p.SessionID))
			result := protocol.HostToolSchemasResult{Items: []protocol.HostToolSchema{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.HostToolSchema{Module: value.Module, Name: protocol.ID(value.Name), Description: value.Description, InputSchema: value.InputSchema})
			}
			return result, err
		})
	case "tool.call":
		return decode(raw, func(p protocol.CallHostToolParams) (any, error) {
			if len(p.Operation.ArgumentsBase64) > base64.StdEncoding.EncodedLen(session.MaxDocumentBytes/2) {
				return nil, session.ErrInvalid
			}
			arguments, err := base64.StdEncoding.Strict().DecodeString(p.Operation.ArgumentsBase64)
			if err != nil {
				return nil, session.ErrInvalid
			}
			result, err := r.AdmitHostOperation(ctx, identity(p.Identity), session.SessionID(p.SessionID), session.HostOperation{Module: p.Operation.Module, Name: string(p.Operation.Name), Arguments: arguments})
			return admission(result), err
		})
	case "shell.run":
		return decode(raw, func(p protocol.RunShellParams) (any, error) {
			args := map[string]any{"command": p.Command, "interactive": p.Interactive}
			if p.Timeout != nil {
				args["timeout"] = *p.Timeout
			}
			arguments, err := json.Marshal(args)
			if err != nil {
				return nil, err
			}
			result, err := r.AdmitHostOperation(ctx, identity(p.Identity), session.SessionID(p.SessionID), session.HostOperation{Module: "shell", Name: "run", Arguments: arguments})
			return admission(result), err
		})
	default:
		return nil, ErrMethod
	}
}
