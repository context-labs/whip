package client

import (
	"errors"
	"net"
	"testing"

	"github.com/context-labs/whip/internal/daemonconn"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/protocoltransport"
)

func TestClientIgnoresFailureFromReplacedSubscription(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer func() { _ = serverSide.Close(); _ = clientSide.Close() }()
	stale, _ := protocoltransport.MarshalFrame(protocoltransport.Message{Method: "subscription.failed", Params: mustJSON(t, protocol.SubscriptionFailure{RootID: "root", SubscriptionID: "old", Error: &protocol.RPCError{Code: -32010, Message: "expired old stream", Data: &protocol.ErrorData{Kind: "resynchronization_required"}}})})
	current, _ := protocoltransport.MarshalFrame(protocoltransport.Message{Method: "event", Params: mustJSON(t, daemonconn.EventNotification{Event: protocol.ProtocolEvent{RootID: "root", SubscriptionID: "new", Seq: 4, Kind: "turn.started", Payload: []byte(`{}`)}})})
	transport := protocoltransport.NewUnix(clientSide)
	written := make(chan struct{})
	go func() {
		defer close(written)
		defer serverSide.Close()
		_, _ = serverSide.Write(append(stale, current...))
	}()
	client := &Client{conn: transport, pending: make(map[string]chan callResponse), subscriptions: map[string]string{"root": "new"}, events: make(chan protocol.ProtocolEvent, 2), commandChanged: make(chan struct{}), done: make(chan struct{})}
	client.readLoop()
	<-written
	if _, ok := errors.AsType[*protocol.RPCError](client.Err()); ok {
		t.Fatalf("stale failure closed current stream: %v", client.Err())
	}
	select {
	case event := <-client.Events():
		if event.Seq != 4 {
			t.Fatalf("event=%+v", event)
		}
	default:
		t.Fatal("current subscription event lost")
	}
}
