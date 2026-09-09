package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

type v2Fixture struct {
	server   *Server
	store    *session.Store
	rootID   string
	dial     func(string, string) *Client
	endpoint string
}

func newV2Fixture(t *testing.T, runner Runner, origins ...string) v2Fixture {
	t.Helper()
	paths, err := Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := openStore(t, filepath.Join(paths.Home, "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(owner, ServerOptions{Generation: 17, BuildID: "daemon-fixture", RuntimeDir: paths.Runtime, Network: NetworkOptions{Enabled: true, AllowedOrigins: origins}})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := listenLocal(paths)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		if err := <-served; err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	initial, err := DialClient(ctx, paths, InitializeParams{ProtocolMajor: ProtocolMajor, BuildID: "different-client-build", ClientKind: "human", ClientID: "probe"})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "ws" + strings.TrimPrefix(initial.InitializeResult().NetworkEndpoint, "http") + "/api/v3/ws"
	if initial.InitializeResult().RuntimeID == "" {
		t.Fatal("missing persistent runtime identity")
	}
	_ = initial.Close()
	return v2Fixture{server: server, endpoint: endpoint, store: store, rootID: rootID, dial: func(transport, clientID string) *Client {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		initialize := InitializeParams{ProtocolMajor: ProtocolMajor, BuildID: "different-client-build", ClientKind: "human", ClientID: clientID}
		var client *Client
		var err error
		if transport == "unix" {
			client, err = DialClient(ctx, paths, initialize)
		} else {
			client, err = DialWebSocketClient(ctx, endpoint, initialize)
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		return client
	}}
}

func TestV2CrossTransportAcceptanceAndReconnect(t *testing.T) {
	for _, transport := range []string{"unix", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			runner := &fakeRunner{turn: func(ctx context.Context, input string, _ bool) (string, error) {
				close(started)
				select {
				case <-release:
					return input, nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}}
			fixture := newV2Fixture(t, runner)
			client := fixture.dial(transport, "retry-client")
			params := CommandParams{CommandID: "durable-command", Scope: "root", RootID: fixture.rootID, Operation: "submit", Payload: json.RawMessage(`{"text":"hello"}`)}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			receipt, err := client.Submit(ctx, params)
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Status != "queued" && receipt.Status != "running" {
				t.Fatalf("acceptance waited for completion: %+v", receipt)
			}
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// The original receipt is deliberately discarded by the reconnecting client.
			_ = client.Close()
			opposite := "unix"
			if transport == "unix" {
				opposite = "websocket"
			}
			reconnect := fixture.dial(opposite, "retry-client")
			duplicate, err := reconnect.Submit(ctx, params)
			if err != nil || duplicate.IngressSeq != receipt.IngressSeq {
				t.Fatalf("duplicate receipt %+v %v", duplicate, err)
			}
			changed := params
			changed.Payload = json.RawMessage(`{"text":"changed"}`)
			if _, err := reconnect.Submit(ctx, changed); err == nil {
				t.Fatal("changed retry accepted")
			}
			close(release)
			result, err := reconnect.SubmitAndWait(ctx, params)
			if err != nil || result.Status != "succeeded" {
				t.Fatalf("completion %+v %v", result, err)
			}
			var text protocol.TextResult
			if err := json.Unmarshal(result.Result, &text); err != nil || text.Text != "hello" {
				t.Fatalf("structured result %s: %v", result.Result, err)
			}
			if runner.calls.Load() != 1 {
				t.Fatalf("executed %d times", runner.calls.Load())
			}
			status, err := reconnect.CommandStatus(ctx, params.CommandID)
			if err != nil || string(status.Result) != string(result.Result) {
				t.Fatalf("status mismatch %+v %v", status, err)
			}
			snapshot, err := reconnect.Snapshot(ctx, fixture.rootID)
			if err != nil || len(snapshot.Messages) != 2 {
				t.Fatalf("snapshot %+v %v", snapshot, err)
			}
			replay, err := reconnect.Replay(ctx, ReplayParams{RootID: fixture.rootID, Cursor: 0, Limit: 100})
			if err != nil || replay.Latest != snapshot.Cursor {
				t.Fatalf("cursor mismatch %+v %v", replay, err)
			}
			if _, err := reconnect.Replay(ctx, ReplayParams{RootID: fixture.rootID, Cursor: replay.Latest + 1, Limit: 100}); err == nil {
				t.Fatal("future cursor accepted")
			}
		})
	}
}

func TestV2CrossTransportSubscriptionBoundaries(t *testing.T) {
	fixture := newV2Fixture(t, &fakeRunner{})
	unix := fixture.dial("unix", "unix-client")
	websocket := fixture.dial("websocket", "websocket-client")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	snapshot, err := unix.Snapshot(ctx, fixture.rootID)
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := websocket.Subscribe(ctx, fixture.rootID, snapshot.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	result, err := unix.SubmitAndWait(ctx, CommandParams{CommandID: "subscription-work", Scope: "root", RootID: fixture.rootID, Operation: "submit", Payload: json.RawMessage(`{"text":"event"}`)})
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("submit %+v %v", result, err)
	}
	select {
	case event := <-websocket.Events():
		if event.Seq <= snapshot.Cursor || event.SubscriptionID != subscription.SubscriptionID {
			t.Fatalf("invalid subscription boundary %+v", event)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for index := 1; index < MaxSubscriptions; index++ {
		err := websocket.Call(ctx, "events.subscribe", SubscribeParams{RootID: fixture.rootID, SubscriptionID: fmt.Sprintf("extra-%d", index), Cursor: snapshot.Cursor}, nil)
		if err != nil {
			t.Fatalf("subscription %d: %v", index, err)
		}
	}
	if err := websocket.Call(ctx, "events.subscribe", SubscribeParams{RootID: fixture.rootID, SubscriptionID: "over-limit", Cursor: snapshot.Cursor}, nil); err == nil {
		t.Fatal("subscription limit ignored")
	}
	if err := websocket.Unsubscribe(ctx, subscription.SubscriptionID); err != nil {
		t.Fatal(err)
	}
	if err := websocket.Call(ctx, "events.subscribe", SubscribeParams{RootID: fixture.rootID, SubscriptionID: "replacement", Cursor: snapshot.Cursor}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestV3CrossTransportPermissionModesWithoutSetup(t *testing.T) {
	fixture := newV2Fixture(t, &permissionModeRunner{fakeRunner: &fakeRunner{}})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for _, transport := range []string{"unix", "websocket"} {
		clientID := transport + "-client"
		client := fixture.dial(transport, clientID)
		for _, external := range []bool{false, true} {
			command := CommandParams{
				CommandID: fmt.Sprintf("permission-%t", external), Scope: "root", RootID: fixture.rootID,
				Operation: "permission.mode", Payload: mustJSON(t, map[string]bool{"external_permissions": external}),
			}
			result, err := client.Command(ctx, command)
			if err != nil || result.Status != "succeeded" {
				t.Fatalf("permission mode external=%t = %+v, %v", external, result, err)
			}
			record, err := fixture.store.LoadCommand(ctx, clientID, command.CommandID)
			if err != nil || record.Status != "succeeded" || record.Operation != "permission.mode" {
				t.Fatalf("durable mode command = %+v, %v", record, err)
			}
			snapshot, err := client.Snapshot(ctx, fixture.rootID)
			if err != nil {
				t.Fatal(err)
			}
			want := "automatic"
			if external {
				want = "prompt"
			}
			if snapshot.PermissionMode != want {
				t.Fatalf("%s snapshot permission mode = %q, want %q", transport, snapshot.PermissionMode, want)
			}
		}
	}
}
