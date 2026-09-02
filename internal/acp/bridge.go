// bridge.go maps ACP methods and updates onto reconnecting daemon clients.
// Provider execution, persistence, tools, permissions, MCP processes, and
// scheduling remain owned by the daemon.
package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/session"
)

const (
	ModeAuto = "auto"
	ModeAsk  = "ask"
)

var modes = []acp.SessionMode{
	{Id: ModeAsk, Name: "Ask", Description: new("Require an authenticated human decision for side effects")},
	{Id: ModeAuto, Name: "Auto", Description: new("Run within the session's configured grants (paired humans only)")},
}

// Backend creates protocol-only root clients and handles daemon-scoped
// listing. Implementations may resolve config and credentials, but never hand
// an agent, store, tool registry, or process to the ACP bridge.
type Backend interface {
	NewRoot(context.Context, string, map[string]mcp.ServerConfig) (*daemon.RootClient, error)
	LoadRoot(context.Context, string, string, map[string]mcp.ServerConfig) (*daemon.RootClient, error)
	ListSessions(context.Context, int) ([]session.Meta, error)
	Paired(context.Context) bool
}

type Bridge struct {
	version string
	backend Backend
	vision  bool
	mcpBase map[string]mcp.ServerConfig

	conn *acp.AgentSideConnection

	mu       sync.Mutex
	sessions map[acp.SessionId]*acpSession
	loading  map[acp.SessionId]bool
}

var (
	_ acp.Agent       = (*Bridge)(nil)
	_ acp.AgentLoader = (*Bridge)(nil)
)

func NewBridge(version string, backend Backend, vision bool, mcpBase map[string]mcp.ServerConfig) *Bridge {
	return &Bridge{version: version, backend: backend, vision: vision, mcpBase: mcpBase}
}

func (b *Bridge) SetAgentConnection(conn *acp.AgentSideConnection) { b.conn = conn }

type acpSession struct {
	id   acp.SessionId
	root *daemon.RootClient

	lifecycle context.Context
	stop      context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
	turnCh    chan struct{}

	mu        sync.Mutex
	closed    bool
	cancelled bool
	mode      string
	titleSent bool
	toolArgs  map[string]toolInput
	allowed   map[string]bool
}

type toolInput struct{ name, args string }

func newACPSession(root *daemon.RootClient) *acpSession {
	lifecycle, stop := context.WithCancel(context.Background())
	return &acpSession{
		id: acp.SessionId(root.RootID()), root: root, lifecycle: lifecycle, stop: stop,
		done: make(chan struct{}), turnCh: make(chan struct{}, 1), mode: ModeAsk,
		toolArgs: make(map[string]toolInput), allowed: make(map[string]bool),
	}
}

func (s *acpSession) close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.stop()
		_ = s.root.Close()
		<-s.done
	})
}

func (b *Bridge) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	v := acp.ProtocolVersion(acp.ProtocolVersionNumber)
	return acp.InitializeResponse{
		ProtocolVersion: v,
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: true,
			PromptCapabilities: acp.PromptCapabilities{
				Image: b.vision, EmbeddedContext: true,
			},
			McpCapabilities: acp.McpCapabilities{Http: true},
			SessionCapabilities: acp.SessionCapabilities{
				List: &acp.SessionListCapabilities{}, Close: &acp.SessionCloseCapabilities{},
			},
		},
		AgentInfo:   &acp.Implementation{Name: "whip", Title: new("whip"), Version: b.version},
		AuthMethods: []acp.AuthMethod{},
	}, nil
}

func (b *Bridge) Authenticate(context.Context, acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, acp.NewMethodNotFound(acp.AgentMethodAuthenticate)
}

func (b *Bridge) Logout(context.Context, acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, acp.NewMethodNotFound("logout")
}

func (b *Bridge) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionResume)
}

func (b *Bridge) SetSessionConfigOption(context.Context, acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionSetConfigOption)
}

func (b *Bridge) CloseSession(_ context.Context, params acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	b.mu.Lock()
	s, ok := b.sessions[params.SessionId]
	if ok {
		delete(b.sessions, params.SessionId)
	}
	b.mu.Unlock()
	if !ok {
		return acp.CloseSessionResponse{}, acp.NewInternalError(fmt.Sprintf("unknown session %q", params.SessionId))
	}
	s.close()
	return acp.CloseSessionResponse{}, nil
}

func (b *Bridge) CloseAll() {
	b.mu.Lock()
	sessions := make([]*acpSession, 0, len(b.sessions))
	for id, value := range b.sessions {
		sessions = append(sessions, value)
		delete(b.sessions, id)
	}
	b.mu.Unlock()
	for _, value := range sessions {
		value.close()
	}
}

