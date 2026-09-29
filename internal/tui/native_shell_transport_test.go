package tui

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func nativeShellPeer(t *testing.T, handler func(context.Context, protocol.Request) any) *nativeModel {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "whip-shell-ui-") //nolint:usetesting // Bounded Unix socket path on macOS.
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "rpc")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	var workers sync.WaitGroup
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Go(func() {
				defer func() { _ = conn.Close() }()
				stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
				defer stop()
				scanner := bufio.NewScanner(conn)
				scanner.Buffer(make([]byte, 4096), protocol.MaxFrameBytes)
				for scanner.Scan() {
					var request protocol.Request
					if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
						t.Error(err)
						return
					}
					var result any
					if request.Method == "initialize" {
						result = protocol.InitializeResult{Major: protocol.Major, Minor: protocol.Minor, RuntimeID: "runtime", ProcessEpoch: "boot", Builtins: []protocol.DefinitionRef{}}
					} else {
						result = handler(ctx, request)
					}
					if result == nil {
						return
					}
					raw, err := json.Marshal(result)
					if err != nil {
						t.Error(err)
						return
					}
					if err := json.NewEncoder(conn).Encode(protocol.Response{JSONRPC: "2.0", ID: request.ID, Result: raw}); err != nil {
						return
					}
				}
			})
		}
	}()
	t.Cleanup(func() { cancel(); _ = listener.Close(); <-joined; workers.Wait(); _ = os.RemoveAll(dir) })
	connection, err := client.Connect(t.Context(), socket, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	m, err := newNativeModel(t.Context(), connection, protocol.Session{ID: "owner", TreeID: "tree"})
	if err != nil {
		t.Fatal(err)
	}
	m.ready, m.history.epoch = true, "boot"
	t.Cleanup(m.close)
	return m
}

func TestNativeShellLostAcknowledgementNeverReplaysBufferedKeys(t *testing.T) {
	var mu sync.Mutex
	var received []string
	m := nativeShellPeer(t, func(_ context.Context, request protocol.Request) any {
		mu.Lock()
		defer mu.Unlock()
		switch request.Method {
		case "shell.interaction":
			return protocol.ShellInteractionResult{Interaction: &protocol.ShellInteraction{OperationID: "op", StartedAt: "2026-09-29T00:00:00Z", NextInput: protocol.Counter(len(received) + 1)}}
		case "shell.input":
			var params protocol.ShellInputParams
			if err := json.Unmarshal(request.Params, &params); err != nil {
				t.Error(err)
				return nil
			}
			data, err := base64.StdEncoding.DecodeString(params.DataBase64)
			if err != nil || params.SessionID != "owner" || params.OperationID != "op" || params.Sequence != protocol.Counter(len(received)+1) {
				t.Error("input scope/sequence mismatch", params, err)
				return nil
			}
			received = append(received, string(data))
			if len(received) == 1 {
				return nil // The queue accepted the first write; its acknowledgement is lost.
			}
			return protocol.ShellInputResult{Sequence: params.Sequence}
		}
		t.Error("unexpected shell method", request.Method)
		return nil
	})
	nativeShellFocusFixture(t, m)
	first := m.queueShellInput("accepted")
	m.queueShellInput("must-not-replay")
	_, next := m.Update(first())
	if next != nil || m.shellFocus != nil || !strings.Contains(m.status, "uncertain") {
		t.Fatal("unknown write kept typing authority", m.status)
	}
	m.Update(tea.PasteMsg{Content: "composer-only"})
	if m.input.Value() != "composer-only" {
		t.Fatal("failed shell retained composer routing")
	}
	nativeShellFocusFixture(t, m)
	m.Update(m.queueShellInput("explicit-new")())
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 || received[0] != "accepted" || received[1] != "explicit-new" {
		t.Fatal("unknown input or buffered keys replayed", received)
	}
}

func TestNativeShellDetachJoinsBlockedInputTransport(t *testing.T) {
	entered := make(chan struct{})
	m := nativeShellPeer(t, func(ctx context.Context, request protocol.Request) any {
		if request.Method == "shell.interaction" {
			return protocol.ShellInteractionResult{Interaction: &protocol.ShellInteraction{OperationID: "op", StartedAt: "2026-09-29T00:00:00Z", NextInput: 1}}
		}
		close(entered)
		<-ctx.Done()
		return nil
	})
	nativeShellFocusFixture(t, m)
	command := m.queueShellInput("pending")
	result := make(chan tea.Msg, 1)
	go func() { result <- command() }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("input transport did not start")
	}
	start := time.Now()
	m.close()
	if time.Since(start) > time.Second {
		t.Fatal("detach did not join cancelled input transport promptly")
	}
	select {
	case message := <-result:
		if message.(nativeShellSent).err == nil {
			t.Fatal("interrupted input reported acknowledgment")
		}
		m.Update(message)
	case <-time.After(time.Second):
		t.Fatal("input command outlived joined work owner")
	}
}
