package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/gobwas/ws"
)

func TestRuntimeAcknowledgementIsRequiredBeforeRelay(t *testing.T) {
	for _, field := range []string{"network_client", "process_epoch", "runtime_id"} {
		t.Run(field, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "whip-gateway-fake-") //nolint:usetesting // Keep Unix socket path short on macOS.
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			path := filepath.Join(dir, "runtime.sock")
			listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", path)
			if err != nil {
				t.Fatal(err)
			}
			var workers sync.WaitGroup
			defer func() { _ = listener.Close(); workers.Wait() }()
			var forwarded bool
			workers.Go(func() {
				for attempt := range 2 {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					func() {
						defer conn.Close()
						_ = conn.SetDeadline(time.Now().Add(time.Second))
						stream := NewUnix(conn)
						raw, err := stream.ReadMessage()
						if err != nil {
							return
						}
						var request protocol.Request
						_ = json.Unmarshal(raw, &request)
						result := map[string]any{"major": 4, "minor": 0, "runtime_id": "runtime", "process_epoch": "epoch", "network_client": true, "builtins": []any{}}
						if attempt == 1 {
							switch field {
							case "network_client":
								delete(result, field)
							case "process_epoch":
								result[field] = "wrong"
							case "runtime_id":
								result[field] = "wrong"
							}
						}
						value, _ := json.Marshal(result)
						reply, _ := json.Marshal(protocol.Response{JSONRPC: "2.0", ID: request.ID, Result: value})
						_ = stream.WriteMessage(reply)
						if attempt == 1 {
							_, err = stream.ReadMessage()
							forwarded = err == nil
						}
					}()
				}
			})
			s, err := Start(t.Context(), Options{Address: "127.0.0.1:0", SocketPath: path, RuntimeID: "runtime", ProcessEpoch: "epoch", BackendDone: make(chan struct{})})
			if err != nil {
				t.Fatal(err)
			}
			stream := browser(t, s)
			first := []byte(`{"jsonrpc":"2.0","id":"first","method":"initialize","params":{"major":4}}`)
			second := []byte(`{"jsonrpc":"2.0","id":"second","method":"trees.catalog","params":{}}`)
			if err := stream.WriteMessage(first); err != nil {
				t.Fatal(err)
			}
			_ = stream.WriteMessage(second)
			raw, err := stream.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			var reply protocol.Response
			if json.Unmarshal(raw, &reply) != nil || reply.Error == nil {
				t.Fatalf("accepted missing/wrong ack: %s", raw)
			}
			_ = s.Close()
			_ = listener.Close()
			workers.Wait()
			if forwarded {
				t.Fatal("pipelined request reached backend before restricted acknowledgement")
			}
		})
	}
}

func TestConnectionAndTransferReservationsAreBoundedAndJoined(t *testing.T) {
	s, _, _ := gatewayFixture(t)
	var streams []*WebSocket
	for range maxConnections {
		streams = append(streams, browser(t, s))
	}
	conn, _, _, err := ws.Dial(t.Context(), "ws"+s.Endpoint()[4:]+"/api/v4/ws")
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatal("accepted beyond connection limit")
	}
	for range maxTransfers {
		if !s.acquire(s.transfers) {
			t.Fatal("missing transfer capacity")
		}
	}
	if s.acquire(s.transfers) {
		t.Fatal("accepted excess transfer")
	}
	done := make(chan struct{})
	go func() { _ = s.Close(); close(done) }()
	for range maxTransfers {
		s.release(s.transfers)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not join reservations")
	}
	if s.acquire(s.transfers) || s.acquire(s.connections) {
		t.Fatal("shutdown admitted new reservations")
	}
	for _, stream := range streams {
		_ = stream.Close()
	}
}

func TestFramingBoundsFragmentationUTF8AndPing(t *testing.T) {
	for _, size := range []int{0, 4096, maxFrameBytes - 1, maxFrameBytes} {
		input := append(bytes.Repeat([]byte("x"), size), '\n')
		result, err := ReadFrame(bufio.NewReader(bytes.NewReader(input)))
		if size+1 > maxFrameBytes {
			if !errors.Is(err, ErrFrameTooLarge) {
				t.Fatal(err)
			}
		} else if err != nil || !bytes.Equal(input, result) {
			t.Fatalf("size=%d %v", size, err)
		}
	}
	if _, err := ReadFrame(bufio.NewReader(bytes.NewReader([]byte("unterminated")))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"jsonrpc":"2.0","x":{"a":1,"a":2}}`, `{"jsonrpc":"2.0","x":{"a":1,"\u0061":2}}`, `{"jsonrpc":"2.0"} true`} {
		if _, _, err := compactEnvelope([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, _, err := compactEnvelope([]byte(`{"jsonrpc":"2.0","value":1234567890123456789012345678901234567890}`)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		frames    []ws.Frame
		wantError bool
	}{
		{"fragmented", []ws.Frame{ws.NewFrame(ws.OpText, false, []byte("hel")), ws.NewFrame(ws.OpContinuation, true, []byte("lo"))}, false},
		{"invalid utf8", []ws.Frame{ws.NewFrame(ws.OpText, true, []byte{0xff})}, true},
		{"binary", []ws.Frame{ws.NewFrame(ws.OpBinary, true, []byte("no"))}, true},
		{"aggregate size", []ws.Frame{ws.NewFrame(ws.OpText, false, bytes.Repeat([]byte("a"), maxFrameBytes/2)), ws.NewFrame(ws.OpContinuation, true, bytes.Repeat([]byte("b"), maxFrameBytes/2+1))}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var data bytes.Buffer
			for _, frame := range tc.frames {
				if err := ws.WriteFrame(&data, ws.MaskFrame(frame)); err != nil {
					t.Fatal(err)
				}
			}
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()
			stream := NewWebSocket(server, &data)
			raw, err := stream.ReadMessage()
			if (err != nil) != tc.wantError {
				t.Fatalf("result=%d err=%v", len(raw), err)
			}
			if !tc.wantError && string(raw) != "hello" {
				t.Fatal(string(raw))
			}
		})
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	stream := NewWebSocket(server, server)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	stop := context.AfterFunc(ctx, func() { _ = server.Close() })
	defer stop()
	done := make(chan error, 1)
	go func() { _, err := stream.ReadMessage(); done <- err }()
	if err := ws.WriteFrame(client, ws.MaskFrame(ws.NewPingFrame([]byte("ping")))); err != nil {
		t.Fatal(err)
	}
	frame, err := ws.ReadFrame(client)
	if err != nil || frame.Header.OpCode != ws.OpPong || string(frame.Payload) != "ping" {
		t.Fatalf("pong=%v %v", frame, err)
	}
	_ = client.Close()
	<-done
}
