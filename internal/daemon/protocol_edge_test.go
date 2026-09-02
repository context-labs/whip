package daemon

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestProtocolCodecRejectsInvalidAndOversizedValues(t *testing.T) {
	if _, err := requestDigest("root", "r", "submit", json.RawMessage(`{`)); err == nil {
		t.Fatal("invalid request payload was digested")
	}
	if _, err := marshalFrame(rpcMessage{Result: strings.Repeat("x", MaxFrameSize)}); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversized marshal = %v", err)
	}
	for _, frame := range [][]byte{nil, []byte(`{`), []byte(`{"jsonrpc":"1.0"}`)} {
		if _, err := decodeFrame(frame); err == nil {
			t.Fatalf("invalid frame %q decoded", frame)
		}
	}
}

func TestClientValidationAndCancellationPaths(t *testing.T) {
	if _, err := NewClient(context.Background(), nil, InitializeParams{}); err == nil {
		t.Fatal("nil connection initialized")
	}
	serverSide, clientSide := net.Pipe()
	go func() {
		reader := bufio.NewReader(serverSide)
		_, _ = readProtocolFrame(reader)
		_ = writeProtocolMessage(serverSide, rpcMessage{ID: json.RawMessage("1"), Result: InitializeResult{ProtocolMajor: 1}})
		_, _ = readProtocolFrame(reader)
		<-time.After(50 * time.Millisecond)
		_ = serverSide.Close()
	}()
	client, err := NewClient(context.Background(), clientSide, InitializeParams{ProtocolMajor: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Call(context.Background(), "", struct{}{}, nil); err == nil {
		t.Fatal("empty method was accepted")
	}
	if err := client.Call(context.Background(), "invalid.params", make(chan int), nil); err == nil {
		t.Fatal("unmarshalable call params were accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := client.Call(ctx, "blocked", struct{}{}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled call = %v", err)
	}
	_ = client.Close()
	select {
	case <-client.Done():
	default:
		t.Fatal("closed client did not close Done")
	}
	if client.Err() == nil {
		t.Fatal("closed client has no terminal error")
	}
	if err := client.Call(context.Background(), "after.close", struct{}{}, nil); err == nil {
		t.Fatal("closed client accepted a call")
	}
	if err := client.RequestRestart(context.Background(), 1); err == nil {
		t.Fatal("closed client requested restart")
	}
	if _, err := client.Upload(context.Background(), UploadBeginParams{Size: 2}, []byte("x")); err == nil {
		t.Fatal("upload size mismatch was accepted")
	}
	if _, err := client.EnrollIdentity(context.Background(), nil, true, "", nil); err == nil {
		t.Fatal("invalid enrollment key was accepted")
	}
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := client.EnrollIdentity(context.Background(), private, false, "authorizer", nil); err == nil {
		t.Fatal("missing enrollment authorizer key was accepted")
	}
	if _, err := client.DecidePermission(context.Background(), nil, PermissionDecision{}); err == nil {
		t.Fatal("invalid decision key was accepted")
	}
}

func TestClientRejectsBadInitializeAndSnapshotReplies(t *testing.T) {
	for _, result := range []rpcMessage{
		{ID: json.RawMessage("1"), Error: rpcFailure(-1, "refused")},
		{ID: json.RawMessage("1"), Result: InitializeResult{ProtocolMajor: 99}},
		{ID: json.RawMessage("1"), Result: "not-an-initialize-result"},
	} {
		serverSide, clientSide := net.Pipe()
		go func() {
			reader := bufio.NewReader(serverSide)
			_, _ = readProtocolFrame(reader)
			_ = writeProtocolMessage(serverSide, result)
			_ = serverSide.Close()
		}()
		if _, err := NewClient(context.Background(), clientSide, InitializeParams{ProtocolMajor: 1}); err == nil {
			t.Fatalf("bad initialize reply %+v was accepted", result)
		}
	}

	serverSide, clientSide := net.Pipe()
	go func() {
		reader := bufio.NewReader(serverSide)
		_, _ = readProtocolFrame(reader)
		_ = writeProtocolMessage(serverSide, rpcMessage{ID: json.RawMessage("1"), Result: InitializeResult{ProtocolMajor: 1}})
		requestFrame, _ := readProtocolFrame(reader)
		request, _ := decodeFrame(requestFrame)
		_ = writeProtocolMessage(serverSide, rpcMessage{ID: request.ID, Result: SnapshotResult{SnapshotID: "snapshot", Count: 1, Cursor: 2}})
		chunkFrame, _ := readProtocolFrame(reader)
		chunkRequest, _ := decodeFrame(chunkFrame)
		_ = writeProtocolMessage(serverSide, rpcMessage{ID: chunkRequest.ID, Result: SnapshotChunk{Index: 2, Count: 1}})
		_ = serverSide.Close()
	}()
	client, err := NewClient(context.Background(), clientSide, InitializeParams{ProtocolMajor: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Snapshot(context.Background(), "root"); err == nil {
		t.Fatal("invalid snapshot chunks were accepted")
	}
	_ = client.Close()

	wrongRoot, _ := json.Marshal(session.RootSnapshot{RootID: "other", Cursor: 2})
	for _, fixture := range []struct {
		result SnapshotResult
		chunk  SnapshotChunk
	}{
		{},
		{result: SnapshotResult{SnapshotID: "snapshot", Count: 1, Cursor: 2}, chunk: SnapshotChunk{Index: 0, Count: 1, Cursor: 2, Data: []byte("not-json")}},
		{result: SnapshotResult{SnapshotID: "snapshot", Count: 1, Cursor: 2}, chunk: SnapshotChunk{Index: 0, Count: 1, Cursor: 2, Data: wrongRoot}},
	} {
		serverSide, clientSide := net.Pipe()
		go func() {
			reader := bufio.NewReader(serverSide)
			_, _ = readProtocolFrame(reader)
			_ = writeProtocolMessage(serverSide, rpcMessage{ID: json.RawMessage("1"), Result: InitializeResult{ProtocolMajor: 1}})
			requestFrame, _ := readProtocolFrame(reader)
			request, _ := decodeFrame(requestFrame)
			_ = writeProtocolMessage(serverSide, rpcMessage{ID: request.ID, Result: fixture.result})
			if fixture.result.Count > 0 {
				chunkFrame, _ := readProtocolFrame(reader)
				chunkRequest, _ := decodeFrame(chunkFrame)
				_ = writeProtocolMessage(serverSide, rpcMessage{ID: chunkRequest.ID, Result: fixture.chunk})
			}
			_ = serverSide.Close()
		}()
		client, err := NewClient(context.Background(), clientSide, InitializeParams{ProtocolMajor: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Snapshot(context.Background(), "root"); err == nil {
			t.Fatalf("invalid snapshot fixture was accepted: %+v", fixture)
		}
		_ = client.Close()
	}
}

func TestClientConnectionAndReadLoopFailures(t *testing.T) {
	closedServer, closedClient := net.Pipe()
	_ = closedServer.Close()
	if _, err := NewClient(context.Background(), closedClient, InitializeParams{}); err == nil {
		t.Fatal("client initialized across a closed connection")
	}
	for name, response := range map[string][]byte{
		"eof":           nil,
		"invalid frame": []byte("{}\n"),
	} {
		t.Run(name, func(t *testing.T) {
			serverSide, clientSide := net.Pipe()
			go func() {
				_, _ = readProtocolFrame(bufio.NewReader(serverSide))
				if response != nil {
					_, _ = serverSide.Write(response)
				}
				_ = serverSide.Close()
			}()
			if _, err := NewClient(context.Background(), clientSide, InitializeParams{}); err == nil {
				t.Fatal("client accepted a broken initialization response")
			}
		})
	}

	frames := map[string]rpcMessage{
		"invalid event":  {Method: "event", Params: json.RawMessage(`true`)},
		"event overflow": {Method: "event", Params: mustJSON(t, eventNotification{Event: ProtocolEvent{RootID: "root", Seq: 2}})},
		"orphan reply":   {ID: json.RawMessage("99"), Result: true},
	}
	for name, message := range frames {
		t.Run(name, func(t *testing.T) {
			serverSide, clientSide := net.Pipe()
			client := &Client{
				conn: clientSide, pending: make(map[string]chan callResponse),
				events: make(chan ProtocolEvent, 1), done: make(chan struct{}),
			}
			if name == "event overflow" {
				client.events <- ProtocolEvent{Seq: 1}
			}
			frame, err := marshalFrame(message)
			if err != nil {
				t.Fatal(err)
			}
			client.readLoop(bufio.NewReader(strings.NewReader(string(frame))))
			if client.Err() == nil {
				t.Fatal("read loop exited without a terminal error")
			}
			_ = serverSide.Close()
		})
	}
}

func TestClientCallRejectsInvalidResultAndRestartCancellation(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	go func() {
		reader := bufio.NewReader(serverSide)
		_, _ = readProtocolFrame(reader)
		_ = writeProtocolMessage(serverSide, rpcMessage{ID: json.RawMessage("1"), Result: InitializeResult{ProtocolMajor: 1}})
		frame, _ := readProtocolFrame(reader)
		request, _ := decodeFrame(frame)
		_ = writeProtocolMessage(serverSide, rpcMessage{ID: request.ID, Result: "wrong shape"})
		_, _ = readProtocolFrame(reader)
		<-time.After(20 * time.Millisecond)
		_ = serverSide.Close()
	}()
	client, err := NewClient(context.Background(), clientSide, InitializeParams{})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := client.Call(context.Background(), "invalid.result", struct{}{}, &result); err == nil {
		t.Fatal("invalid call result decoded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := client.RequestRestart(ctx, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled restart = %v", err)
	}
	_ = client.Close()
}

func TestClientUploadPropagatesEachProtocolFailure(t *testing.T) {
	for _, failedMethod := range []string{"upload.begin", "upload.chunk", "upload.finish"} {
		t.Run(failedMethod, func(t *testing.T) {
			serverSide, clientSide := net.Pipe()
			go func() {
				reader := bufio.NewReader(serverSide)
				_, _ = readProtocolFrame(reader)
				_ = writeProtocolMessage(serverSide, rpcMessage{ID: json.RawMessage("1"), Result: InitializeResult{ProtocolMajor: 1}})
				for {
					frame, err := readProtocolFrame(reader)
					if err != nil {
						return
					}
					request, _ := decodeFrame(frame)
					if request.Method == failedMethod {
						_ = writeProtocolMessage(serverSide, rpcMessage{ID: request.ID, Error: rpcFailure(-1, "stopped")})
						return
					}
					_ = writeProtocolMessage(serverSide, rpcMessage{ID: request.ID, Result: map[string]bool{"accepted": true}})
				}
			}()
			client, err := NewClient(context.Background(), clientSide, InitializeParams{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Upload(context.Background(), UploadBeginParams{UploadID: "upload", Size: 1}, []byte("x")); err == nil {
				t.Fatal("upload protocol failure was ignored")
			}
			_ = client.Close()
		})
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
