package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func checkedManager(t *testing.T, cfg ServerConfig, srv *sdkmcp.Server) *Manager {
	t.Helper()
	m := NewManager(map[string]ServerConfig{"exact.server": cfg})
	m.connectTransport = func(context.Context, ServerConfig, *ringBuffer) (sdkmcp.Transport, error) {
		client, transport := sdkmcp.NewInMemoryTransports()
		session, err := srv.Connect(context.Background(), transport, nil)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { m.Close(); _ = session.Close() })
		return client, nil
	}
	t.Cleanup(m.Close)
	m.Start(t.Context())
	waitReady(t, m)
	return m
}

func checkedServer(count *atomic.Int32) *sdkmcp.Server {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "checked"}, &sdkmcp.ServerOptions{Instructions: strings.Repeat("guidance ", 20000)})
	srv.AddReceivingMiddleware(func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, request sdkmcp.Request) (sdkmcp.Result, error) {
			if method == "tools/call" {
				count.Add(1)
			}
			return next(ctx, method, request)
		}
	})
	for _, name := range []string{"write.item", "write item"} {
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: name, InputSchema: map[string]any{
			"type": "object", "required": []string{"count"}, "additionalProperties": false,
			"properties": map[string]any{"count": map[string]any{"type": "integer", "minimum": 1, "maximum": 5}},
		}}, func(_ context.Context, request *sdkmcp.CallToolRequest, _ map[string]any) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: request.Params.Name}}}, nil, nil
		})
	}
	return srv
}

func checkedCall(t *testing.T, m *Manager, tool string) capability.MCPCall {
	t.Helper()
	call, err := m.ResolveTool("exact.server", tool)
	if err != nil {
		t.Fatal(err)
	}
	call.Arguments = json.RawMessage(`{"count":2}`)
	return call
}

func TestCheckedManagerRejectsSchemaAndUnknownToolsBeforeTransmission(t *testing.T) {
	var effects atomic.Int32
	m := checkedManager(t, testCfg("checked"), checkedServer(&effects))
	for _, arguments := range []string{`{`, `null`, `[]`, `{} {}`, `{}`, `{"count":"2"}`, `{"count":1.5}`, `{"count":6}`, `{"count":2,"extra":true}`} {
		t.Run(arguments, func(t *testing.T) {
			call := checkedCall(t, m, "write.item")
			call.Arguments = json.RawMessage(arguments)
			before := false
			_, err := m.CallChecked(t.Context(), call, func(context.Context) error { before = true; return nil })
			if err == nil || before || effects.Load() != 0 {
				t.Fatalf("invalid arguments reached admission/server: before=%v effects=%d error=%v", before, effects.Load(), err)
			}
		})
	}
	if _, err := m.ResolveTool("exact.server", "write_item"); err == nil {
		t.Fatal("sanitized alias resolved as an authority key")
	}
	if _, err := m.ResolveTool("exact_server", "write.item"); err == nil {
		t.Fatal("sanitized server resolved as an authority key")
	}
	first, second := checkedCall(t, m, "write.item"), checkedCall(t, m, "write item")
	if first.MCPSelector == second.MCPSelector || first.Definition == second.Definition {
		t.Fatal("raw-name collision merged authority")
	}
	for _, call := range []capability.MCPCall{first, second} {
		out, err := m.CallChecked(t.Context(), call, nil)
		if err != nil || out != call.Tool {
			t.Fatalf("exact dispatch = %q, %v", out, err)
		}
	}
	if effects.Load() != 2 {
		t.Fatalf("transmissions=%d", effects.Load())
	}
}

