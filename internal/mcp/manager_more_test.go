package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/tools"
)

func TestEnvPairs(t *testing.T) {
	got := envPairs(map[string]string{"A": "1", "B": "2"})
	slices.Sort(got)
	if !reflect.DeepEqual(got, []string{"A=1", "B=2"}) {
		t.Errorf("envPairs = %v", got)
	}
	if got := envPairs(nil); len(got) != 0 {
		t.Errorf("envPairs(nil) = %v", got)
	}
}

// TestDefaultTransportStdio: the stdio branch builds a command in its own
// process group, with whip's env plus the server's vars, and a cwd.
func TestDefaultTransportStdio(t *testing.T) {
	dir := t.TempDir()
	stderr := newRingBuffer(64)
	tr, err := defaultTransport(context.Background(), ServerConfig{
		Command: []string{"srv", "--stdio"},
		Cwd:     dir,
		Env:     map[string]string{"WHIP_MCP_TRANSPORT_TEST": "yes"},
	}, stderr)
	if err != nil {
		t.Fatal(err)
	}
	ct, ok := tr.(*sdkmcp.CommandTransport)
	if !ok {
		t.Fatalf("transport type %T, want *sdkmcp.CommandTransport", tr)
	}
	cmd := ct.Command
	if cmd.Dir != dir {
		t.Errorf("cwd = %q, want %q", cmd.Dir, dir)
	}
	if !slices.Contains(cmd.Env, "WHIP_MCP_TRANSPORT_TEST=yes") {
		t.Error("server env var not layered onto the inherited environment")
	}
	if len(cmd.Env) < 2 {
		t.Errorf("environment not inherited: %v", cmd.Env)
	}
	if cmd.Stderr != stderr {
		t.Error("stderr ring buffer not wired to the child")
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("child must get its own process group")
	}
	if !strings.HasSuffix(cmd.Path, "srv") || !reflect.DeepEqual(cmd.Args[1:], []string{"--stdio"}) {
		t.Errorf("command = %v (%q)", cmd.Args, cmd.Path)
	}
}

