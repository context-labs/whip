package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/context-labs/whip/internal/account"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferenceaccount"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/providerhost"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/terminal"
)

var ErrNetworkRestricted = errors.New("human terminals are disabled for network clients")

// HostServices are borrowed command-owned authorities, separate from sessions.
type HostServices struct {
	Terminals        *terminal.Manager
	NetworkTerminals bool
	OpenAI           *account.Service
	Inference        *inferenceaccount.Service
	Config           *config.Authority
	ProviderHost     *providerhost.Service
}

type Server struct {
	runtime  *runtime.Runtime
	host     HostServices
	listener *net.UnixListener
	serving  atomic.Bool
}

// Listen uses the private directory whose execution lock the runtime holds.
// Runtime.Open already removed any dead owner's socket under that lock.
func Listen(r *runtime.Runtime, host HostServices) (*Server, error) {
	path := r.SocketPath()
	if len(path) > 100 {
		return nil, errors.New("runtime socket path exceeds 100 bytes; choose a shorter directory")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return &Server{runtime: r, host: host, listener: listener}, nil
}
func (s *Server) Close() error { return s.listener.Close() }

// Serve bounds connections and frame sizes. Its context owns only transport;
// the runtime's separate lifetime owns every accepted input.
func (s *Server) Serve(ctx context.Context) error {
	if !s.serving.CompareAndSwap(false, true) {
		return errors.New("RPC server already served")
	}
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	var workers sync.WaitGroup
	shutdown := func() {
		_ = s.listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for conn := range connections {
			_ = conn.Close()
		}
	}
	stop := context.AfterFunc(ctx, shutdown)
	defer func() { stop(); shutdown(); workers.Wait() }()
	slots := make(chan struct{}, 64)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		mu.Lock()
		connections[conn] = true
		mu.Unlock()
		workers.Go(func() {
			defer func() { _ = conn.Close(); mu.Lock(); delete(connections, conn); mu.Unlock(); <-slots }()
			s.connection(ctx, conn)
		})
	}
}

func (s *Server) connection(ctx context.Context, conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), protocol.MaxFrameBytes+1)
	initialized := false
	network := false
	for {
		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			return
		}
		if !scanner.Scan() {
			return
		}
		raw := scanner.Bytes()
		if protocol.Validate("Request", raw) != nil {
			return
		}
		var request protocol.Request
		if json.Unmarshal(raw, &request) != nil {
			return
		}
		if initialized && request.Method == "executor.bind" {
			s.executorConnection(ctx, conn, scanner, request)
			return
		}
		var result any
		var err error
		if initialized && request.Method == "initialize" {
			err = fmt.Errorf("%w: connection is already initialized", session.ErrInvalid)
		} else if network && !s.host.NetworkTerminals && networkRestrictedMethod(request.Method) {
			err = ErrNetworkRestricted
		} else if !initialized && request.Method != "initialize" {
			err = fmt.Errorf("%w: initialize is required", session.ErrInvalid)
		} else {
			requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			result, err = Dispatch(requestCtx, s.runtime, s.host, request.Method, request.Params)
			cancel()
		}
		response := protocol.Response{JSONRPC: "2.0", ID: request.ID}
		if err == nil {
			response.Result, err = json.Marshal(result)
		}
		if err == nil && len(response.Result) > protocol.MaxFrameBytes-1024 {
			err = errors.New("RPC response exceeds frame limit")
			response.Result = nil
		}
		if err != nil {
			response.Result = nil
			response.Error = wireError(err)
		}
		if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return
		}
		if json.NewEncoder(conn).Encode(response) != nil {
			return
		}
		if request.Method == "initialize" {
			if err != nil {
				return
			}
			var params protocol.InitializeParams
			if json.Unmarshal(request.Params, &params) != nil {
				return
			}
			network = params.NetworkClient
			initialized = true
		}
	}
}

func networkRestrictedMethod(method string) bool {
	return method == "shell.input" || strings.HasPrefix(method, "terminal.") || strings.HasPrefix(method, "terminals.")
}
