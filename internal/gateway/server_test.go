package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/gobwas/ws"
)

func fixture(t *testing.T) (*runtime.Runtime, *client.Client, Options) {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "whip-gateway-") //nolint:usetesting // Private short socket path on macOS; removed below.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	r, err := runtime.Open(t.Context(), directory, model.Scripted{Delay: 80 * time.Millisecond}, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	rpcServer, err := rpc.Listen(r, rpc.HostServices{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- rpcServer.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := client.Connect(t.Context(), r.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return r, c, Options{Address: "127.0.0.1:0", SocketPath: r.SocketPath(), RuntimeID: protocol.ID(r.Identity()), ProcessEpoch: protocol.ID(r.ProcessEpoch()), BackendDone: r.Done()}
}

func gatewayFixture(t *testing.T) (*Server, *runtime.Runtime, *client.Client) {
	t.Helper()
	r, c, o := fixture(t)
	o.AllowedOrigins = []string{"whip-app://bundle", "https://trusted.example"}
	s, err := Start(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, r, c
}

func browser(t *testing.T, s *Server) *WebSocket {
	t.Helper()
	conn, reader, _, err := ws.Dial(t.Context(), "ws"+strings.TrimPrefix(s.Endpoint(), "http")+"/api/v4/ws")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if reader == nil {
		return NewWebSocketClient(conn, conn)
	}
	return NewWebSocketClient(conn, reader)
}

func request(t *testing.T, stream Transport, method string, params any) protocol.Response {
	t.Helper()
	p, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(protocol.Request{JSONRPC: "2.0", ID: "request", Method: method, Params: p})
	if err := stream.WriteMessage(raw); err != nil {
		t.Fatal(err)
	}
	data, err := stream.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var response protocol.Response
	if protocol.Validate("Response", data) != nil || json.Unmarshal(data, &response) != nil {
		t.Fatalf("invalid response %s", data)
	}
	return response
}

func initialize(t *testing.T, stream Transport) protocol.InitializeResult {
	t.Helper()
	response := request(t, stream, "initialize", protocol.InitializeParams{Major: 4})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	var result protocol.InitializeResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func create(t *testing.T, c *client.Client, engine, id string) protocol.CreateTreeResult {
	t.Helper()
	var result protocol.CreateTreeResult
	if err := c.Call(t.Context(), "trees.create", protocol.CreateTreeParams{CreationID: protocol.ID(id), Engine: engine, Definition: c.Builtins()[0], WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{AutomaticTitle: new(false), ReportMode: new("message"), Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}}}, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHostOriginPolicyAndDiscovery(t *testing.T) {
	s, _, _ := gatewayFixture(t)
	for _, tc := range []struct {
		name, host string
		origins    []string
		status     int
	}{
		{"no origin", "", nil, 200},
		{"same origin", "", []string{s.Endpoint()}, 200},
		{"allowed", "", []string{"https://trusted.example"}, 200},
		{"app", "", []string{"whip-app://bundle"}, 200},
		{"null", "", []string{"null"}, 403},
		{"foreign", "", []string{"https://evil.example"}, 403},
		{"comma", "", []string{s.Endpoint() + ", https://trusted.example"}, 403},
		{"duplicate", "", []string{s.Endpoint(), s.Endpoint()}, 403},
		{"wrong host", "evil.example", nil, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, s.Endpoint()+"/api/v4/web", nil)
			req.Host = tc.host
			for _, origin := range tc.origins {
				req.Header.Add("Origin", origin)
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status {
				t.Fatalf("status=%d", res.StatusCode)
			}
			if tc.status == 200 {
				var body map[string]any
				if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body["available"] != false || body["runtime_id"] != string(s.options.RuntimeID) || body["process_epoch"] != string(s.options.ProcessEpoch) || body["max_content_bytes"] != float64(maxContentBytes) {
					t.Fatalf("discovery=%v", body)
				}
			}
		})
	}
	for _, origin := range []string{"*", "null", "http://user@host", "http://host/path", "https://host/", "whip-app://other"} {
		if validateOrigins([]string{origin}) == nil {
			t.Fatalf("accepted origin %q", origin)
		}
	}
	for _, host := range []string{"", "host/path", "user@host", "host?x", "host#fragment"} {
		if validateHosts([]string{host}) == nil {
			t.Fatalf("accepted host %q", host)
		}
	}
}

func TestPinnedStartupAndBrowserNetworkClassification(t *testing.T) {
	s, r, _ := gatewayFixture(t)
	for _, mutation := range []func(*Options){func(o *Options) { o.RuntimeID = "wrong" }, func(o *Options) { o.ProcessEpoch = "wrong" }, func(o *Options) { done := make(chan struct{}); close(done); o.BackendDone = done }} {
		o := s.options
		mutation(&o)
		other, err := Start(t.Context(), o)
		if err == nil {
			_ = other.Close()
			t.Fatal("accepted incorrect or ended runtime")
		}
	}
	stream := browser(t, s)
	initial := initialize(t, stream)
	if !initial.NetworkClient || initial.RuntimeID != protocol.ID(r.Identity()) || initial.ProcessEpoch != protocol.ID(r.ProcessEpoch()) {
		t.Fatalf("initial=%+v", initial)
	}
	response := request(t, stream, "terminal.open", map[string]any{})
	if response.Error == nil || response.Error.Kind != "NETWORK_RESTRICTED" {
		t.Fatalf("terminal=%+v", response)
	}
	response = request(t, stream, "initialize", protocol.InitializeParams{Major: 4, NetworkClient: false})
	if response.Error == nil {
		t.Fatal("network downgrade accepted")
	}
	stream = browser(t, s)
	response = request(t, stream, "initialize", protocol.InitializeParams{Major: 4, ExpectedProcessEpoch: new(protocol.ID("other"))})
	if response.Error == nil || response.Error.Kind != "IDENTITY" {
		t.Fatalf("epoch mismatch=%+v", response)
	}
}

func TestAmbiguousFramesCannotReachRuntime(t *testing.T) {
	s, _, _ := gatewayFixture(t)
	for _, raw := range []string{
		`{"jsonrpc":"2.0","id":"x","method":"initialize","params":{"major":4,"network_client":true,"network_client":false}}`,
		`{"jsonrpc":"2.0","id":"x","method":"initialize","params":{"major":4,"Network_Client":false}}`,
		`{"jsonrpc":"2.0","id":"x","Method":"initialize","params":{"major":4}}`,
		`{"jsonrpc":"2.0","id":"x","method":"initialize","params":{"major":4}}` + "\n" + `{"jsonrpc":"2.0","id":"evil","method":"trees.create","params":{}}`,
	} {
		stream := browser(t, s)
		if err := stream.WriteMessage([]byte(raw)); err != nil {
			t.Fatal(err)
		}
		reply, err := stream.ReadMessage()
		if err == nil {
			var response protocol.Response
			if json.Unmarshal(reply, &response) != nil || response.Error == nil {
				t.Fatalf("ambiguous frame accepted: %s", reply)
			}
		}
		_ = stream.Close()
	}
	stream := browser(t, s)
	if err := stream.WriteMessage([]byte("{\n\"jsonrpc\":\"2.0\",\n\"id\":\"x\",\n\"method\":\"initialize\",\n\"params\":{\"major\":4}\n}")); err != nil {
		t.Fatal(err)
	}
	if data, err := stream.ReadMessage(); err != nil || bytes.Contains(data, []byte(`"error"`)) {
		t.Fatalf("pretty initialize=%s %v", data, err)
	}
	response := request(t, stream, "trees.catalog", map[string]any{})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
}

func TestAcceptedWorkOutlivesBrowserAndGateway(t *testing.T) {
	s, _, c := gatewayFixture(t)
	for _, engine := range []string{"starlark", "quickjs"} {
		tree := create(t, c, engine, "create_"+engine)
		stream := browser(t, s)
		initialize(t, stream)
		identity := protocol.RequestIdentity{ClientID: "browser", RequestID: protocol.ID(engine)}
		response := request(t, stream, "sessions.submit", protocol.SubmitParams{SessionID: tree.Root.ID, Identity: identity, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "continue after browser disconnect"}}})
		if response.Error != nil {
			t.Fatal(response.Error)
		}
		_ = stream.Close()
		if engine == "quickjs" {
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		result, err := c.Wait(ctx, identity)
		cancel()
		if err != nil || result.Turn == nil || result.Turn.State != "succeeded" {
			t.Fatalf("accepted work: %+v %v", result, err)
		}
	}
}

func TestShutdownClosesIdleAndHandshakingConnections(t *testing.T) {
	s, r, _ := gatewayFixture(t)
	idle := browser(t, s)
	initialize(t, idle)
	handshake := browser(t, s)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("gateway did not join active connections")
	}
	if s.Err() == nil {
		t.Fatal("runtime shutdown not reported")
	}
	for _, stream := range []*WebSocket{idle, handshake} {
		if _, err := stream.ReadMessage(); err == nil {
			t.Fatal("connection survived shutdown")
		}
	}
	if conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp", strings.TrimPrefix(s.Endpoint(), "http://")); err == nil {
		_ = conn.Close()
		t.Fatal("gateway listener survived runtime")
	}
}

