package mcp

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// streamHeaderGrace bounds how long the standalone GET stream may withhold the
// end of its header block before connect proceeds without it. The SDK opens
// that stream synchronously inside Client.Connect, so a server that sends its
// status line and headers but not the terminating blank line until its first
// event (Executor 1.0.0 does) would otherwise stall every connect until the
// startup deadline. A variable so tests can shorten it.
var streamHeaderGrace = 2 * time.Second

// bindHTTPContext gives detached SDK requests the connection's owned lifetime.
// The SDK opens standalone SSE before Client.Connect returns, and its Close
// sends DELETE before cancelling that stream. Both requests must remain
// cancellable even while no ClientSession has been published to the manager.
func bindHTTPContext(ctx context.Context, transport sdkmcp.Transport, budget *Budget) (sdkmcp.Transport, func()) {
	remote, ok := transport.(*sdkmcp.StreamableClientTransport)
	if !ok {
		return transport, func() {}
	}
	client := *http.DefaultClient
	if remote.HTTPClient != nil {
		client = *remote.HTTPClient
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	owned := &connectionHTTPTransport{lifetime: ctx, base: base, budget: budget}
	client.Transport = owned
	bound := *remote
	bound.HTTPClient = &client
	return &connectionHTTPStartup{Transport: &bound, lifetime: ctx, owned: owned}, owned.join
}

type connectionHTTPStartup struct {
	sdkmcp.Transport
	lifetime context.Context
	owned    *connectionHTTPTransport
}

func (t *connectionHTTPStartup) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	connection, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	// Closing also stops the SDK's retry backoff. HTTP cancellation alone leaves
	// that loop on the detached SDK context. DELETE has its own cleanup deadline.
	t.owned.workers.Add(1)
	context.AfterFunc(t.lifetime, func() { defer t.owned.workers.Done(); _ = connection.Close() })
	// Return the raw connection: wrapping it would hide the SDK's private
	// sessionUpdated method and silently disable standalone notifications.
	return connection, nil
}

// lifetime belongs to this connection, not to an individual request or turn.
type connectionHTTPTransport struct {
	budget         *Budget
	mu             sync.Mutex
	closing        bool
	workers        sync.WaitGroup
	lifetime       context.Context
	base           http.RoundTripper
	deleteStarted  sync.Once
	deleteDeadline time.Time
}

func (t *connectionHTTPTransport) join() {
	t.mu.Lock()
	t.closing = true
	t.mu.Unlock()
	t.workers.Wait()
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (t *connectionHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	if t.closing && request.Method != http.MethodDelete {
		t.mu.Unlock()
		return nil, errors.New("MCP HTTP transport is closed")
	}
	t.workers.Add(1)
	t.mu.Unlock()
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
			t.workers.Done()
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
		t.workers.Done()
	})
	if isStandaloneStream(request) {
		return t.roundTripStream(request.Clone(ctx), finish)
	}
	response, err := t.base.RoundTrip(request.Clone(ctx))
	if err != nil {
		finish()
		return nil, err
	}
	// SSE bodies outlive RoundTrip. Retain cancellation until EOF or Close,
	// rather than cancelling a healthy stream when its headers arrive.
	boundHTTPBody(response, t.budget)
	response.Body = &connectionHTTPBody{ReadCloser: response.Body, finish: finish}
	return response, nil
}

// isStandaloneStream recognizes the SDK's server-to-client notification
// stream: the only GET the streamable client issues.
func isStandaloneStream(request *http.Request) bool {
	return request.Method == http.MethodGet && strings.Contains(request.Header.Get("Accept"), "text/event-stream")
}

// roundTripStream opens the standalone stream without letting it gate the
// connect. A response whose headers complete within streamHeaderGrace is
// returned as is, so a prompt 405 or a healthy stream behaves exactly as the
// SDK expects. Past the grace, the SDK gets an idle 200 text/event-stream now
// and the real body is spliced in if the server ever finishes its headers.
// A late non-stream answer leaves the idle stream open rather than ending it:
// the SDK fails the whole connection after a few no-progress reconnects, and
// a server that declined a stream is no worse off idle.
func (t *connectionHTTPTransport) roundTripStream(request *http.Request, finish func()) (*http.Response, error) {
	type result struct {
		response *http.Response
		err      error
	}
	results := make(chan result, 1)
	t.workers.Add(1)
	go func() {
		defer t.workers.Done()
		response, err := t.base.RoundTrip(request) //nolint:bodyclose // the caller owns and closes the returned response
		results <- result{response, err}
	}()
	grace := time.NewTimer(streamHeaderGrace)
	defer grace.Stop()
	select {
	case r := <-results:
		if r.err != nil {
			finish()
			return nil, r.err
		}
		boundHTTPBody(r.response, t.budget)
		r.response.Body = &connectionHTTPBody{ReadCloser: r.response.Body, finish: finish}
		return r.response, nil
	case <-request.Context().Done():
		r := <-results
		if r.response != nil {
			_ = r.response.Body.Close()
		}
		finish()
		return nil, request.Context().Err()
	case <-grace.C:
	}
	reader, writer := io.Pipe()
	t.workers.Add(1)
	go func() {
		defer t.workers.Done()
		r := <-results
		if r.err != nil {
			_ = writer.CloseWithError(r.err)
			finish()
			return
		}
		mediaType, _, _ := mime.ParseMediaType(r.response.Header.Get("Content-Type"))
		if r.response.StatusCode != http.StatusOK || mediaType != "text/event-stream" {
			_ = r.response.Body.Close()
			logf("standalone stream answered %d %s after the header grace; leaving it idle", r.response.StatusCode, mediaType)
			return
		}
		boundHTTPBody(r.response, t.budget)
		_, err := io.Copy(writer, r.response.Body)
		_ = r.response.Body.Close()
		_ = writer.CloseWithError(err)
		finish()
	}()
	return &http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       &connectionHTTPBody{ReadCloser: reader, finish: finish},
		Request:    request,
	}, nil
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
