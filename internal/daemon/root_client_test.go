package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

type reconnectServer struct {
	mu       sync.Mutex
	events   []ProtocolEvent
	attempts int
	ids      []string
}

type reconnectConnection struct {
	server *reconnectServer
	events chan ProtocolEvent
	done   chan struct{}
	once   sync.Once
}

func (s *reconnectServer) connect(context.Context, map[string]int64) (RootConnection, error) {
	return &reconnectConnection{server: s, events: make(chan ProtocolEvent, 4), done: make(chan struct{})}, nil
}

func (c *reconnectConnection) Command(_ context.Context, params CommandParams) (CommandResult, error) {
	c.server.mu.Lock()
	c.server.attempts++
	c.server.ids = append(c.server.ids, params.CommandID)
	attempt := c.server.attempts
	if len(c.server.events) == 0 {
		c.server.events = append(c.server.events, ProtocolEvent{RootID: "root", Seq: 1, Kind: "stream.text", Payload: []byte(`{"text":"one"}`)})
	}
	event := c.server.events[0]
	c.server.mu.Unlock()
	if attempt == 1 {
		c.events <- event
		_ = c.Close()
		return CommandResult{}, net.ErrClosed
	}
	return CommandResult{CommandID: params.CommandID, Status: "succeeded", Output: "one"}, nil
}

func (c *reconnectConnection) Replay(_ context.Context, params ReplayParams) (ReplayResult, error) {
	c.server.mu.Lock()
	defer c.server.mu.Unlock()
	result := ReplayResult{Latest: int64(len(c.server.events))}
	for _, event := range c.server.events {
		if event.Seq > params.Cursor {
			result.Events = append(result.Events, event)
		}
	}
	return result, nil
}

