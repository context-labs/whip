package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchActivity(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "inputs.recent_text":
		return decode(raw, func(p protocol.RecentInputTextParams) (any, error) {
			var before int64
			if p.Before != nil {
				before = int64(*p.Before)
			}
			value, err := r.RecentInputText(ctx, before, p.Limit)
			return protocol.InputTextPageFromDomain(value), err
		})
	case "inputs.steer":
		return decode(raw, func(p protocol.SteerInputParams) (any, error) {
			value, err := r.SteerInput(ctx, session.SteerInputRequest{ID: session.InputSteeringID(p.EditID), SessionID: session.SessionID(p.SessionID), InputID: session.InputID(p.InputID), TurnID: session.TurnID(p.TurnID)})
			return protocol.InputSteeringFromDomain(value), err
		})
	case "inputs.steering":
		return decode(raw, func(p protocol.InputSteeringParams) (any, error) {
			value, err := r.InputSteering(ctx, session.SessionID(p.SessionID), session.InputSteeringID(p.EditID))
			return protocol.InputSteeringFromDomain(value), err
		})
	case "sessions.activity":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			value, err := r.Activity(ctx, session.SessionID(p.SessionID))
			return protocol.ActivityFromDomain(value), err
		})
	case "inputs.page":
		return decode(raw, func(p protocol.InputPageParams) (any, error) {
			var after int64
			if p.After != nil {
				after = int64(*p.After)
			}
			value, err := r.InputPage(ctx, session.SessionID(p.SessionID), p.State, after, p.Limit)
			return protocol.InputPageFromDomain(value), err
		})
	case "inputs.get":
		return decode(raw, func(p protocol.SessionInputParams) (any, error) {
			value, err := r.SessionInput(ctx, session.SessionID(p.SessionID), session.InputID(p.InputID))
			return protocol.InputFromDomain(value), err
		})
	default:
		return nil, ErrMethod
	}
}
