package acp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/session"
)

type fakeACPBackend struct {
	mu       sync.Mutex
	next     int
	paired   bool
	roots    map[string]*fakeRoot
	attached map[string]map[string]mcp.ServerConfig
	private  ed25519.PrivateKey
}

type fakeRoot struct {
	mu           sync.Mutex
	id           string
	cwd          string
	title        string
	messages     []llm.Message
	events       []daemon.ProtocolEvent
	presentation []session.SnapshotEvent
	permissions  []session.PermissionSnapshot
	external     bool
	lastSubmit   daemon.SubmitPayload
	cancel       chan struct{}
	permission   chan bool
	decisions    int
	closeCount   int
}

func newFakeBackend(t *testing.T, paired bool) *fakeACPBackend {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeACPBackend{paired: paired, roots: make(map[string]*fakeRoot), attached: make(map[string]map[string]mcp.ServerConfig), private: private}
}

func (b *fakeACPBackend) NewRoot(ctx context.Context, cwd string, servers map[string]mcp.ServerConfig) (*daemon.RootClient, error) {
	b.mu.Lock()
	b.next++
	id := fmt.Sprintf("root-%d", b.next)
	root := &fakeRoot{id: id, cwd: cwd, cancel: make(chan struct{}, 1), permission: make(chan bool, 1)}
	b.roots[id] = root
	b.attached[id] = servers
	b.mu.Unlock()
	return b.client(ctx, root)
}

func (b *fakeACPBackend) LoadRoot(ctx context.Context, id, _ string, servers map[string]mcp.ServerConfig) (*daemon.RootClient, error) {
	b.mu.Lock()
	root := b.roots[id]
	if root != nil {
		b.attached[id] = servers
	}
	b.mu.Unlock()
	if root == nil {
		return nil, errors.New("no session")
	}
	return b.client(ctx, root)
}

func (b *fakeACPBackend) client(ctx context.Context, root *fakeRoot) (*daemon.RootClient, error) {
	client, err := daemon.NewRootClient(daemon.RootClientOptions{
		ClientID: "acp-test", PrivateKey: b.private, RootID: root.id,
		Connector: func(context.Context, map[string]int64) (daemon.RootConnection, error) {
			return newFakeConnection(b, root), nil
		},
		RetryMin: time.Millisecond, RetryMax: 5 * time.Millisecond,
	})
	if err != nil {
		return nil, err
	}
	client.Start()
	if err := client.WaitLive(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (b *fakeACPBackend) ListSessions(context.Context, int) ([]session.Meta, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]session.Meta, 0, len(b.roots))
	for _, root := range b.roots {
		root.mu.Lock()
		out = append(out, session.Meta{ID: root.id, CWD: root.cwd, Title: root.title, UpdatedAt: time.Now()})
		root.mu.Unlock()
	}
	return out, nil
}

func (b *fakeACPBackend) Paired(context.Context) bool { return b.paired }

func (b *fakeACPBackend) seed(cwd string, messages ...llm.Message) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	id := fmt.Sprintf("root-%d", b.next)
	b.roots[id] = &fakeRoot{id: id, cwd: cwd, messages: messages, cancel: make(chan struct{}, 1), permission: make(chan bool, 1)}
	return id
}

type fakeConnection struct {
	backend *fakeACPBackend
	root    *fakeRoot
	events  chan daemon.ProtocolEvent
	done    chan struct{}
	once    sync.Once
}

func newFakeConnection(backend *fakeACPBackend, root *fakeRoot) *fakeConnection {
	return &fakeConnection{backend: backend, root: root, events: make(chan daemon.ProtocolEvent, 64), done: make(chan struct{})}
}

func (c *fakeConnection) Command(ctx context.Context, params daemon.CommandParams) (daemon.CommandResult, error) {
	result := daemon.CommandResult{CommandID: params.CommandID, Status: "succeeded"}
	switch params.Operation {
	case "permission.mode":
		var payload struct {
			External bool `json:"external_permissions"`
		}
		_ = json.Unmarshal(params.Payload, &payload)
		if !payload.External && !c.backend.paired {
			result.Status, result.Error = "failed", "automatic permissions require a paired human identity"
			return result, nil
		}
		c.root.mu.Lock()
		c.root.external = payload.External
		c.root.mu.Unlock()
		return result, nil
	case "cancel":
		select {
		case c.root.cancel <- struct{}{}:
		default:
		}
		return result, nil
	case "submit":
		return c.submit(ctx, params, result)
	default:
		return result, nil
	}
}

