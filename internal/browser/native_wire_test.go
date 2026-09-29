package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func testNativeWire(t *testing.T) (*nativeWire, net.Conn) {
	t.Helper()
	client, peer := net.Pipe()
	c := newNativeWire(t.Context(), client, client)
	t.Cleanup(func() { _ = peer.Close(); _ = c.Close() })
	return c, peer
}

func nativeWireRequest(t *testing.T, peer net.Conn) int64 {
	t.Helper()
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	defer func() { _ = peer.SetReadDeadline(time.Time{}) }()
	data, err := wsutil.ReadClientText(peer)
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(data, &value) != nil || value.ID < 1 {
		t.Fatalf("invalid request %s", data)
	}
	return value.ID
}

func nativeWireResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("CDP call did not join")
		return nil
	}
}

func TestNativeWireCancellationClosesWithoutReplay(t *testing.T) {
	c, peer := testNativeWire(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(ctx, "page", "Runtime.evaluate", map[string]any{"expression": "effect()"})
		done <- err
	}()
	if id := nativeWireRequest(t, peer); id != 1 {
		t.Fatal(id)
	}
	cancel()
	if err := nativeWireResult(t, done); !errors.Is(err, context.Canceled) && !errors.Is(err, errNativeConnection) {
		t.Fatal(err)
	}
	if _, err := c.Call(t.Context(), "page", "Runtime.evaluate", nil); !errors.Is(err, errNativeConnection) {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := wsutil.ReadClientText(peer); err == nil {
		t.Fatal("retired connection accepted another effect")
	}
}

func TestNativeWireExactResponseAndPassiveEvents(t *testing.T) {
	c, peer := testNativeWire(t)
	done := make(chan error, 1)
	go func() {
		value, err := c.Call(t.Context(), "page", "Runtime.evaluate", map[string]any{})
		if err == nil && string(value) != `{"value":2}` {
			err = errors.New("wrong result")
		}
		done <- err
	}()
	nativeWireRequest(t, peer)
	if err := wsutil.WriteServerText(peer, []byte(`{"method":"Page.frameNavigated","sessionId":"page","params":{"frame":{}}}`)); err != nil {
		t.Fatal(err)
	}
	if err := wsutil.WriteServerText(peer, []byte(`{"id":1,"result":{"value":2}}`)); err != nil {
		t.Fatal(err)
	}
	if err := nativeWireResult(t, done); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-c.Event():
		if event.Method != "Page.frameNavigated" || event.SessionID != "page" {
			t.Fatal(event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing exact event")
	}
}

func TestNativeWireQueuedCancellationCannotAllocateOrSend(t *testing.T) {
	c, peer := testNativeWire(t)
	active := make(chan error, 1)
	go func() { _, err := c.Call(t.Context(), "page", "Runtime.evaluate", nil); active <- err }()
	nativeWireRequest(t, peer)
	ctx, cancel := context.WithCancel(t.Context())
	queued := make(chan error, 1)
	go func() { _, err := c.Call(ctx, "page", "Runtime.evaluate", nil); queued <- err }()
	cancel()
	if err := nativeWireResult(t, queued); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := wsutil.WriteServerText(peer, []byte(`{"id":1,"result":{}}`)); err != nil {
		t.Fatal(err)
	}
	if err := nativeWireResult(t, active); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	next := c.next
	c.mu.Unlock()
	if next != 1 {
		t.Fatalf("canceled waiter allocated request %d", next)
	}
}

func TestNativeWireProtocolBoundsRetireConnection(t *testing.T) {
	for _, name := range []string{"oversized header", "malformed JSON", "event overflow", "foreign response"} {
		t.Run(name, func(t *testing.T) {
			c, peer := testNativeWire(t)
			switch name {
			case "oversized header":
				_ = ws.WriteHeader(peer, ws.Header{Fin: true, OpCode: ws.OpText, Length: nativeMessageBytes + 1})
			case "malformed JSON":
				_ = wsutil.WriteServerText(peer, []byte(`{"id":`))
			case "foreign response":
				_ = wsutil.WriteServerText(peer, []byte(`{"id":99,"result":{}}`))
			case "event overflow":
				for range 33 {
					if err := wsutil.WriteServerText(peer, []byte(`{"method":"Page.changed","params":{}}`)); err != nil {
						break
					}
				}
			}
			select {
			case <-c.Lifetime().Done():
			case <-time.After(time.Second):
				t.Fatal("invalid peer remained live")
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeWireParameterBoundsBeforeDelivery(t *testing.T) {
	c, _ := testNativeWire(t)
	if _, err := c.Call(t.Context(), "", "Runtime.evaluate", strings.Repeat("x", 256<<10)); err == nil {
		t.Fatal("accepted oversized params")
	}
	c.mu.Lock()
	next := c.next
	c.mu.Unlock()
	if next != 0 {
		t.Fatal("allocated invalid request")
	}
}
