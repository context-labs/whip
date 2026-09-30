package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/daemonconn"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/protocoltransport"
	"github.com/context-labs/whip/internal/session"
)

func TestClientValidationAndCancellationPaths(t *testing.T) {
	if _, err := NewClient(context.Background(), nil, protocol.InitializeParams{}); err == nil {
		t.Fatal("nil connection initialized")
	}
	serverSide, clientSide := net.Pipe()
	go func() {
		reader := bufio.NewReader(serverSide)
		_, _ = protocoltransport.ReadFrame(reader)
		_ = writeProtocolMessage(serverSide, protocoltransport.Message{ID: json.RawMessage("1"), Result: protocol.InitializeResult{ProtocolMajor: protocol.Major}})
		_, _ = protocoltransport.ReadFrame(reader)
		_, _ = protocoltransport.ReadFrame(reader)
		<-time.After(50 * time.Millisecond)
		_ = serverSide.Close()
	}()
	client, err := NewClient(context.Background(), clientSide, protocol.InitializeParams{ProtocolMajor: protocol.Major})
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
	go client.heartbeat(context.Background(), time.Millisecond, 5*time.Millisecond)
	select {
	case <-client.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("failed heartbeat did not close the client")
	}
	if err := client.Err(); err == nil || !strings.Contains(err.Error(), "daemon heartbeat") {
		t.Fatalf("heartbeat error = %v", err)
	}
	if err := client.Call(context.Background(), "after.close", struct{}{}, nil); err == nil {
		t.Fatal("closed client accepted a call")
	}
	if err := client.RequestRestart(context.Background(), 1); err == nil {
		t.Fatal("closed client requested restart")
	}
	if _, err := client.Upload(context.Background(), protocol.UploadBeginParams{Size: 2}, []byte("x")); err == nil {
		t.Fatal("upload size mismatch was accepted")
	}
	for _, decision := range []protocol.PermissionDecision{
		{RootID: "root", PermissionID: "permission"},
		{CommandID: "command", PermissionID: "permission"},
		{CommandID: "command", RootID: "root"},
	} {
		if _, err := client.DecidePermission(context.Background(), decision); err == nil {
			t.Fatalf("incomplete decision identities were accepted: %+v", decision)
		}
	}
	if _, err := client.DecidePermission(context.Background(), protocol.PermissionDecision{
		CommandID: "command", RootID: "root", PermissionID: "permission", Allow: true,
	}); err == nil {
		t.Fatal("closed client accepted a permission decision")
	}
}

func TestClientRejectsBadInitializeAndSnapshotReplies(t *testing.T) {
	for _, result := range []protocoltransport.Message{
		{ID: json.RawMessage("1"), Error: &protocol.RPCError{Code: -1, Message: "refused", Data: &protocol.ErrorData{Kind: "execution_failed"}}},
		{ID: json.RawMessage("1"), Result: protocol.InitializeResult{ProtocolMajor: 99}},
		{ID: json.RawMessage("1"), Result: "not-an-initialize-result"},
	} {
		serverSide, clientSide := net.Pipe()
		go func() {
			reader := bufio.NewReader(serverSide)
			_, _ = protocoltransport.ReadFrame(reader)
			_ = writeProtocolMessage(serverSide, result)
			_ = serverSide.Close()
		}()
		if _, err := NewClient(context.Background(), clientSide, protocol.InitializeParams{ProtocolMajor: protocol.Major}); err == nil {
			t.Fatalf("bad initialize reply %+v was accepted", result)
		}
	}

	for _, result := range []any{session.RootSnapshot{RootID: "other"}, "invalid", struct{}{}} {
		serverSide, clientSide := net.Pipe()
		go func() {
			defer serverSide.Close()
			reader := bufio.NewReader(serverSide)
			_, _ = protocoltransport.ReadFrame(reader)
			_ = writeProtocolMessage(serverSide, protocoltransport.Message{ID: json.RawMessage("1"), Result: protocol.InitializeResult{ProtocolMajor: protocol.Major}})
			frame, _ := protocoltransport.ReadFrame(reader)
			request, _ := protocoltransport.DecodeFrame(frame)
			_ = writeProtocolMessage(serverSide, protocoltransport.Message{ID: request.ID, Result: result})
		}()
		client, err := NewClient(t.Context(), clientSide, protocol.InitializeParams{ProtocolMajor: protocol.Major})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Snapshot(t.Context(), "root"); err == nil {
			t.Fatal("inconsistent snapshot accepted")
		}
		_ = client.Close()
	}
}