func (c *fakeConnection) SetPermissionMode(ctx context.Context, _ ed25519.PrivateKey, params daemon.CommandParams) (daemon.CommandResult, error) {
	return c.Command(ctx, params)
}

func (c *fakeConnection) submit(ctx context.Context, params daemon.CommandParams, result daemon.CommandResult) (daemon.CommandResult, error) {
	var payload daemon.SubmitPayload
	if err := json.Unmarshal(params.Payload, &payload); err != nil {
		return daemon.CommandResult{}, err
	}
	c.root.mu.Lock()
	c.root.lastSubmit = payload
	c.root.messages = append(c.root.messages, llm.Message{Role: "user", Content: payload.Text, Parts: payload.Parts})
	c.root.mu.Unlock()
	if strings.Contains(payload.Text, "events") {
		c.emit("stream.reasoning", daemon.StreamEvent{Text: "thinking"})
		c.emit("stream.tool.started", daemon.StreamEvent{ID: "tool-1", Name: "read", Args: `{"path":"a.go"}`})
		c.emit("stream.tool.completed", daemon.StreamEvent{ID: "tool-1", Name: "read", Result: "contents"})
		usage, _ := json.Marshal(daemon.UsageEvent{Used: 7, Size: 100})
		c.emit("stream.usage", daemon.StreamEvent{Result: string(usage)})
		plan, _ := json.Marshal(daemon.PlanEvent{Items: []daemon.PlanItem{{Content: "check", Status: "completed"}}})
		c.emit("stream.plan", daemon.StreamEvent{Result: string(plan)})
	}
	if strings.Contains(payload.Text, "permission") {
		c.emitRaw("permission.pending", []byte(`{"permission_id":"permission-1","operation_id":"operation-1","operation":"write","canonical_path":"/tmp/a.go"}`))
		select {
		case allow := <-c.root.permission:
			if !allow {
				result.Status, result.Error = "failed", "permission denied"
				c.emitRaw("turn.failed", nil)
				return result, nil
			}
		case <-c.root.cancel:
			result.Status, result.Error = "failed", "context canceled"
			c.emitRaw("turn.failed", nil)
			return result, nil
		case <-ctx.Done():
			return daemon.CommandResult{}, ctx.Err()
		}
	}
	answer := "answer"
	c.emit("stream.text", daemon.StreamEvent{Text: answer})
	c.root.mu.Lock()
	c.root.title = "Test session"
	c.root.messages = append(c.root.messages, llm.Message{Role: "assistant", Content: answer})
	c.root.mu.Unlock()
	c.emitRaw("turn.succeeded", nil)
	result.Output = answer
	return result, nil
}

func (c *fakeConnection) emit(kind string, stream daemon.StreamEvent) {
	payload, _ := json.Marshal(stream)
	c.emitRaw(kind, payload)
}

func (c *fakeConnection) emitRaw(kind string, payload []byte) {
	c.root.mu.Lock()
	seq := int64(len(c.root.events) + 1)
	event := daemon.ProtocolEvent{RootID: c.root.id, Seq: seq, Kind: kind, Payload: payload}
	c.root.events = append(c.root.events, event)
	c.root.mu.Unlock()
	select {
	case <-c.done:
	case c.events <- event:
	}
}

func (c *fakeConnection) Replay(_ context.Context, params daemon.ReplayParams) (daemon.ReplayResult, error) {
	c.root.mu.Lock()
	defer c.root.mu.Unlock()
	result := daemon.ReplayResult{Latest: int64(len(c.root.events))}
	for _, event := range c.root.events {
		if event.Seq > params.Cursor {
			result.Events = append(result.Events, event)
		}
	}
	return result, nil
}

func (c *fakeConnection) Snapshot(context.Context, string) (session.RootSnapshot, error) {
	c.root.mu.Lock()
	defer c.root.mu.Unlock()
	return session.RootSnapshot{
		RootID: c.root.id, Cursor: int64(len(c.root.events)),
		Meta:         session.Meta{ID: c.root.id, CWD: c.root.cwd, Title: c.root.title},
		Messages:     append([]llm.Message(nil), c.root.messages...),
		Presentation: append([]session.SnapshotEvent(nil), c.root.presentation...),
		Permissions:  append([]session.PermissionSnapshot(nil), c.root.permissions...),
	}, nil
}

func (c *fakeConnection) DecidePermission(_ context.Context, _ ed25519.PrivateKey, decision daemon.PermissionDecision) (daemon.PermissionDecisionResult, error) {
	if !c.backend.paired {
		return daemon.PermissionDecisionResult{}, errors.New("unpaired")
	}
	c.root.mu.Lock()
	c.root.decisions++
	c.root.mu.Unlock()
	select {
	case c.root.permission <- decision.Allow:
	default:
	}
	return daemon.PermissionDecisionResult{OperationID: "operation-1"}, nil
}

