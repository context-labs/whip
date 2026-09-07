package daemon

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

const (
	initializationTimeout = 5 * time.Second
	clientIdleTimeout     = 90 * time.Second
)

type ServerOptions struct {
	Network               NetworkOptions
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
	providers       *ProviderService
	daemon          *Daemon
	options         ServerOptions
	ctx             context.Context
	cancel          context.CancelFunc
	listener        net.Listener
	httpServer      *http.Server
	networkListener net.Listener
	networkEndpoint string
	runtimeID       string

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
	ctx           context.Context
	cancel        context.CancelFunc
	subscriptions map[string]*subscription
	server        *Server
	conn          messageTransport
	id            string
	client        InitializeParams
	out           chan []byte
	inFlight      chan struct{}

	mu        sync.Mutex
	outBytes  int64
	lifecycle int64
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
	runtimeID, err := value.store.RuntimeID(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	return &Server{
		daemon: value, options: options, ctx: ctx, cancel: cancel, runtimeID: runtimeID, providers: NewProviderService(ctx, strconv.FormatInt(options.Generation, 10)),
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
	if err := s.startNetwork(); err != nil {
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
		if s.httpServer != nil {
			err = errors.Join(err, s.httpServer.Close())
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
		s.providers.Close()
		s.wg.Wait()
		err = errors.Join(err, s.daemon.Close())
	})
	return err
}

func (s *Server) serveConn(raw net.Conn) {
	s.serveTransport(newUnixMessageTransport(raw))
}

func (s *Server) serveTransport(raw messageTransport) {
	ctx, cancel := context.WithCancel(s.ctx)
	connection := &serverConn{
		ctx: ctx, cancel: cancel, subscriptions: make(map[string]*subscription),
		server: s, conn: raw, id: rand.Text(), out: make(chan []byte, s.options.MaxOutbound),
		inFlight: make(chan struct{}, s.options.MaxInFlight),
		done:     make(chan struct{}),
	}
	defer connection.close()
	_ = raw.SetReadDeadline(time.Now().Add(s.options.InitializationTimeout))
	frame, err := raw.ReadMessage()
	if err != nil {
		return
	}
	message, err := decodeFrame(frame)
	if err != nil || message.Method != "initialize" || len(message.ID) == 0 {
		_ = writeTransportMessage(raw, rpcMessage{ID: message.ID, Error: rpcFailure(-32600, "initialize must be the first request")})
		return
	}
	if err := protocol.ValidateRPC("initialize", message.Params); err != nil {
		_ = writeTransportMessage(raw, rpcMessage{ID: message.ID, Error: rpcFailure(-32602, err.Error())})
		return
	}
	var initialize InitializeParams
	if err := json.Unmarshal(message.Params, &initialize); err != nil {
		_ = writeTransportMessage(raw, rpcMessage{ID: message.ID, Error: rpcFailure(-32602, "invalid initialize params")})
		return
	}
	if err := s.register(connection, initialize); err != nil {
		_ = writeTransportMessage(raw, rpcMessage{ID: message.ID, Error: rpcFailure(-32001, err.Error())})
		return
	}
	defer s.unregister(connection)
	_ = raw.SetReadDeadline(time.Time{})
	capabilities := []string{"commands", "events", "snapshots", "uploads", "permissions", "history_pages", "collections", "host_configuration", "workspace_completion", "host_views", "themes", "mailbox_inspection", "input_attachments"}
	negotiated := []string{}
	for _, feature := range initialize.Capabilities {
		if slices.Contains(capabilities, feature) && !slices.Contains(negotiated, feature) {
			negotiated = append(negotiated, feature)
		}
	}
	if err := writeTransportMessage(raw, rpcMessage{ID: message.ID, Result: InitializeResult{
		Operations: protocol.Operations(), NegotiatedCapabilities: negotiated,
		Limits:        protocol.ProtocolLimits{FrameBytes: MaxFrameSize, Connections: s.options.MaxConnections, InFlightRequests: s.options.MaxInFlight, OutboundMessages: s.options.MaxOutbound, OutboundBytes: s.options.MaxOutboundBytes, RootSubscriptions: MaxSubscriptions, ContentChunkBytes: MaxContentChunk, UploadBytes: MaxUploadSize},
		ProtocolMajor: ProtocolMajor, ProtocolMinor: ProtocolMinor, RuntimeID: s.runtimeID, ConnectionID: connection.id, HostPlatform: runtime.GOOS, HostArchitecture: runtime.GOARCH, NetworkEndpoint: s.networkEndpoint, BuildID: s.options.BuildID, Generation: s.options.Generation,
		PID: s.options.PID, StartedAt: s.options.StartedAt.Format(time.RFC3339Nano),
		Capabilities: capabilities,
	}}); err != nil {
		return
	}
	if !s.goWorker(connection.writeLoop) {
		return
	}

	for {
		_ = raw.SetReadDeadline(time.Now().Add(s.options.ClientIdleTimeout))
		frame, err := raw.ReadMessage()
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
	if operation, known := protocol.Lookup(request.Method); !known || operation.Surface != "rpc" {
		return nil, rpcFailure(-32601, "unsupported operation")
	}
	if err := protocol.ValidateRPC(request.Method, request.Params); err != nil {
		return nil, rpcFailure(-32602, err.Error())
	}
	if result, failure, handled := s.handleProvider(connection, request); handled {
		return result, failure
	}
	if result, failure, handled := s.handleHost(connection.ctx, request); handled {
		return result, failure
	}

	switch request.Method {
	case "sessions.list":
		var params protocol.SessionCatalogParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error())
		}
		result, err := s.sessionCatalog(connection.ctx, params)
		return result, rpcFromError(err)
	case "sessions.revision":
		result, err := s.daemon.store.SessionCatalogRevision(connection.ctx)
		return result, rpcFromError(err)
	case "workspace.complete":
		var params protocol.CompletionParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error())
		}
		result, err := s.completeWorkspace(connection.ctx, params)
		return result, rpcFromError(err)
	case "root.collection":
		var params protocol.RootCollectionParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error())
		}
		result, err := s.rootCollection(connection.ctx, params)
		return result, rpcFromError(err)
	case "content.read":
		var params protocol.ContentReadParams
		if err := decodeProviderParams(request.Params, &params); err != nil || params.Offset < 0 || params.Limit < 1 || params.Limit > MaxContentChunk {
			return nil, rpcFailure(-32602, "invalid bounded content read")
		}
		data, meta, err := s.daemon.store.ReadContent(connection.ctx, params.ReferenceID, params.RootID, params.AgentID, params.Offset, params.Limit)
		return protocol.ContentReadResult{Data: data, Content: ContentHandle{ReferenceID: meta.ReferenceID, Digest: meta.Digest, Size: meta.Size, MediaType: meta.MediaType, Source: meta.Source}}, rpcFromError(err)
	case "operation.invoke":
		var params protocol.QueryParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid operation params")
		}
		result, err := s.invoke(connection.ctx, params)
		return result, rpcFromError(err)
	case "query":
		var params protocol.QueryParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid query params")
		}
		result, err := s.query(connection.ctx, params)
		return result, rpcFromError(err)
	case "daemon.ping":
		return protocol.PingResult{Generation: s.options.Generation, BuildID: s.options.BuildID}, nil
	case "command.submit":
		var params CommandParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid command params")
		}
		_, err := s.command(connection, params)
		if err != nil {
			return nil, rpcFromError(err)
		}
		record, err := s.daemon.store.LoadCommand(s.ctx, connection.client.ClientID, params.CommandID)
		result, err := s.commandRecordResult(s.ctx, record, err)
		return result, rpcFromError(err)
	case "events.subscribe":
		var params SubscribeParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid subscription")
		}
		result, err := s.subscribe(connection, params)
		return result, rpcFromError(err)
	case "events.unsubscribe":
		var params UnsubscribeParams
		if err := json.Unmarshal(request.Params, &params); err != nil || params.SubscriptionID == "" {
			return nil, rpcFailure(-32602, "subscription ID is required")
		}
		connection.unsubscribe(params.SubscriptionID)
		return struct{}{}, nil
	case "command.status":
		var params CommandStatusParams
		if err := json.Unmarshal(request.Params, &params); err != nil || params.CommandID == "" {
			return nil, rpcFailure(-32602, "command ID is required")
		}
		record, err := s.daemon.store.LoadCommand(connection.ctx, connection.client.ClientID, params.CommandID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, rpcFailure(-32011, "command not found")
		}
		result, err := s.commandRecordResult(connection.ctx, record, err)
		return result, rpcFromError(err)
	case "events.replay":
		var params ReplayParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid replay params")
		}
		result, err := s.replayContext(connection.ctx, params)
		return result, rpcFromError(err)
	case "root.snapshot":
		var params SnapshotParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid snapshot params")
		}
		root, err := s.daemon.Open(params.RootID)
		if err != nil {
			return nil, rpcFromError(err)
		}
		result, err := root.SnapshotView(connection.ctx)
		return result, rpcFromError(err)
	case "history.page":
		var params HistoryPageParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid history params")
		}
		result, err := s.historyPage(connection.ctx, params)
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
	case "permission.decide":
		var params PermissionDecisionParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, "invalid permission decision")
		}
		decision := params.Decision
		if decision.CommandID == "" || decision.RootID == "" || decision.PermissionID == "" {
			return nil, rpcFailure(-32602, "permission decision requires command, root, and permission IDs")
		}
		root, err := s.daemon.Open(decision.RootID)
		if err != nil {
			return nil, rpcFromError(err)
		}
		payload, err := json.Marshal(decision)
		if err != nil {
			return nil, rpcFromError(err)
		}
		digest, err := requestDigest(string(session.CommandScopeRoot), decision.RootID, "permission.decide", payload)
		if err != nil {
			return nil, rpcFromError(err)
		}
		ticket, err := root.DecidePermissionCommand(s.ctx, session.CommandAdmission{
			ClientID: connection.client.ClientID, CommandID: decision.CommandID, RequestDigest: digest,
			Payload: session.RuntimePayload{Data: payload, MediaType: "application/json", Source: "permission decision"},
		}, decision.PermissionID, capability.Decision{
			Allow: decision.Allow, PrincipalID: connection.client.ClientID, Reason: decision.Reason, Remember: decision.Remember,
		})
		if err != nil {
			return nil, rpcFromError(err)
		}
		return PermissionDecisionResult{OperationID: ticket.OperationID, LeaseID: ticket.LeaseID}, nil
	default:
		return nil, rpcFailure(-32601, "method not found")
	}
}

