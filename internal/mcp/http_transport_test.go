package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This local legacy endpoint exercises the SDK's standalone SSE path. The
// modern discover protocol does not open that GET and would miss the regression.
type legacyHTTPFixture struct {
	server        *httptest.Server
	getStarted    chan struct{}
	getStopped    chan struct{}
	deleteStarted chan struct{}
	deleteStopped chan struct{}
	notify        chan struct{}
	lists         chan struct{}
	updated       atomic.Bool
	release       func()
}

func newLegacyHTTPFixture(t *testing.T, stallGET, stallDELETE bool) *legacyHTTPFixture {
	t.Helper()
	t.Setenv("WHIP_HOME", t.TempDir())
	f := &legacyHTTPFixture{
		getStarted: make(chan struct{}), getStopped: make(chan struct{}),
		deleteStarted: make(chan struct{}), deleteStopped: make(chan struct{}),
		notify: make(chan struct{}, 1), lists: make(chan struct{}, 8),
	}
	release := make(chan struct{})
	f.release = sync.OnceFunc(func() { close(release) })
	getStarted := sync.OnceFunc(func() { close(f.getStarted) })
	getStopped := sync.OnceFunc(func() { close(f.getStopped) })
	deleteStarted := sync.OnceFunc(func() { close(f.deleteStarted) })
	deleteStopped := sync.OnceFunc(func() { close(f.deleteStopped) })
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getStarted()
			defer getStopped()
			if !stallGET {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
			}
			for {
				select {
				case <-r.Context().Done():
					return
				case <-release:
					return
				case <-f.notify:
					_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n")
					w.(http.Flusher).Flush()
				}
			}
		case http.MethodDelete:
			deleteStarted()
			defer deleteStopped()
			if stallDELETE {
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPost:
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, "invalid fixture request", http.StatusBadRequest)
				return
			}
			if len(request.ID) == 0 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
			switch request.Method {
			case "initialize":
				w.Header().Set("Mcp-Session-Id", "local-fixture")
				response["result"] = map[string]any{
					"protocolVersion": "2025-11-25",
					"serverInfo":      map[string]string{"name": "local-fixture", "version": "1"},
					"capabilities":    map[string]any{"tools": map[string]bool{"listChanged": true}},
				}
			case "tools/list":
				name := "before"
				if f.updated.Load() {
					name = "after"
				}
				response["result"] = map[string]any{"tools": []any{
					map[string]any{"name": name, "inputSchema": map[string]string{"type": "object"}},
				}}
				select {
				case f.lists <- struct{}{}:
				default:
				}
			case "tools/call":
				response["result"] = map[string]any{"content": []any{map[string]string{"type": "text", "text": "alive"}}}
			default:
				response["error"] = map[string]any{"code": -32601, "message": "Method not found"}
			}
			_ = json.NewEncoder(w).Encode(response)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(func() { f.release(); f.server.Close() })
	return f
}

func (f *legacyHTTPFixture) manager(t *testing.T, timeout int) *Manager {
	t.Helper()
	m := NewManager(map[string]ServerConfig{
		"local": {URL: f.server.URL, StartupTimeout: timeout, ToolTimeout: 2},
	})
	// A failed assertion must release the intentional stalls before joining.
	t.Cleanup(func() { f.release(); m.Close() })
	return m
}

func awaitHTTPFixture(t *testing.T, done <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal(message)
	}
}

func closeHTTPManager(m *Manager) <-chan struct{} {
	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	return done
}

func TestManagerCloseCancelsStalledStandaloneSSE(t *testing.T) {
	f := newLegacyHTTPFixture(t, true, true)
	m := f.manager(t, 120)
	m.Start(t.Context())
	awaitHTTPFixture(t, f.getStarted, "SDK never reached standalone SSE GET")
	awaitHTTPFixture(t, closeHTTPManager(m), "Manager.Close remained blocked in standalone SSE startup")
	awaitHTTPFixture(t, f.getStopped, "manager shutdown did not cancel the GET request")
}

func TestManagerStartupDeadlineCancelsStalledStandaloneSSE(t *testing.T) {
	f := newLegacyHTTPFixture(t, true, true)
	m := f.manager(t, 1)
	m.Start(t.Context())
	awaitHTTPFixture(t, f.getStarted, "SDK never reached standalone SSE GET")
	awaitHTTPFixture(t, m.servers["local"].ready, "startup deadline did not settle the stalled SSE connection")
	status := m.Statuses()[0]
	if status.Status != StatusFailed || status.Err != "timed out after 1s" {
		t.Fatalf("startup status = %+v", status)
	}
	awaitHTTPFixture(t, f.getStopped, "startup deadline did not cancel the GET request")
	awaitHTTPFixture(t, closeHTTPManager(m), "manager shutdown did not join the failed connection")
}