func (c *fakeConnection) Events() <-chan daemon.ProtocolEvent { return c.events }
func (c *fakeConnection) Done() <-chan struct{}               { return c.done }
func (c *fakeConnection) Err() error                          { return nil }
func (c *fakeConnection) Close() error {
	c.once.Do(func() {
		c.root.mu.Lock()
		c.root.closeCount++
		c.root.mu.Unlock()
		close(c.done)
	})
	return nil
}

type fakeACPClient struct {
	mu      sync.Mutex
	updates []acpsdk.SessionNotification
	perms   []acpsdk.RequestPermissionRequest
	answer  string
}

func (c *fakeACPClient) SessionUpdate(_ context.Context, update acpsdk.SessionNotification) error {
	c.mu.Lock()
	c.updates = append(c.updates, update)
	c.mu.Unlock()
	return nil
}

func (c *fakeACPClient) RequestPermission(_ context.Context, request acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	c.mu.Lock()
	c.perms = append(c.perms, request)
	answer := c.answer
	c.mu.Unlock()
	if answer == "" {
		answer = optAllowOnce
	}
	return acpsdk.RequestPermissionResponse{Outcome: acpsdk.RequestPermissionOutcome{
		Selected: &acpsdk.RequestPermissionOutcomeSelected{OptionId: acpsdk.PermissionOptionId(answer)},
	}}, nil
}

func (c *fakeACPClient) ReadTextFile(context.Context, acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	return acpsdk.ReadTextFileResponse{}, acpsdk.NewMethodNotFound("fs/read_text_file")
}

func (c *fakeACPClient) WriteTextFile(context.Context, acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	return acpsdk.WriteTextFileResponse{}, acpsdk.NewMethodNotFound("fs/write_text_file")
}

func (c *fakeACPClient) CreateTerminal(context.Context, acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	return acpsdk.CreateTerminalResponse{}, acpsdk.NewMethodNotFound("terminal/create")
}

func (c *fakeACPClient) KillTerminal(context.Context, acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error) {
	return acpsdk.KillTerminalResponse{}, nil
}

func (c *fakeACPClient) TerminalOutput(context.Context, acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{}, acpsdk.NewMethodNotFound("terminal/output")
}

func (c *fakeACPClient) ReleaseTerminal(context.Context, acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, nil
}

func (c *fakeACPClient) WaitForTerminalExit(context.Context, acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	return acpsdk.WaitForTerminalExitResponse{}, nil
}

func (c *fakeACPClient) UnstableCompleteElicitation(context.Context, acpsdk.UnstableCompleteElicitationNotification) error {
	return nil
}

func (c *fakeACPClient) UnstableCreateElicitation(context.Context, acpsdk.UnstableCreateElicitationRequest) (acpsdk.UnstableCreateElicitationResponse, error) {
	return acpsdk.UnstableCreateElicitationResponse{}, acpsdk.NewMethodNotFound("elicitation/create")
}

func (c *fakeACPClient) UnstableConnectMcp(context.Context, acpsdk.UnstableConnectMcpRequest) (acpsdk.UnstableConnectMcpResponse, error) {
	return acpsdk.UnstableConnectMcpResponse{}, acpsdk.NewMethodNotFound("mcp/connect")
}

func (c *fakeACPClient) UnstableDisconnectMcp(context.Context, acpsdk.UnstableDisconnectMcpRequest) (acpsdk.UnstableDisconnectMcpResponse, error) {
	return acpsdk.UnstableDisconnectMcpResponse{}, acpsdk.NewMethodNotFound("mcp/disconnect")
}

func (c *fakeACPClient) kinds() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]string, 0, len(c.updates))
	for _, notification := range c.updates {
		update := notification.Update
		switch {
		case update.UserMessageChunk != nil:
			result = append(result, "user")
		case update.AgentMessageChunk != nil:
			result = append(result, "agent")
		case update.AgentThoughtChunk != nil:
			result = append(result, "thought")
		case update.ToolCall != nil:
			result = append(result, "tool")
		case update.ToolCallUpdate != nil:
			result = append(result, "tool-update")
		case update.Plan != nil:
			result = append(result, "plan")
		case update.UsageUpdate != nil:
			result = append(result, "usage")
		case update.SessionInfoUpdate != nil:
			result = append(result, "title")
		}
	}
	return result
}

