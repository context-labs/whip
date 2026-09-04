package daemon

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

const (
	initializationTimeout = 5 * time.Second
	clientIdleTimeout     = 90 * time.Second
)

type ServerOptions struct {
	BuildID               string
	Generation            int64
	PID                   int
	StartedAt             time.Time
	RuntimeDir            string
	InitializationTimeout time.Duration
	ClientIdleTimeout     time.Duration
	MaxConnections        int
	MaxInFlight           int
	MaxOutbound           int
	MaxOutboundBytes      int64
	Restart               func()
	Stop                  func()
}

type Server struct {
	daemon   *Daemon
	options  ServerOptions
	ctx      context.Context
	cancel   context.CancelFunc
	listener net.Listener

	mu        sync.Mutex
	clients   map[*serverConn]struct{}
	slots     chan struct{}
	lifeMu    sync.Mutex
	wg        sync.WaitGroup
	closed    atomic.Bool
	closeOnce sync.Once
	uploads   *uploadManager
}

type serverConn struct {
	server   *Server
	conn     net.Conn
	id       string
	client   InitializeParams
	out      chan []byte
	inFlight chan struct{}

	mu        sync.Mutex
	authMu    sync.Mutex
	outBytes  int64
	nonce     []byte
	lifecycle int64
	snapshots map[string][]SnapshotChunk
	done      chan struct{}
	closed    bool
	once      sync.Once
}

func NewServer(value *Daemon, options ServerOptions) (*Server, error) {
	if value == nil {
		return nil, errors.New("protocol server requires a daemon")
	}
	if options.MaxConnections <= 0 {
		options.MaxConnections = MaxConnections
	}
	if options.MaxInFlight <= 0 {
		options.MaxInFlight = MaxInFlight
	}
	if options.MaxOutbound <= 0 {
		options.MaxOutbound = MaxOutboundEnvelopes
	}
	if options.MaxOutboundBytes <= 0 {
		options.MaxOutboundBytes = MaxOutboundBytes
	}
	if options.InitializationTimeout <= 0 {
		options.InitializationTimeout = initializationTimeout
	}
	if options.ClientIdleTimeout <= 0 {
		options.ClientIdleTimeout = clientIdleTimeout
	}
	if options.PID <= 0 {
		options.PID = os.Getpid()
	}
	if options.StartedAt.IsZero() {
		options.StartedAt = time.Now().UTC()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		daemon: value, options: options, ctx: ctx, cancel: cancel,
		clients: make(map[*serverConn]struct{}), slots: make(chan struct{}, options.MaxConnections),
		uploads: newUploadManager(value.store, options.RuntimeDir),
	}, nil
}

func (s *Server) ListenAndServe(paths RuntimePaths) error {
	listener, err := listenLocal(paths)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	return s.Serve(listener)
}

func (s *Server) Serve(listener net.Listener) error {
	if listener == nil {
		return errors.New("protocol listener is required")
	}
	s.lifeMu.Lock()
	if s.closed.Load() {
		s.lifeMu.Unlock()
		_ = listener.Close()
		return nil
	}
	s.listener = listener
	s.lifeMu.Unlock()
	if err := s.daemon.ResumeActive(s.ctx); err != nil {
		return err
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			if s.closed.Load() || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		select {
		case s.slots <- struct{}{}:
			if !s.goWorker(func() {
				defer func() { <-s.slots }()
				s.serveConn(conn)
			}) {
				<-s.slots
				_ = conn.Close()
				return nil
			}
		default:
			_ = conn.Close()
		}
	}
}

func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.lifeMu.Lock()
		s.closed.Store(true)
		s.cancel()
		if s.listener != nil {
			err = s.listener.Close()
		}
		s.lifeMu.Unlock()
		s.mu.Lock()
		connections := make([]*serverConn, 0, len(s.clients))
		for connection := range s.clients {
			connections = append(connections, connection)
		}
		s.mu.Unlock()
		for _, connection := range connections {
			connection.close()
		}
		s.uploads.abortClient("")
		s.wg.Wait()
		err = errors.Join(err, s.daemon.Close())
	})
	return err
}

