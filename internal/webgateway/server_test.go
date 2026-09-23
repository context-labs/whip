package webgateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

type fakeClient struct {
	init protocol.InitializeResult
	done chan struct{}
	once sync.Once
	call func(context.Context, string, any, any) error
}

func newFakeClient() *fakeClient {
	return &fakeClient{done: make(chan struct{}), init: protocol.InitializeResult{
		ProtocolMajor: protocol.Major, RuntimeID: "test-runtime", Generation: 7,
		NegotiatedCapabilities: []string{protocol.NetworkClientCapability},
	}}
}
func (c *fakeClient) Call(ctx context.Context, method string, params, result any) error {
	if c.call == nil {
		return errors.New("unexpected call: " + method)
	}
	return c.call(ctx, method, params, result)
}
func (c *fakeClient) Close() error                                { c.once.Do(func() { close(c.done) }); return nil }
func (c *fakeClient) Done() <-chan struct{}                       { return c.done }
func (c *fakeClient) InitializeResult() protocol.InitializeResult { return c.init }

func testServer(t *testing.T, socket string, monitor *fakeClient) *Server {
	t.Helper()
	s, err := Start(t.Context(), Options{Address: "127.0.0.1:0", SocketPath: socket,
		Open: func(context.Context) (Client, error) { return monitor, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMonitorLossClosesListener(t *testing.T) {
	t.Parallel()
	monitor := newFakeClient()
	s := testServer(t, "unused", monitor)
	_ = monitor.Close()
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("gateway remained live after daemon loss")
	}
	if s.Err() == nil {
		t.Fatal("daemon loss not reported")
	}
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(s.Endpoint(), "http://"), time.Second)
	if err == nil {
		_ = conn.Close()
		t.Fatal("listener survived backend loss")
	}
}

func TestStartupRequiresAcknowledgedRestriction(t *testing.T) {
	t.Parallel()
	monitor := newFakeClient()
	monitor.init.NegotiatedCapabilities = nil
	if s, err := Start(t.Context(), Options{Address: "127.0.0.1:0", SocketPath: "unused",
		Open: func(context.Context) (Client, error) { return monitor, nil },
	}); err == nil {
		_ = s.Close()
		t.Fatal("accepted an old daemon")
	}
	select {
	case <-monitor.Done():
	default:
		t.Fatal("failed startup leaked client")
	}
}

func TestExplicitBindDoesNotFallback(t *testing.T) {
	t.Parallel()
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	listener, err := listen(t.Context(), occupied.Addr().String())
	if err == nil {
		_ = listener.Close()
		t.Fatal("explicit busy address moved")
	}
}

func TestImplicitBindFallsBackOnlyWhenBusy(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:4444")
	if err != nil {
		t.Skipf("isolated reservation of default port unavailable: %v", err)
	}
	defer occupied.Close()
	listener, err := listen(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if listener.Addr().String() == occupied.Addr().String() {
		t.Fatal("did not choose fallback")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if listener, err := listen(ctx, ""); err == nil {
		_ = listener.Close()
		t.Fatal("canceled bind fell back")
	}
}

func bareServer() *Server {
	return &Server{
		options:     Options{AllowedHosts: []string{"localhost:8080"}, AllowedOrigins: []string{"http://localhost:3000"}},
		initialize:  newFakeClient().init,
		connections: make(chan struct{}, maxConnections), transfers: make(chan struct{}, maxTransfers),
	}
}

func TestHostOriginAndDiscovery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, host, origin string
		status             int
	}{
		{"allowed no origin", "localhost:8080", "", 200},
		{"same origin", "localhost:8080", "http://localhost:8080", 200},
		{"explicit origin", "localhost:8080", "http://localhost:3000", 200},
		{"wrong host", "evil.test", "http://localhost:3000", 403},
		{"host suffix", "localhost:8080.evil.test", "", 403},
		{"wrong origin", "localhost:8080", "http://evil.test", 403},
		{"origin suffix", "localhost:8080", "http://localhost:3000.evil.test", 403},
		{"null origin", "localhost:8080", "null", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"/", "/sessions/root", "/api/v3/web", "/api/v3/content/ref"} {
				request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost:8080"+path, nil)
				request.Host = tc.host
				request.Header.Set("Origin", tc.origin)
				response := httptest.NewRecorder()
				if tc.status == 200 && path != "/api/v3/web" {
					continue
				}
				bareServer().handler().ServeHTTP(response, request)
				if response.Code != tc.status {
					t.Fatalf("%s status %d: %s", path, response.Code, response.Body.String())
				}
				if tc.status == 200 && !strings.Contains(response.Body.String(), `"websocket_path":"/api/v3/ws"`) {
					t.Fatal("discovery contract changed")
				}
			}
		})
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "http://localhost:8080/api/v3/web", nil)
	request.Header.Add("Origin", "http://localhost:8080")
	request.Header.Add("Origin", "http://localhost:3000")
	response := httptest.NewRecorder()
	bareServer().handler().ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("ambiguous Origin accepted")
	}
}

func TestDesktopOriginExplicitAndExact(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{"whip-app://evil", "whip-app://bundle.evil", "whip-app://bundle:80", "whip-app://bundle/", "null", "*", "https://example.test/"} {
		if validateOrigins([]string{origin}) == nil {
			t.Errorf("invalid origin %q accepted", origin)
		}
	}
	if err := validateOrigins([]string{"whip-app://bundle", "http://localhost:3000", "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	for _, configured := range []bool{false, true} {
		s := bareServer()
		if configured {
			s.options.AllowedOrigins = []string{"whip-app://bundle"}
		}
		for _, method := range []string{http.MethodGet, http.MethodOptions} {
			request := httptest.NewRequestWithContext(t.Context(), method, "http://localhost:8080/api/v3/web", nil)
			request.Header.Set("Origin", "whip-app://bundle")
			response := httptest.NewRecorder()
			s.handler().ServeHTTP(response, request)
			if !configured && response.Code != 403 {
				t.Fatal("unconfigured desktop origin accepted")
			}
			if configured && response.Header().Get("Access-Control-Allow-Origin") != "whip-app://bundle" {
				t.Fatal("explicit origin not accepted")
			}
		}
	}
}

func TestAdmissionRejectsBeforeUpgrade(t *testing.T) {
	t.Parallel()
	s := bareServer()
	for range maxConnections {
		s.connections <- struct{}{}
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost:8080/api/v3/ws", nil)
	response := httptest.NewRecorder()
	s.handler().ServeHTTP(response, request)
	if response.Code != 503 {
		t.Fatalf("status %d", response.Code)
	}
	for range maxTransfers {
		s.transfers <- struct{}{}
	}
	request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost:8080/api/v3/content/ref", nil)
	response = httptest.NewRecorder()
	s.handler().ServeHTTP(response, request)
	if response.Code != 503 {
		t.Fatalf("transfer status %d", response.Code)
	}
}

func TestInvalidUpgradeClosesHijackedConnection(t *testing.T) {
	t.Parallel()
	monitor := newFakeClient()
	s := testServer(t, "unused", monitor)
	for _, origin := range []string{"", s.Endpoint()} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.Endpoint()+"/api/v3/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Origin", origin)
		request.Header.Set("Connection", "Upgrade")
		request.Header.Set("Upgrade", "websocket")
		request.Header.Set("Sec-WebSocket-Version", "13")
		request.Header.Set("Sec-WebSocket-Key", "invalid")
		response, err := (&http.Client{Timeout: time.Second}).Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != 400 {
			t.Fatalf("status %d", response.StatusCode)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
