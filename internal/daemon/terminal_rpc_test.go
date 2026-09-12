package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func terminalTestServer(t *testing.T, network NetworkOptions) *Server {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(value, ServerOptions{BuildID: "test-build", Network: network})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server
}

func terminalTestConn(t *testing.T, server *Server, network bool) *serverConn {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	serverSide, clientSide := net.Pipe()
	conn := &serverConn{ctx: ctx, cancel: cancel, server: server, conn: newUnixMessageTransport(serverSide), network: network, id: "conn-" + t.Name(),
		out: make(chan []byte, 512), done: make(chan struct{}), inFlight: make(chan struct{}, 4),
		client: InitializeParams{ClientID: "terminal-test", ClientKind: "human"}, subscriptions: map[string]*subscription{}}
	t.Cleanup(func() { conn.close(); _ = clientSide.Close() })
	return conn
}

func terminalCall(t *testing.T, server *Server, conn *serverConn, method string, params any, into any) *RPCError {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	result, failure := server.handle(conn, rpcMessage{ID: json.RawMessage(`1`), Method: method, Params: raw})
	if failure != nil {
		return failure
	}
	if into != nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, into); err != nil {
			t.Fatalf("decode %s result %s: %v", method, encoded, err)
		}
	}
	return nil
}

// nextNotification returns the next queued notification for method, skipping
// others, or fails after the deadline.
func nextNotification(t *testing.T, conn *serverConn, method string) rpcMessage {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case frame := <-conn.out:
			message, err := decodeFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			if message.Method == method {
				return message
			}
		case <-deadline:
			t.Fatalf("no %s notification arrived", method)
		}
	}
}

// outputUntil concatenates terminal.output notifications until marker appears.
func outputUntil(t *testing.T, conn *serverConn, marker string) (string, int64) {
	t.Helper()
	var text strings.Builder
	var next int64 = -1
	for !strings.Contains(text.String(), marker) {
		message := nextNotification(t, conn, "terminal.output")
		var params protocol.TerminalOutputParams
		if err := json.Unmarshal(message.Params, &params); err != nil {
			t.Fatal(err)
		}
		if next >= 0 && params.Cursor != next {
			t.Fatalf("output cursor %d, want %d", params.Cursor, next)
		}
		next = params.Cursor + int64(len(params.Bytes))
		text.Write(params.Bytes)
	}
	return text.String(), next
}

func TestTerminalRPCOpenWriteOutputClose(t *testing.T) {
	server := terminalTestServer(t, NetworkOptions{})
	conn := terminalTestConn(t, server, false)
	dir := t.TempDir()
	var opened protocol.TerminalOpenResult
	if failure := terminalCall(t, server, conn, "terminal.open", protocol.TerminalOpenParams{Cwd: dir, Cols: 80, Rows: 24}, &opened); failure != nil {
		t.Fatalf("open: %+v", failure)
	}
	if opened.ID == "" || opened.Shell != "/bin/sh" || opened.Cwd != dir {
		t.Fatalf("open result = %+v", opened)
	}
	var attached protocol.TerminalAttachResult
	if failure := terminalCall(t, server, conn, "terminal.attach", protocol.TerminalAttachParams{ID: opened.ID, Cursor: 0}, &attached); failure != nil {
		t.Fatalf("attach: %+v", failure)
	}
	if attached.Cursor != 0 || attached.Exited || attached.Cols != 80 || attached.Rows != 24 || attached.Cwd != dir {
		t.Fatalf("attach result = %+v", attached)
	}
	if failure := terminalCall(t, server, conn, "terminal.write", protocol.TerminalWriteParams{ID: opened.ID, Bytes: []byte("pwd; echo rpc-$((40+2))\n")}, nil); failure != nil {
		t.Fatalf("write: %+v", failure)
	}
	text, _ := outputUntil(t, conn, "rpc-42")
	if !strings.Contains(text, dir) {
		t.Fatalf("shell did not start in %s: %q", dir, text)
	}
	if failure := terminalCall(t, server, conn, "terminal.resize", protocol.TerminalResizeParams{ID: opened.ID, Cols: 120, Rows: 50}, nil); failure != nil {
		t.Fatalf("resize: %+v", failure)
	}
	if failure := terminalCall(t, server, conn, "terminal.write", protocol.TerminalWriteParams{ID: opened.ID, Bytes: []byte("stty size\n")}, nil); failure != nil {
		t.Fatalf("write: %+v", failure)
	}
	outputUntil(t, conn, "50 120")
	if failure := terminalCall(t, server, conn, "terminal.close", protocol.TerminalIDParams{ID: opened.ID}, nil); failure != nil {
		t.Fatalf("close: %+v", failure)
	}
	if failure := terminalCall(t, server, conn, "terminal.attach", protocol.TerminalAttachParams{ID: opened.ID}, nil); failure == nil || failure.Code != -32003 {
		t.Fatalf("attach after close = %+v, want -32003", failure)
	}
	if failure := terminalCall(t, server, conn, "terminal.close", protocol.TerminalIDParams{ID: opened.ID}, nil); failure == nil || failure.Code != -32003 {
		t.Fatalf("second close = %+v, want -32003", failure)
	}
}