func (s *Server) serveConn(raw net.Conn) {
	nonce, err := randomNonce()
	if err != nil {
		_ = raw.Close()
		return
	}
	connection := &serverConn{
		server: s, conn: raw, id: hex.EncodeToString(nonce[:16]), out: make(chan []byte, s.options.MaxOutbound),
		inFlight: make(chan struct{}, s.options.MaxInFlight), nonce: nonce, snapshots: make(map[string][]SnapshotChunk),
		done: make(chan struct{}),
	}
	defer connection.close()
	_ = raw.SetReadDeadline(time.Now().Add(s.options.InitializationTimeout))
	reader := bufio.NewReaderSize(raw, MaxFrameSize)
	frame, err := readProtocolFrame(reader)
	if err != nil {
		return
	}
	message, err := decodeFrame(frame)
	if err != nil || message.Method != "initialize" || len(message.ID) == 0 {
		_ = writeProtocolMessage(raw, rpcMessage{ID: message.ID, Error: rpcFailure(-32600, "initialize must be the first request")})
		return
	}
	var initialize InitializeParams
	if err := json.Unmarshal(message.Params, &initialize); err != nil {
		_ = writeProtocolMessage(raw, rpcMessage{ID: message.ID, Error: rpcFailure(-32602, "invalid initialize params")})
		return
	}
	if err := s.register(connection, initialize); err != nil {
		_ = writeProtocolMessage(raw, rpcMessage{ID: message.ID, Error: rpcFailure(-32001, err.Error())})
		return
	}
	defer s.unregister(connection)
	_ = raw.SetReadDeadline(time.Time{})
	if err := writeProtocolMessage(raw, rpcMessage{ID: message.ID, Result: InitializeResult{
		ProtocolMajor: ProtocolMajor, BuildID: s.options.BuildID, Generation: s.options.Generation,
		PID: s.options.PID, StartedAt: s.options.StartedAt.Format(time.RFC3339Nano),
		Capabilities: []string{"commands", "events", "snapshots", "uploads", "identities"}, Nonce: connection.nonceValue(),
	}}); err != nil {
		return
	}
	if !s.goWorker(connection.writeLoop) {
		return
	}
	for rootID, cursor := range initialize.Cursors {
		if !s.goWorker(func() { s.pumpEvents(connection, rootID, cursor) }) {
			return
		}
	}
	for {
		_ = raw.SetReadDeadline(time.Now().Add(s.options.ClientIdleTimeout))
		frame, err := readProtocolFrame(reader)
		if err != nil {
			return
		}
		message, err := decodeFrame(frame)
		if err == nil && (message.Method == "daemon.restart" || message.Method == "daemon.stop") && len(message.ID) == 0 {
			var params RestartParams
			if json.Unmarshal(message.Params, &params) == nil && connection.consumeLifecycle(params.Generation) {
				if message.Method == "daemon.restart" && s.options.Restart != nil {
					go s.options.Restart()
				} else if message.Method == "daemon.stop" && s.options.Stop != nil {
					go s.options.Stop()
				}
			}
			return
		}
		if err != nil || message.Method == "" || len(message.ID) == 0 {
			connection.reply(message.ID, nil, rpcFailure(-32600, "invalid request"))
			if err != nil {
				return
			}
			continue
		}
		select {
		case connection.inFlight <- struct{}{}:
			request := message
			if !s.goWorker(func() {
				defer func() { <-connection.inFlight }()
				result, requestErr := s.handle(connection, request)
				connection.reply(request.ID, result, requestErr)
			}) {
				<-connection.inFlight
				return
			}
		default:
			connection.reply(message.ID, nil, rpcFailure(-32002, "too many in-flight requests"))
		}
	}
}

func (s *Server) goWorker(work func()) bool {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	if s.closed.Load() {
		return false
	}
	s.wg.Go(work)
	return true
}