func (b *Bridge) mergeMCPServers(client []acp.McpServer) map[string]mcp.ServerConfig {
	out := make(map[string]mcp.ServerConfig, len(b.mcpBase)+len(client))
	maps.Copy(out, b.mcpBase)
	for _, server := range client {
		var name string
		var value mcp.ServerConfig
		switch {
		case server.Stdio != nil:
			name = server.Stdio.Name
			value.Command = append([]string{server.Stdio.Command}, server.Stdio.Args...)
			value.Env = make(map[string]string, len(server.Stdio.Env))
			for _, item := range server.Stdio.Env {
				value.Env[item.Name] = item.Value
			}
		case server.Http != nil:
			name = server.Http.Name
			value.URL = server.Http.Url
			value.Headers = make(map[string]string, len(server.Http.Headers))
			for _, item := range server.Http.Headers {
				value.Headers[item.Name] = item.Value
			}
		default:
			continue
		}
		if name == "" {
			continue
		}
		if _, exists := b.mcpBase[name]; exists {
			config_logf("client MCP server %q shadowed by whip config — skipped", name)
			continue
		}
		out[name] = value
	}
	return out
}

func (b *Bridge) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	if params.Cwd == "" {
		return acp.NewSessionResponse{}, acp.NewInvalidParams("cwd is required")
	}
	if b.backend == nil {
		return acp.NewSessionResponse{}, acp.NewInternalError("daemon backend is unavailable")
	}
	root, err := b.backend.NewRoot(ctx, params.Cwd, b.mergeMCPServers(params.McpServers))
	if err != nil {
		return acp.NewSessionResponse{}, acp.NewInternalError(err.Error())
	}
	s := newACPSession(root)
	if err := b.setPermissionMode(ctx, s, ModeAsk); err != nil {
		s.stop()
		_ = root.Close()
		close(s.done)
		return acp.NewSessionResponse{}, acp.NewInternalError(err.Error())
	}
	if err := b.register(s); err != nil {
		s.stop()
		_ = root.Close()
		close(s.done)
		return acp.NewSessionResponse{}, err
	}
	go b.consume(s)
	return acp.NewSessionResponse{
		SessionId: s.id,
		Modes:     &acp.SessionModeState{CurrentModeId: ModeAsk, AvailableModes: modes},
	}, nil
}

func (b *Bridge) LoadSession(ctx context.Context, params acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	if b.backend == nil {
		return acp.LoadSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionLoad)
	}
	b.mu.Lock()
	if _, active := b.sessions[params.SessionId]; active || b.loading[params.SessionId] {
		b.mu.Unlock()
		return acp.LoadSessionResponse{}, acp.NewInvalidParams(fmt.Sprintf("session %q is already active", params.SessionId))
	}
	if b.loading == nil {
		b.loading = make(map[acp.SessionId]bool)
	}
	b.loading[params.SessionId] = true
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.loading, params.SessionId)
		b.mu.Unlock()
	}()

	root, err := b.backend.LoadRoot(ctx, string(params.SessionId), params.Cwd, b.mergeMCPServers(params.McpServers))
	if err != nil {
		return acp.LoadSessionResponse{}, &acp.RequestError{Code: -32002, Message: "Resource not found", Data: map[string]any{"sessionId": string(params.SessionId)}}
	}
	if root.RootID() != string(params.SessionId) {
		_ = root.Close()
		return acp.LoadSessionResponse{}, acp.NewInvalidParams(fmt.Sprintf("session id %q is not exact", params.SessionId))
	}
	snapshot, err := root.Snapshot(ctx)
	if err != nil {
		_ = root.Close()
		return acp.LoadSessionResponse{}, acp.NewInternalError(err.Error())
	}
	if params.Cwd != "" && snapshot.Meta.CWD != "" && params.Cwd != snapshot.Meta.CWD {
		_ = root.Close()
		return acp.LoadSessionResponse{}, acp.NewInvalidParams(fmt.Sprintf("cwd %q does not match session cwd %q", params.Cwd, snapshot.Meta.CWD))
	}
	s := newACPSession(root)
	if err := b.setPermissionMode(ctx, s, ModeAsk); err != nil {
		_ = root.Close()
		return acp.LoadSessionResponse{}, acp.NewInternalError(err.Error())
	}
	for _, update := range replayUpdates(snapshot.Messages) {
		if err := b.update(ctx, s.id, update); err != nil {
			_ = root.Close()
			return acp.LoadSessionResponse{}, acp.NewInternalError(err.Error())
		}
	}
	for _, event := range snapshot.Presentation {
		b.consumeEvent(s, daemon.ProtocolEvent{RootID: snapshot.RootID, Seq: event.Seq, Kind: event.Kind, Payload: event.Payload})
	}
	if err := b.register(s); err != nil {
		_ = root.Close()
		return acp.LoadSessionResponse{}, err
	}
	go b.consume(s)
	for _, permission := range snapshot.Permissions {
		payload, err := json.Marshal(pendingPermission{
			PermissionID: permission.ID, OperationID: permission.OperationID,
			Operation: permission.Operation, CanonicalPath: permission.CanonicalPath,
		})
		if err == nil {
			go b.handlePermission(s, payload)
		}
	}
	return acp.LoadSessionResponse{
		Modes: &acp.SessionModeState{CurrentModeId: ModeAsk, AvailableModes: modes},
	}, nil
}

