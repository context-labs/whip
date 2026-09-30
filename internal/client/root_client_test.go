package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

type statusRootConnection struct {
	*staticRootConnection
	status string
}

func (c *statusRootConnection) Command(_ context.Context, params protocol.CommandParams) (protocol.CommandResult, error) {
	return protocol.CommandResult{CommandID: params.CommandID, Status: c.status}, nil
}

func TestRootClientCancellationTargetsPreserveStatusBoundaries(t *testing.T) {
	for _, test := range []struct {
		status   string
		terminal bool
	}{
		{"succeeded", true}, {"failed", true}, {"cancelled", true}, {"interrupted", true},
		{"queued", false}, {"running", false}, {"waiting", false},
		{"stopped", false}, {"deleted", false}, {"unknown", false},
		{"", false}, {"Succeeded", false}, {" succeeded", false},
	} {
		t.Run(fmt.Sprintf("status=%q", test.status), func(t *testing.T) {
			connection := &statusRootConnection{staticRootConnection: newStaticRootConnection(), status: test.status}
			client, err := NewRootClient(RootClientOptions{
				ClientID: "client", RootID: "root",
				Connector: func(context.Context, map[string]int64) (RootConnection, error) { return connection, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			client.state, client.conn = RootLive, connection
			for index, event := range []struct{ kind, status string }{
				{"turn.started", "running"}, {"turn." + test.status, test.status},
			} {
				body, err := json.Marshal(session.LifecycleEvent{AgentID: "root", TurnID: "turn", Status: event.status})
				if err != nil {
					t.Fatal(err)
				}
				if !client.emitEvent(protocol.ProtocolEvent{RootID: "root", Seq: int64(index + 1), Kind: event.kind, Payload: body}) {
					t.Fatal("event was not accepted")
				}
			}
			cancel, err := client.NewAction("cancel", nil)
			if err != nil {
				t.Fatal(err)
			}
			var target struct {
				TurnID          string `json:"turn_id"`
				TargetCommandID string `json:"target_command_id"`
			}
			if err := json.Unmarshal(cancel.Payload, &target); err != nil {
				t.Fatal(err)
			}
			wantTurn := "turn"
			if test.terminal {
				wantTurn = ""
			}
			if target.TurnID != wantTurn || target.TargetCommandID != "" {
				t.Fatalf("turn cancellation = %+v; want turn %q", target, wantTurn)
			}
			if _, err := client.Command(t.Context(), RootAction{CommandID: "command", Operation: "submit", RootID: "root"}); err != nil {
				t.Fatal(err)
			}
			cancel, err = client.NewAction("cancel", nil)
			if err != nil {
				t.Fatal(err)
			}
			target.TurnID, target.TargetCommandID = "", ""
			if err := json.Unmarshal(cancel.Payload, &target); err != nil {
				t.Fatal(err)
			}
			wantCommand := "command"
			if test.terminal {
				wantCommand = ""
			}
			if target.TargetCommandID != wantCommand || target.TurnID != "" {
				t.Fatalf("command cancellation = %+v; want command %q", target, wantCommand)
			}
		})
	}
}

type deferredRootConnection struct {
	*staticRootConnection
	commands  chan protocol.CommandParams
	loseReply bool
}

func (c *deferredRootConnection) Call(_ context.Context, method string, _, result any) error {
	if method != "provider.list" {
		return errors.New("unexpected host RPC")
	}
	*result.(*protocol.ProviderList) = protocol.ProviderList{Selection: &protocol.ProviderSelection{Ready: false}}
	return nil
}

func (c *deferredRootConnection) Command(_ context.Context, params protocol.CommandParams) (protocol.CommandResult, error) {
	c.commands <- params
	if c.loseReply {
		_ = c.Close()
		return protocol.CommandResult{}, net.ErrClosed
	}
	return protocol.CommandResult{Status: "succeeded", Output: "created-root"}, nil
}

func TestRootClientDeferredSessionUsesHostConnectionAndStableCreate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	commands := make(chan protocol.CommandParams, 4)
	connections := make(chan *deferredRootConnection, 4)
	attempt := 0
	client, err := NewRootClient(RootClientOptions{
		ClientID: "onboarding", Create: &session.CreateSession{Kind: session.SessionKindAgent, CWD: "/workspace"}, DeferCreate: true,
		Connector: func(context.Context, map[string]int64) (RootConnection, error) {
			attempt++
			connection := &deferredRootConnection{staticRootConnection: newStaticRootConnection(), commands: commands, loseReply: attempt == 3}
			connections <- connection
			return connection, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	client.Start()
	if err := client.WaitLive(ctx); err != nil {
		t.Fatal(err)
	}
	if client.RootID() != "" {
		t.Fatal("created a session before a provider was chosen")
	}
	list, err := client.ListProvidersFor(ctx, "", "")
	if err != nil || list.Selection == nil {
		t.Fatalf("host inventory: %+v %v", list, err)
	}
	// Closing and reconnecting while still on the home screen must not create.
	first := <-connections
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connections:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := client.WaitLive(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case command := <-commands:
		t.Fatalf("early command: %+v", command)
	default:
	}
	if err := client.StartSession("coding-model", "openrouter"); err != nil {
		t.Fatal(err)
	}
	if err := client.StartSession("other-model", "other-provider"); err == nil {
		t.Fatal("accepted a second session start")
	}
	if err := client.WaitLive(ctx); err != nil {
		t.Fatal(err)
	}
	if client.RootID() != "created-root" {
		t.Fatal("did not attach the created session")
	}
	a, b := <-commands, <-commands
	if a.CommandID != b.CommandID || string(a.Payload) != string(b.Payload) || a.Operation != "session.create" {
		t.Fatal("lost acknowledgement changed the create identity or selected route")
	}
	var create session.CreateSession
	if err := json.Unmarshal(b.Payload, &create); err != nil {
		t.Fatal(err)
	}
	if create.Model != "coding-model" || create.Provider != "openrouter" || create.CWD != "/workspace" {
		t.Fatalf("wrong session template: %+v", create)
	}
}

func TestRootClientStartSessionDuringConnectionRetirement(t *testing.T) {
	client, err := NewRootClient(RootClientOptions{
		ClientID: "retiring", Create: &session.CreateSession{}, DeferCreate: true,
		Connector: func(context.Context, map[string]int64) (RootConnection, error) { return nil, net.ErrClosed },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	// Retirement clears the connection before publishing the next state.
	client.state = RootLive
	if err := client.StartSession("model", "provider"); err == nil || !client.deferCreate {
		t.Fatal("session was admitted without an active connection")
	}
}

type reconnectServer struct {
	mu       sync.Mutex
	events   []protocol.ProtocolEvent
	attempts int
	ids      []string
}

type reconnectConnection struct {
	server *reconnectServer
	events chan protocol.ProtocolEvent
	done   chan struct{}
	once   sync.Once
}

func (s *reconnectServer) connect(context.Context, map[string]int64) (RootConnection, error) {
	return &reconnectConnection{server: s, events: make(chan protocol.ProtocolEvent, 4), done: make(chan struct{})}, nil
}

func (c *reconnectConnection) Command(_ context.Context, params protocol.CommandParams) (protocol.CommandResult, error) {
	c.server.mu.Lock()
	c.server.attempts++
	c.server.ids = append(c.server.ids, params.CommandID)
	attempt := c.server.attempts
	if len(c.server.events) == 0 {
		c.server.events = append(c.server.events, protocol.ProtocolEvent{RootID: "root", Seq: 1, Kind: "stream.text", Payload: []byte(`{"text":"one"}`)})
	}
	event := c.server.events[0]
	c.server.mu.Unlock()
	if attempt == 1 {
		c.events <- event
		_ = c.Close()
		return protocol.CommandResult{}, net.ErrClosed
	}
	return protocol.CommandResult{CommandID: params.CommandID, Status: "succeeded", Output: "one"}, nil
}

func (c *reconnectConnection) Replay(_ context.Context, params protocol.ReplayParams) (protocol.ReplayResult, error) {
	c.server.mu.Lock()
	defer c.server.mu.Unlock()
	result := protocol.ReplayResult{Latest: int64(len(c.server.events))}
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

func (c *reconnectConnection) Events() <-chan protocol.ProtocolEvent { return c.events }

func (c *reconnectConnection) Done() <-chan struct{} { return c.done }

func (*reconnectConnection) Err() error { return net.ErrClosed }

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
	action, err := client.NewAction("submit", protocol.SubmitPayload{Text: "go"})
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
			return &failingRootConnection{reconnectConnection{events: make(chan protocol.ProtocolEvent), done: make(chan struct{})}}, nil
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
	events     chan protocol.ProtocolEvent
	done       chan struct{}
	once       sync.Once
	commandErr error
}

func newStaticRootConnection() *staticRootConnection {
	return &staticRootConnection{events: make(chan protocol.ProtocolEvent), done: make(chan struct{})}
}

func (c *staticRootConnection) Command(_ context.Context, params protocol.CommandParams) (protocol.CommandResult, error) {
	if c.commandErr != nil {
		return protocol.CommandResult{}, c.commandErr
	}
	return protocol.CommandResult{CommandID: params.CommandID, Status: "succeeded", Output: "ok"}, nil
}

func (*staticRootConnection) Replay(context.Context, protocol.ReplayParams) (protocol.ReplayResult, error) {
	return protocol.ReplayResult{}, nil
}

func (*staticRootConnection) Snapshot(_ context.Context, rootID string) (session.RootSnapshot, error) {
	return session.RootSnapshot{RootID: rootID}, nil
}

func (c *staticRootConnection) Events() <-chan protocol.ProtocolEvent { return c.events }

func (c *staticRootConnection) Done() <-chan struct{} { return c.done }

func (c *staticRootConnection) Err() error { return c.commandErr }

func (c *staticRootConnection) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

type permissionRootConnection struct {
	*staticRootConnection
	decision protocol.PermissionDecision
	decide   func(protocol.PermissionDecision) (protocol.PermissionDecisionResult, error)
}

func (c *permissionRootConnection) DecidePermission(_ context.Context, decision protocol.PermissionDecision) (protocol.PermissionDecisionResult, error) {
	c.decision = decision
	if c.decide != nil {
		return c.decide(decision)
	}
	return protocol.PermissionDecisionResult{OperationID: "operation", LeaseID: "lease"}, nil
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
		{ClientID: "client", RootID: "root", Create: &session.CreateSession{}, Connector: connector},
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
	action, err := client.NewAction("submit", protocol.SubmitPayload{Text: "hello"})
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
	mode, err := client.NewAction("permission.mode", map[string]bool{"external_permissions": false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetPermissionMode(t.Context(), RootAction{}); err == nil {
		t.Fatal("identity-free permission mode should fail")
	}
	if _, err := client.SetPermissionMode(t.Context(), mode); err == nil {
		t.Fatal("permission mode should be disabled before start")
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
	firstAction, _ := first.NewAction("submit", protocol.SubmitPayload{Text: "one"})
	secondAction, _ := second.NewAction("submit", protocol.SubmitPayload{Text: "two"})
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

func TestRootClientRetriesAndApprovesWithoutCredentials(t *testing.T) {
	connection := &permissionRootConnection{staticRootConnection: newStaticRootConnection()}
	attempts := 0
	client, err := NewRootClient(RootClientOptions{
		ClientID: "interactive", RootID: "root",
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
	if result, err := client.SetPermissionMode(t.Context(), external); err != nil || result.Output != "ok" {
		t.Fatalf("external mode = %+v, %v", result, err)
	}
	automatic, _ := client.NewAction("permission.mode", map[string]bool{"external_permissions": false})
	if result, err := client.SetPermissionMode(t.Context(), automatic); err != nil || result.Output != "ok" {
		t.Fatalf("automatic mode = %+v, %v", result, err)
	}
	permission, _ := client.NewAction("permission.decide", struct{}{})
	decision, err := client.DecidePermission(t.Context(), permission, "permission-1", true, "approved", "")
	if err != nil || decision.LeaseID != "lease" {
		t.Fatalf("permission decision = %+v, %v", decision, err)
	}
	if connection.decision.PermissionID != "permission-1" || !connection.decision.Allow {
		t.Fatalf("permission decision=%+v", connection.decision)
	}
}

func TestRootClientReportsUnsupportedDecisionsAndCommandFailure(t *testing.T) {
	connection := newStaticRootConnection()
	client, err := NewRootClient(RootClientOptions{
		ClientID: "plain", RootID: "root",
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
	if _, err := client.SetPermissionMode(t.Context(), mode); err != nil {
		t.Fatalf("ordinary command connection should support automatic mode: %v", err)
	}
	connection.commandErr = errors.New("command rejected")
	action, _ := client.NewAction("submit", protocol.SubmitPayload{Text: "hello"})
	if _, err := client.Command(t.Context(), action); !errors.Is(err, connection.commandErr) {
		t.Fatalf("command error = %v", err)
	}
}

func (*reconnectConnection) Subscribe(context.Context, string, int64) (protocol.SubscribeResult, error) {
	return protocol.SubscribeResult{}, nil
}

func (*failingRootConnection) Subscribe(context.Context, string, int64) (protocol.SubscribeResult, error) {
	return protocol.SubscribeResult{}, nil
}

func (*staticRootConnection) Subscribe(context.Context, string, int64) (protocol.SubscribeResult, error) {
	return protocol.SubscribeResult{}, nil
}

func TestRootClientRetriesPermissionDecisionWithSameCommandAfterDisconnect(t *testing.T) {
	var mu sync.Mutex
	decisions := []protocol.PermissionDecision{}
	client, err := NewRootClient(RootClientOptions{
		ClientID: "client", RootID: "root", RetryMin: time.Millisecond, RetryMax: time.Millisecond,
		Connector: func(context.Context, map[string]int64) (RootConnection, error) {
			connection := &permissionRootConnection{staticRootConnection: newStaticRootConnection()}
			connection.decide = func(decision protocol.PermissionDecision) (protocol.PermissionDecisionResult, error) {
				select {
				case <-connection.Done():
					return protocol.PermissionDecisionResult{}, net.ErrClosed
				default:
				}
				mu.Lock()
				decisions = append(decisions, decision)
				attempt := len(decisions)
				mu.Unlock()
				if attempt == 1 {
					_ = connection.Close()
					return protocol.PermissionDecisionResult{}, net.ErrClosed
				}
				return protocol.PermissionDecisionResult{OperationID: "operation", LeaseID: "lease"}, nil
			}
			return connection, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := client.WaitLive(ctx); err != nil {
		t.Fatal(err)
	}
	action, err := client.NewAction("permission.decide", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.DecidePermission(ctx, action, "permission", true, "approved", "tree")
	if err != nil || result.OperationID != "operation" {
		t.Fatalf("recovered decision = %+v, %v", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(decisions) != 2 || decisions[0] != decisions[1] || decisions[0].CommandID != action.CommandID {
		t.Fatalf("permission retry changed its identity or payload: %+v", decisions)
	}
}

type engineCreationConnection struct {
	*staticRootConnection
	engine   string
	requests []protocol.CommandParams
}

func (c *engineCreationConnection) InitializeResult() protocol.InitializeResult {
	return protocol.InitializeResult{DefaultExecutionEngine: c.engine}
}

func (c *engineCreationConnection) Command(_ context.Context, p protocol.CommandParams) (protocol.CommandResult, error) {
	c.requests = append(c.requests, p)
	return protocol.CommandResult{}, errors.New("lost acknowledgement")
}

func TestRootClientFreezesDiscoveredEngineAcrossLostAcknowledgement(t *testing.T) {
	template := &session.CreateSession{Kind: session.SessionKindAgent, CWD: "/tmp", Model: "m", Provider: "p"}
	client, err := NewRootClient(RootClientOptions{
		ClientID: "engine", Create: template,
		Connector: func(context.Context, map[string]int64) (RootConnection, error) {
			return nil, errors.New("unexpected connection attempt: test synchronizes directly")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	first := &engineCreationConnection{staticRootConnection: newStaticRootConnection(), engine: "quickjs"}
	if err := client.synchronize(first); err == nil {
		t.Fatal("expected lost acknowledgement")
	}
	template.ExecutionEngine = "starlark"
	second := &engineCreationConnection{staticRootConnection: newStaticRootConnection(), engine: "starlark"}
	if err := client.synchronize(second); err == nil {
		t.Fatal("expected lost acknowledgement")
	}
	if len(first.requests) != 1 || len(second.requests) != 1 || string(first.requests[0].Payload) != string(second.requests[0].Payload) || first.requests[0].CommandID != second.requests[0].CommandID {
		t.Fatal("retried creation changed")
	}
	var create session.CreateSession
	if err := json.Unmarshal(second.requests[0].Payload, &create); err != nil || create.ExecutionEngine != "quickjs" {
		t.Fatalf("create=%+v %v", create, err)
	}
}
