package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

type StateSubscription struct {
	ID          ID      `json:"id"`
	TreeID      ID      `json:"tree_id"`
	SessionID   ID      `json:"session_id"`
	Key         string  `json:"key"`
	Delivery    string  `json:"delivery" enum:"queued,steer,next_turn"`
	Cursor      Counter `json:"cursor"`
	CancelledAt *string `json:"cancelled_at"`
	CreatedAt   string  `json:"created_at"`
}

type SubscribeStateParams struct {
	SubscriptionID ID      `json:"subscription_id"`
	SessionID      ID      `json:"session_id"`
	Key            string  `json:"key"`
	After          Counter `json:"after"`
	Delivery       string  `json:"delivery" enum:"queued,steer,next_turn"`
}

type StateSubscriptionsParams struct {
	SessionID ID  `json:"session_id"`
	After     *ID `json:"after,omitempty"`
	Limit     int `json:"limit" min:"1" max:"100"`
}

type StateSubscriptionsResult struct {
	Items []StateSubscription `json:"items"`
}

type UnsubscribeStateParams struct {
	SessionID      ID `json:"session_id"`
	SubscriptionID ID `json:"subscription_id"`
}

func StateSubscriptionFromDomain(v session.StateSubscription) StateSubscription {
	return StateSubscription{ID: ID(v.ID), TreeID: ID(v.TreeID), SessionID: ID(v.SessionID), Key: v.Key, Delivery: string(v.Delivery), Cursor: Counter(v.Cursor), CancelledAt: timeString(v.CancelledAt), CreatedAt: v.CreatedAt.Format(time.RFC3339Nano)}
}
