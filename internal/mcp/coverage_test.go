package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/context-labs/whip/internal/config"
)

func TestEnableDisableCycle(t *testing.T) {
	m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs")})
	m.Start(context.Background())
	waitReady(t, m)

	if !m.Disable("docs") {
		t.Fatal("disable returned false")
	}
	if st := m.Statuses()[0]; st.Status != StatusDisabled {
		t.Fatalf("after disable: %+v", st)
	}
	if len(m.Tools()) != 0 {
		t.Error("disabled server contributes no tools")
	}
	if _, err := m.servers["docs"].call(context.Background(), "greet", nil); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("call to disabled server: %v", err)
	}

	if !m.Enable("docs") {
		t.Fatal("enable returned false")
	}
	deadline := probeDeadline()
	for !deadline.Done() {
		if st := m.Statuses()[0]; st.Status == StatusReady {
			break
		}
		deadline.Sleep()
	}
	if st := m.Statuses()[0]; st.Status != StatusReady {
		t.Fatalf("after enable: %+v", st)
	}
	out := ""
	for _, tool := range m.Tools() {
		if tool.Def.Function.Name == "mcp__docs__greet" {
			out, _ = tool.Run(context.Background(), nil)
		}
	}
	if out == "" {
		t.Error("re-enabled server's tools should work")
	}

	for _, bad := range []string{"nope"} {
		if m.Disable(bad) || m.Enable(bad) {
			t.Error("unknown name should return false")
		}
	}
	if _, ok := m.Config("docs"); !ok {
		t.Error("Config should return the live definition")
	}
	if _, ok := m.Config("nope"); ok {
		t.Error("Config unknown name should be !ok")
	}
}

// probeDeadline is a tiny poll helper: ~3s of 10ms naps.
type deadline struct{ n int }

func probeDeadline() *deadline { return &deadline{} }
func (d *deadline) Done() bool { d.n++; return d.n > 300 }
func (d *deadline) Sleep()     { time.Sleep(10 * time.Millisecond) }

func TestHeaderTransport(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rt := headerTransport{"Authorization": "Bearer abc"}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotAuth != "Bearer abc" {
		t.Errorf("header not injected: %q", gotAuth)
	}
}

func TestRingBuffer(t *testing.T) {
	r := newRingBuffer(8)
	r.Write([]byte("abc"))
	r.Write([]byte("defghij")) // wraps: keep last 8
	if got := r.String(); got != "cdefghij" {
		t.Errorf("wrap = %q", got)
	}
	r.Write([]byte("0123456789")) // oversize: keep tail
	if got := r.String(); got != "23456789" {
		t.Errorf("oversize = %q", got)
	}
	zero := newRingBuffer(0)
	zero.Write([]byte("anything"))
	if zero.String() != "" {
		t.Error("zero-capacity sink should discard")
	}
}

func TestFromConfigMap(t *testing.T) {
	t.Setenv("FROMCFG_KEY", "v1")
	in := map[string]config.MCPServer{
		"docs": {Command: []string{"npx", "-y"}, Env: map[string]string{"K": "$FROMCFG_KEY"}, StartupTimeout: 3},
		"web":  {URL: "https://x", Headers: map[string]string{"A": "b"}},
	}
	out := FromConfigMap(in)
	// env references stay references through config load — resolution happens
	// at spawn time (config.ResolveEnvMap), so a var set between launches
	// still resolves and resolved secrets never sit in the config file
	if got := out["docs"]; len(got.Command) != 2 || got.Env["K"] != "$FROMCFG_KEY" || got.StartupTimeout != 3 {
		t.Errorf("docs = %+v", got)
	}
	if !out["web"].Remote() || out["web"].Headers["A"] != "b" {
		t.Errorf("web = %+v", out["web"])
	}
	if FromConfigMap(nil) != nil {
		t.Error("nil in, nil out")
	}
}

func TestFlattenRemainingContentTypes(t *testing.T) {
	res := flattenResult(&sdkmcp.CallToolResult{Content: []sdkmcp.Content{
		&sdkmcp.AudioContent{MIMEType: "audio/wav", Data: []byte{1}},
		&sdkmcp.EmbeddedResource{Resource: &sdkmcp.ResourceContents{URI: "file:///x", Text: "contents"}},
		&sdkmcp.EmbeddedResource{Resource: &sdkmcp.ResourceContents{URI: "file:///bin", Blob: []byte{9, 9}}},
		&sdkmcp.ResourceLink{URI: "https://link", Name: "ref"},
	}})
	for _, want := range []string{"audio content omitted", "[resource file:///x]", "contents", "binary resource omitted", "resource link: https://link (ref)"} {
		if !strings.Contains(res, want) {
			t.Errorf("missing %q in %q", want, res)
		}
	}
}

func TestDefaultTransportResolvesHeaderSecrets(t *testing.T) {
	t.Setenv("WHIP_MCP_SECRET_TEST", "resolved-token")

	// References resolve at connect time; the config keeps the raw reference.
	cfg := ServerConfig{URL: "https://mcp.example.com", Headers: map[string]string{
		"Authorization": "${WHIP_MCP_SECRET_TEST}",
		"X-Cmd":         "!printf cmd-token",
		"X-Literal":     "plain",
		"X-Dropped":     "$WHIP_MCP_SECRET_UNSET",
	}}
	tr, err := defaultTransport(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Headers["Authorization"] != "${WHIP_MCP_SECRET_TEST}" {
		t.Fatalf("config mutated: %q", cfg.Headers["Authorization"])
	}
	st, ok := tr.(*sdkmcp.StreamableClientTransport)
	if !ok {
		t.Fatalf("transport type %T", tr)
	}
	ht, ok := st.HTTPClient.Transport.(headerTransport)
	if !ok {
		t.Fatalf("inner transport type %T", st.HTTPClient.Transport)
	}
	if ht["Authorization"] != "resolved-token" || ht["X-Cmd"] != "cmd-token" || ht["X-Literal"] != "plain" {
		t.Fatalf("resolved headers: %+v", ht)
	}
	if _, present := ht["X-Dropped"]; present {
		t.Fatalf("unresolvable reference must be dropped, got %q", ht["X-Dropped"])
	}
}