func (b *Bridge) register(s *acpSession) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions == nil {
		b.sessions = make(map[acp.SessionId]*acpSession)
	}
	if _, exists := b.sessions[s.id]; exists {
		return acp.NewInvalidParams(fmt.Sprintf("session %q is already active", s.id))
	}
	b.sessions[s.id] = s
	return nil
}

func (b *Bridge) ListSessions(ctx context.Context, params acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	if b.backend == nil {
		return acp.ListSessionsResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionList)
	}
	metas, err := b.backend.ListSessions(ctx, 100)
	if err != nil {
		return acp.ListSessionsResponse{}, acp.NewInternalError(err.Error())
	}
	out := make([]acp.SessionInfo, 0, len(metas))
	for _, meta := range metas {
		if params.Cwd != nil && *params.Cwd != meta.CWD {
			continue
		}
		info := acp.SessionInfo{SessionId: acp.SessionId(meta.ID), Cwd: meta.CWD}
		if meta.Title != "" {
			info.Title = new(meta.Title)
		}
		if !meta.UpdatedAt.IsZero() {
			info.UpdatedAt = new(meta.UpdatedAt.UTC().Format(time.RFC3339))
		}
		out = append(out, info)
	}
	return acp.ListSessionsResponse{Sessions: out}, nil
}

func (b *Bridge) SetSessionMode(ctx context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	s := b.getSession(params.SessionId)
	if s == nil {
		return acp.SetSessionModeResponse{}, acp.NewInternalError(fmt.Sprintf("unknown session %q", params.SessionId))
	}
	mode := string(params.ModeId)
	if mode != ModeAsk && mode != ModeAuto {
		return acp.SetSessionModeResponse{}, acp.NewInvalidParams(fmt.Sprintf("unknown mode %q", params.ModeId))
	}
	if mode == ModeAuto && !b.backend.Paired(ctx) {
		return acp.SetSessionModeResponse{}, acp.NewInvalidParams("automatic permissions require a paired human identity")
	}
	if err := b.setPermissionMode(ctx, s, mode); err != nil {
		return acp.SetSessionModeResponse{}, acp.NewInternalError(err.Error())
	}
	_ = b.update(context.Background(), s.id, acp.SessionUpdate{CurrentModeUpdate: &acp.SessionCurrentModeUpdate{
		SessionUpdate: "current_mode_update", CurrentModeId: params.ModeId,
	}})
	return acp.SetSessionModeResponse{}, nil
}

func (b *Bridge) setPermissionMode(ctx context.Context, s *acpSession, mode string) error {
	action, err := s.root.NewAction("permission.mode", map[string]bool{"external_permissions": mode == ModeAsk})
	if err != nil {
		return err
	}
	result, err := s.root.SetPermissionMode(ctx, action, mode == ModeAsk)
	if err != nil {
		return err
	}
	if result.Status != "succeeded" {
		return errors.New(result.Error)
	}
	s.mu.Lock()
	s.mode = mode
	s.mu.Unlock()
	return nil
}

func (b *Bridge) Prompt(_ context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	s := b.getSession(params.SessionId)
	if s == nil {
		return acp.PromptResponse{}, acp.NewInternalError(fmt.Sprintf("unknown session %q", params.SessionId))
	}
	select {
	case s.turnCh <- struct{}{}:
	default:
		return acp.PromptResponse{}, acp.NewInternalError("session busy: a prompt turn is already running")
	}
	defer func() { <-s.turnCh }()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return acp.PromptResponse{}, acp.NewInternalError("session is closed")
	}
	s.cancelled = false
	s.mu.Unlock()

	text, parts := promptFromBlocks(params.Prompt, b.vision)
	action, err := s.root.NewAction("submit", daemon.SubmitPayload{Text: text, Parts: parts})
	if err != nil {
		return acp.PromptResponse{}, acp.NewInternalError(err.Error())
	}
	result, err := s.root.Command(s.lifecycle, action)
	if err != nil {
		if s.wasCancelled() || errors.Is(err, context.Canceled) {
			return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
		}
		return acp.PromptResponse{}, acp.NewInternalError(err.Error())
	}
	if result.Status != "succeeded" {
		if s.wasCancelled() || strings.Contains(strings.ToLower(result.Error), "canceled") {
			return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
		}
		if strings.Contains(strings.ToLower(result.Error), "context") && strings.Contains(strings.ToLower(result.Error), "limit") {
			return acp.PromptResponse{StopReason: acp.StopReasonMaxTokens}, nil
		}
		return acp.PromptResponse{}, acp.NewInternalError(result.Error)
	}
	b.sendTitle(s)
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (s *acpSession) wasCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