func TestManagedTransportUsesProcessScope(t *testing.T) {
	processes := capability.NewProcessManager()
	m := NewManager(nil)
	dir := t.TempDir()
	m.SetProcessOptions(processes, "root", dir, map[string]string{"SESSION": "one"})
	transport, err := m.defaultTransport(context.Background(), ServerConfig{
		Command: []string{"server", "--stdio"},
		Env:     map[string]string{"SERVER": "two"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	managed, ok := transport.(*managedTransport)
	if !ok {
		t.Fatalf("transport type %T", transport)
	}
	if managed.rootID != "root" || managed.cwd != dir || managed.env["SESSION"] != "one" || managed.env["SERVER"] != "two" {
		t.Fatalf("managed scope = %+v", managed)
	}
}

func TestFlattenResultEdges(t *testing.T) {
	multi := flattenResult(&sdkmcp.CallToolResult{Content: []sdkmcp.Content{
		&sdkmcp.TextContent{Text: "one"},
		&sdkmcp.TextContent{Text: "two"},
	}})
	if multi != "one\ntwo" {
		t.Errorf("two text parts = %q", multi)
	}
	if got := flattenResult(&sdkmcp.CallToolResult{}); got != "(no output)" {
		t.Errorf("empty result = %q", got)
	}
	if got := flattenResult(&sdkmcp.CallToolResult{IsError: true}); got != "Error: (no output)" {
		t.Errorf("empty error result = %q", got)
	}
	// A resource part with no contents contributes nothing rather than a
	// half-rendered placeholder.
	got := flattenResult(&sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.EmbeddedResource{}}})
	if got != "(no output)" {
		t.Errorf("empty embedded resource = %q", got)
	}
}

// TestNormalizeSchemaUnmarshalable: a schema JSON can't encode falls back to
// the empty object shape instead of emitting broken JSON to the provider.
func TestNormalizeSchemaUnmarshalable(t *testing.T) {
	got := normalizeSchema(map[string]any{"properties": map[string]any{}, "bad": make(chan int)})
	if got != `{"type":"object","properties":{}}` {
		t.Errorf("unmarshalable schema = %s", got)
	}
}

// TestBridgeTitleFallback: a tool with only a Title uses it as the
// description, so the model isn't handed "[MCP x] ".
func TestBridgeTitleFallback(t *testing.T) {
	s := &server{name: "docs"}
	tool := s.bridge(&sdkmcp.Tool{Name: "search", Title: "Search the docs"})
	if tool.Def.Function.Name != "mcp__docs__search" {
		t.Errorf("name = %q", tool.Def.Function.Name)
	}
	if tool.Def.Function.Description != "[MCP docs] Search the docs" {
		t.Errorf("description = %q", tool.Def.Function.Description)
	}
	// An explicit description wins over the title.
	tool = s.bridge(&sdkmcp.Tool{Name: "search", Title: "Search the docs", Description: "real description"})
	if tool.Def.Function.Description != "[MCP docs] real description" {
		t.Errorf("description = %q", tool.Def.Function.Description)
	}
}

func TestSetBlockedAndBlockedByPolicy(t *testing.T) {
	m := NewManager(nil)
	if m.BlockedByPolicy("anything") {
		t.Error("nothing is blocked yet")
	}
	m.SetBlocked(map[string]ServerConfig{
		"zeta":  {Note: "blocked by mcpImport", Source: "~/.codex/config.toml"},
		"alpha": {Note: "blocked by mcpImport", Source: ".mcp.json"},
	})
	b := m.Blocked()
	if len(b) != 2 || b[0].Name != "alpha" || b[1].Name != "zeta" {
		t.Fatalf("blocked = %+v, want name-sorted alpha,zeta", b)
	}
	if b[0].Status != StatusDisabled || b[0].Note != "blocked by mcpImport" || b[0].Source != ".mcp.json" {
		t.Errorf("blocked[0] = %+v", b[0])
	}
	if !m.BlockedByPolicy("zeta") || !m.BlockedByPolicy("alpha") {
		t.Error("blocked servers must report true")
	}
	if m.BlockedByPolicy("never-configured") {
		t.Error("unknown name must report false")
	}
}

// TestInstructionsBlockSortedAndEmpty: no instructions → no block; several →
// name-sorted sections.
func TestInstructionsBlockSortedAndEmpty(t *testing.T) {
	if got := NewManager(nil).InstructionsBlock(); got != "" {
		t.Errorf("no servers → %q, want empty", got)
	}

	mk := func(name, instr string) *sdkmcp.Server {
		srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: name}, &sdkmcp.ServerOptions{Instructions: instr})
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}},
			func(ctx context.Context, req *sdkmcp.CallToolRequest, in struct{}) (*sdkmcp.CallToolResult, any, error) {
				return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "pong"}}}, nil, nil
			})
		return srv
	}
	srvs := map[string]*sdkmcp.Server{"zeta": mk("zeta", "zeta rules"), "alpha": mk("alpha", "alpha rules")}

	m := NewManager(map[string]ServerConfig{"zeta": testCfg("zeta"), "alpha": testCfg("alpha")})
	m.connectTransport = func(_ context.Context, cfg ServerConfig, _ *ringBuffer) (sdkmcp.Transport, error) {
		ct, st := sdkmcp.NewInMemoryTransports()
		ss, err := srvs[cfg.Command[0]].Connect(context.Background(), st, nil)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { ss.Close() })
		return ct, nil
	}
	t.Cleanup(m.Close)
	m.Start(context.Background())
	waitReady(t, m)

	block := m.InstructionsBlock()
	ai, zi := strings.Index(block, "alpha rules"), strings.Index(block, "zeta rules")
	if ai < 0 || zi < 0 || ai > zi {
		t.Errorf("instructions must be name-sorted:\n%s", block)
	}
}

