package webgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/protocoltransport"
	"github.com/gobwas/ws"
)

func TestRestrictedInitializePreservesFieldsAndCompacts(t *testing.T) {
	t.Parallel()
	original := []byte(`{
 "jsonrpc":"2.0", "id":"original-id", "method":"initialize",
 "params":{"protocol_major":3,"client_id":"chosen-client","client_kind":"desktop",
 "build_id":"browser","capabilities":["provider-v1"],"unknown":{"provider":"keep-me"},"cursors":{"root":"42"}}
 }`)
	forced, id, err := restrictInitialize(original)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(forced), "\n") || string(id) != `"original-id"` {
		t.Fatalf("unsafe frame %s", forced)
	}
	var envelope struct{ Params map[string]json.RawMessage }
	if err := json.Unmarshal(forced, &envelope); err != nil {
		t.Fatal(err)
	}
	if string(envelope.Params["unknown"]) != `{"provider":"keep-me"}` || string(envelope.Params["client_id"]) != `"chosen-client"` ||
		string(envelope.Params["cursors"]) != `{"root":"42"}` {
		t.Fatalf("lost initialization fields: %s", forced)
	}
	var capabilities []string
	_ = json.Unmarshal(envelope.Params["capabilities"], &capabilities)
	if !slices.Contains(capabilities, "provider-v1") || !slices.Contains(capabilities, protocol.NetworkClientCapability) {
		t.Fatalf("capabilities: %v", capabilities)
	}
}

func TestInitializeCaseVariantsCannotReorderIntoDowngrade(t *testing.T) {
	t.Parallel()
	forced, _, err := restrictInitialize([]byte(`{"jsonrpc":"2.0","id":3,"method":"command.submit","Method":"initialize","params":{"capabilities":[],"Capabilities":["provider"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	message, err := protocoltransport.DecodeFrame(forced)
	if err != nil || message.Method != "initialize" {
		t.Fatalf("rewriting changed method: %s %v", forced, err)
	}
	var params protocol.InitializeParams
	if err := json.Unmarshal(message.Params, &params); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(params.Capabilities, protocol.NetworkClientCapability) {
		t.Fatal("marker lost")
	}
}

func TestRejectsSmuggledOrUninitializedFrames(t *testing.T) {
	t.Parallel()
	for _, frame := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"command.submit"}`,
		`[{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}]`,
		`{"jsonrpc":"2.0","id":1,"method":"command.submit","params":{}}`,
		`{"jsonrpc":"2.0","method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":null,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":null}`,
	} {
		if _, _, err := restrictInitialize([]byte(frame)); err == nil {
			t.Errorf("accepted %s", frame)
		}
	}
}

// rawBackend owns isolated Unix sockets and drains all workers at test cleanup.
func rawBackend(t *testing.T, serve func(protocoltransport.Transport)) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "wg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	socket := filepath.Join(directory, "backend.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	var mu sync.Mutex
	var connections []net.Conn
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
			workers.Go(func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				serve(protocoltransport.NewUnix(conn))
			})
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-accepted
		mu.Lock()
		for _, conn := range connections {
			_ = conn.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	return socket
}

func dialBrowser(t *testing.T, endpoint string) *protocoltransport.WebSocket {
	t.Helper()
	conn, reader, _, err := (ws.Dialer{Timeout: time.Second}).Dial(t.Context(), "ws"+strings.TrimPrefix(endpoint, "http")+"/api/v3/ws")
	if err != nil {
		t.Fatal(err)
	}
	var source io.Reader = conn
	if reader != nil {
		source = reader
	}
	transport := protocoltransport.NewWebSocketClient(conn, source)
	t.Cleanup(func() { _ = transport.Close() })
	_ = transport.SetReadDeadline(time.Now().Add(3 * time.Second))
	return transport
}

