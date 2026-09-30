package client

import (
	"context"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func (c *Client) HistoryPage(ctx context.Context, params protocol.HistoryPageParams) (session.BoundedTranscriptPage, error) {
	var page session.BoundedTranscriptPage
	err := c.Call(ctx, "history.page", params, &page)
	return page, err
}

// TracePage reads a root's spans after a cursor; see protocol.TracePageParams.
func (c *Client) TracePage(ctx context.Context, params protocol.TracePageParams) (session.SpanPage, error) {
	var page session.SpanPage
	err := c.Call(ctx, "trace.page", params, &page)
	return page, err
}

// TraceExport renders a session as OTLP/JSON; see protocol.TraceExportParams.
func (c *Client) TraceExport(ctx context.Context, params protocol.TraceExportParams) (protocol.TraceExportResult, error) {
	var result protocol.TraceExportResult
	err := c.Call(ctx, "trace.export", params, &result)
	return result, err
}
