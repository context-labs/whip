package acp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"github.com/google/uuid"
)

type fakeACPClient struct {
	permissionGate <-chan struct{}
	mu             sync.Mutex
	updates        []acpsdk.SessionNotification
	perms          []acpsdk.RequestPermissionRequest
	answer         string
}

func (c *fakeACPClient) SessionUpdate(_ context.Context, update acpsdk.SessionNotification) error {
	c.mu.Lock()
	c.updates = append(c.updates, update)
	c.mu.Unlock()
	return nil
}

func (c *fakeACPClient) RequestPermission(ctx context.Context, request acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	c.mu.Lock()
	c.perms = append(c.perms, request)
	answer := c.answer
	c.mu.Unlock()
	if c.permissionGate != nil {
		select {
		case <-c.permissionGate:
		case <-ctx.Done():
			return acpsdk.RequestPermissionResponse{}, ctx.Err()
		}
	}
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

func TestACPWorker(t *testing.T) {
	split := slices.Index(os.Args, "--")
	if split < 0 {
		return
	}
	if err := process.WorkerMain(os.Args[split+1:], os.Stdin, os.Stdout, nil); err != nil {
		t.Fatal(err)
	}
}

type providerFunc func(context.Context, model.Request, func(model.Chunk)) (model.Response, error)

func (f providerFunc) Prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	value, err := (model.Scripted{}).Prepare(ctx, request)
	if err == nil {
		value.Execute = func(ctx context.Context, emit func(model.Chunk)) (model.Response, error) {
			return f(ctx, request, emit)
		}
	}
	return value, err
}

func textResponse(value string) model.Response {
	return model.Response{Parts: []session.Part{{Type: "text", Text: value}}}
}

func codeProvider(code string) providerFunc {
	return func(_ context.Context, request model.Request, emit func(model.Chunk)) (model.Response, error) {
		last := request.Messages[len(request.Messages)-1]
		if last.Role == session.Tool {
			return textResponse(last.Parts[0].Result.Output), nil
		}
		emit(model.Chunk{Reasoning: "thinking"})
		raw, _ := json.Marshal(map[string]string{"code": code})
		return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "execute-" + uuid.NewString(), Name: "execute", Arguments: raw}}}}, nil
	}
}

type acpFixture struct {
	bridge *Bridge
	conn   *acpsdk.ClientSideConnection
	editor *fakeACPClient
	host   *runtime.Runtime
	native *client.Client
	cwd    string
}

