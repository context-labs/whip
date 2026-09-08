package mcp

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// bindHTTPContext gives detached SDK requests the connection's owned lifetime.
// The SDK opens standalone SSE before Client.Connect returns, and its Close
// sends DELETE before cancelling that stream. Both requests must remain
// cancellable even while no ClientSession has been published to the manager.
func bindHTTPContext(ctx context.Context, transport sdkmcp.Transport) sdkmcp.Transport {
	remote, ok := transport.(*sdkmcp.StreamableClientTransport)
	if !ok {
		return transport
	}
	client := *http.DefaultClient
	if remote.HTTPClient != nil {
		client = *remote.HTTPClient
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = &connectionHTTPTransport{lifetime: ctx, base: base}
	bound := *remote
	bound.HTTPClient = &client
	return &connectionHTTPStartup{Transport: &bound, lifetime: ctx}
}

type connectionHTTPStartup struct {
	sdkmcp.Transport
	lifetime context.Context
}

func (t *connectionHTTPStartup) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	connection, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	// Closing also stops the SDK's retry backoff. HTTP cancellation alone leaves
	// that loop on the detached SDK context. DELETE has its own cleanup deadline.
	context.AfterFunc(t.lifetime, func() { _ = connection.Close() })
	// Return the raw connection: wrapping it would hide the SDK's private
	// sessionUpdated method and silently disable standalone notifications.
	return connection, nil
}

// lifetime belongs to this connection, not to an individual request or turn.
type connectionHTTPTransport struct {
	lifetime       context.Context
	base           http.RoundTripper
	deleteStarted  sync.Once
	deleteDeadline time.Time
}

func (t *connectionHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	ctx := request.Context()
	var cancel context.CancelFunc
	var stop func() bool
	if request.Method == http.MethodDelete {
		// SDK Close sends DELETE before cancelling its detached context. Allow
		// best-effort remote session cleanup after our lifetime ends, but never
		// let an unresponsive server hold retirement open beyond one second.
		// Redirects share that budget instead of starting a new timeout per hop.
		t.deleteStarted.Do(func() { t.deleteDeadline = time.Now().Add(time.Second) })
		ctx, cancel = context.WithDeadline(ctx, t.deleteDeadline)
	} else {
		if err := t.lifetime.Err(); err != nil {
			return nil, err
		}
		ctx, cancel = context.WithCancel(ctx)
		stop = context.AfterFunc(t.lifetime, cancel)
	}
	finish := sync.OnceFunc(func() {
		if stop != nil {
			stop()
		}
		cancel()
	})
	response, err := t.base.RoundTrip(request.Clone(ctx))
	if err != nil {
		finish()
		return nil, err
	}
	// SSE bodies outlive RoundTrip. Retain cancellation until EOF or Close,
	// rather than cancelling a healthy stream when its headers arrive.
	response.Body = &connectionHTTPBody{ReadCloser: response.Body, finish: finish}
	return response, nil
}

type connectionHTTPBody struct {
	io.ReadCloser
	finish func()
}

func (b *connectionHTTPBody) Read(buffer []byte) (int, error) {
	n, err := b.ReadCloser.Read(buffer)
	if err != nil {
		b.finish()
	}
	return n, err
}

func (b *connectionHTTPBody) Close() error {
	b.finish()
	return b.ReadCloser.Close()
}
