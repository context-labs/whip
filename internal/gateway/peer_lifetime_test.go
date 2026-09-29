package gateway

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func TestForegroundPeerLifetimeClosesOnOwnerLoss(t *testing.T) {
	r, _, options := fixture(t)
	options.BackendDone = nil
	s, err := Start(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	connection := browser(t, s)
	if got := initialize(t, connection); !got.NetworkClient || got.ProcessEpoch != options.ProcessEpoch {
		t.Fatal(got)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
		if s.Err() == nil || !strings.Contains(s.Err().Error(), "runtime connection ended") {
			t.Fatal(s.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("gateway retained a lost peer")
	}
}

func TestForegroundPeerHeartbeatsNeverRebindIdentity(t *testing.T) {
	left, right := net.Pipe()
	t.Cleanup(func() { _ = left.Close(); _ = right.Close() })
	server := &Server{options: Options{RuntimeID: "runtime", ProcessEpoch: "original"}}
	done := make(chan struct{})
	go func() { defer close(done); server.watchPeer(t.Context(), NewUnix(left), 20*time.Millisecond) }()
	peer := NewUnix(right)
	for attempt := range 4 {
		if err := right.SetDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		raw, err := peer.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var request protocol.Request
		if json.Unmarshal(raw, &request) != nil || request.Method != "host.status" || string(request.Params) != "{}" {
			t.Fatalf("heartbeat sent work: %s", raw)
		}
		status := protocol.HostStatus{RuntimeID: "runtime", ProcessEpoch: "original", PID: 123, Build: "fixture", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if attempt == 3 {
			status.ProcessEpoch = "replacement"
		}
		value, _ := json.Marshal(status)
		reply, _ := json.Marshal(protocol.Response{JSONRPC: "2.0", ID: request.ID, Result: value})
		if err := peer.WriteMessage(reply); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor accepted a replacement owner")
	}
}

func TestForegroundPeerCancellationJoinsBlockedIO(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); (&Server{}).watchPeer(ctx, NewUnix(left), time.Millisecond) }()
	// Reading one byte proves the write started; leave the remainder blocked.
	if err := right.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor cancellation did not join blocked I/O")
	}
}

func TestForegroundPeerMonitorJoinsWithoutStoppingOwner(t *testing.T) {
	r, client, options := fixture(t)
	options.BackendDone = nil
	s, err := Start(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("gateway did not join its peer monitor")
	}
	select {
	case <-r.Done():
		t.Fatal("foreground gateway stopped runtime")
	default:
	}
	create(t, client, "starlark", "after-gateway-close")
}
