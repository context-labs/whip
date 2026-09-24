package daemon

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/protocol"

	"github.com/context-labs/whip/internal/session"
)

type SubscribeParams = protocol.SubscribeParams

type SubscribeResult = protocol.SubscribeResult

type UnsubscribeParams = protocol.UnsubscribeParams

type subscription struct {
	id     string
	rootID string
	cancel context.CancelFunc
}

func (s *Server) subscribe(c *serverConn, params SubscribeParams) (SubscribeResult, error) {
	if params.RootID == "" || params.SubscriptionID == "" {
		return SubscribeResult{}, errors.New("root and subscription IDs are required")
	}
	// Validate the requested boundary before acknowledging the stream.
	if _, _, err := s.daemon.store.ReplayEvents(c.ctx, params.RootID, params.Cursor, 1); err != nil {
		return SubscribeResult{}, err
	}
	ctx, cancel := context.WithCancel(c.ctx)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		cancel()
		return SubscribeResult{}, ErrStopped
	}
	if _, exists := c.subscriptions[params.SubscriptionID]; exists {
		c.mu.Unlock()
		cancel()
		return SubscribeResult{}, rpcFailure(-32009, "subscription ID already active")
	}
	if len(c.subscriptions) >= MaxSubscriptions {
		c.mu.Unlock()
		cancel()
		return SubscribeResult{}, rpcFailure(-32002, "subscription limit reached")
	}
	sub := &subscription{id: params.SubscriptionID, rootID: params.RootID, cancel: cancel}
	c.subscriptions[sub.id] = sub
	c.mu.Unlock()
	if !s.goWorker(func() { s.pumpSubscription(ctx, c, sub, params.Cursor) }) {
		c.unsubscribe(sub.id)
		return SubscribeResult{}, ErrStopped
	}
	return SubscribeResult{SubscriptionID: sub.id, Cursor: params.Cursor}, nil
}

func (c *serverConn) unsubscribe(id string) {
	c.mu.Lock()
	sub := c.subscriptions[id]
	delete(c.subscriptions, id)
	c.mu.Unlock()
	if sub != nil {
		sub.cancel()
	}
}

func (s *Server) pumpSubscription(ctx context.Context, c *serverConn, sub *subscription, cursor int64) {
	defer func() {
		c.mu.Lock()
		if c.subscriptions[sub.id] == sub {
			delete(c.subscriptions, sub.id)
		}
		c.mu.Unlock()
		sub.cancel()
	}()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := s.replayContext(ctx, ReplayParams{RootID: sub.rootID, Cursor: cursor, Limit: 100})
		if ctx.Err() != nil {
			return
		}
		if err != nil || result.Expired {
			failure := rpcFromError(err)
			if result.Expired {
				failure = rpcFromError(session.ErrCursorExpired)
			}
			c.notify("subscription.failed", struct {
				SubscriptionID string    `json:"subscription_id"`
				RootID         string    `json:"root_id"`
				Error          *RPCError `json:"error"`
			}{SubscriptionID: sub.id, RootID: sub.rootID, Error: failure})
			return
		}
		for _, event := range result.Events {
			if ctx.Err() != nil {
				return
			}
			event.SubscriptionID = sub.id
			if !c.notify("event", eventNotification{Event: event}) {
				return
			}
			cursor = event.Seq
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Client) Subscribe(ctx context.Context, rootID string, cursor int64) (SubscribeResult, error) {
	id := rand.Text()
	c.mu.Lock()
	previous := c.subscriptions[rootID]
	c.subscriptions[rootID] = id
	c.mu.Unlock()
	if previous != "" {
		if err := c.Unsubscribe(ctx, previous); err != nil {
			return SubscribeResult{}, err
		}
	}
	var result SubscribeResult
	err := c.Call(ctx, "events.subscribe", SubscribeParams{RootID: rootID, SubscriptionID: id, Cursor: cursor}, &result)
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
	if err := c.Call(ctx, "events.unsubscribe", UnsubscribeParams{SubscriptionID: id}, nil); err != nil {
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