func TestTerminalRPCExitIsReportedAfterOutput(t *testing.T) {
	server := terminalTestServer(t, NetworkOptions{})
	conn := terminalTestConn(t, server, false)
	var opened protocol.TerminalOpenResult
	if failure := terminalCall(t, server, conn, "terminal.open", protocol.TerminalOpenParams{Cwd: t.TempDir(), Cols: 80, Rows: 24}, &opened); failure != nil {
		t.Fatalf("open: %+v", failure)
	}
	if failure := terminalCall(t, server, conn, "terminal.attach", protocol.TerminalAttachParams{ID: opened.ID}, nil); failure != nil {
		t.Fatalf("attach: %+v", failure)
	}
	if failure := terminalCall(t, server, conn, "terminal.write", protocol.TerminalWriteParams{ID: opened.ID, Bytes: []byte("echo last-words; exit 5\n")}, nil); failure != nil {
		t.Fatalf("write: %+v", failure)
	}
	outputUntil(t, conn, "last-words")
	message := nextNotification(t, conn, "terminal.exited")
	var exited protocol.TerminalExitedParams
	if err := json.Unmarshal(message.Params, &exited); err != nil {
		t.Fatal(err)
	}
	if exited.ID != opened.ID || exited.ExitCode != 5 || exited.Signal != "" {
		t.Fatalf("exited = %+v", exited)
	}
	if failure := terminalCall(t, server, conn, "terminal.write", protocol.TerminalWriteParams{ID: opened.ID, Bytes: []byte("x")}, nil); failure == nil || failure.Code != -32003 {
		t.Fatalf("write after exit = %+v, want -32003", failure)
	}
	// An exited terminal still attaches: replay first, then the exit again.
	late := terminalTestConn(t, server, false)
	var attached protocol.TerminalAttachResult
	if failure := terminalCall(t, server, late, "terminal.attach", protocol.TerminalAttachParams{ID: opened.ID}, &attached); failure != nil || !attached.Exited || attached.ExitCode != 5 {
		t.Fatalf("late attach = %+v, %+v", attached, failure)
	}
	outputUntil(t, late, "last-words")
	nextNotification(t, late, "terminal.exited")
}

func TestTerminalRPCDisconnectDetachesAndReattachReplays(t *testing.T) {
	server := terminalTestServer(t, NetworkOptions{})
	first := terminalTestConn(t, server, false)
	var opened protocol.TerminalOpenResult
	if failure := terminalCall(t, server, first, "terminal.open", protocol.TerminalOpenParams{Cwd: t.TempDir(), Cols: 80, Rows: 24}, &opened); failure != nil {
		t.Fatalf("open: %+v", failure)
	}
	if failure := terminalCall(t, server, first, "terminal.attach", protocol.TerminalAttachParams{ID: opened.ID}, nil); failure != nil {
		t.Fatalf("attach: %+v", failure)
	}
	if failure := terminalCall(t, server, first, "terminal.write", protocol.TerminalWriteParams{ID: opened.ID, Bytes: []byte("echo one-$((1+1))-marker\n")}, nil); failure != nil {
		t.Fatalf("write: %+v", failure)
	}
	_, seen := outputUntil(t, first, "one-2-marker")
	// The connection drops; the shell keeps running and buffering.
	first.close()
	// An in-flight output callback may already have passed its closed check.
	if first.notify("terminal.output", protocol.TerminalOutputParams{ID: opened.ID, Cursor: seen, Bytes: []byte("late output")}) {
		t.Fatal("disconnected client accepted terminal output")
	}
	server.unregister(first)
	second := terminalTestConn(t, server, false)
	if failure := terminalCall(t, server, second, "terminal.write", protocol.TerminalWriteParams{ID: opened.ID, Bytes: []byte("echo two-$((2+2))-marker\n")}, nil); failure != nil {
		t.Fatalf("write while detached: %+v", failure)
	}
	// Give the shell a moment so the replay, not live output, carries the marker.
	time.Sleep(300 * time.Millisecond)
	var attached protocol.TerminalAttachResult
	if failure := terminalCall(t, server, second, "terminal.attach", protocol.TerminalAttachParams{ID: opened.ID, Cursor: seen}, &attached); failure != nil {
		t.Fatalf("reattach: %+v", failure)
	}
	if attached.Cursor != seen {
		t.Fatalf("replay started at %d, want %d", attached.Cursor, seen)
	}
	text, _ := outputUntil(t, second, "two-4-marker")
	if strings.Contains(text, "one-2-marker") {
		t.Fatalf("replay from cursor repeated earlier output: %q", text)
	}
	// A third attachment takes over and the second is told so.
	third := terminalTestConn(t, server, false)
	if failure := terminalCall(t, server, third, "terminal.attach", protocol.TerminalAttachParams{ID: opened.ID, Cursor: -1}, nil); failure != nil {
		t.Fatalf("third attach: %+v", failure)
	}
	message := nextNotification(t, second, "terminal.detached")
	var detached protocol.TerminalDetachedParams
	if err := json.Unmarshal(message.Params, &detached); err != nil || detached.ID != opened.ID {
		t.Fatalf("detached = %+v, %v", detached, err)
	}
	if failure := terminalCall(t, server, third, "terminal.write", protocol.TerminalWriteParams{ID: opened.ID, Bytes: []byte("echo three-$((3+3))-marker\n")}, nil); failure != nil {
		t.Fatalf("write: %+v", failure)
	}
	outputUntil(t, third, "three-6-marker")
	select {
	case frame := <-second.out:
		message, _ := decodeFrame(frame)
		if message.Method == "terminal.output" {
			t.Fatal("replaced attachment still received output")
		}
	default:
	}
}