func TestScopedContentUsesSameOwnerAndReference(t *testing.T) {
	s, _, c := gatewayFixture(t)
	one := create(t, c, "starlark", "one")
	two := create(t, c, "starlark", "two")
	urlFor := func(owner protocol.ID) string {
		return s.Endpoint() + "/api/v4/content/shared?runtime_id=" + string(s.options.RuntimeID) + "&session_id=" + string(owner)
	}
	for _, tc := range []struct {
		owner protocol.ID
		body  []byte
	}{{one.Root.ID, bytes.Repeat([]byte("a"), maxContentBytes)}, {two.Root.ID, []byte("other owner")}} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, urlFor(tc.owner), bytes.NewReader(tc.body))
		req.Header.Set("Content-Type", "text/html")
		req.Header.Set("X-Content-Sha256", digest(tc.body))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("upload=%d %s", res.StatusCode, body)
		}
		req, _ = http.NewRequestWithContext(t.Context(), http.MethodGet, urlFor(tc.owner), nil)
		res, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err = io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK || !bytes.Equal(body, tc.body) || res.Header.Get("Content-Disposition") != "attachment" || res.Header.Get("Content-Type") != "application/octet-stream" || res.Header.Get("X-Content-Sha256") != digest(tc.body) {
			t.Fatalf("download=%d len=%d %v", res.StatusCode, len(body), err)
		}
	}
	for _, tc := range []struct {
		url, hash string
		size      int
		status    int
	}{{urlFor(one.Root.ID), digest([]byte("wrong")), 4, 400}, {urlFor(one.Root.ID), "invalid", 4, 400}, {urlFor(one.Root.ID), digest(nil), maxContentBytes + 1, 413}, {urlFor("missing"), digest(nil), 0, 502}, {urlFor(one.Root.ID) + "&session_id=other", digest(nil), 0, 400}, {strings.ReplaceAll(urlFor(one.Root.ID), string(s.options.RuntimeID), "wrong"), digest(nil), 0, 409}} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, tc.url, bytes.NewReader(make([]byte, tc.size)))
		req.Header.Set("X-Content-Sha256", tc.hash)
		req.Header.Set("Content-Type", "text/plain")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != tc.status {
			t.Fatalf("bad upload status=%d want=%d", res.StatusCode, tc.status)
		}
	}
}