func nativeFixture(t *testing.T, p providerFunc, editor *fakeACPClient) *acpFixture {
	t.Helper()
	if p == nil {
		p = func(_ context.Context, _ model.Request, emit func(model.Chunk)) (model.Response, error) {
			emit(model.Chunk{Text: "hello"})
			return textResponse("hello!"), nil
		}
	}
	if editor == nil {
		editor = &fakeACPClient{}
	}
	dir, err := os.MkdirTemp("/tmp", "whip-acp-") //nolint:usetesting // macOS socket path length.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	host, err := runtime.Open(t.Context(), dir, p, runtime.Options{PollInterval: time.Millisecond, EngineCommand: []string{executable, "-test.run=^TestACPWorker$", "--"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	snapshot, err := host.HostConfiguration().Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = host.HostConfiguration().Update(t.Context(), snapshot.Revision, func(value *config.Host) error {
		value.Defaults.Model = session.ModelSelection{Provider: "scripted", Name: "model"}
		value.Providers["scripted"] = config.Provider{Kind: "openai-chat", BaseURL: "http://127.0.0.1:1", CredentialSource: "none"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	server, err := rpc.Listen(host, rpc.HostServices{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-serverDone; err != nil {
			t.Error(err)
		}
	})
	native, err := client.Connect(t.Context(), host.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := native.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := host.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	agentRead, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	clientRead, agentWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewBridge("fixture", native, Options{Vision: true})
	agent := acpsdk.NewAgentSideConnection(bridge, agentWrite, agentRead)
	if err := bridge.SetAgentConnection(agent); err != nil {
		t.Fatal(err)
	}
	conn := acpsdk.NewClientSideConnection(editor, clientWrite, clientRead)
	t.Cleanup(func() {
		_ = agentRead.Close()
		_ = agentWrite.Close()
		_ = clientWrite.Close()
		_ = clientRead.Close()
		bridge.CloseAll()
		<-agent.Done()
		<-conn.Done()
	})
	return &acpFixture{bridge: bridge, conn: conn, editor: editor, host: host, native: native, cwd: t.TempDir()}
}

func (f *acpFixture) newSession(t *testing.T) acpsdk.SessionId {
	t.Helper()
	value, err := f.conn.NewSession(t.Context(), acpsdk.NewSessionRequest{Cwd: f.cwd, McpServers: []acpsdk.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	return value.SessionId
}

func (f *acpFixture) prompt(t *testing.T, id acpsdk.SessionId, text string) acpsdk.PromptResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	value, err := f.conn.Prompt(ctx, acpsdk.PromptRequest{SessionId: id, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock(text)}})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func await(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !ready() {
		select {
		case <-deadline.C:
			t.Fatal("condition did not settle")
		case <-ticker.C:
		}
	}
}

func TestBridgeNativeSessionPromptAndCanonicalReplay(t *testing.T) {
	f := nativeFixture(t, nil, nil)
	initialized, err := f.conn.Initialize(t.Context(), acpsdk.InitializeRequest{ProtocolVersion: acpsdk.ProtocolVersionNumber})
	if err != nil {
		t.Fatal(err)
	}
	if !initialized.AgentCapabilities.PromptCapabilities.Image || !initialized.AgentCapabilities.LoadSession {
		t.Fatalf("capabilities=%+v", initialized)
	}
	id := f.newSession(t)
	if value := f.prompt(t, id, "hello"); value.StopReason != acpsdk.StopReasonEndTurn || value.UserMessageId == nil || value.Meta["whip_cumulative_usage"] == nil {
		t.Fatalf("response=%+v", value)
	}
	var input protocol.Input
	value := f.bridge.getSession(id)
	value.mu.Lock()
	command := value.current
	value.mu.Unlock()
	admitted, found, err := command.Check(t.Context())
	if err != nil || !found || admitted.Input == nil {
		t.Fatalf("receipt=%+v %v", admitted, err)
	}
	input = *admitted.Input
	if input.Source != "user" || len(input.Parts) != 1 || input.Parts[0].Text != "hello" {
		t.Fatalf("input=%+v", input)
	}
	list, err := f.conn.ListSessions(t.Context(), acpsdk.ListSessionsRequest{})
	if err != nil || len(list.Sessions) != 1 || list.Sessions[0].SessionId != id {
		t.Fatalf("list=%+v %v", list, err)
	}
	if _, err = f.conn.CloseSession(t.Context(), acpsdk.CloseSessionRequest{SessionId: id}); err != nil {
		t.Fatal(err)
	}
	f.editor.mu.Lock()
	f.editor.updates = nil
	f.editor.mu.Unlock()
	if _, err = f.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: id, Cwd: f.cwd, McpServers: []acpsdk.McpServer{}}); err != nil {
		t.Fatal(err)
	}
	f.editor.mu.Lock()
	defer f.editor.mu.Unlock()
	var replay strings.Builder
	for _, event := range f.editor.updates {
		if event.Update.UserMessageChunk != nil {
			replay.WriteString(event.Update.UserMessageChunk.Content.Text.Text)
		}
		if event.Update.AgentMessageChunk != nil {
			replay.WriteString(event.Update.AgentMessageChunk.Content.Text.Text)
		}
	}
	if replay.String() != "hellohello!" {
		t.Fatalf("load replied before exact replay: %q", replay.String())
	}
}

func TestBridgePermissionAndModesWithoutCredentials(t *testing.T) {
	f := nativeFixture(t, codeProvider(`files.write(path="proof.txt", content="written")`), nil)
	id := f.newSession(t)
	if f.prompt(t, id, "write").StopReason != acpsdk.StopReasonEndTurn {
		t.Fatal("turn did not complete")
	}
	data, err := os.ReadFile(filepath.Join(f.cwd, "proof.txt"))
	if err != nil || string(data) != "written" {
		t.Fatalf("effect=%q %v", data, err)
	}
	f.editor.mu.Lock()
	permissions := append([]acpsdk.RequestPermissionRequest{}, f.editor.perms...)
	f.editor.mu.Unlock()
	if len(permissions) != 1 || !strings.Contains(*permissions[0].ToolCall.Title, "files.write") || !strings.Contains(permissions[0].ToolCall.Content[0].Content.Content.Text.Text, "proof.txt") {
		t.Fatalf("permission lost canonical intent: %+v", permissions)
	}
	if _, err := f.conn.SetSessionMode(t.Context(), acpsdk.SetSessionModeRequest{SessionId: id, ModeId: ModeAuto}); err != nil {
		t.Fatal(err)
	}
	s := f.bridge.getSession(id)
	await(t, func() bool { return s.mode() == ModeAuto })
	if _, err := f.conn.CloseSession(t.Context(), acpsdk.CloseSessionRequest{SessionId: id}); err != nil {
		t.Fatal(err)
	}
	loaded, err := f.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: id, Cwd: f.cwd, McpServers: []acpsdk.McpServer{}})
	if err != nil || loaded.Modes.CurrentModeId != ModeAuto {
		t.Fatalf("saved policy changed: %+v %v", loaded, err)
	}
}