func TestCheckedManagerRechecksAuthorityAfterQueue(t *testing.T) {
	var effects atomic.Int32
	m := checkedManager(t, testCfg("checked"), checkedServer(&effects))
	call := checkedCall(t, m, "write.item")
	s := m.servers[call.Server]
	s.calling <- struct{}{}
	defer func() {
		select {
		case <-s.calling:
		default:
		}
	}()
	revoked := errors.New("authority revoked while queued")
	before := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := m.CallChecked(t.Context(), call, func(context.Context) error { before <- struct{}{}; return revoked })
		done <- err
	}()
	select {
	case <-before:
		t.Fatal("authority check ran before the serialized slot")
	case <-time.After(20 * time.Millisecond):
	}
	<-s.calling
	select {
	case err := <-done:
		if !errors.Is(err, revoked) || effects.Load() != 0 {
			t.Fatalf("queued revoke = %v, transmissions=%d", err, effects.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued call did not settle")
	}
}

func TestCheckedManagerCloseCancelsActiveAndQueuedCallsWithoutReplay(t *testing.T) {
	var effects atomic.Int32
	entered := make(chan struct{}, 1)
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "blocked"}, nil)
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "block", InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ map[string]any) (*sdkmcp.CallToolResult, any, error) {
		effects.Add(1)
		entered <- struct{}{}
		<-ctx.Done()
		return nil, nil, ctx.Err()
	})
	m := checkedManager(t, testCfg("blocked"), srv)
	call := checkedCall(t, m, "block")
	call.Arguments = json.RawMessage(`{}`)
	done := make(chan error, 2)
	invoke := func() { _, err := m.CallChecked(context.Background(), call, nil); done <- err }
	go invoke()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("tool did not enter")
	}
	go invoke()
	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	for range 2 {
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("retired call succeeded")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("manager close did not cancel active/queued call")
		}
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("manager close did not complete")
	}
	if effects.Load() != 1 {
		t.Fatalf("queued effect transmitted or active effect replayed: %d", effects.Load())
	}
}

func TestCheckedManagerCatalogChangeInvalidatesQueuedDefinition(t *testing.T) {
	var effects atomic.Int32
	srv := checkedServer(&effects)
	m := checkedManager(t, testCfg("checked"), srv)
	call := checkedCall(t, m, "write.item")
	admissionCtx, err := m.CallContext(call)
	if err != nil {
		t.Fatal(err)
	}
	s := m.servers[call.Server]
	s.calling <- struct{}{}
	defer func() {
		select {
		case <-s.calling:
		default:
		}
	}()
	done := make(chan error, 1)
	go func() { _, err := m.CallChecked(t.Context(), call, nil); done <- err }()
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: call.Tool, InputSchema: map[string]any{"type": "object", "required": []string{"new_field"}}}, func(context.Context, *sdkmcp.CallToolRequest, map[string]any) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{}, nil, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	var replacement capability.MCPCall
	for time.Now().Before(deadline) {
		var err error
		replacement, err = m.ResolveTool(call.Server, call.Tool)
		if err == nil && replacement.Definition != call.Definition {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if replacement.Definition == "" || replacement.Definition == call.Definition || replacement.Generation == call.Generation {
		t.Fatal("live catalog notification did not replace identity")
	}
	if !errors.Is(admissionCtx.Err(), context.Canceled) {
		t.Fatal("catalog change did not retire pending admission")
	}
	if _, err := m.CallContext(call); err == nil {
		t.Fatal("obsolete descriptor acquired a fresh admission lifetime")
	}
	if freshCtx, err := m.CallContext(replacement); err != nil || freshCtx.Err() != nil {
		t.Fatalf("replacement lifetime unavailable: %v", err)
	}
	<-s.calling
	select {
	case err := <-done:
		if err == nil || effects.Load() != 0 {
			t.Fatalf("obsolete queued call transmitted: %v, %d", err, effects.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("obsolete call did not settle")
	}
	if _, err := m.CallChecked(t.Context(), call, nil); err == nil {
		t.Fatal("stale approval survived schema replacement")
	}
}

func TestCheckedManagerReplacementAndConfigIdentity(t *testing.T) {
	var effects atomic.Int32
	cfg := testCfg("checked")
	cfg.Source, cfg.Origin, cfg.Trusted = "/native/config.json", "whip", true
	cfg.Headers = map[string]string{"Authorization": "secret-test-value"}
	first := checkedManager(t, cfg, checkedServer(&effects))
	call := checkedCall(t, first, "write.item")
	second := checkedManager(t, cfg, checkedServer(&effects))
	replacement := checkedCall(t, second, "write.item")
	if call.Definition != replacement.Definition || call.Generation == replacement.Generation {
		t.Fatal("definition is not stable, or generation survived replacement")
	}
	if _, err := second.CallChecked(t.Context(), call, nil); err == nil {
		t.Fatal("old manager admission ran on replacement")
	}
	if _, err := second.CallContext(call); err == nil {
		t.Fatal("old manager descriptor acquired replacement lifetime")
	}
	encoded, _ := json.Marshal(call)
	if strings.Contains(string(encoded), "secret-test-value") || !call.Trusted || call.Source != cfg.Source {
		t.Fatalf("descriptor exposed secrets or lost provenance: %s", encoded)
	}
	cfg.Command = []string{"different-endpoint"}
	third := checkedManager(t, cfg, checkedServer(&effects))
	if changed := checkedCall(t, third, "write.item"); changed.Definition == call.Definition {
		t.Fatal("endpoint change reused definition identity")
	}
	forged := replacement
	forged.Source = "/forged-source"
	if _, err := second.CallChecked(t.Context(), forged, nil); err == nil {
		t.Fatal("caller changed descriptor source without re-admission")
	}
}

func TestCheckedManagerInstructionsAndToolError(t *testing.T) {
	m := newTestManager(t, map[string]ServerConfig{"docs": testCfg("docs")})
	m.Start(t.Context())
	waitReady(t, m)
	call, err := m.ResolveTool("docs", "fail")
	if err != nil {
		t.Fatal(err)
	}
	out, err := m.CallChecked(t.Context(), call, nil)
	if err == nil || out != "Error: boom" {
		t.Fatalf("tool IsError did not fail while retaining output: %q, %v", out, err)
	}
	var effects atomic.Int32
	instructions := checkedManager(t, testCfg("checked"), checkedServer(&effects))
	text, generation, _, err := instructions.Instructions("exact.server")
	if err != nil || len(text) <= 64<<10 || generation == "" || !strings.HasSuffix(text, "guidance") {
		t.Fatalf("large instructions lost: bytes=%d generation=%s err=%v", len(text), generation, err)
	}
	instructions.Disable("exact.server")
	if _, _, _, err := instructions.Instructions("exact.server"); err == nil {
		t.Fatal("disabled guidance remained available")
	}
	if _, err := instructions.ResolveTool("exact.server", "write.item"); err == nil {
		t.Fatal("disabled tool remained callable")
	}
}

func TestCheckedManagerSchemaReferenceDoesNotFetchNetwork(t *testing.T) {
	_, err := toolArguments(map[string]any{"$ref": "https://unavailable.invalid/schema"}, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("external schema reference was accepted without a local definition")
	}
	if !strings.Contains(fmt.Sprint(err), "schema") {
		t.Fatal(err)
	}
}

func TestCheckedManagerPreservesExactArgumentNumbers(t *testing.T) {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "numbers"}, nil)
	srv.AddTool(&sdkmcp.Tool{Name: "echo", InputSchema: map[string]any{
		"type": "object", "properties": map[string]any{"id": map[string]any{"type": "integer"}},
	}}, func(_ context.Context, request *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(request.Params.Arguments)}}}, nil
	})
	m := checkedManager(t, testCfg("numbers"), srv)
	call := checkedCall(t, m, "echo")
	call.Arguments = json.RawMessage(`{"id":9007199254740993}`)
	output, err := m.CallChecked(t.Context(), call, nil)
	if err != nil || output != string(call.Arguments) {
		t.Fatalf("argument number changed before transmission: %q, %v", output, err)
	}
}