func TestClientConnectionAndReadLoopFailures(t *testing.T) {
	closedServer, closedClient := net.Pipe()
	_ = closedServer.Close()
	if _, err := NewClient(context.Background(), closedClient, protocol.InitializeParams{}); err == nil {
		t.Fatal("client initialized across a closed connection")
	}
	for name, response := range map[string][]byte{
		"eof":           nil,
		"invalid frame": []byte("{}\n"),
	} {
		t.Run(name, func(t *testing.T) {
			serverSide, clientSide := net.Pipe()
			go func() {
				_, _ = protocoltransport.ReadFrame(bufio.NewReader(serverSide))
				if response != nil {
					_, _ = serverSide.Write(response)
				}
				_ = serverSide.Close()
			}()
			if _, err := NewClient(context.Background(), clientSide, protocol.InitializeParams{}); err == nil {
				t.Fatal("client accepted a broken initialization response")
			}
		})
	}

	frames := map[string]protocoltransport.Message{
		"invalid event":  {Method: "event", Params: json.RawMessage(`true`)},
		"event overflow": {Method: "event", Params: mustJSON(t, daemonconn.EventNotification{Event: protocol.ProtocolEvent{RootID: "root", Seq: 2}})},
		"orphan reply":   {ID: json.RawMessage("99"), Result: true},
	}
	for name, message := range frames {
		t.Run(name, func(t *testing.T) {
			serverSide, clientSide := net.Pipe()
			client := &Client{
				conn: protocoltransport.NewUnix(clientSide), pending: make(map[string]chan callResponse),
				commandChanged: make(chan struct{}), events: make(chan protocol.ProtocolEvent, 1), done: make(chan struct{}),
			}
			if name == "event overflow" {
				client.events <- protocol.ProtocolEvent{Seq: 1}
			}
			frame, err := protocoltransport.MarshalFrame(message)
			if err != nil {
				t.Fatal(err)
			}
			written := make(chan struct{})
			go func() {
				defer close(written)
				defer serverSide.Close()
				_, _ = serverSide.Write(frame)
			}()
			client.readLoop()
			<-written
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
		_, _ = protocoltransport.ReadFrame(reader)
		_ = writeProtocolMessage(serverSide, protocoltransport.Message{ID: json.RawMessage("1"), Result: protocol.InitializeResult{ProtocolMajor: protocol.Major}})
		frame, _ := protocoltransport.ReadFrame(reader)
		request, _ := protocoltransport.DecodeFrame(frame)
		_ = writeProtocolMessage(serverSide, protocoltransport.Message{ID: request.ID, Result: "wrong shape"})
		_, _ = protocoltransport.ReadFrame(reader)
		<-time.After(20 * time.Millisecond)
		_ = serverSide.Close()
	}()
	client, err := NewClient(context.Background(), clientSide, protocol.InitializeParams{})
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
				_, _ = protocoltransport.ReadFrame(reader)
				_ = writeProtocolMessage(serverSide, protocoltransport.Message{ID: json.RawMessage("1"), Result: protocol.InitializeResult{ProtocolMajor: protocol.Major}})
				for {
					frame, err := protocoltransport.ReadFrame(reader)
					if err != nil {
						return
					}
					request, _ := protocoltransport.DecodeFrame(frame)
					if request.Method == failedMethod {
						_ = writeProtocolMessage(serverSide, protocoltransport.Message{ID: request.ID, Error: &protocol.RPCError{Code: -1, Message: "stopped", Data: &protocol.ErrorData{Kind: "execution_failed"}}})
						return
					}
					_ = writeProtocolMessage(serverSide, protocoltransport.Message{ID: request.ID, Result: map[string]bool{"accepted": true}})
				}
			}()
			client, err := NewClient(context.Background(), clientSide, protocol.InitializeParams{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Upload(context.Background(), protocol.UploadBeginParams{UploadID: "upload", Size: 1}, []byte("x")); err == nil {
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

func TestClientPermissionDecisionUsesUnsignedPayload(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	requests := make(chan protocoltransport.Message, 1)
	go func() {
		defer serverSide.Close()
		reader := bufio.NewReader(serverSide)
		_, _ = protocoltransport.ReadFrame(reader)
		_ = writeProtocolMessage(serverSide, protocoltransport.Message{
			ID: json.RawMessage("1"), Result: protocol.InitializeResult{ProtocolMajor: protocol.Major},
		})
		frame, _ := protocoltransport.ReadFrame(reader)
		request, _ := protocoltransport.DecodeFrame(frame)
		requests <- request
		_ = writeProtocolMessage(serverSide, protocoltransport.Message{
			ID: request.ID, Result: protocol.PermissionDecisionResult{OperationID: "operation", LeaseID: "lease"},
		})
	}()
	client, err := NewClient(t.Context(), clientSide, protocol.InitializeParams{ProtocolMajor: protocol.Major})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	decision := protocol.PermissionDecision{
		CommandID: "decision-1", RootID: "root", PermissionID: "permission",
		Allow: true, Reason: "approved", Remember: "tree",
	}
	result, err := client.DecidePermission(t.Context(), decision)
	if err != nil || result.OperationID != "operation" || result.LeaseID != "lease" {
		t.Fatalf("decision = %+v, %v", result, err)
	}
	request := <-requests
	want := mustJSON(t, struct {
		Decision protocol.PermissionDecision `json:"decision"`
	}{Decision: decision})
	if request.Method != "permission.decide" || string(request.Params) != string(want) {
		t.Fatalf("decision wire message = %s %s, want permission.decide %s", request.Method, request.Params, want)
	}
}
