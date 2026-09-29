package gateway

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
	maxContentBytes  = 4 << 20
)

// Options are explicit launcher-owned policy. BackendDone is the actual runtime
// generation's lifetime, not a browser connection or a second execution owner.
// Assets must be a v4 application; nil leaves only API discovery available.
type Options struct {
	Address                      string
	AllowedHosts, AllowedOrigins []string
	SocketPath                   string
	RuntimeID, ProcessEpoch      protocol.ID
	BackendDone                  <-chan struct{}
	Assets                       http.Handler
}

type Server struct {
	options                Options
	endpoint               string
	cancel                 context.CancelFunc
	done                   chan struct{}
	mu                     sync.Mutex
	closed                 bool
	err                    error
	workers                sync.WaitGroup
	connections, transfers chan struct{}
}

func Start(ctx context.Context, options Options) (*Server, error) {
	if options.SocketPath == "" || options.RuntimeID == "" || options.ProcessEpoch == "" || options.BackendDone == nil {
		return nil, errors.New("gateway requires a pinned live runtime")
	}
	select {
	case <-options.BackendDone:
		return nil, errors.New("runtime generation has ended")
	default:
	}
	options.AllowedHosts = slices.Clone(options.AllowedHosts)
	options.AllowedOrigins = slices.Clone(options.AllowedOrigins)
	if err := validateOrigins(options.AllowedOrigins); err != nil {
		return nil, err
	}
	if err := validateHosts(options.AllowedHosts); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Server{options: options, cancel: cancel, done: make(chan struct{}), connections: make(chan struct{}, maxConnections), transfers: make(chan struct{}, maxTransfers)}
	upstream, err := s.connect(ctx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("verify gateway runtime: %w", err)
	}
	_ = upstream.Close()
	listener, err := listen(ctx, options.Address)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("listen for gateway: %w", err)
	}
	s.endpoint = "http://" + listener.Addr().String()
	if len(s.options.AllowedHosts) == 0 {
		s.options.AllowedHosts = []string{listener.Addr().String()}
	}
	server := &http.Server{Handler: s.handler(), BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: handshakeTimeout, ReadTimeout: requestTimeout, WriteTimeout: requestTimeout, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	go func() {
		var terminal error
		finished := false
		select {
		case <-ctx.Done():
		case <-options.BackendDone:
			terminal = errors.New("runtime generation ended; explicitly start a new gateway")
		case err := <-served:
			finished = true
			if !errors.Is(err, http.ErrServerClosed) {
				terminal = err
			}
		}
		s.mu.Lock()
		s.closed = true
		s.err = terminal
		s.mu.Unlock()
		cancel()
		_ = server.Close()
		if !finished {
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

func (s *Server) Endpoint() string      { return s.endpoint }
func (s *Server) Done() <-chan struct{} { return s.done }
func (s *Server) Err() error            { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *Server) Close() error          { s.cancel(); <-s.done; return s.Err() }
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