func TestCheckedManagerPreservesLargeResult(t *testing.T) {
	want := strings.Repeat("left ", 12000) + "evidence in the middle" + strings.Repeat(" right", 12000)
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "large-result"}, nil)
	srv.AddTool(&sdkmcp.Tool{Name: "read", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: want}}}, nil
	})
	m := checkedManager(t, testCfg("large-result"), srv)
	call := checkedCall(t, m, "read")
	call.Arguments = json.RawMessage(`{}`)
	output, err := m.CallChecked(t.Context(), call, nil)
	if err != nil || output != want {
		t.Fatalf("large result lost before host storage: got %d bytes, want %d, error=%v", len(output), len(want), err)
	}
}

func TestCheckedManagerReconnectCancelsQueuedCallAndChangesGeneration(t *testing.T) {
	var effects atomic.Int32
	m := checkedManager(t, testCfg("checked"), checkedServer(&effects))
	call := checkedCall(t, m, "write.item")
	s := m.servers[call.Server]
	s.calling <- struct{}{}
	defer func() { <-s.calling }()
	done := make(chan error, 1)
	go func() { _, err := m.CallChecked(context.Background(), call, nil); done <- err }()
	if !m.Reconnect(call.Server) {
		t.Fatal("reconnect failed")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("retired queued call succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect left a queued call blocked")
	}
	waitStatus(t, m, call.Server, StatusReady)
	fresh := checkedCall(t, m, call.Tool)
	if fresh.Definition != call.Definition || fresh.Generation == call.Generation || effects.Load() != 0 {
		t.Fatalf("reconnect reused admission or transmitted: old=%+v new=%+v effects=%d", call, fresh, effects.Load())
	}
}

