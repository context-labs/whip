// Package acp adapts native host sessions to the editor-facing ACP protocol.
// The host owns execution, receipts, history, permissions and processes.
package acp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/google/uuid"
)

const (
	ModeAuto = "auto"
	ModeAsk  = "ask"
)

var modes = []acp.SessionMode{
	{Id: ModeAsk, Name: "Ask", Description: new("Ask before performing side effects")},
	{Id: ModeAuto, Name: "Full Access", Description: new("Automatically approve eligible root actions; explicit grants and agent limits still apply")},
}

type Options struct {
	Model  *protocol.ModelSelection
	Vision bool
}

// Bridge borrows one native client. CloseAll detaches and joins only editor
// observers; callers close the client separately. It never stops host turns.
type Bridge struct {
	version   string
	client    *client.Client
	options   Options
	conn      *acp.AgentSideConnection
	lifecycle context.Context
	stop      context.CancelFunc
	mu        sync.Mutex
	closed    bool
	sessions  map[acp.SessionId]*acpSession
	loading   map[acp.SessionId]bool
	requests  sync.WaitGroup
	slots     chan struct{}
	decisions chan struct{}
}

var (
	_ acp.Agent       = (*Bridge)(nil)
	_ acp.AgentLoader = (*Bridge)(nil)
)

func NewBridge(version string, c *client.Client, options Options) *Bridge {
	ctx, stop := context.WithCancel(context.Background())
	if options.Model != nil {
		options.Model = new(*options.Model)
	}
	return &Bridge{version: version, client: c, options: options, lifecycle: ctx, stop: stop, sessions: map[acp.SessionId]*acpSession{}, loading: map[acp.SessionId]bool{}, slots: make(chan struct{}, 32), decisions: make(chan struct{}, 32)}
}
func (b *Bridge) SetAgentConnection(conn *acp.AgentSideConnection) { b.conn = conn }

func (b *Bridge) begin(parent context.Context) (context.Context, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.client == nil {
		return nil, nil, acp.NewInternalError("native ACP connection is closed")
	}
	select {
	case b.slots <- struct{}{}:
	default:
		return nil, nil, acp.NewInternalError("ACP request capacity is busy")
	}
	b.requests.Add(1)
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(b.lifecycle, cancel)
	return ctx, func() { stop(); cancel(); <-b.slots; b.requests.Done() }, nil
}

func (b *Bridge) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion(acp.ProtocolVersionNumber), AgentCapabilities: acp.AgentCapabilities{LoadSession: true, PromptCapabilities: acp.PromptCapabilities{Image: b.options.Vision, EmbeddedContext: true}, McpCapabilities: acp.McpCapabilities{Http: true}, SessionCapabilities: acp.SessionCapabilities{List: &acp.SessionListCapabilities{}, Close: &acp.SessionCloseCapabilities{}}}, AgentInfo: &acp.Implementation{Name: "whip", Title: new("whip"), Version: b.version}, AuthMethods: []acp.AuthMethod{}}, nil
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

func (b *Bridge) CloseSession(parent context.Context, p acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	_, done, err := b.begin(parent)
	if err != nil {
		return acp.CloseSessionResponse{}, err
	}
	defer done()
	b.mu.Lock()
	s := b.sessions[p.SessionId]
	delete(b.sessions, p.SessionId)
	b.mu.Unlock()
	if s == nil {
		return acp.CloseSessionResponse{}, acp.NewInvalidParams("unknown session")
	}
	s.close()
	return acp.CloseSessionResponse{}, nil
}

func (b *Bridge) CloseAll() {
	b.mu.Lock()
	b.closed = true
	b.stop()
	sessions := b.sessions
	b.sessions = map[acp.SessionId]*acpSession{}
	b.mu.Unlock()
	for _, s := range sessions {
		s.stop()
	}
	b.requests.Wait()
	for _, s := range sessions {
		s.close()
	}
}