func sendFrame(t *testing.T, transport protocoltransport.Transport, frame string) {
	t.Helper()
	if err := transport.WriteMessage([]byte(frame)); err != nil {
		t.Fatal(err)
	}
}
func readMessage(t *testing.T, transport protocoltransport.Transport) protocoltransport.Message {
	t.Helper()
	frame, err := transport.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	message, err := protocoltransport.DecodeFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func TestHandshakeFailClosedBeforePipelinedTraffic(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"old-daemon", "different-runtime", "different-generation"} {
		t.Run(mode, func(t *testing.T) {
			result := newFakeClient().init
			switch mode {
			case "old-daemon":
				result.NegotiatedCapabilities = nil
			case "different-runtime":
				result.RuntimeID = "replacement"
			case "different-generation":
				result.Generation++
			}
			closed := make(chan error, 1)
			socket := rawBackend(t, func(upstream protocoltransport.Transport) {
				first, err := upstream.ReadMessage()
				if err != nil {
					closed <- err
					return
				}
				var req protocoltransport.Message
				req, err = protocoltransport.DecodeFrame(first)
				if err != nil {
					closed <- err
					return
				}
				var params protocol.InitializeParams
				if err := json.Unmarshal(req.Params, &params); err != nil {
					closed <- err
					return
				}
				if !slices.Contains(params.Capabilities, protocol.NetworkClientCapability) {
					closed <- fmt.Errorf("missing network marker")
					return
				}
				reply, _ := protocoltransport.MarshalFrame(protocoltransport.Message{ID: req.ID, Result: result})
				if err := upstream.WriteMessage(reply); err != nil {
					closed <- err
					return
				}
				if frame, err := upstream.ReadMessage(); err == nil {
					closed <- fmt.Errorf("forwarded before ack: %s", frame)
				} else {
					closed <- nil
				}
			})
			s := testServer(t, socket, newFakeClient())
			browser := dialBrowser(t, s.Endpoint())
			sendFrame(t, browser, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocol_major":3}}`)
			sendFrame(t, browser, `{"jsonrpc":"2.0","id":2,"method":"daemon.ping"}`)
			reply := readMessage(t, browser)
			if reply.Error == nil {
				t.Fatal("incompatible daemon accepted")
			}
			if err := <-closed; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIndependentRelaysPreserveTrafficAndClose(t *testing.T) {
	t.Parallel()
	socket := rawBackend(t, func(upstream protocoltransport.Transport) {
		frame, err := upstream.ReadMessage()
		if err != nil {
			return
		}
		message, err := protocoltransport.DecodeFrame(frame)
		if err != nil {
			return
		}
		reply, _ := protocoltransport.MarshalFrame(protocoltransport.Message{ID: message.ID, Result: newFakeClient().init})
		if upstream.WriteMessage(reply) != nil {
			return
		}
		for {
			frame, err := upstream.ReadMessage()
			if err != nil {
				return
			}
			message, err := protocoltransport.DecodeFrame(frame)
			if err != nil || message.Method == "initialize" {
				t.Error("invalid or repeated initialization reached backend")
				return
			}
			if err := upstream.WriteMessage(frame); err != nil {
				return
			}
		}
	})
	s := testServer(t, socket, newFakeClient())
	first, second := dialBrowser(t, s.Endpoint()), dialBrowser(t, s.Endpoint())
	for _, browser := range []protocoltransport.Transport{first, second} {
		sendFrame(t, browser, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
		if message := readMessage(t, browser); message.Error != nil {
			t.Fatal(message.Error)
		}
	}
	// Provider responses, notifications and IDs remain on their own connection.
	for i, browser := range []protocoltransport.Transport{first, second} {
		frame := fmt.Sprintf("{\n \"jsonrpc\":\"2.0\",\n \"id\":\"provider-%d\", \"result\":{\"keep\":true}}", i)
		sendFrame(t, browser, frame)
		message := readMessage(t, browser)
		if !bytes.Equal(message.ID, []byte(fmt.Sprintf(`"provider-%d"`, i))) {
			t.Fatalf("id changed %s", message.ID)
		}
	}
	sendFrame(t, first, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"capabilities":[]}}`)
	if readMessage(t, first).Error == nil {
		t.Fatal("reinitialization not rejected")
	}
	sendFrame(t, second, `{"jsonrpc":"2.0","method":"notify","params":{"ok":true}}`)
	if readMessage(t, second).Method != "notify" {
		t.Fatal("one browser closed another")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := second.ReadMessage(); err == nil {
		t.Fatal("hijacked socket outlived gateway")
	}
}

func TestClosingWhileHandshakePending(t *testing.T) {
	t.Parallel()
	s := testServer(t, "unused", newFakeClient())
	browser := dialBrowser(t, s.Endpoint())
	closed := make(chan struct{})
	go func() { _ = s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for handshake timeout")
	}
	if _, err := browser.ReadMessage(); err == nil {
		t.Fatal("pending browser survived shutdown")
	}
}