func (b *Bridge) Cancel(_ context.Context, params acp.CancelNotification) error {
	s := b.getSession(params.SessionId)
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.cancelled = true
	s.mu.Unlock()
	action, err := s.root.NewAction("cancel", struct{}{})
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.root.Command(ctx, action)
	return nil
}

func (b *Bridge) getSession(id acp.SessionId) *acpSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[id]
}

func (b *Bridge) consume(s *acpSession) {
	defer close(s.done)
	for {
		select {
		case <-s.lifecycle.Done():
			return
		case update, ok := <-s.root.Updates():
			if !ok {
				return
			}
			if update.Event != nil {
				b.consumeEvent(s, *update.Event)
			}
		}
	}
}

func (b *Bridge) consumeEvent(s *acpSession, event daemon.ProtocolEvent) {
	var stream daemon.StreamEvent
	if strings.HasPrefix(event.Kind, "stream.") {
		if err := json.Unmarshal(event.Payload, &stream); err != nil {
			return
		}
	}
	switch event.Kind {
	case "stream.text":
		_ = b.update(s.lifecycle, s.id, acp.UpdateAgentMessageText(stream.Text))
	case "stream.reasoning":
		_ = b.update(s.lifecycle, s.id, updateThoughtText(stream.Text))
	case "stream.tool.started":
		s.mu.Lock()
		s.toolArgs[stream.ID] = toolInput{name: stream.Name, args: stream.Args}
		s.mu.Unlock()
		_ = b.update(s.lifecycle, s.id, startToolCall(stream.ID, stream.Name, stream.Args))
	case "stream.tool.completed":
		s.mu.Lock()
		input := s.toolArgs[stream.ID]
		delete(s.toolArgs, stream.ID)
		s.mu.Unlock()
		if input.name == "" {
			input.name = stream.Name
		}
		_ = b.update(s.lifecycle, s.id, endToolCall(stream.ID, input.name, input.args, stream.Result))
	case "stream.usage":
		var usage daemon.UsageEvent
		if json.Unmarshal([]byte(stream.Result), &usage) == nil && usage.Size > 0 {
			_ = b.update(s.lifecycle, s.id, acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{
				SessionUpdate: "usage_update", Used: usage.Used, Size: usage.Size,
			}})
		}
	case "stream.plan":
		var plan daemon.PlanEvent
		if json.Unmarshal([]byte(stream.Result), &plan) == nil {
			entries := make([]acp.PlanEntry, 0, len(plan.Items))
			for _, item := range plan.Items {
				entries = append(entries, acp.PlanEntry{Content: item.Content, Priority: acp.PlanEntryPriorityMedium, Status: todoStatusToACP(item.Status)})
			}
			_ = b.update(s.lifecycle, s.id, acp.UpdatePlan(entries...))
		}
	case "permission.pending":
		go b.handlePermission(s, event.Payload)
	}
}

func (b *Bridge) update(ctx context.Context, id acp.SessionId, update acp.SessionUpdate) error {
	if b.conn == nil {
		return nil
	}
	if err := b.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: id, Update: update}); err != nil {
		if ctx.Err() == nil {
			config_logf("session/update: %v", err)
		}
		return err
	}
	return nil
}

func (b *Bridge) sendTitle(s *acpSession) {
	ctx, cancel := context.WithTimeout(s.lifecycle, 5*time.Second)
	defer cancel()
	snapshot, err := s.root.Snapshot(ctx)
	if err != nil || snapshot.Meta.Title == "" {
		return
	}
	s.mu.Lock()
	if s.titleSent {
		s.mu.Unlock()
		return
	}
	s.titleSent = true
	s.mu.Unlock()
	_ = b.update(ctx, s.id, acp.SessionUpdate{SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
		SessionUpdate: "session_info_update", Title: new(snapshot.Meta.Title),
	}})
}