func TestBridgeQuestionMapsToPermissionPromptAndAnswerOp(t *testing.T) {
	f := nativeFixture(t, codeProvider(`answer=user.ask(questions=[{"question":"First", "options":[{"label":"A","recommended":True},{"label":"B"}], "multiple":True},{"question":"Second","options":[{"label":"C"},{"label":"D"}]}])`+"\n"+`print(answer)`), &fakeACPClient{answer: "0"})
	id := f.newSession(t)
	f.prompt(t, id, "ask")
	questions, err := f.host.Questions(t.Context(), session.SessionID(id), false, "", 100)
	if err != nil || len(questions) != 1 {
		t.Fatalf("questions=%+v %v", questions, err)
	}
	question := questions[0]
	if question.State != session.QuestionAnswered || len(question.Answers) != 2 || question.Answers[0].Answer[0] != "A" || question.Answers[1].Answer[0] != "C" {
		t.Fatalf("answer=%+v", question)
	}
	f.editor.mu.Lock()
	defer f.editor.mu.Unlock()
	if len(f.editor.perms) != 2 || len(f.editor.perms[0].Options) != 3 || f.editor.perms[0].Options[0].Name != "A (recommended)" {
		t.Fatalf("editor questions=%+v", f.editor.perms)
	}
}

func TestBridgeCancelsWhilePermissionDecisionIsPending(t *testing.T) {
	gate := make(chan struct{})
	f := nativeFixture(t, codeProvider(`files.write(path="never.txt",content="never")`), &fakeACPClient{permissionGate: gate})
	id := f.newSession(t)
	done := make(chan error, 1)
	go func() {
		value, err := f.conn.Prompt(t.Context(), acpsdk.PromptRequest{SessionId: id, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock("write")}})
		if err == nil && value.StopReason != acpsdk.StopReasonCancelled {
			err = errors.New("not cancelled")
		}
		done <- err
	}()
	await(t, func() bool { f.editor.mu.Lock(); defer f.editor.mu.Unlock(); return len(f.editor.perms) == 1 })
	if err := f.conn.Cancel(t.Context(), acpsdk.CancelNotification{SessionId: id}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not settle")
	}
	if _, err := os.Stat(filepath.Join(f.cwd, "never.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled permission effected a write: %v", err)
	}
}

func TestBridgeConcurrentSessionsAndCloseDetach(t *testing.T) {
	entered, release := make(chan struct{}, 2), make(chan struct{})
	f := nativeFixture(t, func(ctx context.Context, _ model.Request, _ func(model.Chunk)) (model.Response, error) {
		entered <- struct{}{}
		select {
		case <-release:
			return textResponse("done"), nil
		case <-ctx.Done():
			return model.Response{}, ctx.Err()
		}
	}, nil)
	one, two := f.newSession(t), f.newSession(t)
	done := make(chan error, 2)
	for _, id := range []acpsdk.SessionId{one, two} {
		go func() {
			_, err := f.conn.Prompt(t.Context(), acpsdk.PromptRequest{SessionId: id, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock("wait")}})
			done <- err
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("sessions did not execute independently")
		}
	}
	first := f.bridge.getSession(one)
	first.mu.Lock()
	command := first.current
	first.mu.Unlock()
	if _, err := f.conn.CloseSession(t.Context(), acpsdk.CloseSessionRequest{SessionId: one}); err != nil {
		t.Fatal(err)
	}
	admitted, found, err := command.Check(t.Context())
	if err != nil || !found || admitted.Turn == nil || admitted.Turn.State != "running" {
		t.Fatalf("close cancelled host execution: %+v %v", admitted, err)
	}
	close(release)
	for range 2 {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("prompt observation did not settle")
		}
	}
	if _, err := command.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeRejectsUnsupportedAndInvalidProtocolRequests(t *testing.T) {
	f := nativeFixture(t, nil, nil)
	if _, err := f.conn.NewSession(t.Context(), acpsdk.NewSessionRequest{McpServers: []acpsdk.McpServer{}}); err == nil {
		t.Fatal("empty cwd accepted")
	}
	id := f.newSession(t)
	if _, err := f.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: id, Cwd: f.cwd, McpServers: []acpsdk.McpServer{}}); err == nil {
		t.Fatal("duplicate attachment accepted")
	}
	if _, err := f.conn.SetSessionMode(t.Context(), acpsdk.SetSessionModeRequest{SessionId: id, ModeId: "forged"}); err == nil {
		t.Fatal("unknown mode accepted")
	}
	if _, err := f.conn.Prompt(t.Context(), acpsdk.PromptRequest{SessionId: "missing", Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock("bad")}}); err == nil {
		t.Fatal("unknown session accepted")
	}
	if _, err := f.conn.CloseSession(t.Context(), acpsdk.CloseSessionRequest{SessionId: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: id, Cwd: "/wrong", McpServers: []acpsdk.McpServer{}}); err == nil {
		t.Fatal("wrong cwd accepted")
	}
	if _, err := f.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: id, Cwd: f.cwd, McpServers: []acpsdk.McpServer{}}); err != nil {
		t.Fatalf("failed setup reserved session forever: %v", err)
	}
	if _, err := f.bridge.Authenticate(t.Context(), acpsdk.AuthenticateRequest{}); err == nil {
		t.Fatal("unsupported auth accepted")
	}
}