func (s *Server) command(connection *serverConn, params CommandParams) (CommandResult, error) {
	if metadata, ok := protocol.LookupRuntime(params.Operation); !ok || metadata.Execution != protocol.Command {
		return CommandResult{}, rpcFailure(-32601, "operation is not a command")
	}
	if err := protocol.ValidateRuntime(params.Operation, params.Payload); err != nil {
		return CommandResult{}, rpcFailure(-32602, err.Error())
	}
	if params.CommandID == "" || params.Operation == "" {
		return CommandResult{}, errors.New("command ID and operation are required")
	}
	digest, err := requestDigest(params.Scope, params.RootID, params.Operation, params.Payload)
	if err != nil {
		return CommandResult{}, err
	}
	if params.Scope == string(session.CommandScopeDaemon) {
		if params.Operation != "session.create" && params.Operation != "session.delete" && params.Operation != "daemon.checkpoint" {
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
			output, err := s.daemon.store.ResolveRuntimeValue(s.ctx, record.RootID, record.Outcome)
			if err == nil {
				connection.armLifecycle(s.options.Generation)
			}
			return CommandResult{CommandID: params.CommandID, IngressSeq: record.IngressSeq, Status: record.Status, Output: string(output)}, err
		}
		admission := session.CommandAdmission{
			ClientID: connection.client.ClientID, CommandID: params.CommandID, RequestDigest: digest,
			Payload: session.RuntimePayload{Data: params.Payload, MediaType: "application/json", Source: params.Operation},
		}

		if params.Operation == "session.delete" {
			var payload struct {
				RootID string `json:"root_id"`
			}
			if err := json.Unmarshal(params.Payload, &payload); err != nil || payload.RootID == "" {
				return CommandResult{}, errors.New("invalid session delete payload")
			}
			record, err := s.daemon.control.AcceptDeleteSession(s.ctx, admission, payload.RootID, s.daemon.DeleteSession)
			return s.commandRecordResult(s.ctx, record, err)
		}
		var create CreateSession
		if err := json.Unmarshal(params.Payload, &create); err != nil {
			return CommandResult{}, errors.New("invalid session creation payload")
		}
		record, err := s.daemon.control.CreateSession(s.ctx, admission, create)
		if err != nil {
			return CommandResult{}, err
		}
		output, err := s.daemon.store.ResolveRuntimeValue(s.ctx, record.RootID, record.Outcome)
		return CommandResult{CommandID: params.CommandID, IngressSeq: record.IngressSeq, Status: record.Status, Output: string(output)}, err
	}
	if params.Scope != string(session.CommandScopeRoot) {
		return CommandResult{}, errors.New("command scope must be daemon or root")
	}
	root, err := s.daemon.Open(params.RootID)
	if err != nil {
		return CommandResult{}, err
	}
	if existing, err := s.validateCommandAttachments(s.ctx, connection.client.ClientID, params, digest, root); err != nil {
		return CommandResult{}, err
	} else if existing != nil {
		return *existing, nil
	}
	if params.Operation != "submit" && params.Operation != "steer" {
		if !isClientOperation(params.Operation) {
			return CommandResult{}, fmt.Errorf("unsupported root command %q", params.Operation)
		}
		return root.AcceptClientCommand(s.ctx, session.CommandAdmission{
			ClientID: connection.client.ClientID, CommandID: params.CommandID, Kind: params.Operation, RequestDigest: digest,
			Payload: session.RuntimePayload{Data: params.Payload, MediaType: "application/json", Source: params.Operation},
		}, params.Operation, params.Payload)
	}
	var payload SubmitPayload
	if err := json.Unmarshal(params.Payload, &payload); err != nil || payload.Text == "" && len(payload.Parts) == 0 && len(payload.Attachments) == 0 {
		return CommandResult{}, errors.New("root command requires non-empty content")
	}
	commandKind := params.Operation
	commandPayload := session.RuntimePayload{Data: []byte(payload.Text), MediaType: "text/plain", Source: params.Operation}
	if len(payload.Parts) > 0 || len(payload.Attachments) > 0 {
		commandKind = params.Operation + ".parts"
		commandPayload = session.RuntimePayload{Data: params.Payload, MediaType: "application/json", Source: commandKind}
	}
	admission, err := root.AcceptCommand(s.ctx, session.CommandAdmission{
		ClientID: connection.client.ClientID, CommandID: params.CommandID, Kind: commandKind, Operation: params.Operation, RequestDigest: digest,
		Payload: commandPayload,
	})
	if err != nil {
		return CommandResult{}, err
	}
	return s.commandRecordResult(s.ctx, admission.Command, nil)
}

