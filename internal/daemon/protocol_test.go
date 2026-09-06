package daemon

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/gobwas/ws"
)

func TestResponseEnvelopeHasExactlyOneOutcomeOnBothTransports(t *testing.T) {
	for _, transportName := range []string{"unix", "websocket"} {
		t.Run(transportName, func(t *testing.T) {
			for _, test := range []struct {
				name    string
				result  any
				failure *RPCError
			}{
				{
					name: "partial history with recovery error", result: session.BoundedTranscriptPage{},
					failure: rpcFailure(-32010, "history revision changed"),
				},
				{
					name: "unused result cannot prevent error encoding", result: make(chan int),
					failure: rpcFailure(-32602, "invalid arguments"),
				},
				{name: "successful nil result"},
				{name: "successful object result", result: map[string]bool{"accepted": true}},
			} {
				t.Run(test.name, func(t *testing.T) {
					serverConn, clientConn := net.Pipe()
					defer func() { _ = serverConn.Close(); _ = clientConn.Close() }()
					if err := clientConn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
						t.Fatal(err)
					}
					var sender messageTransport = newUnixMessageTransport(serverConn)
					var receiver messageTransport = newUnixMessageTransport(clientConn)
					if transportName == "websocket" {
						sender = newWebsocketMessageTransport(serverConn, serverConn)
						client := newWebsocketMessageTransport(clientConn, clientConn)
						client.clientSide = true
						client.reader.State = ws.StateClientSide
						receiver = client
					}
					written := make(chan error, 1)
					go func() {
						written <- writeTransportMessage(sender, rpcMessage{
							ID: json.RawMessage(`"request"`), Result: test.result, Error: test.failure,
						})
					}()
					frame, err := receiver.ReadMessage()
					if err != nil {
						t.Fatal(err)
					}
					if err := <-written; err != nil {
						t.Fatal(err)
					}
					var envelope map[string]json.RawMessage
					if err := json.Unmarshal(frame, &envelope); err != nil {
						t.Fatal(err)
					}
					_, resultPresent := envelope["result"]
					_, errorPresent := envelope["error"]
					if resultPresent == errorPresent || errorPresent != (test.failure != nil) {
						t.Fatalf("invalid response envelope: %s", frame)
					}
					if string(envelope["id"]) != `"request"` {
						t.Fatalf("response ID changed: %s", frame)
					}
					if test.failure == nil && test.result == nil && string(envelope["result"]) != "null" {
						t.Fatalf("nil success must be explicit JSON null: %s", frame)
					}
				})
			}
		})
	}
}