func (*reconnectConnection) Snapshot(context.Context, string) (session.RootSnapshot, error) {
	return session.RootSnapshot{RootID: "root"}, nil
}
func (c *reconnectConnection) Events() <-chan ProtocolEvent { return c.events }
func (c *reconnectConnection) Done() <-chan struct{}        { return c.done }
func (*reconnectConnection) Err() error                     { return net.ErrClosed }
func (c *reconnectConnection) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func TestRootClientReconnectsStableCommandWithoutDuplicateEvent(t *testing.T) {
	server := &reconnectServer{}
	client, err := NewRootClient(RootClientOptions{
		ClientID: "test", RootID: "root", Connector: server.connect,
		RetryMin: time.Millisecond, RetryMax: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	if err := client.WaitLive(t.Context()); err != nil {
		t.Fatal(err)
	}
	action, err := client.NewAction("submit", SubmitPayload{Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Command(t.Context(), action)
	if err != nil || result.Output != "one" {
		t.Fatalf("command = %+v, %v", result, err)
	}
	deadline := time.After(time.Second)
	events := 0
	for events < 1 {
		select {
		case update := <-client.Updates():
			if update.Event != nil {
				events++
			}
		case <-deadline:
			t.Fatal("event was not replayed")
		}
	}
	time.Sleep(10 * time.Millisecond)
	for {
		select {
		case update := <-client.Updates():
			if update.Event != nil {
				events++
			}
		default:
			if events != 1 {
				t.Fatalf("events = %d, want one", events)
			}
			server.mu.Lock()
			defer server.mu.Unlock()
			if server.attempts != 2 || len(server.ids) != 2 || server.ids[0] != server.ids[1] {
				t.Fatalf("attempts=%d ids=%v", server.attempts, server.ids)
			}
			return
		}
	}
}

type failingRootConnection struct{ reconnectConnection }

func (*failingRootConnection) Snapshot(context.Context, string) (session.RootSnapshot, error) {
	return session.RootSnapshot{}, errors.New("no session")
}

func TestRootClientStopsOnPermanentSynchronizationError(t *testing.T) {
	client, err := NewRootClient(RootClientOptions{
		ClientID: "test", RootID: "missing",
		Connector: func(context.Context, map[string]int64) (RootConnection, error) {
			return &failingRootConnection{reconnectConnection{events: make(chan ProtocolEvent), done: make(chan struct{})}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	if err := client.WaitLive(t.Context()); err == nil || err.Error() != "no session" {
		t.Fatalf("WaitLive error = %v", err)
	}
	_ = client.Close()
}

func TestRootClientClosesBeforeStart(t *testing.T) {
	client, err := NewRootClient(RootClientOptions{
		ClientID: "client", RootID: "root",
		Connector: func(context.Context, map[string]int64) (RootConnection, error) {
			t.Fatal("closed client attempted to connect")
			return nil, errors.New("unexpected connection")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	client.Start()
	select {
	case <-client.Updates():
	default:
		t.Fatal("updates remained open after close")
	}
}

type staticRootConnection struct {
	events     chan ProtocolEvent
	done       chan struct{}
	once       sync.Once
	commandErr error
}

func newStaticRootConnection() *staticRootConnection {
	return &staticRootConnection{events: make(chan ProtocolEvent), done: make(chan struct{})}
}

func (c *staticRootConnection) Command(_ context.Context, params CommandParams) (CommandResult, error) {
	if c.commandErr != nil {
		return CommandResult{}, c.commandErr
	}
	return CommandResult{CommandID: params.CommandID, Status: "succeeded", Output: "ok"}, nil
}

func (*staticRootConnection) Replay(context.Context, ReplayParams) (ReplayResult, error) {
	return ReplayResult{}, nil
}

func (*staticRootConnection) Snapshot(_ context.Context, rootID string) (session.RootSnapshot, error) {
	return session.RootSnapshot{RootID: rootID}, nil
}

func (c *staticRootConnection) Events() <-chan ProtocolEvent { return c.events }
func (c *staticRootConnection) Done() <-chan struct{}        { return c.done }
func (c *staticRootConnection) Err() error                   { return c.commandErr }
func (c *staticRootConnection) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

type privilegedRootConnection struct {
	*staticRootConnection
	decision PermissionDecision
	mode     CommandParams
}

func (c *privilegedRootConnection) DecidePermission(_ context.Context, _ ed25519.PrivateKey, decision PermissionDecision) (PermissionDecisionResult, error) {
	c.decision = decision
	return PermissionDecisionResult{OperationID: "operation", LeaseID: "lease"}, nil
}

func (c *privilegedRootConnection) SetPermissionMode(_ context.Context, _ ed25519.PrivateKey, command CommandParams) (CommandResult, error) {
	c.mode = command
	return CommandResult{CommandID: command.CommandID, Status: "succeeded", Output: "configured"}, nil
}

func TestRootClientValidationAndDisconnectedSurface(t *testing.T) {
	for state, want := range map[RootClientState]string{
		RootDisconnected: "disconnected", RootReconnecting: "reconnecting",
		RootSnapshotting: "snapshotting", RootLive: "live", RootClientState(99): "unknown",
	} {
		if got := state.String(); got != want {
			t.Errorf("state %d = %q, want %q", state, got, want)
		}
	}
	connector := func(context.Context, map[string]int64) (RootConnection, error) {
		return newStaticRootConnection(), nil
	}
	for _, options := range []RootClientOptions{
		{RootID: "root", Connector: connector},
		{ClientID: "client", Connector: connector},
		{ClientID: "client", RootID: "root", Create: &CreateSession{}, Connector: connector},
	} {
		if _, err := NewRootClient(options); err == nil {
			t.Fatalf("invalid options accepted: %+v", options)
		}
	}
	client, err := NewRootClient(RootClientOptions{ClientID: "client", RootID: "root", Connector: connector})
	if err != nil {
		t.Fatal(err)
	}
	if client.State() != RootDisconnected {
		t.Fatalf("initial state = %s", client.State())
	}
	if _, err := client.Snapshot(t.Context()); err == nil {
		t.Fatal("snapshot should be unavailable before start")
	}
	if _, err := client.NewAction("", struct{}{}); err == nil {
		t.Fatal("empty operation should fail")
	}
	if _, err := client.NewAction("bad-payload", func() {}); err == nil {
		t.Fatal("unmarshalable payload should fail")
	}
	action, err := client.NewAction("submit", SubmitPayload{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if action.RootID != "root" || action.CommandID == "" {
		t.Fatalf("action = %+v", action)
	}
	if _, err := client.Command(t.Context(), RootAction{}); err == nil {
		t.Fatal("identity-free command should fail")
	}
	if _, err := client.Command(t.Context(), action); err == nil {
		t.Fatal("command should be disabled before start")
	}
	permission, err := client.NewAction("permission.decide", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.DecidePermission(t.Context(), permission, "", true, "", ""); err == nil {
		t.Fatal("permission without an id should fail")
	}
	if _, err := client.DecidePermission(t.Context(), permission, "permission", true, "", ""); err == nil {
		t.Fatal("permission without a private key should fail")
	}
	mode, err := client.NewAction("permission.mode", map[string]bool{"external_permissions": false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetPermissionMode(t.Context(), RootAction{}, false); err == nil {
		t.Fatal("identity-free permission mode should fail")
	}
	if _, err := client.SetPermissionMode(t.Context(), mode, false); err == nil {
		t.Fatal("automatic mode without a private key should fail")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRootClientProcessInstancesNeverReuseActionOrCreationIDs(t *testing.T) {
	connector := func(context.Context, map[string]int64) (RootConnection, error) {
		return newStaticRootConnection(), nil
	}
	first, err := NewRootClient(RootClientOptions{ClientID: "durable", RootID: "root", Connector: connector})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRootClient(RootClientOptions{ClientID: "durable", RootID: "root", Connector: connector})
	if err != nil {
		t.Fatal(err)
	}
	firstAction, _ := first.NewAction("submit", SubmitPayload{Text: "one"})
	secondAction, _ := second.NewAction("submit", SubmitPayload{Text: "two"})
	if firstAction.CommandID == secondAction.CommandID {
		t.Fatalf("separate process instances reused action id %q", firstAction.CommandID)
	}
	firstCreationID := first.clientID + "-session-" + first.instanceID
	secondCreationID := second.clientID + "-session-" + second.instanceID
	if firstCreationID == secondCreationID {
		t.Fatalf("separate process instances reused session creation id %q", firstCreationID)
	}
	if first.instanceID == second.instanceID || first.instanceID == "" || second.instanceID == "" {
		t.Fatalf("process nonces first=%q second=%q", first.instanceID, second.instanceID)
	}
	_ = first.Close()
	_ = second.Close()
}

func TestRootClientSwitchRootClosesConnectionAndResynchronizes(t *testing.T) {
	first := newStaticRootConnection()
	second := newStaticRootConnection()
	connections := []*staticRootConnection{first, second}
	var calls int
	client, err := NewRootClient(RootClientOptions{
		ClientID: "client", RootID: "one", RetryMin: time.Millisecond, RetryMax: time.Millisecond,
		Connector: func(context.Context, map[string]int64) (RootConnection, error) {
			connection := connections[min(calls, len(connections)-1)]
			calls++
			return connection, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	if err := client.WaitLive(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.SwitchRoot("two"); err != nil {
		t.Fatal(err)
	}
	if err := client.WaitLive(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.Done():
	default:
		t.Fatal("old root connection remained open")
	}
	if client.RootID() != "two" || client.Cursor() != 0 || calls < 2 {
		t.Fatalf("switched root=%q cursor=%d connects=%d", client.RootID(), client.Cursor(), calls)
	}
}

func TestRootClientReceivesEventsAfterExpiredCursorSnapshot(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, databasePath)
	t.Cleanup(func() { _ = store.Close() })
	rootID := createRoot(t, store)
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.ExecContext(t.Context(), `INSERT INTO events(root_id,seq,kind,created_at) VALUES(?,?,?,?),(?,?,?,?)`,
		rootID, 2, "fixture", stamp, rootID, session.EventRetention+1, "fixture", stamp); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

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
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		<-served
	})

	client, err := NewRootClient(RootClientOptions{
		ClientID: "expired-cursor", RootID: rootID, RetryMin: time.Millisecond, RetryMax: time.Millisecond,
		Connector: func(ctx context.Context, cursors map[string]int64) (RootConnection, error) {
			connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
			if err != nil {
				return nil, err
			}
			return NewClient(ctx, connection, InitializeParams{
				ProtocolMajor: ProtocolMajor, ClientID: "expired-cursor", ClientKind: "test", Cursors: cursors,
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	if err := client.WaitLive(t.Context()); err != nil {
		t.Fatal(err)
	}
	sequence, err := store.AppendRootEvent(t.Context(), rootID, "fresh", session.RuntimePayload{Data: []byte(`{"ok":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case update := <-client.Updates():
			if update.Event != nil && update.Event.Seq == sequence && update.Event.Kind == "fresh" {
				return
			}
		case <-deadline:
			t.Fatal("live event pump stopped after the expired-cursor snapshot")
		}
	}
}

func TestRootClientRetriesAndUsesPrivilegedConnection(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	connection := &privilegedRootConnection{staticRootConnection: newStaticRootConnection()}
	attempts := 0
	client, err := NewRootClient(RootClientOptions{
		ClientID: "paired", PrivateKey: private, RootID: "root",
		RetryMin: time.Millisecond, RetryMax: time.Millisecond,
		Connector: func(context.Context, map[string]int64) (RootConnection, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("daemon starting")
			}
			return connection, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	defer func() { _ = client.Close() }()
	if err := client.WaitLive(t.Context()); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || client.State() != RootLive || client.Cursor() != 0 || client.Err() != nil {
		t.Fatalf("attempts=%d state=%s cursor=%d err=%v", attempts, client.State(), client.Cursor(), client.Err())
	}
	if snapshot, err := client.Snapshot(t.Context()); err != nil || snapshot.RootID != "root" {
		t.Fatalf("snapshot = %+v, %v", snapshot, err)
	}
	external, _ := client.NewAction("permission.mode", map[string]bool{"external_permissions": true})
	if result, err := client.SetPermissionMode(t.Context(), external, true); err != nil || result.Output != "ok" {
		t.Fatalf("external mode = %+v, %v", result, err)
	}
	automatic, _ := client.NewAction("permission.mode", map[string]bool{"external_permissions": false})
	if result, err := client.SetPermissionMode(t.Context(), automatic, false); err != nil || result.Output != "configured" {
		t.Fatalf("automatic mode = %+v, %v", result, err)
	}
	permission, _ := client.NewAction("permission.decide", struct{}{})
	decision, err := client.DecidePermission(t.Context(), permission, "permission-1", true, "approved", "")
	if err != nil || decision.LeaseID != "lease" {
		t.Fatalf("permission decision = %+v, %v", decision, err)
	}
	if connection.decision.PermissionID != "permission-1" || !connection.decision.Allow || connection.mode.Operation != "permission.mode" {
		t.Fatalf("privileged calls decision=%+v mode=%+v", connection.decision, connection.mode)
	}
}

func TestRootClientReportsUnsupportedPrivilegesAndCommandFailure(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	connection := newStaticRootConnection()
	client, err := NewRootClient(RootClientOptions{
		ClientID: "plain", PrivateKey: private, RootID: "root",
		Connector: func(context.Context, map[string]int64) (RootConnection, error) { return connection, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	defer func() { _ = client.Close() }()
	if err := client.WaitLive(t.Context()); err != nil {
		t.Fatal(err)
	}
	permission, _ := client.NewAction("permission.decide", struct{}{})
	if _, err := client.DecidePermission(t.Context(), permission, "permission", true, "", ""); err == nil {
		t.Fatal("plain connection should not approve permissions")
	}
	mode, _ := client.NewAction("permission.mode", map[string]bool{"external_permissions": false})
	if _, err := client.SetPermissionMode(t.Context(), mode, false); err == nil {
		t.Fatal("plain connection should not authorize automatic mode")
	}
	connection.commandErr = errors.New("command rejected")
	action, _ := client.NewAction("submit", SubmitPayload{Text: "hello"})
	if _, err := client.Command(t.Context(), action); !errors.Is(err, connection.commandErr) {
		t.Fatalf("command error = %v", err)
	}
}