func TestCheckedManagerEnablesInitiallyDisabledServer(t *testing.T) {
	cfg := testCfg("docs")
	cfg.Enabled = new(false)
	m := newTestManager(t, map[string]ServerConfig{"docs": cfg})
	m.Start(t.Context())
	if !m.Enable("docs") {
		t.Fatal("enable refused")
	}
	waitStatus(t, m, "docs", StatusReady)
	if _, err := m.ResolveTool("docs", "greet"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckedManagerRetiresAdmissionBeforeDispatch(t *testing.T) {
	for _, transition := range []string{"close", "disable", "remove", "reconnect"} {
		t.Run(transition, func(t *testing.T) {
			var effects atomic.Int32
			m := checkedManager(t, testCfg("checked"), checkedServer(&effects))
			call := checkedCall(t, m, "write.item")
			ctx, err := m.CallContext(call)
			if err != nil || ctx.Err() != nil {
				t.Fatalf("current descriptor has no live lifetime: %v", err)
			}
			switch transition {
			case "close":
				m.Close()
			case "disable":
				m.Disable(call.Server)
			case "remove":
				m.RemoveServers(call.Server)
			case "reconnect":
				m.Reconnect(call.Server)
			}
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatal("retired descriptor left admission alive without a dispatched call")
			}
			if _, err := m.CallContext(call); err == nil {
				t.Fatal("retired descriptor acquired a fresh lifetime")
			}
			if effects.Load() != 0 {
				t.Fatal("admission lifetime check transmitted an effect")
			}
		})
	}
}

func TestCheckedManagerHTTPCatalogAndClose(t *testing.T) {
	var effects atomic.Int32
	srv := checkedServer(&effects)
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil))
	defer func() { httpServer.CloseClientConnections(); httpServer.Close() }()
	m := NewManager(map[string]ServerConfig{"exact.server": {URL: httpServer.URL, StartupTimeout: 2, ToolTimeout: 2}})
	defer m.Close()
	m.Start(t.Context())
	waitReady(t, m)
	before := checkedCall(t, m, "write.item")
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "new_tool", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdkmcp.CallToolRequest, map[string]any) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{}, nil, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		if _, err := m.ResolveTool("exact.server", "new_tool"); err == nil {
			found = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !found {
		t.Fatal("HTTP notification did not refresh catalog")
	}
	if _, err := m.CallChecked(t.Context(), before, nil); err == nil || effects.Load() != 0 {
		t.Fatal("HTTP catalog change reused stale admission")
	}
	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP manager close exceeded bound")
	}
}

func TestCheckedManagerRetainsCatalogNotificationBeforePublication(t *testing.T) {
	var effects, lists atomic.Int32
	srv := checkedServer(&effects)
	listed := make(chan struct{})
	release := make(chan struct{}, 1)
	defer close(release)
	srv.AddReceivingMiddleware(func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, request sdkmcp.Request) (sdkmcp.Result, error) {
			result, err := next(ctx, method, request)
			if method == "tools/list" && lists.Add(1) == 1 {
				close(listed)
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return result, err
		}
	})
	m := NewManager(map[string]ServerConfig{"exact.server": testCfg("checked")})
	client, transport := sdkmcp.NewInMemoryTransports()
	session, err := srv.Connect(t.Context(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(); _ = session.Close() })
	m.connectTransport = func(context.Context, ServerConfig, *ringBuffer) (sdkmcp.Transport, error) { return client, nil }
	m.Start(t.Context())
	select {
	case <-listed:
	case <-time.After(2 * time.Second):
		t.Fatal("initial catalog was not requested")
	}
	s := m.servers["exact.server"]
	s.mu.Lock()
	before := s.catalogChanges
	s.mu.Unlock()
	srv.AddTool(&sdkmcp.Tool{Name: "new_before_ready", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	observed := false
	for time.Now().Before(deadline) {
		s.mu.Lock()
		observed = s.catalogChanges != before && s.sess == nil
		s.mu.Unlock()
		if observed {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !observed {
		t.Fatal("catalog notification did not precede initial publication")
	}
	release <- struct{}{}
	waitReady(t, m)
	if _, err := m.ResolveTool("exact.server", "new_before_ready"); err != nil {
		t.Fatalf("initial publication lost a delivered catalog notification: %v", err)
	}
	if lists.Load() < 2 || effects.Load() != 0 {
		t.Fatalf("initial catalog was not refreshed safely: lists=%d effects=%d", lists.Load(), effects.Load())
	}
}