// TestCallUnavailableVariants: the sess==nil branches speak in the user's
// terms — construct the states directly, since a real server can't be held
// in "settled but connecting" on demand.
func TestCallUnavailableVariants(t *testing.T) {
	settled := func(st Status, errMsg string) *server {
		s := &server{name: "svc", settled: true, status: st, err: errMsg, ready: make(chan struct{}), calling: make(chan struct{}, 1)}
		close(s.ready)
		return s
	}
	for _, tc := range []struct {
		s    *server
		want string
	}{
		{settled(StatusFailed, ""), `mcp server "svc" unavailable (/mcp svc reconnect)`},
		{settled(StatusFailed, "spawn failed"), `mcp server "svc" unavailable: spawn failed (/mcp svc reconnect)`},
		{settled(StatusDisabled, ""), `mcp server "svc" is disabled (/mcp svc enable)`},
		{settled(StatusConnecting, ""), `mcp server "svc" is connecting`},
	} {
		_, err := tc.s.call(context.Background(), "anything", nil)
		if err == nil || err.Error() != tc.want {
			t.Errorf("call err = %v, want %q", err, tc.want)
		}
	}
}

// TestCallLiveErrorPaths: bad arguments, an unknown tool, a cancelled context
// while another call holds the per-server slot, and a tool that outruns the
// configured tool timeout.
func TestCallLiveErrorPaths(t *testing.T) {
	m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs")})
	m.Start(context.Background())
	waitReady(t, m)
	s := m.servers["docs"]

	if _, err := s.call(context.Background(), "greet", json.RawMessage("{not json")); err == nil ||
		!strings.Contains(err.Error(), "invalid tool arguments") {
		t.Errorf("bad args err = %v", err)
	}
	if _, err := s.call(context.Background(), "no-such-tool", nil); err == nil {
		t.Error("unknown tool should surface the server's error")
	}

	// The per-server slot is taken: a cancelled caller gives up rather than
	// queueing behind it.
	s.calling <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.call(ctx, "greet", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("contended call err = %v, want context.Canceled", err)
	}
	<-s.calling
}

// TestCallToolTimeout: a tool that never returns is cut off at the configured
// tool timeout with a message naming the tool.
func TestCallToolTimeout(t *testing.T) {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "slow"}, nil)
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "wedge", InputSchema: map[string]any{"type": "object"}},
		func(ctx context.Context, req *sdkmcp.CallToolRequest, in struct{}) (*sdkmcp.CallToolResult, any, error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		})

	m := NewManager(map[string]ServerConfig{"slow": {Command: []string{"slow"}, StartupTimeout: 5, ToolTimeout: 1}})
	m.connectTransport = func(_ context.Context, _ ServerConfig, _ *ringBuffer) (sdkmcp.Transport, error) {
		ct, st := sdkmcp.NewInMemoryTransports()
		ss, err := srv.Connect(context.Background(), st, nil)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { ss.Close() })
		return ct, nil
	}
	t.Cleanup(m.Close)
	m.Start(context.Background())
	waitReady(t, m)

	_, err := m.servers["slow"].call(context.Background(), "wedge", nil)
	if err == nil || !strings.Contains(err.Error(), "mcp tool wedge timed out after 1s") {
		t.Errorf("timed-out call err = %v", err)
	}
}

