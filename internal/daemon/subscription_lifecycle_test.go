package daemon

import (
	"bufio"
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestClientIgnoresFailureFromReplacedSubscription(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer func() { _ = serverSide.Close(); _ = clientSide.Close() }()
	stale, _ := marshalFrame(rpcMessage{Method: "subscription.failed", Params: mustJSON(t, protocol.SubscriptionFailure{RootID: "root", SubscriptionID: "old", Error: rpcFailure(-32010, "expired old stream")})})
	current, _ := marshalFrame(rpcMessage{Method: "event", Params: mustJSON(t, eventNotification{Event: ProtocolEvent{RootID: "root", SubscriptionID: "new", Seq: 4, Kind: "turn.started", Payload: []byte(`{}`)}})})
	transport := newUnixMessageTransport(clientSide)
	transport.reader = bufio.NewReaderSize(strings.NewReader(string(stale)+string(current)), MaxFrameSize)
	client := &Client{conn: transport, pending: make(map[string]chan callResponse), subscriptions: map[string]string{"root": "new"}, events: make(chan ProtocolEvent, 2), commandChanged: make(chan struct{}), done: make(chan struct{})}
	client.readLoop()
	if _, ok := errors.AsType[*RPCError](client.Err()); ok {
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

func TestSubscriptionRetirementCannotDeleteReplacement(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "runtime.db"))
	rootID := createRoot(t, store)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	old := &subscription{id: "reused", rootID: rootID, cancel: func() {}}
	replacement := &subscription{id: "reused", rootID: rootID, cancel: func() {}}
	server := &Server{ctx: t.Context(), daemon: &Daemon{store: store}}
	connection := &serverConn{subscriptions: map[string]*subscription{"reused": replacement}, out: make(chan []byte, 1)}
	server.pumpSubscription(ctx, connection, old, 0)
	if connection.subscriptions["reused"] != replacement {
		t.Fatal("retiring stream removed replacement")
	}
	if len(connection.out) != 0 {
		t.Fatal("normal unsubscribe emitted a recovery failure")
	}
}
