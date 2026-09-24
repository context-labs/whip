package daemon

import (
	"context"
	"errors"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

// HistoryPage reads a bounded raw transcript page without adding it to any
// agent's model context.
func (c *RootClient) HistoryPage(ctx context.Context, p HistoryPageParams) (session.BoundedTranscriptPage, error) {
	if err := c.WaitLive(ctx); err != nil {
		return session.BoundedTranscriptPage{}, err
	}
	c.mu.RLock()
	connection := c.conn
	c.mu.RUnlock()
	reader, ok := connection.(interface {
		HistoryPage(context.Context, HistoryPageParams) (session.BoundedTranscriptPage, error)
	})
	if !ok {
		return session.BoundedTranscriptPage{}, errors.New("daemon does not support transcript pagination")
	}
	return reader.HistoryPage(ctx, p)
}

func (c *RootClient) ReadContent(ctx context.Context, p protocol.ContentReadParams) (protocol.ContentReadResult, error) {
	var result protocol.ContentReadResult
	err := c.providerCall(ctx, "content.read", p, &result)
	return result, err
}