// TestCallWaitsForLateConnect: a call that arrives before the first connect
// settles parks on s.ready and runs once the server lands.
func TestCallWaitsForLateConnect(t *testing.T) {
	release := make(chan struct{})
	m := NewManager(map[string]ServerConfig{"late": testCfg("late")})
	m.connectTransport = func(_ context.Context, _ ServerConfig, _ *ringBuffer) (sdkmcp.Transport, error) {
		<-release
		return serveTestServer(t, "late"), nil
	}
	t.Cleanup(m.Close)
	m.Start(context.Background())

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		out, err := m.servers["late"].call(context.Background(), "greet", json.RawMessage(`{"name":"late"}`))
		done <- result{out, err}
	}()
	<-started
	time.Sleep(50 * time.Millisecond) // let the call park on s.ready (well inside connectGrace)
	close(release)

	select {
	case r := <-done:
		if r.err != nil || r.out != "hi late" {
			t.Errorf("late call = %q, %v", r.out, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("call never returned after the server connected")
	}
}

// TestConnectSurfacesStderrTail: whatever the child wrote to stderr before
// dying is appended to the failure message — the whole point of the ring
// buffer.
func TestConnectSurfacesStderrTail(t *testing.T) {
	m := NewManager(map[string]ServerConfig{"noisy": testCfg("noisy")})
	m.connectTransport = func(_ context.Context, _ ServerConfig, stderr *ringBuffer) (sdkmcp.Transport, error) {
		stderr.Write([]byte("  panic: missing API key\n  "))
		return nil, errors.New("spawn failed")
	}
	t.Cleanup(m.Close)
	m.Start(context.Background())
	waitReady(t, m)

	st := m.Statuses()[0]
	if st.Status != StatusFailed || st.Err != "spawn failed — stderr: panic: missing API key" {
		t.Errorf("status = %+v", st)
	}
}

// TestConnectListToolsFailureClosesSession: a server that connects but
// refuses tools/list is failed, not half-registered.
func TestConnectListToolsFailureClosesSession(t *testing.T) {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "grump"}, nil)
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}},
		func(ctx context.Context, req *sdkmcp.CallToolRequest, in struct{}) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{}, nil, nil
		})
	srv.AddReceivingMiddleware(func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if method == "tools/list" {
				return nil, errors.New("list refused")
			}
			return next(ctx, method, req)
		}
	})

	m := NewManager(map[string]ServerConfig{"grump": testCfg("grump")})
	m.connectTransport = func(_ context.Context, _ ServerConfig, _ *ringBuffer) (sdkmcp.Transport, error) {
		ct, st := sdkmcp.NewInMemoryTransports()
		ss, err := srv.Connect(context.Background(), st, nil)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { ss.Close() })
		return ct, nil
	}
	t.Cleanup(m.Close)
	m.Start(context.Background())
	waitReady(t, m)

	st := m.Statuses()[0]
	if st.Status != StatusFailed || !strings.Contains(st.Err, "list refused") {
		t.Fatalf("status = %+v", st)
	}
	if len(m.Tools()) != 0 {
		t.Error("a server that never listed tools must contribute none")
	}
}

// TestConnectDuringCloseDiscardsSession: a connect that lands after the
// manager is closing throws the session away instead of storing it.
func TestConnectDuringCloseDiscardsSession(t *testing.T) {
	m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs")})
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()

	s := m.servers["docs"]
	s.connect(context.Background(), m) // synchronous: no lifecycle goroutine started
	s.mu.Lock()
	sess, status := s.sess, s.status
	s.mu.Unlock()
	if sess != nil {
		t.Error("a closing manager must not store the session")
	}
	if status == StatusReady {
		t.Errorf("status = %v, want not ready", status)
	}
	if len(m.Tools()) != 0 {
		t.Error("discarded session must contribute no tools")
	}
}

// TestRunDropsRedundantReconnect: a reconnect request that lands while the
// server is already ready is dropped — the caller wanted a fresh connection
// and already has one, so the live session must survive untouched.
func TestRunDropsRedundantReconnect(t *testing.T) {
	var connects atomic.Int64
	m := NewManager(map[string]ServerConfig{"docs": testCfg("docs")})
	m.connectTransport = func(_ context.Context, _ ServerConfig, _ *ringBuffer) (sdkmcp.Transport, error) {
		connects.Add(1)
		return serveTestServer(t, "docs"), nil
	}
	t.Cleanup(m.Close)
	m.Start(context.Background())
	waitReady(t, m)

	s := m.servers["docs"]
	s.mu.Lock()
	gen := s.gen
	s.mu.Unlock()

	s.reconnect <- struct{}{} // queued directly: the session stays live
	waitDrained(t, s)
	time.Sleep(50 * time.Millisecond) // a redial, if any, would have happened by now

	if got := connects.Load(); got != 1 {
		t.Errorf("redundant reconnect redialed: %d connects, want 1", got)
	}
	s.mu.Lock()
	gen2, live := s.gen, s.sess != nil
	s.mu.Unlock()
	if gen2 != gen || !live {
		t.Errorf("live session disturbed: gen %d→%d, live=%v", gen, gen2, live)
	}
	out := tools.Execute(context.Background(), m.Tools(), "mcp__docs__greet", json.RawMessage(`{"name":"still here"}`))
	if out != "hi still here" {
		t.Errorf("greet after redundant reconnect = %q", out)
	}
}