func (b *Bridge) getSession(id acp.SessionId) *acpSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[id]
}

func (b *Bridge) reserve(id acp.SessionId) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errors.New("ACP is closed")
	}
	if b.loading[id] || b.sessions[id] != nil {
		return errors.New("session is already attached")
	}
	if len(b.sessions)+len(b.loading) >= 16 {
		return errors.New("ACP has reached its 16 attached session limit")
	}
	b.loading[id] = true
	return nil
}
func (b *Bridge) unreserve(id acp.SessionId) { b.mu.Lock(); delete(b.loading, id); b.mu.Unlock() }
func (b *Bridge) register(s *acpSession) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.sessions[s.id] != nil {
		return errors.New("session attachment is no longer available")
	}
	b.sessions[s.id] = s
	s.workers.Go(func() { b.consume(s) })
	return nil
}

func (b *Bridge) NewSession(parent context.Context, p acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	ctx, done, err := b.begin(parent)
	if err != nil {
		return acp.NewSessionResponse{}, err
	}
	defer done()
	if p.Cwd == "" {
		return acp.NewSessionResponse{}, acp.NewInvalidParams("cwd is required")
	}
	identity := protocol.ID(uuid.NewString())
	reservation := acp.SessionId("create:" + identity)
	if err := b.reserve(reservation); err != nil {
		return acp.NewSessionResponse{}, acp.NewInvalidParams(err.Error())
	}
	defer b.unreserve(reservation)
	var ref protocol.DefinitionRef
	for _, candidate := range b.client.Builtins() {
		if candidate.ID == "coding" {
			ref = candidate
			break
		}
	}
	if ref.ID == "" {
		return acp.NewSessionResponse{}, acp.NewInternalError("host did not advertise the coding definition")
	}
	var created protocol.CreateTreeResult
	err = b.client.Call(ctx, "trees.create", protocol.CreateTreeParams{CreationID: identity, Definition: ref, WorkingDirectory: p.Cwd, Overrides: protocol.ConfigPatch{Model: b.options.Model}}, &created)
	if err != nil {
		return acp.NewSessionResponse{}, acp.NewInternalError(fmt.Sprintf("create %s: %v; creation may have been accepted", identity, err))
	}
	if created.Root == nil || created.Deleted {
		return acp.NewSessionResponse{}, acp.NewInternalError("created session was deleted")
	}
	s, err := b.attach(ctx, *created.Root, p.McpServers, false)
	if err != nil {
		return acp.NewSessionResponse{}, acp.NewInternalError(fmt.Sprintf("session %s: %v", created.Root.ID, err))
	}
	return acp.NewSessionResponse{SessionId: s.id, Modes: &acp.SessionModeState{CurrentModeId: s.mode(), AvailableModes: modes}}, nil
}

func (b *Bridge) LoadSession(parent context.Context, p acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	ctx, done, err := b.begin(parent)
	if err != nil {
		return acp.LoadSessionResponse{}, err
	}
	defer done()
	if err := b.reserve(p.SessionId); err != nil {
		return acp.LoadSessionResponse{}, acp.NewInvalidParams(err.Error())
	}
	defer b.unreserve(p.SessionId)
	handle, err := b.client.Session(protocol.ID(p.SessionId))
	if err != nil {
		return acp.LoadSessionResponse{}, acp.NewInvalidParams(err.Error())
	}
	owner, err := handle.Get(ctx)
	if err != nil {
		return acp.LoadSessionResponse{}, &acp.RequestError{Code: -32002, Message: "Resource not found", Data: map[string]any{"sessionId": string(p.SessionId)}}
	}
	if owner.ParentID != nil {
		return acp.LoadSessionResponse{}, acp.NewInvalidParams("ACP attaches root sessions only")
	}
	if p.Cwd != "" && p.Cwd != owner.WorkingDirectory {
		return acp.LoadSessionResponse{}, acp.NewInvalidParams("cwd does not match the saved session")
	}
	s, err := b.attach(ctx, owner, p.McpServers, true)
	if err != nil {
		return acp.LoadSessionResponse{}, acp.NewInternalError(err.Error())
	}
	return acp.LoadSessionResponse{Modes: &acp.SessionModeState{CurrentModeId: s.mode(), AvailableModes: modes}}, nil
}