func TestManagerSuccessfulSSESurvivesStartupAndRefreshesCatalog(t *testing.T) {
	f := newLegacyHTTPFixture(t, false, false)
	m := f.manager(t, 1)
	m.Start(t.Context())
	awaitHTTPFixture(t, m.servers["local"].ready, "remote server never became ready")
	if status := m.Statuses()[0]; status.Status != StatusReady {
		t.Fatalf("connect = %+v", status)
	}
	awaitHTTPFixture(t, f.lists, "initial tools/list was not observed")
	select {
	case <-f.getStopped:
		t.Fatal("successful SSE was cancelled by startup completion")
	case <-time.After(1100 * time.Millisecond):
	}
	result, err := m.Call(t.Context(), "local", "before", nil)
	if err != nil || result != "alive" {
		t.Fatalf("call after startup deadline = %q, %v", result, err)
	}
	f.updated.Store(true)
	f.notify <- struct{}{}
	awaitHTTPFixture(t, f.lists, "persistent SSE did not deliver tools/list_changed")
	// The response reaching the server precedes its catalog being published.
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		listed, err := m.ListTools("local")
		if err == nil && len(listed) == 1 && listed[0].Name == "after" {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("updated tool catalog was not published")
		case <-ticker.C:
		}
	}
	result, err = m.Call(t.Context(), "local", "after", nil)
	if err != nil || result != "alive" {
		t.Fatalf("call after SSE catalog refresh = %q, %v", result, err)
	}
	awaitHTTPFixture(t, closeHTTPManager(m), "healthy SSE prevented shutdown")
	awaitHTTPFixture(t, f.getStopped, "healthy SSE survived manager shutdown")
	awaitHTTPFixture(t, f.deleteStarted, "manager shutdown omitted remote session DELETE")
	awaitHTTPFixture(t, f.deleteStopped, "remote session DELETE did not finish")
}

func TestManagerCloseCancelsStalledSessionDelete(t *testing.T) {
	f := newLegacyHTTPFixture(t, false, true)
	m := f.manager(t, 120)
	m.Start(t.Context())
	awaitHTTPFixture(t, m.servers["local"].ready, "remote server never became ready")
	s := m.servers["local"]
	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess == nil {
		t.Fatal("remote session was not initialized")
	}
	// SDK-initiated closure can already be blocked in DELETE before the owner
	// begins shutdown. Its cleanup deadline must release that same Close call.
	sessionClosed := make(chan struct{})
	go func() { _ = sess.Close(); close(sessionClosed) }()
	awaitHTTPFixture(t, f.deleteStarted, "SDK never reached session DELETE")
	awaitHTTPFixture(t, closeHTTPManager(m), "Manager.Close remained blocked in session DELETE")
	awaitHTTPFixture(t, sessionClosed, "SDK session Close did not finish")
	awaitHTTPFixture(t, f.deleteStopped, "cleanup deadline did not cancel the DELETE request")
}

func TestConnectionHTTPTransportPreservesRequestDeadline(t *testing.T) {
	stopped := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stalled" {
			defer close(stopped)
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(func() { close(release); server.Close() })
	lifetime, stop := context.WithCancel(t.Context())
	defer stop()
	client := &http.Client{Transport: &connectionHTTPTransport{lifetime: lifetime, base: http.DefaultTransport}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/stalled", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		response, err := client.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("stalled request = %v, want deadline exceeded", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connection lifetime replaced the request deadline")
	}
	awaitHTTPFixture(t, stopped, "request deadline did not cancel the HTTP request")
	if err := lifetime.Err(); err != nil {
		t.Fatalf("one request cancelled the connection: %v", err)
	}
	response, err := client.Get(server.URL + "/healthy")
	if err != nil {
		t.Fatalf("connection did not survive a request deadline: %v", err)
	}
	_ = response.Body.Close()
}

func TestConnectionHTTPTransportDeleteRedirectsShareCleanupDeadline(t *testing.T) {
	release := make(chan struct{})
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := requests.Add(1)
		select {
		case <-r.Context().Done():
			return
		case <-release:
			return
		case <-time.After(400 * time.Millisecond):
		}
		if request < 3 {
			http.Redirect(w, r, "/cleanup", http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(func() { close(release); server.Close() })
	lifetime, stop := context.WithCancel(t.Context())
	stop() // Owner retirement still permits bounded best-effort cleanup.
	client := &http.Client{Transport: &connectionHTTPTransport{lifetime: lifetime, base: http.DefaultTransport}}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		response, err := client.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("redirected DELETE = %v, want shared cleanup deadline exceeded", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("redirected DELETE exceeded the cleanup deadline")
	}
	if requests.Load() < 2 {
		t.Fatal("DELETE never followed the fixture redirect")
	}
}
