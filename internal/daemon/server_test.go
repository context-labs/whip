package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestProtocolClientCommandReplayAndSnapshot(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &fakeRunner{}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(value, ServerOptions{BuildID: "test-build", Generation: 7})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close server: %v", err)
		}
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})

	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(context.Background(), conn, InitializeParams{
		ProtocolMajor: ProtocolMajor, BuildID: "client-build", ClientKind: "test", ClientID: "client-1",
		Cursors: map[string]int64{rootID: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if got := client.InitializeResult(); got.Generation != 7 || got.BuildID != "test-build" {
		t.Fatalf("initialize = %+v", got)
	}
	payload, _ := json.Marshal(map[string]string{"text": "hello"})
	params := CommandParams{CommandID: "command-1", Scope: "root", RootID: rootID, Operation: "submit", Payload: payload}
	result, err := client.Command(context.Background(), params)
	if err != nil || result.Status != "succeeded" || result.Output != "hello" || result.IngressSeq != 1 {
		t.Fatalf("command = %+v, %v", result, err)
	}
	retry, err := client.Command(context.Background(), params)
	if err != nil || retry != result || runner.calls.Load() != 1 {
		t.Fatalf("retry = %+v, calls=%d, err=%v", retry, runner.calls.Load(), err)
	}
	params.Payload, _ = json.Marshal(map[string]string{"text": "different"})
	if _, err := client.Command(context.Background(), params); err == nil || !strings.Contains(err.Error(), "different request") {
		t.Fatalf("conflict = %v", err)
	}
	replay, err := client.Replay(context.Background(), ReplayParams{RootID: rootID, Cursor: 0, Limit: 100})
	if err != nil || replay.Latest == 0 || len(replay.Events) == 0 {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	snapshot, err := client.Snapshot(context.Background(), rootID)
	if err != nil || snapshot.RootID != rootID || snapshot.Cursor != replay.Latest || len(snapshot.Messages) != 2 {
		t.Fatalf("snapshot = %+v, %v", snapshot, err)
	}
	select {
	case event := <-client.Events():
		if event.RootID != rootID || event.Seq == 0 {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for streamed event")
	}
}

func TestProtocolRejectsDuplicateClientAndOversizedFrame(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(value, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	firstServer, firstClient := net.Pipe()
	go server.serveConn(firstServer)
	first, err := NewClient(context.Background(), firstClient, InitializeParams{
		ProtocolMajor: ProtocolMajor, ClientKind: "test", ClientID: "duplicate",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close(); _ = server.Close() }()
	secondServer, secondClient := net.Pipe()
	go server.serveConn(secondServer)
	if _, err := NewClient(context.Background(), secondClient, InitializeParams{
		ProtocolMajor: ProtocolMajor, ClientKind: "test", ClientID: "duplicate",
	}); err == nil || !strings.Contains(err.Error(), "already connected") {
		t.Fatalf("duplicate client error = %v", err)
	}

	reader := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", MaxFrameSize)+"\n"), MaxFrameSize)
	if _, err := readProtocolFrame(reader); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversized frame error = %v", err)
	}
}

func TestProtocolBoundsInitializationConnectionsAndInFlightWork(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	started := make(chan struct{})
	release := make(chan struct{})
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{turn: func(context.Context, string, bool) (string, error) {
			close(started)
			<-release
			return "done", nil
		}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(value, ServerOptions{MaxConnections: 1, MaxInFlight: 1, InitializationTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer func() { _ = server.Close(); <-served }()

	idle, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := idle.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(idle).ReadByte(); err == nil {
		t.Fatal("uninitialized connection survived its deadline")
	}
	_ = idle.Close()

	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(context.Background(), conn, InitializeParams{ProtocolMajor: 1, ClientKind: "test", ClientID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	extra, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewClient(context.Background(), extra, InitializeParams{ProtocolMajor: 1, ClientKind: "test", ClientID: "excess"}); err == nil {
		t.Fatal("connection above the configured maximum was initialized")
	}
	_ = extra.Close()

	payload, _ := json.Marshal(map[string]string{"text": "hold"})
	commandDone := make(chan error, 1)
	go func() {
		_, err := client.Command(context.Background(), CommandParams{CommandID: "hold", Scope: "root", RootID: rootID, Operation: "submit", Payload: payload})
		commandDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("command did not start")
	}
	var ping map[string]any
	if err := client.Call(context.Background(), "daemon.ping", struct{}{}, &ping); err == nil || !strings.Contains(err.Error(), "too many in-flight") {
		t.Fatalf("in-flight overflow = %v", err)
	}
	close(release)
	if err := <-commandDone; err != nil {
		t.Fatal(err)
	}
}

func TestSlowOutboundClientClosesWithoutStoppingDaemon(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(value, ServerOptions{MaxOutbound: 1, MaxOutboundBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	serverSide, slowSide := net.Pipe()
	connection := &serverConn{server: server, conn: serverSide, out: make(chan []byte, 1)}
	server.wg.Go(connection.writeLoop)
	for i := range 10 {
		connection.notify("event", map[string]any{"sequence": i, "payload": strings.Repeat("x", 128)})
	}
	select {
	case <-time.After(time.Second):
		t.Fatal("slow connection was not closed")
	case <-func() <-chan struct{} {
		done := make(chan struct{})
		go func() { _, _ = slowSide.Read(make([]byte, 1)); close(done) }()
		return done
	}():
	}
	connection.mu.Lock()
	closed := connection.closed
	connection.mu.Unlock()
	if !closed {
		t.Fatal("slow client remained open")
	}
	if _, err := server.daemon.store.RootCursors(context.Background()); err != nil {
		t.Fatalf("slow client stopped daemon: %v", err)
	}
	_ = slowSide.Close()
	_ = server.Close()
}

func TestSnapshotStreamsActorConsistentBoundedChunks(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	large := strings.Repeat("snapshot-data-", 70_000)
	if err := store.Save(rootID, 0, []llm.Message{{Role: "system", Content: "system"}, {Role: "user", Content: large}}, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, client, served := startTCPClient(t, value, "snapshot-client")
	snapshot, err := client.Snapshot(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Messages) != 2 || snapshot.Messages[1].Content != large {
		t.Fatalf("snapshot messages changed: count=%d", len(snapshot.Messages))
	}
	_ = client.Close()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}
