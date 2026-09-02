package daemon

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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
	upload := []byte(strings.Repeat("uploaded", 50_000))
	digest := sha256.Sum256(upload)
	handle, err := client.Upload(context.Background(), UploadBeginParams{
		UploadID: "protocol-upload", RootID: rootID, ExpectedDigest: hex.EncodeToString(digest[:]),
		Size: int64(len(upload)), MediaType: "application/octet-stream", Source: "protocol test",
	}, upload)
	if err != nil || handle.Digest != hex.EncodeToString(digest[:]) || handle.ReferenceID == "" {
		t.Fatalf("protocol upload = %+v, %v", handle, err)
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
	if _, err := NewServer(nil, ServerOptions{}); err == nil {
		t.Fatal("nil daemon server was created")
	}
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
	if err := first.Call(context.Background(), "unsupported", struct{}{}, nil); err == nil || !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("unsupported method = %v", err)
	}
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
	wrongServer, wrongClient := net.Pipe()
	go server.serveConn(wrongServer)
	if _, err := NewClient(context.Background(), wrongClient, InitializeParams{ProtocolMajor: 99, ClientKind: "test", ClientID: "wrong"}); err == nil {
		t.Fatal("wrong protocol major initialized")
	}
	if err := server.Serve(nil); err == nil {
		t.Fatal("nil listener was accepted")
	}
}

func TestProtocolRejectsMalformedInitialization(t *testing.T) {
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
	t.Cleanup(func() { _ = server.Close() })

	wrongMethod, _ := marshalFrame(rpcMessage{ID: json.RawMessage("1"), Method: "daemon.ping"})
	invalidParams := []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":\"bad\"}\n")
	missingIdentity, _ := marshalFrame(rpcMessage{ID: json.RawMessage("1"), Method: "initialize", Params: json.RawMessage(`{"protocol_major":1}`)})
	for name, frame := range map[string][]byte{
		"invalid envelope":   []byte("{}\n"),
		"wrong first method": wrongMethod,
		"invalid params":     invalidParams,
		"missing identity":   missingIdentity,
	} {
		t.Run(name, func(t *testing.T) {
			serverSide, clientSide := net.Pipe()
			go server.serveConn(serverSide)
			if _, err := clientSide.Write(frame); err != nil {
				t.Fatal(err)
			}
			_ = clientSide.SetReadDeadline(time.Now().Add(time.Second))
			responseFrame, err := readProtocolFrame(bufio.NewReader(clientSide))
			if err != nil {
				t.Fatal(err)
			}
			response, err := decodeFrame(responseFrame)
			if err != nil || response.Error == nil {
				t.Fatalf("initialization response = %+v, %v", response, err)
			}
			_ = clientSide.Close()
		})
	}
}

type failingListener struct {
	err    error
	closed bool
}

func (l *failingListener) Accept() (net.Conn, error) { return nil, l.err }
func (l *failingListener) Close() error              { l.closed = true; return nil }
func (*failingListener) Addr() net.Addr              { return &net.TCPAddr{} }

