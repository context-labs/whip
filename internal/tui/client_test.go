package tui

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

type fakeDaemonConnection struct {
	mu          sync.Mutex
	commands    []daemon.CommandParams
	replay      daemon.ReplayResult
	snapshot    session.RootSnapshot
	events      chan daemon.ProtocolEvent
	done        chan struct{}
	err         error
	closeOnce   sync.Once
	commandFunc func(daemon.CommandParams) (daemon.CommandResult, error)
	decisions   []daemon.PermissionDecision
}

func newFakeDaemonConnection(snapshot session.RootSnapshot) *fakeDaemonConnection {
	return &fakeDaemonConnection{
		snapshot: snapshot, events: make(chan daemon.ProtocolEvent, 16), done: make(chan struct{}),
	}
}

func (f *fakeDaemonConnection) Command(_ context.Context, params daemon.CommandParams) (daemon.CommandResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, params)
	if f.commandFunc != nil {
		return f.commandFunc(params)
	}
	return daemon.CommandResult{CommandID: params.CommandID, Status: "succeeded"}, nil
}

func (f *fakeDaemonConnection) Replay(context.Context, daemon.ReplayParams) (daemon.ReplayResult, error) {
	return f.replay, nil
}

func (f *fakeDaemonConnection) Snapshot(context.Context, string) (session.RootSnapshot, error) {
	return f.snapshot, nil
}

func (f *fakeDaemonConnection) DecidePermission(_ context.Context, _ ed25519.PrivateKey, decision daemon.PermissionDecision) (daemon.PermissionDecisionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decisions = append(f.decisions, decision)
	return daemon.PermissionDecisionResult{OperationID: "operation", LeaseID: "lease"}, nil
}

func (f *fakeDaemonConnection) Events() <-chan daemon.ProtocolEvent { return f.events }
func (f *fakeDaemonConnection) Done() <-chan struct{}               { return f.done }
func (f *fakeDaemonConnection) Err() error                          { return f.err }
func (f *fakeDaemonConnection) Close() error {
	f.closeOnce.Do(func() { close(f.done) })
	return nil
}

func nextClientUpdate(t *testing.T, client *Client) ClientUpdate {
	t.Helper()
	select {
	case update := <-client.Updates():
		return update
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for client update")
		return ClientUpdate{}
	}
}

func waitClientState(t *testing.T, client *Client, want ClientState) []ClientState {
	t.Helper()
	var states []ClientState
	for {
		update := nextClientUpdate(t, client)
		if update.StateChanged {
			states = append(states, update.State)
			if update.State == want {
				return states
			}
		}
	}
}

