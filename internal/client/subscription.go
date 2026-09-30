package client

import (
	"context"
	"crypto/rand"

	"github.com/context-labs/whip/internal/protocol"
)

func (c *Client) Subscribe(ctx context.Context, rootID string, cursor int64) (protocol.SubscribeResult, error) {
	id := rand.Text()
	c.mu.Lock()
	previous := c.subscriptions[rootID]
	c.subscriptions[rootID] = id
	c.mu.Unlock()
	if previous != "" {
		if err := c.Unsubscribe(ctx, previous); err != nil {
			return protocol.SubscribeResult{}, err
		}
	}
	var result protocol.SubscribeResult
	err := c.Call(ctx, "events.subscribe", protocol.SubscribeParams{RootID: rootID, SubscriptionID: id, Cursor: cursor}, &result)
	if err != nil {
		c.mu.Lock()
		if c.subscriptions[rootID] == id {
			delete(c.subscriptions, rootID)
		}
		c.mu.Unlock()
	}
	return result, err
}

func (c *Client) Unsubscribe(ctx context.Context, id string) error {
	if err := c.Call(ctx, "events.unsubscribe", protocol.UnsubscribeParams{SubscriptionID: id}, nil); err != nil {
		return err
	}
	c.mu.Lock()
	for root, active := range c.subscriptions {
		if active == id {
			delete(c.subscriptions, root)
		}
	}
	c.mu.Unlock()
	return nil
}
