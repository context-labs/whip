// bridge.go is the acp.Agent implementation: it owns the session registry
// and maps each protocol method onto whip's agent loop and session store.
// Wire conversions live in translate.go; the permission adapter in
// permission.go. See .ai-docs/plans/acp/README.md and protocol-notes.md.
package acp

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/session"
)

// Permission modes (session/set_mode). auto = whip's headless posture (tools
// run ungated, like `whip run`); ask = gated tools round-trip through the
// client's session/request_permission.
const (
	ModeAuto = "auto"
	ModeAsk  = "ask"
)

var modes = []acp.SessionMode{
	{Id: ModeAuto, Name: "Auto", Description: new("Run tools without asking (trusted automation)")},
	{Id: ModeAsk, Name: "Ask", Description: new("Ask the editor before bash/write/edit calls")},
}

// Factory builds the agent loop + MCP manager for one session rooted at cwd.
// servers is the merged MCP config (whip's own plus the client's, whip wins
// clashes); the factory owns starting the manager and wiring its tools into
// the agent. Keeping this a callback lets cmd/whip inject its full wiring
// (model resolution, system prompt) while tests pass a fake-provider agent.
type Factory func(ctx context.Context, cwd string, servers map[string]mcp.ServerConfig) (*agent.Agent, *mcp.Manager, error)

// Bridge implements acp.Agent (+AgentLoader) over whip's agent.Agent loop.
type Bridge struct {
	version string
	newAg   Factory
	store   *session.Store // may be nil: sessions then live in memory only
	vision  bool           // resolved model accepts image content
	mcpBase map[string]mcp.ServerConfig

	conn *acp.AgentSideConnection // set by SetAgentConnection

	mu       sync.Mutex
	sessions map[acp.SessionId]*acpSession
	loading  map[acp.SessionId]bool
}

// Compile-time conformance with the SDK's agent-side interfaces.
var (
	_ acp.Agent       = (*Bridge)(nil)
	_ acp.AgentLoader = (*Bridge)(nil)
)

// NewBridge builds the agent-side handler. newAgent must return a loop-ready
// agent (tools, model, system prompt rooted at cwd). store may be nil.
func NewBridge(version string, newAgent Factory, store *session.Store, vision bool, mcpBase map[string]mcp.ServerConfig) *Bridge {
	return &Bridge{
		version: version,
		newAg:   newAgent,
		store:   store,
		vision:  vision,
		mcpBase: mcpBase,
	}
}

// SetAgentConnection implements acp.AgentConnAware — the SDK hands us the
// live connection after construction so we can send updates and permission
// requests.
func (b *Bridge) SetAgentConnection(conn *acp.AgentSideConnection) { b.conn = conn }

// acpSession is one editor-facing session: a live agent loop, its SQLite
// backing row, and the turn token.
//
// Concurrency (docs/concurrency.md): turnCh is a 1-capacity channel token —
// send acquires the turn slot, receive releases. A Prompt that arrives while
// the token is held gets a "session busy" error (no queue). The turn
// goroutine owns ag.Messages and storeFrom while it holds the token. cancel
// is safe to call any time; it interrupts the running turn.
type acpSession struct {
	id  acp.SessionId
	ag  *agent.Agent
	mcp *mcp.Manager
	// storeFrom is the messages index the next store.Save starts at. It
	// starts at 1 — index 0 is the system prompt, which must never be
	// persisted (run.go and the TUI share this convention).
	storeFrom int

	turnMu    sync.Mutex // guards cancel, mode, titleSent, allowed
	turnCh    chan struct{}
	cancel    context.CancelFunc
	lifecycle context.Context
	stop      context.CancelFunc
	closeOnce sync.Once
	closed    bool
	mode      string
	titleSent bool
	allowed   map[string]bool // "allow always" rules, this session only
}

func newACPSession(id acp.SessionId, ag *agent.Agent, m *mcp.Manager) *acpSession {
	lifecycle, stop := context.WithCancel(context.Background())
	s := &acpSession{id: id, ag: ag, mcp: m, lifecycle: lifecycle, stop: stop, mode: ModeAuto, storeFrom: 1, allowed: map[string]bool{}}
	s.turnCh = make(chan struct{}, 1)
	return s
}