func (s *Server) register(connection *serverConn, initialize InitializeParams) error {
	if initialize.ProtocolMajor != ProtocolMajor {
		return fmt.Errorf("unsupported protocol major %d", initialize.ProtocolMajor)
	}
	if initialize.ClientID == "" || initialize.ClientKind == "" {
		return errors.New("client ID and kind are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	connection.client = initialize
	s.clients[connection] = struct{}{}
	return nil
}

func (s *Server) unregister(connection *serverConn) {
	s.mu.Lock()
	delete(s.clients, connection)
	s.mu.Unlock()
}

func (s *Server) handle(connection *serverConn, request rpcMessage) (any, *RPCError) {
	switch request.Method {
	case "daemon.ping":
		return map[string]any{"generation": s.options.Generation, "build_id": s.options.BuildID}, nil
	case "command":
		var params CommandParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid command params")
		}
		result, err := s.command(connection, params)
		return result, rpcFromError(err)
	case "events.replay":
		var params ReplayParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid replay params")
		}
		result, err := s.replay(params)
		return result, rpcFromError(err)
	case "snapshot":
		var params SnapshotParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid snapshot params")
		}
		result, err := s.snapshot(connection, params.RootID)
		return result, rpcFromError(err)
	case "snapshot.chunk":
		var params SnapshotChunkParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid snapshot chunk request")
		}
		result, err := connection.snapshotChunk(params)
		return result, rpcFromError(err)
	case "provider.validate":
		var params ProviderValidateParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid provider validation request")
		}
		if params.Name == "" || params.BaseURL == "" || params.Key == "" {
			return nil, rpcFailure(-32602, "provider name, base URL, and key are required")
		}
		validationCtx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		models, err := llm.New(params.BaseURL, params.Key).Models(validationCtx)
		return ProviderValidateResult{Models: models}, rpcFromError(err)
	case "upload.begin":
		var params UploadBeginParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid upload metadata")
		}
		return map[string]bool{"accepted": true}, rpcFromError(s.uploads.begin(connection.id, params))
	case "upload.chunk":
		var params UploadChunkParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid upload chunk")
		}
		return map[string]bool{"accepted": true}, rpcFromError(s.uploads.chunk(connection.id, params))
	case "upload.finish":
		var params UploadFinishParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid upload completion")
		}
		result, err := s.uploads.finish(s.ctx, connection.id, params.UploadID)
		return result, rpcFromError(err)
	case "identity.enroll":
		connection.authMu.Lock()
		defer connection.authMu.Unlock()
		var params EnrollIdentityParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid identity enrollment")
		}
		result, err := s.enrollIdentity(connection, params)
		return result, rpcFromError(err)
	case "identity.status":
		result, err := s.identityStatus(connection)
		return result, rpcFromError(err)
	case "permission.decide":
		connection.authMu.Lock()
		defer connection.authMu.Unlock()
		var params PermissionDecisionParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid permission decision")
		}
		if err := s.verifyPrivileged(connection, request.Method, params.Decision, params.Signature); err != nil {
			return nil, rpcFromError(err)
		}
		root, err := s.daemon.Open(params.Decision.RootID)
		if err != nil {
			return nil, rpcFromError(err)
		}
		payload, err := json.Marshal(params.Decision)
		if err != nil {
			return nil, rpcFromError(err)
		}
		digest, err := requestDigest(string(session.CommandScopeRoot), params.Decision.RootID, "permission.decide", payload)
		if err != nil {
			return nil, rpcFromError(err)
		}
		ticket, err := root.DecidePermissionCommand(s.ctx, session.CommandAdmission{
			ClientID: connection.client.ClientID, CommandID: params.Decision.CommandID, RequestDigest: digest,
			Payload: session.RuntimePayload{Data: payload, MediaType: "application/json", Source: "permission decision"},
		}, params.Decision.PermissionID, capability.Decision{
			Allow: params.Decision.Allow, PrincipalID: connection.client.ClientID, Reason: params.Decision.Reason, Remember: params.Decision.Remember,
		})
		if err != nil {
			return nil, rpcFromError(err)
		}
		nonce, err := connection.rotateNonce()
		return PermissionDecisionResult{OperationID: ticket.OperationID, LeaseID: ticket.LeaseID, Nonce: nonce}, rpcFromError(err)
	case "permission.mode":
		connection.authMu.Lock()
		defer connection.authMu.Unlock()
		var params PermissionModeParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid permission mode")
		}
		if err := s.verifyPrivileged(connection, request.Method, params.Command, params.Signature); err != nil {
			return nil, rpcFromError(err)
		}
		result, err := s.commandAuthorized(connection, params.Command, true)
		if err != nil {
			return nil, rpcFromError(err)
		}
		nonce, err := connection.rotateNonce()
		return PermissionModeResult{Command: result, Nonce: nonce}, rpcFromError(err)
	default:
		return nil, rpcFailure(-32601, "method not found")
	}
}

func (s *Server) command(connection *serverConn, params CommandParams) (CommandResult, error) {
	return s.commandAuthorized(connection, params, false)
}