func TestTerminalRPCNetworkClientsAreGated(t *testing.T) {
	closed := terminalTestServer(t, NetworkOptions{})
	conn := terminalTestConn(t, closed, true)
	failure := terminalCall(t, closed, conn, "terminal.open", protocol.TerminalOpenParams{Cwd: t.TempDir(), Cols: 80, Rows: 24}, nil)
	if failure == nil || failure.Code != -32012 || !strings.Contains(failure.Message, "NETWORK_TERMINALS") {
		t.Fatalf("network open without the switch = %+v", failure)
	}
	if failure := terminalCall(t, closed, terminalTestConn(t, closed, false), "terminal.open", protocol.TerminalOpenParams{Cwd: t.TempDir(), Cols: 80, Rows: 24}, nil); failure != nil {
		t.Fatalf("unix open on the same daemon = %+v", failure)
	}
	open := terminalTestServer(t, NetworkOptions{Terminals: true})
	if failure := terminalCall(t, open, terminalTestConn(t, open, true), "terminal.open", protocol.TerminalOpenParams{Cwd: t.TempDir(), Cols: 80, Rows: 24}, nil); failure != nil {
		t.Fatalf("network open with the switch = %+v", failure)
	}
}

func TestTerminalRPCValidatesInputAndResolvesCwd(t *testing.T) {
	server := terminalTestServer(t, NetworkOptions{})
	conn := terminalTestConn(t, server, false)
	for name, params := range map[string]protocol.TerminalOpenParams{
		"zero columns": {Cwd: t.TempDir(), Cols: 0, Rows: 24},
		"huge rows":    {Cwd: t.TempDir(), Cols: 80, Rows: 1001},
		"relative cwd": {Cwd: "relative", Cols: 80, Rows: 24},
		"missing cwd":  {Cwd: filepath.Join(t.TempDir(), "missing"), Cols: 80, Rows: 24},
		"file as cwd":  {Cwd: writeTempFile(t), Cols: 80, Rows: 24},
	} {
		if failure := terminalCall(t, server, conn, "terminal.open", params, nil); failure == nil || failure.Code != -32602 {
			t.Errorf("%s: open = %+v, want -32602", name, failure)
		}
	}
	if failure := terminalCall(t, server, conn, "terminal.write", protocol.TerminalWriteParams{ID: "term-missing", Bytes: []byte("x")}, nil); failure == nil || failure.Code != -32003 {
		t.Fatalf("write to unknown terminal = %+v", failure)
	}
	if failure := terminalCall(t, server, conn, "terminal.resize", protocol.TerminalResizeParams{ID: "term-missing", Cols: 0, Rows: 1}, nil); failure == nil || failure.Code != -32602 {
		t.Fatalf("resize with zero columns = %+v", failure)
	}
	home, _ := os.UserHomeDir()
	var opened protocol.TerminalOpenResult
	if failure := terminalCall(t, server, conn, "terminal.open", protocol.TerminalOpenParams{Cols: 80, Rows: 24}, &opened); failure != nil {
		t.Fatalf("open without cwd: %+v", failure)
	}
	if opened.Cwd != home {
		t.Fatalf("default cwd = %q, want home %q", opened.Cwd, home)
	}
	if failure := terminalCall(t, server, conn, "terminal.write", protocol.TerminalWriteParams{ID: opened.ID, Bytes: make([]byte, 16<<10+1)}, nil); failure == nil || failure.Code != -32602 {
		t.Fatalf("oversized write = %+v", failure)
	}
}

func writeTempFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