func (s *Server) commandRecordResult(ctx context.Context, record session.CommandRecord, actionErr error) (CommandResult, error) {
	if actionErr != nil {
		return CommandResult{}, actionErr
	}
	if record.Outcome.ReferenceID != "" && record.Status == "succeeded" {
		value := record.Outcome
		return CommandResult{Operation: record.Operation, CommandID: record.CommandID, IngressSeq: record.IngressSeq, Status: record.Status, Content: &ContentHandle{ReferenceID: value.ReferenceID, Digest: value.Digest, Size: value.Size, MediaType: value.MediaType, Source: value.Source}}, nil
	}
	if record.Scope == session.CommandScopeRoot && record.RootID != "" && record.Status == "running" && (record.IngressSeq > 0 || record.Operation == "shell.run" || record.Operation == "tool.call") {
		waiting, err := s.daemon.store.CommandPermissionWaiting(ctx, record)
		if err != nil {
			return CommandResult{}, err
		}
		root, err := s.daemon.Open(record.RootID)
		if err != nil {
			return CommandResult{}, err
		}
		for _, question := range root.questions.open() {
			if question.AgentID == root.authority.AgentID {
				waiting = true
			}
		}
		if waiting {
			record.Status = "waiting"
		}
	}
	output, resolveErr := s.daemon.store.ResolveRuntimeValue(ctx, record.RootID, record.Outcome)
	result := CommandResult{CommandID: record.CommandID, Operation: record.Operation, IngressSeq: record.IngressSeq, Status: record.Status}
	if record.Status == "failed" || record.Status == "cancelled" || record.Status == "interrupted" {
		if len(output) > 0 {
			var failure RPCError
			if err := json.Unmarshal(output, &failure); err != nil {
				return result, err
			}
			result.Failure = &failure
		}
		if result.Failure == nil {
			result.Failure = rpcFailure(-32000, "command is "+record.Status)
		}
	} else if len(output) > 0 {
		result.Result = json.RawMessage(output)
	}
	result.Output, result.Error = decodeCommandPresentation(record.Operation, output, record.Status)

	return result, errors.Join(actionErr, resolveErr)
}