func (s *Server) commandAuthorized(connection *serverConn, params CommandParams, automaticPermissionAuthorized bool) (CommandResult, error) {
	if params.CommandID == "" || params.Operation == "" {
		return CommandResult{}, errors.New("command ID and operation are required")
	}
	digest, err := requestDigest(params.Scope, params.RootID, params.Operation, params.Payload)
	if err != nil {
		return CommandResult{}, err
	}
	if params.Scope == string(session.CommandScopeDaemon) {
		if params.Operation != "session.create" && params.Operation != "session.list" && params.Operation != "session.delete" && params.Operation != "daemon.checkpoint" {
			return CommandResult{}, fmt.Errorf("unsupported daemon command %q", params.Operation)
		}
		if params.Operation == "daemon.checkpoint" {
			record, err := s.daemon.control.Checkpoint(s.ctx, session.CommandAdmission{
				ClientID: connection.client.ClientID, CommandID: params.CommandID, RequestDigest: digest,
				Payload: session.RuntimePayload{Data: params.Payload, MediaType: "application/json", Source: params.Operation},
			}, s.options.Generation)
			if err != nil {
				return CommandResult{}, err
			}
			output, err := s.daemon.store.ResolveRuntimeValue(s.ctx, "", record.Outcome)
			if err == nil {
				connection.armLifecycle(s.options.Generation)
			}
			return CommandResult{CommandID: params.CommandID, IngressSeq: record.IngressSeq, Status: record.Status, Output: string(output)}, err
		}
		admission := session.CommandAdmission{
			ClientID: connection.client.ClientID, CommandID: params.CommandID, RequestDigest: digest,
			Payload: session.RuntimePayload{Data: params.Payload, MediaType: "application/json", Source: params.Operation},
		}
		if params.Operation == "session.list" {
			var payload struct {
				Limit int `json:"limit"`
			}
			if len(params.Payload) > 0 {
				if err := json.Unmarshal(params.Payload, &payload); err != nil {
					return CommandResult{}, errors.New("invalid session list payload")
				}
			}
			if payload.Limit == 0 {
				payload.Limit = 50
			}
			record, err := s.daemon.control.ListSessions(s.ctx, admission, payload.Limit)
			return s.commandRecordResult(record, err)
		}
		if params.Operation == "session.delete" {
			var payload struct {
				RootID string `json:"root_id"`
			}
			if err := json.Unmarshal(params.Payload, &payload); err != nil || payload.RootID == "" {
				return CommandResult{}, errors.New("invalid session delete payload")
			}
			record, err := s.daemon.control.DeleteSession(s.ctx, admission, payload.RootID, s.daemon.DeleteSession)
			return s.commandRecordResult(record, err)
		}
		var create CreateSession
		if err := json.Unmarshal(params.Payload, &create); err != nil {
			return CommandResult{}, errors.New("invalid session creation payload")
		}
		record, err := s.daemon.control.CreateSession(s.ctx, admission, create)
		if err != nil {
			return CommandResult{}, err
		}
		output, err := s.daemon.store.ResolveRuntimeValue(s.ctx, "", record.Outcome)
		return CommandResult{CommandID: params.CommandID, IngressSeq: record.IngressSeq, Status: record.Status, Output: string(output)}, err
	}
	if params.Scope != string(session.CommandScopeRoot) {
		return CommandResult{}, errors.New("command scope must be daemon or root")
	}
	root, err := s.daemon.Open(params.RootID)
	if err != nil {
		return CommandResult{}, err
	}
	if params.Operation == "permission.mode" {
		var mode struct {
			External bool `json:"external_permissions"`
		}
		if err := json.Unmarshal(params.Payload, &mode); err != nil {
			return CommandResult{}, errors.New("invalid permission mode payload")
		}
		if !mode.External && !automaticPermissionAuthorized {
			return CommandResult{}, errors.New("automatic permissions require a signed paired-human request")
		}
	}
	if params.Operation != "submit" && params.Operation != "steer" {
		if !isClientOperation(params.Operation) {
			return CommandResult{}, fmt.Errorf("unsupported root command %q", params.Operation)
		}
		storedPayload := params.Payload
		switch params.Operation {
		case "terminal.input":
			var input clientActionPayload
			if err := json.Unmarshal(params.Payload, &input); err != nil {
				return CommandResult{}, errors.New("invalid terminal input payload")
			}
			storedPayload, _ = json.Marshal(struct {
				ID       string `json:"id"`
				Redacted bool   `json:"redacted"`
			}{ID: input.ID, Redacted: true})
		case "mcp.attach":
			var input struct {
				Servers map[string]json.RawMessage `json:"servers"`
			}
			if err := json.Unmarshal(params.Payload, &input); err != nil {
				return CommandResult{}, errors.New("invalid MCP attachment payload")
			}
			names := make([]string, 0, len(input.Servers))
			for name := range input.Servers {
				names = append(names, name)
			}
			slices.Sort(names)
			storedPayload, _ = json.Marshal(struct {
				Names    []string `json:"names"`
				Redacted bool     `json:"redacted"`
			}{Names: names, Redacted: true})
		}
		return root.ClientCommand(s.ctx, session.CommandAdmission{
			ClientID: connection.client.ClientID, CommandID: params.CommandID, Kind: params.Operation, RequestDigest: digest,
			Payload: session.RuntimePayload{Data: storedPayload, MediaType: "application/json", Source: params.Operation},
		}, params.Operation, params.Payload)
	}
	var payload SubmitPayload
	if err := json.Unmarshal(params.Payload, &payload); err != nil || payload.Text == "" && len(payload.Parts) == 0 {
		return CommandResult{}, errors.New("root command requires non-empty content")
	}
	commandKind := params.Operation
	commandPayload := session.RuntimePayload{Data: []byte(payload.Text), MediaType: "text/plain", Source: params.Operation}
	if len(payload.Parts) > 0 {
		commandKind = params.Operation + ".parts"
		commandPayload = session.RuntimePayload{Data: params.Payload, MediaType: "application/json", Source: commandKind}
	}
	admission, receipt, err := root.AdmitCommand(s.ctx, session.CommandAdmission{
		ClientID: connection.client.ClientID, CommandID: params.CommandID, Kind: commandKind, RequestDigest: digest,
		Payload: commandPayload,
	})
	if err != nil {
		return CommandResult{}, err
	}
	completion, err := receipt.Wait(s.ctx)
	if err != nil {
		return CommandResult{}, err
	}
	result := CommandResult{
		CommandID: params.CommandID, IngressSeq: admission.Command.IngressSeq,
		Status: admission.Command.Status, Output: completion.Output,
	}
	if completion.Err != nil {
		if admission.Command.Status == "queued" || admission.Command.Status == "running" || admission.Command.Status == "waiting" {
			result.Status = "failed"
		}
		result.Error = completion.Err.Error()
	} else {
		result.Status = "succeeded"
	}
	return result, nil
}

