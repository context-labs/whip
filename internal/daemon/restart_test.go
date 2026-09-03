package daemon

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestRestartReturnsAuthoritativeInterruptedCommandWithoutReexecution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createRoot(t, store)
	started := make(chan struct{})
	oldRunner := &fakeRunner{turn: func(ctx context.Context, _ string, _ bool) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}}
	oldDaemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: oldRunner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	oldServer, oldClient, oldServed := startTCPClient(t, oldDaemon, "stable-client")
	payload, _ := json.Marshal(map[string]string{"text": "long mutation"})
	params := CommandParams{CommandID: "stable-command", Scope: "root", RootID: rootID, Operation: "submit", Payload: payload}
	commandDone := make(chan error, 1)
	go func() {
		_, err := oldClient.Command(context.Background(), params)
		commandDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("old command did not start")
	}
	if err := oldServer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-oldServed; err != nil {
		t.Fatal(err)
	}
	if err := <-commandDone; err == nil {
		t.Fatal("detached in-flight call unexpectedly received a reply")
	}

	store, err = session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	newRunner := &fakeRunner{}
	newDaemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: newRunner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	newServer, newClient, newServed := startTCPClient(t, newDaemon, "stable-client")
	result, err := newClient.Command(context.Background(), params)
	if err != nil || result.Status != "interrupted" || result.IngressSeq != 1 || result.Error == "" {
		t.Fatalf("retried command = %+v, %v", result, err)
	}
	if newRunner.calls.Load() != 0 {
		t.Fatalf("interrupted command reexecuted %d times", newRunner.calls.Load())
	}
	_ = newClient.Close()
	if err := newServer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-newServed; err != nil {
		t.Fatal(err)
	}
}

func startTCPClient(t *testing.T, value *Daemon, clientID string) (*Server, *Client, <-chan error) {
	t.Helper()
	server, err := NewServer(value, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(context.Background(), conn, InitializeParams{ProtocolMajor: 1, ClientID: clientID, ClientKind: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return server, client, served
}