// close cancels any running turn and releases the session's MCP manager.
// Idempotent; safe with a nil manager.
func (s *acpSession) close() {
	s.closeOnce.Do(func() {
		s.turnMu.Lock()
		s.closed = true
		if s.stop != nil {
			s.stop()
		}
		if s.cancel != nil {
			s.cancel()
		}
		s.turnMu.Unlock()
		if s.turnCh != nil {
			s.turnCh <- struct{}{}
			defer func() { <-s.turnCh }()
		}
		if s.mcp != nil {
			s.mcp.Close()
		}
		if s.ag != nil && s.ag.Services != nil {
			opts := s.ag.Services.ProcessOptions()
			s.ag.Close()
			s.ag.Services.Close()
			if opts.Processes != nil && opts.RootID != "" {
				_ = opts.Processes.StopRoot(opts.RootID)
			}
		}
	})
}

func startMCP(ctx context.Context, ag *agent.Agent, manager *mcp.Manager) {
	if manager == nil {
		return
	}
	opts := ag.Services.ProcessOptions()
	manager.SetProcessOptions(opts.Processes, opts.RootID, opts.Cwd, opts.Env)
	manager.Start(ctx)
	ag.SetMCPTools(manager.Tools())
}

// --- initialization -------------------------------------------------------

func (b *Bridge) Initialize(_ context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	// Version negotiation: v1 is the only version today, so answering with
	// our latest is the whole negotiation ("respond with the latest version
	// the agent supports" — protocol-notes.md §2).
	v := acp.ProtocolVersion(acp.ProtocolVersionNumber)
	return acp.InitializeResponse{
		ProtocolVersion: v,
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: b.store != nil,
			PromptCapabilities: acp.PromptCapabilities{
				Image:           b.vision,
				EmbeddedContext: true,
			},
			McpCapabilities: acp.McpCapabilities{Http: true},
			SessionCapabilities: acp.SessionCapabilities{
				List:  &acp.SessionListCapabilities{},
				Close: &acp.SessionCloseCapabilities{},
			},
		},
		AgentInfo: &acp.Implementation{
			Name:    "whip",
			Title:   new("whip"),
			Version: b.version,
		},
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

// CloseAll cancels every session's work and releases resources — called when
// the client disconnects or the process is signaled, so no turn goroutine or
// MCP child outlives the connection.
func (b *Bridge) CloseAll() {
	b.mu.Lock()
	ss := make([]*acpSession, 0, len(b.sessions))
	for id, s := range b.sessions {
		ss = append(ss, s)
		delete(b.sessions, id)
	}
	b.mu.Unlock()
	for _, s := range ss {
		s.close()
	}
}

// --- session setup --------------------------------------------------------

// mergeMCPServers layers the client-provided servers over whip's config;
// whip's own entries win name clashes (the client can't shadow whip's MCP).
// The returned map is ready for mcp.NewManager.
func (b *Bridge) mergeMCPServers(client []acp.McpServer) map[string]mcp.ServerConfig {
	out := make(map[string]mcp.ServerConfig, len(b.mcpBase)+len(client))
	maps.Copy(out, b.mcpBase)
	for _, srv := range client {
		var name string
		var cfg mcp.ServerConfig
		switch {
		case srv.Stdio != nil:
			name = srv.Stdio.Name
			cfg.Command = append([]string{srv.Stdio.Command}, srv.Stdio.Args...)
			cfg.Env = map[string]string{}
			for _, e := range srv.Stdio.Env {
				cfg.Env[e.Name] = e.Value
			}
		case srv.Http != nil:
			name = srv.Http.Name
			cfg.URL = srv.Http.Url
			cfg.Headers = map[string]string{}
			for _, h := range srv.Http.Headers {
				cfg.Headers[h.Name] = h.Value
			}
		default:
			continue // SSE deprecated; unstable variants ignored
		}
		if name == "" {
			continue
		}
		if _, taken := b.mcpBase[name]; taken {
			config_logf("client MCP server %q shadowed by whip config — skipped", name)
			continue
		}
		out[name] = cfg
	}
	return out
}

func (b *Bridge) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	if params.Cwd == "" {
		return acp.NewSessionResponse{}, acp.NewInvalidParams("cwd is required")
	}
	ag, mgr, err := b.newAg(ctx, params.Cwd, b.mergeMCPServers(params.McpServers))
	if err != nil {
		return acp.NewSessionResponse{}, acp.NewInternalError(err.Error())
	}
	id := acp.SessionId(newID())
	s := newACPSession(id, ag, mgr)
	b.bindPermissionGate(s)
	if b.store != nil {
		sid, err := b.store.Create(params.Cwd, ag.ModelName, ag.Provider)
		if err != nil {
			s.close()
			return acp.NewSessionResponse{}, acp.NewInternalError(err.Error())
		}
		s.id = acp.SessionId(sid)
		ag.SetSessionID(sid)
		ag.Services.SetProcessMarkers(sid, ag.Model)
		authority, err := b.store.EnsureClassicAuthority(ctx, sid)
		if err == nil {
			err = ag.Services.BindDispatcher(b.store, b.store.Workspaces(), b.store.Processes(), authority)
		}
		if err != nil {
			s.close()
			return acp.NewSessionResponse{}, acp.NewInternalError(err.Error())
		}
	}
	startMCP(s.lifecycle, ag, mgr)
	b.mu.Lock()
	if b.sessions == nil {
		b.sessions = make(map[acp.SessionId]*acpSession)
	}
	b.sessions[s.id] = s
	b.mu.Unlock()
	return acp.NewSessionResponse{
		SessionId: s.id,
		Modes: &acp.SessionModeState{
			CurrentModeId:  ModeAuto,
			AvailableModes: modes,
		},
	}, nil
}