type acpFixture struct {
	bridge *Bridge
	client *fakeACPClient
	conn   *acpsdk.ClientSideConnection
}

func newACPFixture(t *testing.T, backend *fakeACPBackend, client *fakeACPClient) *acpFixture {
	t.Helper()
	if client == nil {
		client = &fakeACPClient{}
	}
	agentRead, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	clientRead, agentWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewBridge("test", backend, true, map[string]mcp.ServerConfig{"base": {URL: "https://base.invalid"}})
	agentConnection := acpsdk.NewAgentSideConnection(bridge, agentWrite, agentRead)
	bridge.SetAgentConnection(agentConnection)
	connection := acpsdk.NewClientSideConnection(client, clientWrite, clientRead)
	t.Cleanup(func() {
		bridge.CloseAll()
		_ = agentWrite.Close()
		_ = clientWrite.Close()
		_ = agentRead.Close()
		_ = clientRead.Close()
	})
	return &acpFixture{bridge: bridge, client: client, conn: connection}
}

func (f *acpFixture) initialize(t *testing.T) {
	t.Helper()
	response, err := f.conn.Initialize(t.Context(), acpsdk.InitializeRequest{ProtocolVersion: acpsdk.ProtocolVersionNumber})
	if err != nil {
		t.Fatal(err)
	}
	if !response.AgentCapabilities.LoadSession || response.AgentInfo == nil || response.AgentInfo.Name != "whip" {
		t.Fatalf("initialize response = %+v", response)
	}
}