func TestInteractiveSetupAppliesCautiousModeBeforeAutomaticTitles(t *testing.T) {
	connection := newFakeDaemonConnection(session.RootSnapshot{RootID: "root"})
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root", RetryMin: time.Millisecond, RetryMax: time.Millisecond,
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	if err := configureInteractiveSession(t.Context(), client, true); err != nil {
		t.Fatal(err)
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if len(connection.commands) != 2 || connection.commands[0].Operation != "permission.mode" || connection.commands[1].Operation != "session.autotitle" {
		t.Fatalf("interactive setup command order=%+v", connection.commands)
	}
}

func TestInteractiveSetupStopsWhenCautiousModeFails(t *testing.T) {
	connection := newFakeDaemonConnection(session.RootSnapshot{RootID: "root"})
	connection.commandFunc = func(params daemon.CommandParams) (daemon.CommandResult, error) {
		if params.Operation == "permission.mode" {
			return daemon.CommandResult{CommandID: params.CommandID, Status: "failed", Error: "identity rejected"}, nil
		}
		return daemon.CommandResult{CommandID: params.CommandID, Status: "succeeded"}, nil
	}
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root", RetryMin: time.Millisecond, RetryMax: time.Millisecond,
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	if err := configureInteractiveSession(t.Context(), client, true); err == nil || !strings.Contains(err.Error(), "identity rejected") {
		t.Fatalf("cautious setup error=%v", err)
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if len(connection.commands) != 1 || connection.commands[0].Operation != "permission.mode" {
		t.Fatalf("failed cautious setup continued: %+v", connection.commands)
	}
}

func TestAuthenticationRefreshesDaemonCatalogsBeforeReload(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	m, connection := liveQueueModel(t)
	m.busy = false
	m.cfg = config.Default()
	connection.commandFunc = func(params daemon.CommandParams) (daemon.CommandResult, error) {
		output := ""
		if params.Operation == "provider.catalogs" {
			output = `{"catalogs":{}}`
		}
		return daemon.CommandResult{CommandID: params.CommandID, Status: "succeeded", Output: output}, nil
	}

	next, command := m.Update(authResultMsg{key: "secret"})
	m = next.(*model)
	if command == nil {
		t.Fatal("successful authentication did not request daemon catalogs")
	}
	message := command().(clientCommandMsg)
	if message.action.Operation != "provider.catalogs" {
		t.Fatalf("first post-auth operation=%q", message.action.Operation)
	}
	next, command = m.Update(message)
	m = next.(*model)
	if command == nil {
		t.Fatal("catalog refresh did not request session reload")
	}
	message = command().(clientCommandMsg)
	if message.action.Operation != "session.reload" || m.reloadAfterCatalogs {
		t.Fatalf("post-catalog operation=%q pending=%t", message.action.Operation, m.reloadAfterCatalogs)
	}
}

func TestClientSnapshotEventsAndStableActions(t *testing.T) {
	snapshot := session.RootSnapshot{
		RootID: "root", Cursor: 2, Meta: session.Meta{ID: "root", Model: "m", Provider: "p"},
		Messages: []llm.Message{{Role: "user", Content: "hello", Authored: true}},
	}
	initial := newFakeDaemonConnection(snapshot)
	connection := newFakeDaemonConnection(snapshot)
	var connects int
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root", RetryMin: time.Millisecond, RetryMax: time.Millisecond,
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) {
			connects++
			if connects == 1 {
				return initial, nil
			}
			return connection, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	defer client.Close()

	var updates []ClientUpdate
	for {
		update := nextClientUpdate(t, client)
		updates = append(updates, update)
		if update.StateChanged && update.State == ClientLive {
			break
		}
	}
	if updates[0].State != ClientReconnecting || updates[1].State != ClientSnapshotting || updates[2].Snapshot == nil || connects != 2 {
		t.Fatalf("initial synchronization updates = %+v", updates)
	}

	action, err := client.NewAction("submit", map[string]string{"text": "work"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(action.CommandID, "tui-") || !strings.HasSuffix(action.CommandID, "-1") {
		t.Fatalf("command ID = %q", action.CommandID)
	}
	if _, err := client.Command(context.Background(), action); err != nil {
		t.Fatal(err)
	}
	connection.mu.Lock()
	got := connection.commands[0]
	connection.mu.Unlock()
	if got.CommandID != action.CommandID || got.Operation != "submit" || got.RootID != "root" {
		t.Fatalf("daemon command = %+v", got)
	}

	connection.events <- daemon.ProtocolEvent{RootID: "root", Seq: 3, Kind: "text", Payload: []byte("answer")}
	update := nextClientUpdate(t, client)
	if update.Event == nil || update.Event.Seq != 3 || client.Cursor() != 3 {
		t.Fatalf("event update = %+v cursor=%d", update, client.Cursor())
	}
	connection.events <- daemon.ProtocolEvent{RootID: "root", Seq: 3, Kind: "duplicate"}
	select {
	case duplicate := <-client.Updates():
		t.Fatalf("duplicate event was published: %+v", duplicate)
	case <-time.After(25 * time.Millisecond):
	}
}

func TestClientReconnectReplayAndSnapshotFallback(t *testing.T) {
	first := newFakeDaemonConnection(session.RootSnapshot{RootID: "root", Meta: session.Meta{ID: "root"}})
	second := newFakeDaemonConnection(session.RootSnapshot{RootID: "root", Cursor: 9, Meta: session.Meta{ID: "root"}})
	second.replay = daemon.ReplayResult{Expired: true, Latest: 9}
	third := newFakeDaemonConnection(session.RootSnapshot{RootID: "root", Cursor: 9, Meta: session.Meta{ID: "root"}})
	var mu sync.Mutex
	var calls int
	var reconnectCursor int64
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root", RetryMin: time.Millisecond, RetryMax: time.Millisecond,
		Connector: func(_ context.Context, cursors map[string]int64) (daemonConnection, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if calls == 1 {
				return first, nil
			}
			if calls == 2 {
				reconnectCursor = cursors["root"]
				return second, nil
			}
			return third, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	defer client.Close()
	waitClientState(t, client, ClientLive)
	first.events <- daemon.ProtocolEvent{RootID: "root", Seq: 1, Kind: "turn.started"}
	for update := nextClientUpdate(t, client); update.Event == nil; update = nextClientUpdate(t, client) {
	}
	first.err = errors.New("lost socket")
	_ = first.Close()

	var sawDisconnected, sawSnapshot bool
	for {
		update := nextClientUpdate(t, client)
		sawDisconnected = sawDisconnected || update.StateChanged && update.State == ClientDisconnected
		sawSnapshot = sawSnapshot || update.Snapshot != nil && update.Snapshot.Cursor == 9
		if update.StateChanged && update.State == ClientLive && sawSnapshot {
			break
		}
	}
	if !sawDisconnected || reconnectCursor != 1 || client.Cursor() != 9 || calls != 3 {
		t.Fatalf("reconnect: disconnected=%v supplied=%d cursor=%d", sawDisconnected, reconnectCursor, client.Cursor())
	}
}

func TestClientReattachesInFlightActionWithSameIdentityAfterDisconnect(t *testing.T) {
	first := newFakeDaemonConnection(session.RootSnapshot{RootID: "root", Meta: session.Meta{ID: "root"}})
	first.err = errors.New("socket lost after admission")
	first.commandFunc = func(daemon.CommandParams) (daemon.CommandResult, error) {
		_ = first.Close()
		return daemon.CommandResult{}, first.err
	}
	second := newFakeDaemonConnection(session.RootSnapshot{RootID: "root", Meta: session.Meta{ID: "root"}})
	second.commandFunc = func(params daemon.CommandParams) (daemon.CommandResult, error) {
		return daemon.CommandResult{CommandID: params.CommandID, Status: "succeeded", Output: "persisted outcome"}, nil
	}
	var connects int
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root", RetryMin: time.Millisecond, RetryMax: time.Millisecond,
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) {
			connects++
			if connects == 1 {
				return first, nil
			}
			return second, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	waitClientState(t, client, ClientLive)
	action, err := client.NewAction("submit", map[string]string{"text": "do it once"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Command(t.Context(), action)
	if err != nil || result.Output != "persisted outcome" {
		t.Fatalf("reattached result = %+v, %v", result, err)
	}
	first.mu.Lock()
	firstCommand := first.commands[0]
	first.mu.Unlock()
	second.mu.Lock()
	secondCommand := second.commands[0]
	second.mu.Unlock()
	if firstCommand.CommandID != secondCommand.CommandID || firstCommand.RootID != secondCommand.RootID || firstCommand.CommandID != action.CommandID {
		t.Fatalf("retried action changed identity: first=%+v second=%+v action=%+v", firstCommand, secondCommand, action)
	}
}

func TestClientCreatesThenReconnectsWithRootSubscription(t *testing.T) {
	creation := newFakeDaemonConnection(session.RootSnapshot{})
	creation.commandFunc = func(params daemon.CommandParams) (daemon.CommandResult, error) {
		return daemon.CommandResult{CommandID: params.CommandID, Status: "succeeded", Output: "new-root"}, nil
	}
	live := newFakeDaemonConnection(session.RootSnapshot{RootID: "new-root", Meta: session.Meta{ID: "new-root"}})
	var calls int
	client, err := NewClient(ClientOptions{
		ClientID: "tui", Create: &daemon.CreateSession{CWD: "/work", Model: "m", Provider: "p"},
		RetryMin: time.Millisecond, RetryMax: time.Millisecond,
		Connector: func(_ context.Context, cursors map[string]int64) (daemonConnection, error) {
			calls++
			if calls == 1 {
				if len(cursors) != 0 {
					t.Fatalf("creation connection cursors = %v", cursors)
				}
				return creation, nil
			}
			if cursors["new-root"] != 0 {
				t.Fatalf("root subscription cursors = %v", cursors)
			}
			return live, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	defer client.Close()
	waitClientState(t, client, ClientLive)
	if client.RootID() != "new-root" || calls != 2 {
		t.Fatalf("created root=%q connector calls=%d", client.RootID(), calls)
	}
	creation.mu.Lock()
	defer creation.mu.Unlock()
	if len(creation.commands) != 1 || !strings.HasPrefix(creation.commands[0].CommandID, "tui-session-") {
		t.Fatalf("creation commands = %+v", creation.commands)
	}
}

func TestClientDisablesCommandsUntilLiveAndValidatesOptions(t *testing.T) {
	if _, err := NewClient(ClientOptions{}); err == nil {
		t.Fatal("empty options were accepted")
	}
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root",
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return nil, errors.New("offline") },
	})
	if err != nil {
		t.Fatal(err)
	}
	action, _ := client.NewAction("submit", map[string]string{"text": "work"})
	if _, err := client.Command(context.Background(), action); err == nil {
		t.Fatal("command was admitted before synchronization")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestThinCommandsMapOneUserActionToOneDaemonCommand(t *testing.T) {
	connection := newFakeDaemonConnection(session.RootSnapshot{RootID: "root"})
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root",
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	waitClientState(t, client, ClientLive)
	m := &model{
		client: client, clientState: ClientLive, input: newInput(), now: time.Now,
	}
	cases := map[string]string{
		"/schedule list": "schedule.manage", "/goal ship": "goal.run", "/fork copy": "session.fork",
		"/goal-from-context 4": "goal.from-context",
		"/cd /tmp":             "workspace.set", "/model next": "session.model", "/effort high": "session.effort",
		"/rewind 2": "history.rewind", "/agents stop child": "agent.control", "/mcp list": "mcp.control",
		"/lsp list": "lsp.control", "/browser status": "browser.control", "/computer status": "computer.control",
		"/context-doctor": "context.audit", "/agents budget child tokens 10": "budget.cap",
		"/agents revoke cap-1": "capability.revoke", "/agents delete child": "agent.delete",
	}
	for input, operation := range cases {
		_, command := m.thinCommand(input)
		if command == nil {
			t.Fatalf("%s returned no command", input)
		}
		message := command().(clientCommandMsg)
		if message.err != nil || message.action.Operation != operation {
			t.Fatalf("%s => %+v", input, message)
		}
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if len(connection.commands) != len(cases) {
		t.Fatalf("daemon commands=%d, actions=%d", len(connection.commands), len(cases))
	}
	seen := make(map[string]bool)
	for _, command := range connection.commands {
		if seen[command.CommandID] {
			t.Fatalf("duplicate command ID %q", command.CommandID)
		}
		seen[command.CommandID] = true
		var payload map[string]string
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			t.Fatalf("payload: %v", err)
		}
	}
}

func TestThinTabCompletionPreservesTheTerminalMenu(t *testing.T) {
	m := &model{
		cfg: &config.Config{}, client: &Client{}, clientState: ClientLive,
		input: newInput(), now: time.Now,
	}
	m.input.SetValue("/mo")
	m.refreshMenu()
	if m.menu == nil || len(m.menu.cands) < 2 {
		t.Fatalf("thin completion menu = %+v", m.menu)
	}
	_, _ = m.thinKey(tea.KeyMsg{Type: tea.KeyTab})
	first := m.input.Value()
	_, _ = m.thinKey(tea.KeyMsg{Type: tea.KeyTab})
	if second := m.input.Value(); first == second {
		t.Fatalf("thin tab did not cycle from %q", first)
	}
	_, _ = m.thinKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.menu != nil || m.input.Value() != first {
		t.Fatalf("thin menu dismissal menu=%+v input=%q want=%q", m.menu, m.input.Value(), first)
	}
}

func TestSnapshotReplacementPreservesPresentationState(t *testing.T) {
	m := &model{client: &Client{}, input: newInput(), follow: false}
	m.input.SetValue("unsent draft")
	m.vp.YOffset = 4
	m.sel = &selection{}
	selection := m.sel
	m.applyClientSnapshot(session.RootSnapshot{
		RootID: "root", Cursor: 4,
		Meta:     session.Meta{ID: "root", Model: "m", Provider: "p", Goal: "finish"},
		Messages: []llm.Message{{Role: "assistant", Content: "authoritative"}},
	})
	if m.input.Value() != "unsent draft" || m.follow || m.sel != selection {
		t.Fatalf("presentation state changed: draft=%q follow=%v selection=%p", m.input.Value(), m.follow, m.sel)
	}
	if messages := m.displayMessages(); len(messages) != 2 || messages[1].Content != "authoritative" {
		t.Fatalf("behavioral snapshot not replaced: messages=%+v", messages)
	}
	m.applyClientSnapshot(session.RootSnapshot{
		RootID: "root", Cursor: 3, Meta: session.Meta{ID: "root"},
		Messages: []llm.Message{{Role: "assistant", Content: "stale"}},
	})
	if messages := m.displayMessages(); messages[1].Content != "authoritative" {
		t.Fatalf("older snapshot replaced newer presentation: messages=%+v", messages)
	}
}

func TestLifecycleRenderingUsesDaemonVocabulary(t *testing.T) {
	line := runtimeAgentLine(session.RuntimeAgent{
		ID: "child", LifecyclePhase: "blocked", BlockingReason: "budget denial",
		TerminalCause: "limit reached", AllowedControls: []string{"stop", "cap-spend"},
	})
	for _, want := range []string{"child", "blocked", "budget denial", "limit reached", "stop, cap-spend"} {
		if !strings.Contains(line, want) {
			t.Fatalf("lifecycle line %q omits %q", line, want)
		}
	}
}

func TestDaemonBackedModelRendersWithoutAnAgent(t *testing.T) {
	m := &model{
		cfg: &config.Config{}, client: &Client{}, clientState: ClientLive,
		input: newInput(), follow: true, width: 80, height: 24,
		clientView: clientPresentation{
			modelID: "api-model", effort: "low", contextLimit: 1_000,
			usage:    llm.Usage{PromptTokens: 10, CompletionTokens: 2},
			messages: []llm.Message{{Role: "system", Content: "system"}, {Role: "assistant", Content: "hello"}},
		},
	}
	m.applyClientSnapshot(session.RootSnapshot{
		RootID: "root", Meta: session.Meta{ID: "root", Model: "model", Provider: "provider"},
		Messages: []llm.Message{{Role: "assistant", Content: "hello"}},
	})
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if rendered := m.View(); !strings.Contains(rendered, "model") || !strings.Contains(rendered, "provider") {
		t.Fatalf("daemon-backed render = %q", rendered)
	}
}

func TestClientStreamEventsRenderLiveAndSnapshotRestoresThem(t *testing.T) {
	stream := func(kind string, event daemon.StreamEvent) daemon.ProtocolEvent {
		payload, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		return daemon.ProtocolEvent{RootID: "root", Kind: kind, Payload: payload}
	}
	m := &model{input: newInput(), follow: true, showThinking: true}
	for _, event := range []daemon.ProtocolEvent{
		stream("stream.reasoning", daemon.StreamEvent{Text: "considering"}),
		stream("stream.text", daemon.StreamEvent{Text: "hello"}),
		stream("stream.tool.call", daemon.StreamEvent{ID: "tool-1", Name: "read", Args: `{"path":"a"}`}),
		stream("stream.tool.started", daemon.StreamEvent{ID: "tool-1", Name: "read", Args: `{"path":"a"}`}),
		stream("stream.tool.completed", daemon.StreamEvent{ID: "tool-1", Name: "read", Result: "done"}),
	} {
		if handled, _ := m.applyClientStream(event.Kind, event.Payload); !handled {
			t.Fatalf("event %q was not handled", event.Kind)
		}
	}
	if m.current != "" || len(m.blocks) < 4 || m.blocks[len(m.blocks)-1].text != "done" {
		t.Fatalf("live stream current=%q blocks=%+v", m.current, m.blocks)
	}

	presentation := []session.SnapshotEvent{}
	for i, event := range []daemon.ProtocolEvent{
		stream("stream.text", daemon.StreamEvent{Text: "restored "}),
		stream("stream.text", daemon.StreamEvent{Text: "answer"}),
	} {
		presentation = append(presentation, session.SnapshotEvent{Seq: int64(i + 1), Kind: event.Kind, Payload: event.Payload})
	}
	m.applyClientSnapshot(session.RootSnapshot{RootID: "root", Meta: session.Meta{ID: "root"}, Presentation: presentation})
	if m.current != "restored answer" {
		t.Fatalf("restored stream = %q", m.current)
	}
}

func TestThinKeyKeepsDraftWhileSynchronizing(t *testing.T) {
	m := &model{client: &Client{}, clientState: ClientSnapshotting, input: newInput()}
	m.input.SetValue("keep me")
	_, command := m.thinKey(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || m.input.Value() != "keep me" {
		t.Fatalf("snapshotting submit command=%v draft=%q", command, m.input.Value())
	}
}

func TestThinPermissionSendsOneStableSignedDecision(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	connection := newFakeDaemonConnection(session.RootSnapshot{RootID: "root"})
	client, err := NewClient(ClientOptions{
		ClientID: "tui", PrivateKey: private, RootID: "root",
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	waitClientState(t, client, ClientLive)
	m := &model{client: client, clientState: ClientLive, input: newInput()}
	m.applyClientPermissions([]session.PermissionSnapshot{{
		ID: "permission", AgentID: "agent", OperationID: "operation", Operation: "write",
		CanonicalPath: "/work/file", RequestDigest: "digest", Status: "pending",
	}})
	if view := m.permView(); !strings.Contains(view, "Allow write") || !strings.Contains(view, "/work/file") {
		t.Fatalf("permission view = %q", view)
	}
	_, command := m.thinKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if command == nil || !m.permDialog.deciding {
		t.Fatal("permission decision was not started")
	}
	if _, duplicate := m.thinKey(tea.KeyMsg{Type: tea.KeyEnter}); duplicate != nil {
		t.Fatal("deciding permission accepted a duplicate action")
	}
	message := command().(clientPermissionMsg)
	if message.err != nil || message.action.CommandID == "" {
		t.Fatalf("permission message = %+v", message)
	}
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if len(connection.decisions) != 1 || connection.decisions[0].CommandID != message.action.CommandID || connection.decisions[0].PermissionID != "permission" {
		t.Fatalf("permission decisions = %+v", connection.decisions)
	}
}

func TestThinSessionPickerSwitchesBetweenPersistedModes(t *testing.T) {
	connection := newFakeDaemonConnection(session.RootSnapshot{RootID: "current-root"})
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "current-root",
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	waitClientState(t, client, ClientLive)
	m := &model{client: client, clientState: ClientLive, input: newInput(), width: 100, height: 30}
	metas := []session.Meta{
		{ID: "new-root", Title: "New work", Model: "m", Provider: "p", UpdatedAt: time.Now()},
		{ID: "older-root", Title: "Older work", Model: "m", Provider: "p", UpdatedAt: time.Now().Add(-time.Hour)},
	}
	raw, _ := json.Marshal(metas)
	_, _ = m.Update(clientCommandMsg{
		action: Action{Operation: "session.list"}, result: daemon.CommandResult{Status: "succeeded", Output: string(raw)},
	})
	if m.picker == nil {
		t.Fatal("session list did not open the daemon-backed picker")
	}
	view := m.pickerView()
	if !strings.Contains(view, "New work") || !strings.Contains(view, "Older work") {
		t.Fatalf("session picker omits sessions: %q", view)
	}
	_, command := m.pickerKey(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("session picker did not send an open action")
	}
	message := command().(clientCommandMsg)
	if message.err != nil || message.action.Operation != "session.open" || message.result.Status != "succeeded" {
		t.Fatalf("session open message = %+v", message)
	}
	message.result.Output = "rlm-root"
	_, _ = m.Update(message)
	if client.RootID() != "rlm-root" || m.clientState != ClientSnapshotting {
		t.Fatalf("session switch root=%q state=%s", client.RootID(), m.clientState)
	}
}

func TestThinPaletteAndAgentControlsStayDaemonBacked(t *testing.T) {
	connection := newFakeDaemonConnection(session.RootSnapshot{RootID: "root"})
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root",
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	waitClientState(t, client, ClientLive)
	m := &model{
		cfg: &config.Config{}, client: client, clientState: ClientLive, input: newInput(), now: time.Now,
		clientView: clientPresentation{agents: []session.RuntimeAgent{
			{ID: "root-agent", LifecyclePhase: "running"},
			{ID: "child", ParentID: "root-agent", LifecyclePhase: "blocked", BlockingReason: "permission", AllowedControls: []string{"stop"}},
		}},
	}
	_, _ = m.thinKey(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.palette == nil || !strings.Contains(m.paletteView(), "Resume session") {
		t.Fatal("client-safe command palette did not open")
	}
	m.palette = nil
	_, _ = m.thinKey(tea.KeyMsg{Type: tea.KeyCtrlT})
	if !m.agentsFocus || !strings.Contains(m.agentsDock(), "blocked: permission") {
		t.Fatalf("daemon lifecycle dock focus=%v view=%q", m.agentsFocus, m.agentsDock())
	}
	_, command := m.thinKey(tea.KeyMsg{Type: tea.KeyCtrlX})
	if command == nil {
		t.Fatal("agent stop did not create a daemon action")
	}
	message := command().(clientCommandMsg)
	if message.action.Operation != "agent.control" || message.action.CommandID == "" {
		t.Fatalf("agent stop action = %+v", message.action)
	}
}

func TestThinInteractiveTerminalRestoresAndForwardsBytes(t *testing.T) {
	connection := newFakeDaemonConnection(session.RootSnapshot{RootID: "root"})
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root",
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	waitClientState(t, client, ClientLive)
	m := &model{client: client, clientState: ClientLive, input: newInput()}
	stream := func(kind string, event daemon.StreamEvent) session.SnapshotEvent {
		payload, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return session.SnapshotEvent{Kind: kind, Payload: payload}
	}
	m.applyClientSnapshot(session.RootSnapshot{
		RootID: "root", Meta: session.Meta{ID: "root"}, Presentation: []session.SnapshotEvent{
			stream("stream.terminal.started", daemon.StreamEvent{ID: "terminal-7"}),
			stream("stream.terminal.output", daemon.StreamEvent{ID: "terminal-7", Text: "Password: "}),
			stream("stream.terminal.awaiting", daemon.StreamEvent{ID: "terminal-7", Text: "9"}),
		},
	})
	if m.clientTerminalID != "terminal-7" || m.iactive == nil || m.iactive.output != "Password: " || !m.iactive.await || m.iactive.awaitcd != 9 {
		t.Fatalf("restored terminal id=%q state=%+v", m.clientTerminalID, m.iactive)
	}

	_, command := m.thinInteractiveKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if command == nil {
		t.Fatal("interactive key did not create a daemon action")
	}
	message := command().(clientTerminalMsg)
	if message.err != nil || message.action.Operation != "terminal.input" || message.action.CommandID == "" {
		t.Fatalf("terminal input message = %+v", message)
	}
	connection.mu.Lock()
	params := connection.commands[0]
	connection.mu.Unlock()
	var input struct {
		ID    string `json:"id"`
		Bytes []byte `json:"bytes"`
	}
	if err := json.Unmarshal(params.Payload, &input); err != nil {
		t.Fatal(err)
	}
	if input.ID != "terminal-7" || string(input.Bytes) != "s" {
		t.Fatalf("terminal input payload = %+v", input)
	}

	completed := stream("stream.terminal.completed", daemon.StreamEvent{ID: "terminal-7"})
	if handled, _ := m.applyClientStream(completed.Kind, completed.Payload); !handled || m.clientTerminalID != "" || m.iactive != nil {
		t.Fatalf("completed terminal id=%q state=%+v", m.clientTerminalID, m.iactive)
	}
}

func TestClosingClientDoesNotCancelDaemonOwnedCommand(t *testing.T) {
	connection := newFakeDaemonConnection(session.RootSnapshot{RootID: "root", Meta: session.Meta{ID: "root"}})
	started, release := make(chan struct{}), make(chan struct{})
	connection.commandFunc = func(params daemon.CommandParams) (daemon.CommandResult, error) {
		close(started)
		<-release
		return daemon.CommandResult{CommandID: params.CommandID, Status: "succeeded", Output: "finished while detached"}, nil
	}
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root",
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	waitClientState(t, client, ClientLive)
	action, _ := client.NewAction("submit", map[string]string{"text": "keep going"})
	type outcome struct {
		result daemon.CommandResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, commandErr := client.Command(context.Background(), action)
		done <- outcome{result: result, err: commandErr}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("command did not start")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		t.Fatalf("closing the client stopped daemon work early: %+v", result)
	default:
	}
	close(release)
	select {
	case result := <-done:
		if result.err != nil || result.result.Output != "finished while detached" {
			t.Fatalf("detached command = %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("daemon-owned command did not finish after client close")
	}
}