// TestRunRefusesReconnectWhenDisabled: /mcp reconnect on a disabled server
// re-asserts disabled instead of resurrecting it behind the user's back.
func TestRunRefusesReconnectWhenDisabled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var connects atomic.Int64
		m := NewManager(map[string]ServerConfig{"dead": testCfg("dead")})
		m.connectTransport = func(_ context.Context, _ ServerConfig, _ *ringBuffer) (sdkmcp.Transport, error) {
			connects.Add(1)
			return nil, errors.New("spawn failed")
		}
		defer m.Close()
		m.Start(t.Context())
		synctest.Wait()

		if !m.Disable("dead") {
			t.Fatal("disable returned false")
		}
		if !m.Reconnect("dead") {
			t.Fatal("reconnect returned false")
		}
		synctest.Wait()
		if st := m.Statuses()[0]; st.Status != StatusDisabled {
			t.Errorf("status after reconnecting a disabled server = %+v", st)
		}
		if got := connects.Load(); got != 1 {
			t.Errorf("disabled server dialed %d times, want only the initial attempt", got)
		}
	})
}

// TestReconnectDropsLiveSession: /mcp reconnect on a healthy server tears the
// session down and the server comes back on its own.
func TestReconnectDropsLiveSession(t *testing.T) {
	t.Setenv("WHIP_TEST_MCP_BACKOFF_MS", "20")
	m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs")})
	m.Start(context.Background())
	waitReady(t, m)

	srv := m.servers["docs"]
	srv.mu.Lock()
	gen := srv.gen
	srv.mu.Unlock()

	if !m.Reconnect("docs") {
		t.Fatal("reconnect returned false")
	}
	deadline := probeDeadline()
	for !deadline.Done() {
		srv.mu.Lock()
		back := srv.status == StatusReady && srv.sess != nil && srv.gen > gen
		srv.mu.Unlock()
		if back {
			break
		}
		deadline.Sleep()
	}
	srv.mu.Lock()
	status, live, gen2 := srv.status, srv.sess != nil, srv.gen
	srv.mu.Unlock()
	if status != StatusReady || !live || gen2 <= gen {
		t.Fatalf("after reconnect: status=%v live=%v gen %d→%d", status, live, gen, gen2)
	}
	out := tools.Execute(context.Background(), m.Tools(), "mcp__docs__greet", json.RawMessage(`{"name":"again"}`))
	if out != "hi again" {
		t.Errorf("greet after reconnect = %q", out)
	}
}

func TestProcessScopeChangeReconnects(t *testing.T) {
	m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs")})
	processes := capability.NewProcessManager()
	defer processes.Close()
	m.SetProcessOptions(processes, "one", t.TempDir(), nil)
	m.Start(context.Background())
	waitReady(t, m)
	srv := m.servers["docs"]
	srv.mu.Lock()
	gen := srv.gen
	srv.mu.Unlock()
	m.SetProcessOptions(processes, "two", t.TempDir(), nil)
	deadline := probeDeadline()
	for !deadline.Done() {
		srv.mu.Lock()
		ready := srv.status == StatusReady && srv.sess != nil && srv.gen > gen
		srv.mu.Unlock()
		if ready {
			return
		}
		deadline.Sleep()
	}
	t.Fatalf("server did not reconnect after process scope changed: %+v", m.Statuses())
}