func (b *Bridge) attach(ctx context.Context, owner protocol.Session, servers []acp.McpServer, replay bool) (*acpSession, error) {
	handle, err := b.client.Session(owner.ID)
	if err != nil {
		return nil, err
	}
	if err := b.attachMCP(ctx, owner.ID, servers); err != nil {
		return nil, err
	}
	var policy protocol.PermissionPolicy
	if err := b.client.Call(ctx, "permissions.policy", protocol.SessionParams{SessionID: owner.ID}, &policy); err != nil {
		return nil, err
	}
	lifecycle, stop := context.WithCancel(b.lifecycle)
	s := &acpSession{id: acp.SessionId(owner.ID), handle: handle, tree: owner.TreeID, lifecycle: lifecycle, stop: stop, policy: policy, turnCh: make(chan struct{}, 1), pending: map[protocol.ID]*decisionWork{}}
	s.observer, err = handle.Observer(client.ObservationCursor{})
	if err != nil {
		stop()
		return nil, err
	}
	s.presentation = presentation{emit: func(update acp.SessionUpdate) error { return b.update(s.lifecycle, s.id, update) }, content: func(id protocol.ID) (acp.ContentBlock, error) { return readContent(s.lifecycle, handle, id) }}
	cancelSetup := context.AfterFunc(ctx, stop)
	defer cancelSetup()
	// Stream pages rather than retaining the transcript. The first snapshot pins
	// the load high water; new work cannot extend setup indefinitely.
	var through protocol.Counter
	for pageIndex := 0; ; pageIndex++ {
		if pageIndex >= 10000 {
			stop()
			return nil, errors.New("ACP replay exceeds 1000000 messages")
		}
		page, err := s.observer.Next(ctx)
		if err != nil {
			stop()
			return nil, err
		}
		if pageIndex == 0 {
			through = page.Snapshot.ThroughSequence
		}
		if replay {
			if err := s.presentation.observe(page, true); err != nil {
				stop()
				return nil, err
			}
		}
		s.cursor = page.Cursor
		if page.Cursor.After >= through {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		stop()
		return nil, err
	}
	if err := b.register(s); err != nil {
		stop()
		return nil, err
	}
	return s, nil
}

func readContent(ctx context.Context, s *client.Session, id protocol.ID) (acp.ContentBlock, error) {
	ref, data, err := s.ReadContent(ctx, id)
	if err != nil {
		return acp.ContentBlock{}, err
	}
	switch ref.MediaType {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return acp.ContentBlock{Image: &acp.ContentBlockImage{Type: "image", MimeType: ref.MediaType, Data: base64.StdEncoding.EncodeToString(data)}}, nil
	default:
		return acp.TextBlock(fmt.Sprintf("[attachment %s: %s, %d bytes]", id, ref.MediaType, ref.Size)), nil
	}
}

type listCursor struct {
	After    protocol.ID      `json:"after"`
	Revision protocol.Counter `json:"revision"`
	Cwd      string           `json:"cwd"`
}

func (b *Bridge) ListSessions(parent context.Context, p acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	ctx, done, err := b.begin(parent)
	if err != nil {
		return acp.ListSessionsResponse{}, err
	}
	defer done()
	query := protocol.ListTreesParams{Limit: 100}
	cwd := ""
	if p.Cwd != nil {
		cwd = *p.Cwd
	}
	if p.Cursor != nil {
		if len(*p.Cursor) > 8192 {
			return acp.ListSessionsResponse{}, acp.NewInvalidParams("invalid list cursor")
		}
		raw, err := base64.RawURLEncoding.DecodeString(*p.Cursor)
		if err != nil {
			return acp.ListSessionsResponse{}, acp.NewInvalidParams("invalid list cursor")
		}
		var cursor listCursor
		if json.Unmarshal(raw, &cursor) != nil || cursor.After == "" || cursor.Revision <= 0 || cursor.Cwd != cwd {
			return acp.ListSessionsResponse{}, acp.NewInvalidParams("list cursor scope changed")
		}
		query.After = &cursor.After
		query.ExpectedRevision = &cursor.Revision
	}
	var page protocol.ListTreesResult
	if err := b.client.Call(ctx, "trees.list", query, &page); err != nil {
		return acp.ListSessionsResponse{}, acp.NewInternalError(err.Error())
	}
	result := acp.ListSessionsResponse{Sessions: []acp.SessionInfo{}}
	for _, item := range page.Items {
		if cwd != "" && item.WorkingDirectory != cwd {
			continue
		}
		result.Sessions = append(result.Sessions, acp.SessionInfo{SessionId: acp.SessionId(item.RootID), Cwd: item.WorkingDirectory, Title: item.Tree.Metadata.Title})
	}
	if page.NextCursor != nil {
		raw, _ := json.Marshal(listCursor{After: *page.NextCursor, Revision: page.Revision, Cwd: cwd})
		result.NextCursor = new(base64.RawURLEncoding.EncodeToString(raw))
	}
	return result, nil
}

func acpPermissionMode(mode string) string {
	if mode == "automatic" {
		return ModeAuto
	}
	return ModeAsk
}

func (b *Bridge) SetSessionMode(parent context.Context, p acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	ctx, done, err := b.begin(parent)
	if err != nil {
		return acp.SetSessionModeResponse{}, err
	}
	defer done()
	s := b.getSession(p.SessionId)
	if s == nil {
		return acp.SetSessionModeResponse{}, acp.NewInvalidParams("unknown session")
	}
	mode := "prompt"
	switch string(p.ModeId) {
	case ModeAsk:
	case ModeAuto:
		mode = "automatic"
	default:
		return acp.SetSessionModeResponse{}, acp.NewInvalidParams("unknown mode")
	}
	s.mu.Lock()
	revision := s.policy.Revision
	s.mu.Unlock()
	var edit protocol.PermissionModeEdit
	err = b.client.Call(ctx, "permissions.set_mode", protocol.SetPermissionModeParams{SessionID: s.handle.ID(), EditID: protocol.ID(uuid.NewString()), ExpectedRevision: revision, Mode: mode}, &edit)
	if err != nil {
		return acp.SetSessionModeResponse{}, acp.NewInternalError(err.Error())
	}
	if err := b.applyPermissionMode(s, edit.Policy); err != nil {
		return acp.SetSessionModeResponse{}, err
	}
	return acp.SetSessionModeResponse{}, nil
}

func (b *Bridge) applyPermissionMode(s *acpSession, policy protocol.PermissionPolicy) error {
	if policy.TreeID != s.tree {
		return errors.New("permission policy belongs to another tree")
	}
	s.mu.Lock()
	changed := policy.Revision > s.policy.Revision && policy.Mode != s.policy.Mode
	if policy.Revision > s.policy.Revision {
		s.policy = policy
	}
	s.mu.Unlock()
	if changed {
		return b.update(s.lifecycle, s.id, acp.SessionUpdate{CurrentModeUpdate: &acp.SessionCurrentModeUpdate{SessionUpdate: "current_mode_update", CurrentModeId: acp.SessionModeId(acpPermissionMode(policy.Mode))}})
	}
	return nil
}

func (b *Bridge) update(ctx context.Context, id acp.SessionId, update acp.SessionUpdate) error {
	if b.conn == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return b.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: id, Update: update})
}
