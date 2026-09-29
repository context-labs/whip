package browserhost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"

	"github.com/go-rod/rod/lib/cdp"
)

// CDPClient adapts one live batch to Rod. It owns one event pump and bounded
// serialized calls, no socket or browser lifetime. Close cancels and joins all
// borrowed calls before the tab reservation can be released.
type CDPClient struct {
	batch   *Batch
	ctx     context.Context
	cancel  context.CancelFunc
	events  chan *cdp.Event
	slot    chan struct{}
	mu      sync.Mutex
	closed  bool
	pending int
	wg      sync.WaitGroup
	once    sync.Once
}

func (b *Batch) NewCDPClient() (*CDPClient, error) {
	h := b.lease.capture.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if b.closed || b.ctx.Err() != nil || b.client != nil {
		return nil, ErrStale
	}
	ctx, cancel := context.WithCancel(b.ctx)
	c := &CDPClient{batch: b, ctx: ctx, cancel: cancel, events: make(chan *cdp.Event, 1), slot: make(chan struct{}, 1)}
	b.client = c
	attachmentID := b.lease.attachment.value.Scope.AttachmentID
	c.wg.Go(func() {
		defer close(c.events)
		for {
			event, e := b.NextEvent(ctx)
			if e != nil {
				return
			}
			wire := &cdp.Event{SessionID: attachmentID, Method: event.Method, Params: event.Params}
			select {
			case c.events <- wire:
			case <-ctx.Done():
				return
			}
		}
	})
	return c, nil
}
func (c *CDPClient) Event() <-chan *cdp.Event { return c.events }
func (c *CDPClient) Close() {
	c.once.Do(func() { c.mu.Lock(); c.closed = true; c.cancel(); c.mu.Unlock(); c.wg.Wait() })
}

func (c *CDPClient) Call(ctx context.Context, sessionID, method string, params any) ([]byte, error) {
	raw, e := json.Marshal(params)
	if e != nil || len(raw) > 256<<10 {
		return nil, errors.New("invalid bounded CDP parameters")
	}
	c.mu.Lock()
	if c.closed || c.ctx.Err() != nil {
		c.mu.Unlock()
		return nil, ErrStale
	}
	if c.pending >= 32 {
		c.mu.Unlock()
		return nil, ErrBusy
	}
	c.pending++
	c.wg.Add(1)
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.pending--; c.mu.Unlock(); c.wg.Done() }()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	defer func() { stop(); cancel() }()
	select {
	case c.slot <- struct{}{}:
		defer func() { <-c.slot }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	reply, e := c.batch.CDP(ctx, sessionID, method, raw)
	if e != nil {
		return nil, e
	}
	if method == "Page.captureScreenshot" {
		if len(reply.Image) == 0 {
			return nil, errors.New("browser screenshot is missing")
		}
		return json.Marshal(map[string]string{"data": base64.StdEncoding.EncodeToString(reply.Image)})
	}
	if len(reply.Result) == 0 {
		return []byte(`{}`), nil
	}
	return reply.Result, nil
}