func (s *Server) commandRecordResult(record session.CommandRecord, actionErr error) (CommandResult, error) {
	output, resolveErr := s.daemon.store.ResolveRuntimeValue(s.ctx, "", record.Outcome)
	result := CommandResult{
		CommandID: record.CommandID, IngressSeq: record.IngressSeq,
		Status: record.Status, Output: string(output),
	}
	if actionErr != nil {
		result.Error = actionErr.Error()
	}
	return result, errors.Join(actionErr, resolveErr)
}

func (s *Server) replay(params ReplayParams) (ReplayResult, error) {
	if params.Limit == 0 {
		params.Limit = session.MaxEventReplay
	}
	events, latest, err := s.daemon.store.ReplayEvents(s.ctx, params.RootID, params.Cursor, params.Limit)
	if errors.Is(err, session.ErrCursorExpired) {
		return ReplayResult{Latest: latest, Expired: true}, nil
	}
	if err != nil {
		return ReplayResult{}, err
	}
	result := ReplayResult{Latest: latest}
	for _, event := range events {
		payload, err := s.daemon.store.ResolveRuntimeValue(s.ctx, params.RootID, event.Payload)
		if err != nil {
			return ReplayResult{}, err
		}
		result.Events = append(result.Events, ProtocolEvent{RootID: params.RootID, Seq: event.Seq, Kind: event.Kind, Payload: payload})
	}
	return result, nil
}

func (s *Server) snapshot(connection *serverConn, rootID string) (SnapshotResult, error) {
	root, err := s.daemon.Open(rootID)
	if err != nil {
		return SnapshotResult{}, err
	}
	snapshot, err := root.Snapshot(s.ctx)
	if err != nil {
		return SnapshotResult{}, err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return SnapshotResult{}, err
	}
	if len(data) > MaxUploadSize {
		return SnapshotResult{}, errors.New("snapshot exceeds the 64 MiB protocol limit")
	}
	count := (len(data) + MaxSnapshotChunk - 1) / MaxSnapshotChunk
	chunks := make([]SnapshotChunk, 0, count)
	for index, start := 0, 0; start < len(data); index, start = index+1, start+MaxSnapshotChunk {
		end := min(start+MaxSnapshotChunk, len(data))
		chunks = append(chunks, SnapshotChunk{Index: index, Count: count, Cursor: snapshot.Cursor, Data: append([]byte(nil), data[start:end]...)})
	}
	nonce, err := randomNonce()
	if err != nil {
		return SnapshotResult{}, err
	}
	snapshotID := hex.EncodeToString(nonce[:16])
	connection.mu.Lock()
	connection.snapshots = map[string][]SnapshotChunk{snapshotID: chunks}
	connection.mu.Unlock()
	return SnapshotResult{SnapshotID: snapshotID, Count: count, Cursor: snapshot.Cursor}, nil
}

