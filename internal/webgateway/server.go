// Package webgateway serves browser traffic over the daemon's Unix protocol.
// Host and Origin checks are not authentication: remote use requires a trusted
// network or an authenticated reverse proxy.
package webgateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

const (
	handshakeTimeout = 5 * time.Second
	requestTimeout   = 2 * time.Minute
	writeTimeout     = 10 * time.Second
	maxConnections   = 48
	maxTransfers     = 16
	maxContentChunk  = 256 << 10
	maxUploadSize    = 64 << 20
)

// Client is the existing initialized protocol client, supplied by the launcher.
// Open must honor cancellation; Close must unblock outstanding Calls.
type Client interface {
	Call(context.Context, string, any, any) error
	Close() error
	Done() <-chan struct{}
	InitializeResult() protocol.InitializeResult
}

// Options selects one backend socket and the exact accepted HTTP authorities.
// An empty Address tries loopback port 4444, then an ephemeral port only if busy.
// An empty AllowedHosts accepts only the actual listener address.
type Options struct {
	Address        string
	AllowedHosts   []string
	AllowedOrigins []string
	SocketPath     string
	Open           func(context.Context) (Client, error)
}

// Server owns only its listener, protocol clients and browser connections.
// Closing it never stops the daemon or cancels admitted runtime commands.
type Server struct {
	options     Options
	endpoint    string
	initialize  protocol.InitializeResult
	cancel      context.CancelFunc
	done        chan struct{}
	mu          sync.Mutex
	closed      bool
	err         error
	workers     sync.WaitGroup
	connections chan struct{}
	transfers   chan struct{}
}

// Start holds a restricted monitor connection for the gateway's entire lifetime.
// A daemon replacement or disconnect ends this gateway; it never reconnects.
func Start(ctx context.Context, options Options) (*Server, error) {
	if options.Open == nil || options.SocketPath == "" {
		return nil, errors.New("web gateway requires a daemon socket and protocol client factory")
	}
	options.AllowedHosts = slices.Clone(options.AllowedHosts)
	options.AllowedOrigins = slices.Clone(options.AllowedOrigins)
	if err := validateOrigins(options.AllowedOrigins); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	monitor, err := openClient(ctx, options.Open)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect web gateway to daemon: %w", err)
	}
	initialized := monitor.InitializeResult()
	if err := checkInitialize(initialized); err != nil {
		cancel()
		_ = monitor.Close()
		return nil, err
	}
	listener, err := listen(ctx, options.Address)
	if err != nil {
		cancel()
		_ = monitor.Close()
		return nil, fmt.Errorf("listen for web gateway: %w", err)
	}
	if len(options.AllowedHosts) == 0 {
		options.AllowedHosts = []string{listener.Addr().String()}
	}
	s := &Server{
		options: options, endpoint: "http://" + listener.Addr().String(), initialize: initialized,
		cancel: cancel, done: make(chan struct{}),
		connections: make(chan struct{}, maxConnections), transfers: make(chan struct{}, maxTransfers),
	}
	server := &http.Server{
		Handler: s.handler(), BaseContext: func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: handshakeTimeout, ReadTimeout: requestTimeout,
		WriteTimeout: requestTimeout, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10,
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	go func() {
		var terminal error
		serveFinished := false
		select {
		case <-ctx.Done():
		case <-monitor.Done():
			if ctx.Err() == nil {
				terminal = errors.New("daemon disconnected; start a new web gateway for the running daemon")
			}
		case err := <-served:
			serveFinished = true
			if !errors.Is(err, http.ErrServerClosed) {
				terminal = fmt.Errorf("web gateway stopped serving: %w", err)
			}
		}
		s.mu.Lock()
		s.closed = true
		s.err = terminal
		s.mu.Unlock()
		cancel() // Unblocks every handler, including explicitly owned hijacked sockets.
		_ = server.Close()
		_ = monitor.Close()
		if !serveFinished {
			<-served
		}
		s.workers.Wait()
		close(s.done)
	}()
	return s, nil
}

func listen(ctx context.Context, address string) (net.Listener, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	explicit := address != ""
	if !explicit {
		address = "127.0.0.1:4444"
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if !explicit && errors.Is(err, syscall.EADDRINUSE) {
		return (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	}
	return listener, err
}

func openClient(ctx context.Context, open func(context.Context) (Client, error)) (Client, error) {
	ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	return open(ctx)
}

func checkInitialize(result protocol.InitializeResult) error {
	if result.ProtocolMajor != protocol.Major || !slices.Contains(result.NegotiatedCapabilities, protocol.NetworkClientCapability) {
		return errors.New("daemon does not acknowledge restricted network clients; update it and explicitly restart before starting the web gateway")
	}
	if result.RuntimeID == "" {
		return errors.New("daemon did not identify its runtime")
	}
	return nil
}

func (s *Server) checkBackend(result protocol.InitializeResult) error {
	if err := checkInitialize(result); err != nil {
		return err
	}
	if result.RuntimeID != s.initialize.RuntimeID || result.Generation != s.initialize.Generation {
		return errors.New("daemon runtime changed; start a new web gateway")
	}
	return nil
}

// Endpoint is the actual bound HTTP origin, including any selected ephemeral port.
func (s *Server) Endpoint() string { return s.endpoint }

// InitializeResult identifies the backend pinned at startup.
func (s *Server) InitializeResult() protocol.InitializeResult { return s.initialize }

// Done closes after the listener, clients and relay workers have stopped.
func (s *Server) Done() <-chan struct{} { return s.done }

// Err reports a terminal listener/backend failure, or nil for requested shutdown.
func (s *Server) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }

// Close is idempotent and waits for all gateway-owned work to exit.
func (s *Server) Close() error { s.cancel(); <-s.done; return s.Err() }

// Admission and WaitGroup.Add share the shutdown lock so Wait cannot race Add.
func (s *Server) acquire(slots chan struct{}) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case slots <- struct{}{}:
		s.workers.Add(1)
		return true
	default:
		return false
	}
}
func (s *Server) release(slots chan struct{}) { <-slots; s.workers.Done() }