func TestProcessScopeChangeInvalidatesInflightConnect(t *testing.T) {
	var connects atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})
	m := NewManager(map[string]ServerConfig{"docs": testCfg("docs")})
	m.connectTransport = func(_ context.Context, _ ServerConfig, _ *ringBuffer) (sdkmcp.Transport, error) {
		if connects.Add(1) == 1 {
			close(started)
			<-release
		}
		return serveTestServer(t, "docs"), nil
	}
	processes := capability.NewProcessManager()
	t.Cleanup(func() {
		m.Close()
		_ = processes.Close()
	})
	m.SetProcessOptions(processes, "one", t.TempDir(), nil)
	m.Start(context.Background())
	<-started
	m.SetProcessOptions(processes, "two", t.TempDir(), nil)
	close(release)
	waitReady(t, m)
	if got := connects.Load(); got != 2 {
		t.Fatalf("process scope change made %d connections, want 2", got)
	}
}

// waitDrained blocks until the lifecycle goroutine has taken the queued
// reconnect request off the channel.
func waitDrained(t *testing.T, s *server) {
	t.Helper()
	deadline := probeDeadline()
	for len(s.reconnect) > 0 {
		if deadline.Done() {
			t.Fatal("reconnect request was never consumed")
		}
		deadline.Sleep()
	}
}

// TestKickAutoReconnectDeclines: the three cases where a dropped session must
// NOT schedule a background retry.
func TestKickAutoReconnectDeclines(t *testing.T) {
	newSrv := func(cfg ServerConfig) *server {
		return &server{name: "s", cfg: cfg, ready: make(chan struct{}), calling: make(chan struct{}, 1), reconnect: make(chan struct{}, 1)}
	}
	// Disabled: the user turned it off.
	m := NewManager(nil)
	s := newSrv(ServerConfig{Command: []string{"x"}, Enabled: new(false)})
	s.kickAutoReconnect(m)

	// Closing manager: whip is shutting down.
	closing := NewManager(nil)
	closing.mu.Lock()
	closing.closed = true
	closing.mu.Unlock()
	s2 := newSrv(ServerConfig{Command: []string{"x"}})
	s2.kickAutoReconnect(closing)

	// Retry budget spent: stop flapping.
	s3 := newSrv(ServerConfig{Command: []string{"x"}})
	s3.autoTries = autoReconnectMax
	s3.kickAutoReconnect(m)

	// Each declines synchronously — nothing is ever queued.
	time.Sleep(50 * time.Millisecond)
	for i, got := range []*server{s, s2, s3} {
		if len(got.reconnect) != 0 {
			t.Errorf("case %d scheduled a reconnect", i)
		}
	}
}

// TestProbeRemote drives Probe through the real defaultTransport against a
// loopback MCP server: the tool-name list is capped at five plus an ellipsis.
func TestProbeRemote(t *testing.T) {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "many"}, nil)
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}},
			func(ctx context.Context, req *sdkmcp.CallToolRequest, in struct{}) (*sdkmcp.CallToolResult, any, error) {
				return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}, nil, nil
			})
	}
	hs := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil))
	defer hs.Close()

	res := Probe(context.Background(), "many", ServerConfig{URL: hs.URL, StartupTimeout: 10})
	if res.Status != StatusReady {
		t.Fatalf("probe = %+v", res)
	}
	if res.Tools != 7 {
		t.Errorf("tools = %d, want 7", res.Tools)
	}
	if len(res.ToolNames) != 6 || res.ToolNames[5] != "…" {
		t.Fatalf("tool names = %v, want 5 names + …", res.ToolNames)
	}
	if res.ToolNames[0] != "mcp__many__a" {
		t.Errorf("tool names = %v", res.ToolNames)
	}
	if res.Elapsed <= 0 {
		t.Error("probe should report elapsed time")
	}
}

// TestProbeContextCancelled: a probe whose caller gives up first reports the
// context error rather than waiting out the startup timeout.
func TestProbeContextCancelled(t *testing.T) {
	stop := make(chan struct{})
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select { // never answers the initialize request
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	// LIFO: release the wedged handlers before Close waits on them.
	defer hs.Close()
	defer close(stop)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	res := Probe(ctx, "wedged", ServerConfig{URL: hs.URL, StartupTimeout: 120})
	if res.Status != StatusFailed || res.Err == "" {
		t.Fatalf("probe = %+v", res)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("probe waited %s, should have given up with the caller's context", elapsed)
	}
}