func (s *Server) pumpEvents(connection *serverConn, rootID string, cursor int64) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := s.replay(ReplayParams{RootID: rootID, Cursor: cursor, Limit: 100})
		if err != nil {
			return
		}
		if result.Expired {
			connection.notify("snapshot.required", snapshotRequired{RootID: rootID, Cursor: result.Latest})
			return
		}
		for _, event := range result.Events {
			if !connection.notify("event", eventNotification{Event: event}) {
				return
			}
			cursor = event.Seq
		}
		select {
		case <-s.ctx.Done():
			return
		case <-connection.done:
			return
		case <-ticker.C:
		}
	}
}

func (c *serverConn) writeLoop() {
	for {
		select {
		case <-c.server.ctx.Done():
			return
		case <-c.done:
			return
		case frame := <-c.out:
			c.mu.Lock()
			c.outBytes -= int64(len(frame))
			closed := c.closed
			c.mu.Unlock()
			if closed {
				return
			}
			if _, err := c.conn.Write(frame); err != nil {
				c.close()
				return
			}
		}
	}
}

func (c *serverConn) send(message rpcMessage) bool {
	frame, err := marshalFrame(message)
	if err != nil {
		c.close()
		return false
	}
	c.mu.Lock()
	if c.closed || c.outBytes+int64(len(frame)) > c.server.options.MaxOutboundBytes {
		c.mu.Unlock()
		c.close()
		return false
	}
	c.outBytes += int64(len(frame))
	c.mu.Unlock()
	select {
	case c.out <- frame:
		return true
	default:
		c.mu.Lock()
		c.outBytes -= int64(len(frame))
		c.mu.Unlock()
		c.close()
		return false
	}
}

func (c *serverConn) reply(id json.RawMessage, result any, failure *RPCError) bool {
	return c.send(rpcMessage{ID: id, Result: result, Error: failure})
}

func (c *serverConn) notify(method string, params any) bool {
	raw, err := json.Marshal(params)
	if err != nil {
		return false
	}
	return c.send(rpcMessage{Method: method, Params: raw})
}

func (c *serverConn) close() {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		if c.done != nil {
			close(c.done)
		}
		_ = c.conn.Close()
		c.server.uploads.abortClient(c.id)
	})
}

func (c *serverConn) nonceValue() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.nonce...)
}

func (c *serverConn) rotateNonce() ([]byte, error) {
	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.nonce = nonce
	c.mu.Unlock()
	return append([]byte(nil), nonce...), nil
}

func (c *serverConn) snapshotChunk(params SnapshotChunkParams) (SnapshotChunk, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	chunks := c.snapshots[params.SnapshotID]
	if params.SnapshotID == "" || params.Index < 0 || params.Index >= len(chunks) {
		return SnapshotChunk{}, errors.New("snapshot chunk is unavailable")
	}
	chunk := chunks[params.Index]
	chunk.Data = append([]byte(nil), chunk.Data...)
	return chunk, nil
}

func (c *serverConn) armLifecycle(generation int64) {
	c.mu.Lock()
	c.lifecycle = generation
	c.mu.Unlock()
}

func (c *serverConn) consumeLifecycle(generation int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.lifecycle || generation == 0 {
		return false
	}
	c.lifecycle = 0
	return true
}

func readProtocolFrame(reader *bufio.Reader) ([]byte, error) {
	frame, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(frame) > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	if errors.Is(err, io.EOF) && len(frame) > 0 {
		return nil, io.ErrUnexpectedEOF
	}
	if err != nil {
		return nil, err
	}
	return frame, nil
}

func writeProtocolMessage(writer io.Writer, message rpcMessage) error {
	frame, err := marshalFrame(message)
	if err != nil {
		return err
	}
	_, err = writer.Write(frame)
	return err
}

func rpcFailure(code int, message string) *RPCError { return &RPCError{Code: code, Message: message} }

func rpcFromError(err error) *RPCError {
	if err == nil {
		return nil
	}
	return rpcFailure(-32000, err.Error())
}