func TestServerServeReportsListenerFailureAndHonorsPreClose(t *testing.T) {
	newValue := func(path string) *Daemon {
		store := openStore(t, path)
		value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
			return Components{Runner: &fakeRunner{}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}

	server, err := NewServer(newValue(filepath.Join(t.TempDir(), "failure.db")), ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("accept failed")
	if err := server.Serve(&failingListener{err: want}); !errors.Is(err, want) {
		t.Fatalf("accept error = %v", err)
	}
	_ = server.Close()

	closed, err := NewServer(newValue(filepath.Join(t.TempDir(), "closed.db")), ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	listener := &failingListener{err: want}
	if err := closed.Serve(listener); err != nil || !listener.closed {
		t.Fatalf("serve after close = %v, listener closed=%t", err, listener.closed)
	}
}

func TestProtocolHandlersRejectMalformedParameters(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(value, ServerOptions{BuildID: "test", Generation: 3})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	connection := &serverConn{server: server, client: InitializeParams{ClientID: "malformed", ClientKind: "test"}}
	for _, method := range []string{
		"command", "events.replay", "snapshot", "snapshot.chunk", "upload.begin",
		"upload.chunk", "upload.finish", "identity.enroll", "permission.decide",
	} {
		if result, failure := server.handle(connection, rpcMessage{Method: method, Params: json.RawMessage(`{`)}); result != nil || failure == nil || failure.Code != -32602 {
			t.Errorf("%s malformed params = %v, %+v", method, result, failure)
		}
	}
	result, failure := server.handle(connection, rpcMessage{Method: "daemon.ping"})
	if failure != nil || result == nil {
		t.Fatalf("daemon ping = %v, %+v", result, failure)
	}
}

func TestServerReportsClosedStoreAndEncodingFailures(t *testing.T) {
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
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := server.replay(ReplayParams{RootID: "root"}); err == nil {
		t.Fatal("closed store replay succeeded")
	}
	server.pumpEvents(nil, "root", 0)
	if err := server.Serve(&failingListener{err: errors.New("unused")}); err == nil {
		t.Fatal("server resumed across a closed store")
	}
	_ = server.Close()
	if err := writeProtocolMessage(io.Discard, rpcMessage{Result: make(chan int)}); err == nil {
		t.Fatal("unencodable protocol message was written")
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
	if connection.send(rpcMessage{Result: make(chan int)}) || connection.send(rpcMessage{Result: "closed"}) {
		t.Fatal("closed connection accepted outbound frames")
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

func TestServerCommandValidation(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
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
	defer func() { _ = server.Close() }()
	connection := &serverConn{server: server, client: InitializeParams{ClientID: "validation"}}
	validText, _ := json.Marshal(map[string]string{"text": "hello"})
	tests := []CommandParams{
		{},
		{CommandID: "id", Operation: "submit", Payload: json.RawMessage(`{`)},
		{CommandID: "id", Operation: "submit", Scope: "invalid"},
		{CommandID: "id", Operation: "invalid", Scope: "daemon"},
		{CommandID: "id", Operation: "session.create", Scope: "daemon", Payload: json.RawMessage(`{`)},
		{CommandID: "id", Operation: "submit", Scope: "root", RootID: rootID, Payload: json.RawMessage(`{}`)},
		{CommandID: "id", Operation: "invalid", Scope: "root", RootID: rootID, Payload: validText},
		{CommandID: "id", Operation: "submit", Scope: "root", RootID: "missing", Payload: validText},
	}
	for _, params := range tests {
		if _, err := server.command(connection, params); err == nil {
			t.Fatalf("invalid command was accepted: %+v", params)
		}
	}
	for _, method := range []string{"command", "events.replay", "snapshot", "snapshot.chunk", "upload.begin", "upload.chunk", "upload.finish", "identity.enroll", "permission.decide"} {
		if _, failure := server.handle(connection, rpcMessage{Method: method, Params: json.RawMessage(`{`)}); failure == nil {
			t.Fatalf("invalid %s params were accepted", method)
		}
	}
	if result, failure := server.handle(connection, rpcMessage{Method: "daemon.ping"}); failure != nil || result == nil {
		t.Fatalf("daemon ping = %v, %v", result, failure)
	}
	if connection.notify("invalid", make(chan int)) {
		t.Fatal("unmarshalable notification was sent")
	}
	if _, err := connection.snapshotChunk(SnapshotChunkParams{SnapshotID: "missing"}); err == nil {
		t.Fatal("missing snapshot chunk was returned")
	}
	connection.armRestart(2)
	if connection.consumeRestart(1) || !connection.consumeRestart(2) || connection.consumeRestart(2) {
		t.Fatal("restart generation was not single-use")
	}
	if _, err := readProtocolFrame(bufio.NewReader(strings.NewReader(`{"jsonrpc":"2.0"}`))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("unterminated frame = %v", err)
	}
}
