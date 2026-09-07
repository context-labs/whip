package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNetworkHandlerValidatesHostOriginAndUpgrade(t *testing.T) {
	options := NetworkOptions{Enabled: true, AllowedHosts: []string{"127.0.0.1:7000"}, AllowedOrigins: []string{"http://localhost:3000"}}
	handler, err := newNetworkHandler(options, func(messageTransport) { t.Error("unexpected upgrade") }, func() bool { return true }, func() {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	server.Client().Timeout = 2 * time.Second
	for _, test := range []struct {
		name, host, origin, key string
		status                  int
	}{
		{name: "host", host: "evil.test", origin: "http://localhost:3000", status: 403},
		{name: "origin", host: "127.0.0.1:7000", origin: "http://evil.test", status: 403},
		{name: "origin suffix", host: "127.0.0.1:7000", origin: "http://localhost:3000.evil.test", status: 403},
		{name: "null origin", host: "127.0.0.1:7000", origin: "null", status: 403},
		{name: "same origin without configuration", host: "127.0.0.1:7000", origin: "http://127.0.0.1:7000", key: "nil", status: 400},
		{name: "normal upgrade checks", host: "127.0.0.1:7000", origin: "http://localhost:3000", key: "nil", status: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/v3/ws", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Host = test.host
			req.Header.Set("Origin", test.origin)
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Sec-WebSocket-Version", "13")
			req.Header.Set("Sec-WebSocket-Key", test.key)
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("status %d", response.StatusCode)
			}
		})
	}
}

func TestNetworkDisabledAndConnectionExhaustion(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		handler, err := newNetworkHandler(NetworkOptions{Enabled: enabled, AllowedHosts: []string{"localhost"}}, func(messageTransport) { t.Error("unexpected upgrade") }, func() bool { return false }, func() { t.Error("released unreserved slot") }, nil)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost/api/v3/ws", nil))
		want := http.StatusNotFound
		if enabled {
			want = http.StatusServiceUnavailable
		}
		if response.Code != want {
			t.Fatalf("enabled=%v status=%d", enabled, response.Code)
		}
	}
	listener, err := (NetworkOptions{}).listen()
	if err != nil || listener != nil {
		t.Fatal("disabled network opened listener")
	}
}

func TestNetworkWebDiscoveryAndHostGuard(t *testing.T) {
	handler, err := newNetworkHandler(NetworkOptions{Enabled: true, AllowedHosts: []string{"localhost:8080"}},
		func(messageTransport) { t.Fatal("unexpected upgrade") }, func() bool { return true }, func() {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/sessions/root", "/api/v3/web"} {
		request := httptest.NewRequest(http.MethodGet, "http://evil.test"+path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 403 {
			t.Fatalf("host bypass on %s: %d", path, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/v3/web", nil)
	request.Header.Set("Origin", "http://localhost:8080")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || response.Header().Get("Content-Type") != "application/json" ||
		!strings.Contains(response.Body.String(), `"websocket_path":"/api/v3/ws"`) {
		t.Fatalf("discovery: %d %s", response.Code, response.Body.String())
	}
}