func (f *acpFixture) newSession(t *testing.T) acpsdk.SessionId {
	t.Helper()
	response, err := f.conn.NewSession(t.Context(), acpsdk.NewSessionRequest{
		Cwd:        t.TempDir(),
		McpServers: []acpsdk.McpServer{{Http: &acpsdk.McpServerHttpInline{Name: "client", Type: "http", Url: "https://client.invalid", Headers: []acpsdk.HttpHeader{}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Modes == nil || response.Modes.CurrentModeId != ModeAsk {
		t.Fatalf("new session modes = %+v", response.Modes)
	}
	return response.SessionId
}

func TestBridgeDaemonCutoverMapsEventsAndContent(t *testing.T) {
	backend := newFakeBackend(t, true)
	fixture := newACPFixture(t, backend, nil)
	fixture.initialize(t)
	id := fixture.newSession(t)
	response, err := fixture.conn.Prompt(t.Context(), acpsdk.PromptRequest{
		SessionId: id,
		Prompt: []acpsdk.ContentBlock{
			acpsdk.TextBlock("events"),
			{Image: &acpsdk.ContentBlockImage{Type: "image", MimeType: "image/png", Data: "aQ=="}},
		},
	})
	if err != nil || response.StopReason != acpsdk.StopReasonEndTurn {
		t.Fatalf("prompt = %+v, %v", response, err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && len(fixture.client.kinds()) < 7 {
		time.Sleep(time.Millisecond)
	}
	kinds := strings.Join(fixture.client.kinds(), ",")
	for _, want := range []string{"thought", "tool", "tool-update", "usage", "plan", "agent", "title"} {
		if !strings.Contains(kinds, want) {
			t.Errorf("updates %q missing %q", kinds, want)
		}
	}
	backend.mu.Lock()
	root := backend.roots[string(id)]
	servers := backend.attached[string(id)]
	backend.mu.Unlock()
	root.mu.Lock()
	parts := len(root.lastSubmit.Parts)
	external := root.external
	root.mu.Unlock()
	if parts != 1 || !external {
		t.Fatalf("daemon submit parts=%d external_permissions=%v", parts, external)
	}
	if _, ok := servers["base"]; !ok || servers["client"].URL == "" {
		t.Fatalf("merged MCP servers = %#v", servers)
	}
}

func TestBridgeLoadReplaysBeforeResponseAndLists(t *testing.T) {
	backend := newFakeBackend(t, false)
	cwd := t.TempDir()
	id := backend.seed(cwd,
		llm.Message{Role: "user", Content: "remember"},
		llm.Message{Role: "assistant", Content: "remembered"},
	)
	presentation, _ := json.Marshal(daemon.StreamEvent{Text: "unfinished thought"})
	backend.roots[id].presentation = []session.SnapshotEvent{{Seq: 1, Kind: "stream.reasoning", Payload: presentation}}
	fixture := newACPFixture(t, backend, nil)
	fixture.initialize(t)
	response, err := fixture.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: acpsdk.SessionId(id), Cwd: cwd, McpServers: []acpsdk.McpServer{}})
	if err != nil || response.Modes == nil {
		t.Fatalf("load = %+v, %v", response, err)
	}
	kinds := fixture.client.kinds()
	if len(kinds) < 3 || kinds[0] != "user" || kinds[1] != "agent" || kinds[2] != "thought" {
		t.Fatalf("load replay order = %v", kinds)
	}
	listed, err := fixture.conn.ListSessions(t.Context(), acpsdk.ListSessionsRequest{Cwd: &cwd})
	if err != nil || len(listed.Sessions) != 1 || listed.Sessions[0].SessionId != acpsdk.SessionId(id) {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	if _, err := fixture.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: acpsdk.SessionId(id), Cwd: cwd, McpServers: []acpsdk.McpServer{}}); err == nil {
		t.Fatal("duplicate load succeeded")
	}
}

func TestBridgePairedPermissionAndModes(t *testing.T) {
	backend := newFakeBackend(t, true)
	client := &fakeACPClient{answer: optAllowAlways}
	fixture := newACPFixture(t, backend, client)
	fixture.initialize(t)
	id := fixture.newSession(t)
	response, err := fixture.conn.Prompt(t.Context(), acpsdk.PromptRequest{SessionId: id, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock("permission")}})
	if err != nil || response.StopReason != acpsdk.StopReasonEndTurn {
		t.Fatalf("permission prompt = %+v, %v", response, err)
	}
	client.mu.Lock()
	permissionRequests := len(client.perms)
	client.mu.Unlock()
	backend.mu.Lock()
	root := backend.roots[string(id)]
	backend.mu.Unlock()
	root.mu.Lock()
	decisions := root.decisions
	root.mu.Unlock()
	if permissionRequests != 1 || decisions != 1 {
		t.Fatalf("permission requests=%d decisions=%d", permissionRequests, decisions)
	}
	if _, err := fixture.conn.SetSessionMode(t.Context(), acpsdk.SetSessionModeRequest{SessionId: id, ModeId: ModeAuto}); err != nil {
		t.Fatalf("paired auto mode: %v", err)
	}
}

func TestBridgeUnpairedCannotApproveOrEnableAuto(t *testing.T) {
	backend := newFakeBackend(t, false)
	fixture := newACPFixture(t, backend, nil)
	fixture.initialize(t)
	id := fixture.newSession(t)
	if _, err := fixture.conn.SetSessionMode(t.Context(), acpsdk.SetSessionModeRequest{SessionId: id, ModeId: ModeAuto}); err == nil {
		t.Fatal("unpaired auto mode succeeded")
	}
	done := make(chan acpsdk.PromptResponse, 1)
	go func() {
		response, _ := fixture.conn.Prompt(context.Background(), acpsdk.PromptRequest{SessionId: id, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock("permission")}})
		done <- response
	}()
	time.Sleep(20 * time.Millisecond)
	if err := fixture.conn.Cancel(t.Context(), acpsdk.CancelNotification{SessionId: id}); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-done:
		if response.StopReason != acpsdk.StopReasonCancelled {
			t.Fatalf("cancelled prompt = %+v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop the prompt")
	}
	fixture.client.mu.Lock()
	requests := len(fixture.client.perms)
	fixture.client.mu.Unlock()
	if requests != 0 {
		t.Fatalf("unpaired client received %d permission requests", requests)
	}
}

func TestBridgeConcurrentSessionsAndCloseDetach(t *testing.T) {
	backend := newFakeBackend(t, true)
	fixture := newACPFixture(t, backend, nil)
	fixture.initialize(t)
	first, second := fixture.newSession(t), fixture.newSession(t)
	var wait sync.WaitGroup
	for _, id := range []acpsdk.SessionId{first, second} {
		wait.Go(func() {
			response, err := fixture.conn.Prompt(context.Background(), acpsdk.PromptRequest{SessionId: id, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock("hello")}})
			if err != nil || response.StopReason != acpsdk.StopReasonEndTurn {
				t.Errorf("prompt %s = %+v, %v", id, response, err)
			}
		})
	}
	wait.Wait()
	if _, err := fixture.conn.CloseSession(t.Context(), acpsdk.CloseSessionRequest{SessionId: first}); err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	_, retained := backend.roots[string(first)]
	backend.mu.Unlock()
	if !retained {
		t.Fatal("ACP close deleted daemon-owned work")
	}
}

func TestFakeConnectionImplementsRootProtocol(t *testing.T) {
	backend := newFakeBackend(t, true)
	root := &fakeRoot{id: "root", cancel: make(chan struct{}, 1), permission: make(chan bool, 1)}
	var _ daemon.RootConnection = newFakeConnection(backend, root)
}
