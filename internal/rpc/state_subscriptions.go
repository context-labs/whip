package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchStateSubscription(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "state.subscribe":
		return decode(raw, func(p protocol.SubscribeStateParams) (any, error) {
			value, err := r.SubscribeState(ctx, session.SessionID(p.SessionID), string(p.SubscriptionID), session.StateSubscribe{Key: p.Key, After: int64(p.After), Delivery: session.MailDelivery(p.Delivery)})
			return protocol.StateSubscriptionFromDomain(value), err
		})
	case "state.unsubscribe":
		return decode(raw, func(p protocol.UnsubscribeStateParams) (any, error) {
			value, err := r.UnsubscribeState(ctx, session.SessionID(p.SessionID), string(p.SubscriptionID))
			return protocol.StateSubscriptionFromDomain(value), err
		})
	case "state.subscriptions":
		return decode(raw, func(p protocol.StateSubscriptionsParams) (any, error) {
			after := ""
			if p.After != nil {
				after = string(*p.After)
			}
			values, err := r.StateSubscriptions(ctx, session.SessionID(p.SessionID), after, p.Limit)
			if err != nil {
				return nil, err
			}
			result := protocol.StateSubscriptionsResult{Items: []protocol.StateSubscription{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.StateSubscriptionFromDomain(value))
			}
			return result, nil
		})
	default:
		return nil, ErrMethod
	}
}