// LoadSession resumes a stored session and replays its full history as
// session/update notifications BEFORE responding (spec ordering).
func (b *Bridge) LoadSession(ctx context.Context, params acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	if b.store == nil {
		return acp.LoadSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionLoad)
	}
	meta, msgs, err := b.store.Load(string(params.SessionId))
	if err != nil {
		return acp.LoadSessionResponse{}, &acp.RequestError{Code: -32002, Message: "Resource not found", Data: map[string]any{"sessionId": string(params.SessionId)}}
	}
	// Reject prefix/ambiguous ids: the client must address the session it
	// asked for, and store.Load happily resolves prefixes (its TUI behavior).
	if meta.ID != string(params.SessionId) {
		return acp.LoadSessionResponse{}, acp.NewInvalidParams(fmt.Sprintf("session id %q is not exact", params.SessionId))
	}
	// Spec: the request cwd must match the session's recorded cwd.
	if params.Cwd != "" && meta.CWD != "" && params.Cwd != meta.CWD {
		return acp.LoadSessionResponse{}, acp.NewInvalidParams(fmt.Sprintf("cwd %q does not match session cwd %q", params.Cwd, meta.CWD))
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
	ag, mgr, err := b.newAg(ctx, meta.CWD, b.mergeMCPServers(params.McpServers))
	if err != nil {
		return acp.LoadSessionResponse{}, acp.NewInternalError(err.Error())
	}
	// Store rows start at message index 0 (the system prompt is never
	// persisted), so stored msgs append after the fresh agent's system
	// prompt; storeFrom tracks the in-memory index the next Save starts at.
	ag.Messages = append(ag.Messages, msgs...)
	s := newACPSession(acp.SessionId(meta.ID), ag, mgr)
	b.bindPermissionGate(s)
	ag.SetSessionID(meta.ID)
	ag.Services.SetProcessMarkers(meta.ID, ag.Model)
	authority, err := b.store.EnsureClassicAuthority(ctx, meta.ID)
	if err == nil {
		err = ag.Services.BindDispatcher(b.store, b.store.Workspaces(), b.store.Processes(), authority)
	}
	if err != nil {
		s.close()
		return acp.LoadSessionResponse{}, acp.NewInternalError(err.Error())
	}
	startMCP(s.lifecycle, ag, mgr)
	s.storeFrom = len(ag.Messages)
	b.mu.Lock()
	if b.sessions == nil {
		b.sessions = make(map[acp.SessionId]*acpSession)
	}
	b.sessions[s.id] = s
	b.mu.Unlock()
	// Spec ordering: replay the ENTIRE history before responding. If the
	// client died mid-replay, unwind — it will never session/close this.
	for _, u := range replayUpdates(msgs) {
		if err := b.update(ctx, s.id, u); err != nil {
			b.mu.Lock()
			delete(b.sessions, s.id)
			b.mu.Unlock()
			s.close()
			return acp.LoadSessionResponse{}, acp.NewInternalError(err.Error())
		}
	}
	return acp.LoadSessionResponse{
		Modes: &acp.SessionModeState{CurrentModeId: ModeAuto, AvailableModes: modes},
	}, nil
}

