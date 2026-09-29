package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func dispatchReceiptMatch(ctx context.Context, r *runtime.Runtime, raw json.RawMessage) (any, error) {
	return decode(raw, func(p protocol.MatchReceiptParams) (any, error) {
		if len(p.ParamsBase64) > base64.StdEncoding.EncodedLen(4<<20) {
			return nil, session.ErrInvalid
		}
		params, err := base64.StdEncoding.Strict().DecodeString(p.ParamsBase64)
		if err != nil {
			return nil, session.ErrInvalid
		}
		for _, op := range protocol.Operations() {
			if op.Name == p.Method {
				if err := protocol.Validate(op.Params.Name(), params); err != nil {
					return nil, fmt.Errorf("%w: %w", session.ErrInvalid, err)
				}
				break
			}
		}
		switch p.Method {
		case "sessions.submit":
			return decode(params, func(p protocol.SubmitParams) (any, error) {
				value, err := r.MatchSubmission(ctx, identity(p.Identity), submissionRequest(p))
				return admission(value), err
			})
		case "sessions.compact":
			return decode(params, func(p protocol.CompactParams) (any, error) {
				value, err := r.MatchSubmission(ctx, identity(p.Identity), store.Submission{SessionID: session.SessionID(p.SessionID), Source: session.UserInput, Kind: session.CompactInput, Parts: []session.Part{}})
				return admission(value), err
			})
		case "sessions.spawn":
			return decode(params, func(p protocol.SpawnSessionParams) (any, error) {
				request, err := childRequest(p)
				if err != nil {
					return nil, err
				}
				value, err := r.MatchChild(ctx, identity(p.Identity), request)
				return admission(value), err
			})
		case "goals.formulate":
			return decode(params, func(p protocol.FormulateGoalParams) (any, error) {
				value, err := r.MatchGoalFormulation(ctx, identity(p.Identity), session.SessionID(p.SessionID), p.Request.Domain())
				return admission(value), err
			})
		case "goals.resume":
			return decode(params, func(p protocol.ResumeGoalParams) (any, error) {
				value, err := r.MatchGoalResume(ctx, identity(p.Identity), session.SessionID(p.SessionID), p.Goal.Domain())
				return admission(value), err
			})
		case "tool.call":
			return decode(params, func(p protocol.CallHostToolParams) (any, error) {
				operation, err := hostToolOperation(p)
				if err != nil {
					return nil, err
				}
				value, err := r.MatchHostOperation(ctx, identity(p.Identity), session.SessionID(p.SessionID), operation)
				return admission(value), err
			})
		case "shell.run":
			return decode(params, func(p protocol.RunShellParams) (any, error) {
				operation, err := shellOperation(p)
				if err != nil {
					return nil, err
				}
				value, err := r.MatchHostOperation(ctx, identity(p.Identity), session.SessionID(p.SessionID), operation)
				return admission(value), err
			})
		default:
			return nil, session.ErrInvalid
		}
	})
}