func (s *Server) replay(params ReplayParams) (ReplayResult, error) {
	return s.replayContext(s.ctx, params)
}

func (s *Server) replayContext(ctx context.Context, params ReplayParams) (ReplayResult, error) {
	if params.Limit == 0 {
		params.Limit = session.MaxEventReplay
	}
	events, latest, err := s.daemon.store.ReplayEvents(ctx, params.RootID, params.Cursor, params.Limit)
	if errors.Is(err, session.ErrCursorExpired) {
		return ReplayResult{Latest: latest, Expired: true}, nil
	}
	if err != nil {
		return ReplayResult{}, err
	}
	result := ReplayResult{Latest: latest, Events: []ProtocolEvent{}}
	remaining := 512 << 10
	for _, event := range events {
		var payload json.RawMessage
		if event.Payload.ReferenceID != "" {
			value := event.Payload
			payload, _ = json.Marshal(struct {
				Content   ContentHandle `json:"content"`
				Truncated bool          `json:"truncated"`
			}{Content: ContentHandle{ReferenceID: value.ReferenceID, Digest: value.Digest, Size: value.Size, MediaType: value.MediaType, Source: value.Source}, Truncated: true})
		} else {
			payload = event.Payload.Inline
		}
		if len(payload) == 0 {
			payload = json.RawMessage(`{}`)
		}
		if !json.Valid(payload) || payload[0] != '{' {
			return ReplayResult{}, errors.New("event payload is not a JSON object")
		}
		if len(payload)+512 > remaining {
			break
		}
		remaining -= len(payload) + 512

		result.Events = append(result.Events, ProtocolEvent{RootID: params.RootID, Seq: event.Seq, Kind: event.Kind, Payload: payload})
	}
	return result, nil
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
			if err := c.conn.WriteMessage(frame); err != nil {
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
		if c.cancel != nil {
			c.cancel()
		}
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

func rpcFailure(code int, message string) *RPCError {
	kind := "execution_failed"
	switch code {
	case -32600, -32602:
		kind = "invalid_arguments"
	case -32601:
		kind = "unsupported_operation"
	case -32001:
		kind = "unsupported_protocol"
	case -32002:
		kind = "resource_limit"
	case -32003:
		kind = "permission_denied"
	case -32004:
		kind = "unavailable_capability"
	case -32009:
		kind = "conflict"
	case -32010:
		kind = "resynchronization_required"
	case -32011:
		kind = "command_not_found"
	case -32603:
		kind = "internal_error"
	}
	return &RPCError{Code: code, Message: message, Data: &protocol.ErrorData{Kind: kind}}
}

func rpcFromError(err error) *RPCError {
	if err == nil {
		return nil
	}
	if failure, ok := errors.AsType[*RPCError](err); ok {
		return failure
	}
	if errors.Is(err, session.ErrCursorExpired) || errors.Is(err, session.ErrCursorAhead) || errors.Is(err, session.ErrHistoryRevision) || errors.Is(err, session.ErrCollectionChanged) {
		return rpcFailure(-32010, err.Error())
	}
	if errors.Is(err, capability.ErrDenied) || errors.Is(err, session.ErrContentAccess) {
		return rpcFailure(-32003, err.Error())
	}
	if errors.Is(err, ErrClosed) || errors.Is(err, ErrStopped) {
		return rpcFailure(-32004, err.Error())
	}
	if errors.Is(err, session.ErrCommandConflict) {
		return rpcFailure(-32009, err.Error())
	}
	return rpcFailure(-32000, err.Error())
}