func (b *Bridge) ListSessions(_ context.Context, params acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	if b.store == nil {
		return acp.ListSessionsResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionList)
	}
	metas, err := b.store.Recent(100) // ponytail: cursor pagination when anyone has >100 sessions
	if err != nil {
		return acp.ListSessionsResponse{}, acp.NewInternalError(err.Error())
	}
	out := make([]acp.SessionInfo, 0, len(metas))
	for _, m := range metas {
		if params.Cwd != nil && *params.Cwd != m.CWD {
			continue
		}
		info := acp.SessionInfo{SessionId: acp.SessionId(m.ID), Cwd: m.CWD}
		if m.Title != "" {
			info.Title = new(m.Title)
		}
		if !m.UpdatedAt.IsZero() {
			info.UpdatedAt = new(m.UpdatedAt.UTC().Format(time.RFC3339))
		}
		out = append(out, info)
	}
	return acp.ListSessionsResponse{Sessions: out}, nil
}

// --- modes ----------------------------------------------------------------

func (b *Bridge) SetSessionMode(_ context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	s := b.getSession(params.SessionId)
	if s == nil {
		return acp.SetSessionModeResponse{}, acp.NewInternalError(fmt.Sprintf("unknown session %q", params.SessionId))
	}
	switch string(params.ModeId) {
	case ModeAuto, ModeAsk:
	default:
		return acp.SetSessionModeResponse{}, acp.NewInvalidParams(fmt.Sprintf("unknown mode %q", params.ModeId))
	}
	s.turnMu.Lock()
	s.mode = string(params.ModeId)
	s.turnMu.Unlock()
	_ = b.update(context.Background(), s.id, acp.SessionUpdate{
		CurrentModeUpdate: &acp.SessionCurrentModeUpdate{
			SessionUpdate: "current_mode_update",
			CurrentModeId: params.ModeId,
		},
	})
	return acp.SetSessionModeResponse{}, nil
}

// --- prompt turn ----------------------------------------------------------

// Prompt runs one turn. One turn at a time per session: a prompt arriving
// mid-turn gets a JSON-RPC error — ACP clients serialize turns (Zed waits
// for the prompt response before sending the next), and queueing prompts
// nobody is watching invites zombie work.
func (b *Bridge) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	s := b.getSession(params.SessionId)
	if s == nil {
		return acp.PromptResponse{}, acp.NewInternalError(fmt.Sprintf("unknown session %q", params.SessionId))
	}

	// Non-blocking acquire of the turn token (1-cap channel, filelocks
	// idiom): busy = error, don't queue.
	select {
	case s.turnCh <- struct{}{}:
	default:
		return acp.PromptResponse{}, acp.NewInternalError("session busy: a prompt turn is already running")
	}
	s.turnMu.Lock()
	closed := s.closed
	s.turnMu.Unlock()
	if closed {
		<-s.turnCh
		return acp.PromptResponse{}, acp.NewInternalError("session is closed")
	}
	defer func() { <-s.turnCh }()

	text, parts := promptFromBlocks(params.Prompt, b.vision)

	// The turn's lifetime is decoupled from the request ctx (the SDK cancels
	// that ctx when a second prompt arrives for the session — with busy-error
	// semantics that second prompt is refused, and an idle cancel's parked
	// marker must not kill fresh work either). Cancellation flows through
	// session/cancel → Cancel() → s.cancel instead; CloseAll covers client
	// disconnect. turnCtx dies with the deferred cleanup below.
	turnCtx, cancel := context.WithCancel(context.Background())
	s.turnMu.Lock()
	if s.closed {
		s.turnMu.Unlock()
		cancel()
		return acp.PromptResponse{}, acp.NewInternalError("session is closed")
	}
	s.cancel = cancel
	s.turnMu.Unlock()
	defer func() {
		cancel()
		s.turnMu.Lock()
		s.cancel = nil
		s.turnMu.Unlock()
	}()

	// todowrite rewrites → ACP plan updates (full list, wholesale replace —
	// spec requires the complete entry list each time).
	s.ag.SetOnTodos(func(items []agent.Todo) {
		entries := make([]acp.PlanEntry, 0, len(items))
		for _, it := range items {
			entries = append(entries, acp.PlanEntry{
				Content:  it.Content,
				Priority: acp.PlanEntryPriorityMedium, // whip todos carry no priority
				Status:   todoStatusToACP(it.Status),
			})
		}
		_ = b.update(turnCtx, s.id, acp.UpdatePlan(entries...))
	})
	defer s.ag.SetOnTodos(nil)

	_, err := s.ag.TurnParts(turnCtx, text, parts, agent.Events{
		OnText:      func(d string) { _ = b.update(turnCtx, s.id, acp.UpdateAgentMessageText(d)) },
		OnThink:     func(d string) { _ = b.update(turnCtx, s.id, updateThoughtText(d)) },
		OnToolStart: func(id, name, args string) { _ = b.update(turnCtx, s.id, startToolCall(id, name, args)) },
		OnToolEnd:   func(id, name, result string) { _ = b.update(turnCtx, s.id, b.endTool(s, id, name, result)) },
		OnUsage:     func(u llm.Usage) { b.sendUsage(turnCtx, s, u) },
	})

	// Persist like run.go: best-effort, incremental. When the save lands and
	// the session just got its auto-title, tell the client.
	if b.store != nil {
		if serr := b.store.Save(string(s.id), s.storeFrom, s.ag.MessagesSnapshot(), s.ag.ModelName, s.ag.Provider); serr == nil {
			s.storeFrom = len(s.ag.MessagesSnapshot())
			b.sendTitle(turnCtx, s)
		}
	}

	switch {
	case err == nil:
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	case errors.Is(err, context.Canceled) || errors.Is(turnCtx.Err(), context.Canceled):
		// Spec: cancellation MUST surface as stopReason, never an error.
		return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
	case llm.IsContextLimit(err):
		// Context overflow survived the compaction retry — the honest
		// stop reason lets the client offer a fresh session.
		return acp.PromptResponse{StopReason: acp.StopReasonMaxTokens}, nil
	default:
		return acp.PromptResponse{}, acp.NewInternalError(err.Error())
	}
}

