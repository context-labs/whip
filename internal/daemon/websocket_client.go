package daemon

import (
	"context"
	"io"
	"time"

	"github.com/gobwas/ws"
)

// DialWebSocketClient uses the same handlers and contract as the Unix client.
func DialWebSocketClient(ctx context.Context, endpoint string, initialize InitializeParams) (*Client, error) {
	conn, buffered, _, err := (ws.Dialer{Timeout: 5 * time.Second}).Dial(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var source io.Reader = conn
	if buffered != nil {
		source = buffered
	}
	transport := newWebsocketMessageTransport(conn, source)
	transport.clientSide = true
	transport.reader.State = ws.StateClientSide
	transport.reader.OnIntermediate = transport.control
	client, err := newTransportClient(ctx, transport, initialize)
	if err != nil {
		_ = transport.Close()
	}
	return client, err
}