// Cancel interrupts the running turn; with no turn running it's a no-op
// (and must not poison the next prompt — the SDK parks a cancel marker on
// the next request's ctx, which the decoupled turnCtx ignores).
func (b *Bridge) Cancel(_ context.Context, params acp.CancelNotification) error {
	s := b.getSession(params.SessionId)
	if s == nil {
		return nil
	}
	s.turnMu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.turnMu.Unlock()
	return nil
}

// --- helpers ---------------------------------------------------------------

func (b *Bridge) getSession(id acp.SessionId) *acpSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[id]
}

// update sends a session/update notification. Delivery failure is swallowed
// (logged) — a notification must never abort the turn.
func (b *Bridge) update(ctx context.Context, id acp.SessionId, u acp.SessionUpdate) error {
	if b.conn == nil {
		return nil
	}
	if err := b.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: id, Update: u}); err != nil {
		// A turn killed by session/cancel will fail late updates with the
		// cancelled ctx — expected noise, not a protocol problem.
		if ctx.Err() == nil {
			config_logf("session/update: %v", err)
		}
		return err
	}
	return nil
}

// sendTitle pushes session_info_update once the store has a real title.
func (b *Bridge) sendTitle(ctx context.Context, s *acpSession) {
	if b.store == nil {
		return
	}
	meta, _, err := b.store.Load(string(s.id))
	if err != nil || meta.Title == "" {
		return
	}
	s.turnMu.Lock()
	sent := s.titleSent
	if !sent {
		s.titleSent = true
	}
	s.turnMu.Unlock()
	if sent {
		return
	}
	_ = b.update(ctx, s.id, acp.SessionUpdate{SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
		SessionUpdate: "session_info_update",
		Title:         new(meta.Title),
	}})
}

// endTool maps a finished tool call; needs the call's args for diff content,
// which the loop doesn't hand back, so we re-derive from the assistant msg.
func (b *Bridge) endTool(s *acpSession, id, name, result string) acp.SessionUpdate {
	args := ""
	for _, m := range s.ag.MessagesSnapshot() {
		for _, tc := range m.ToolCalls {
			if tc.ID == id {
				args = tc.Function.Arguments
			}
		}
	}
	return endToolCall(id, name, args, result)
}

// sendUsage reports the request's prompt tokens as "currently in context"
// (per-request u, NOT the session-cumulative Usage() — usage_update's `used`
// is current occupancy, spec §3.9.11).
func (b *Bridge) sendUsage(ctx context.Context, s *acpSession, u llm.Usage) {
	if s.ag.ContextLimit <= 0 {
		return // usage_update's size is the context window; unknown = skip
	}
	_ = b.update(ctx, s.id, acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{
		SessionUpdate: "usage_update",
		Used:          u.PromptTokens,
		Size:          s.ag.ContextLimit,
	}})
}
