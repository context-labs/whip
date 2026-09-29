package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func nativeMCPFixture(t *testing.T) (string, *client.Client, *nativeMCPTools) {
	t.Helper()
	directory := useNativeAuth(t, func(dir string) {
		host := config.Default()
		host.DefaultPermissionMode = session.PermissionAutomatic
		host.Providers["configured"] = config.Provider{Kind: "openai-chat", BaseURL: "http://127.0.0.1:1", CredentialSource: "none"}
		host.Defaults.Model = session.ModelSelection{Provider: "configured", Name: "configured-model"}
		if err := config.Save(dir, host); err != nil {
			t.Fatal(err)
		}
	})
	t.Chdir(t.TempDir())
	c, err := connectNativeRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	p, err := newNativeMCPTools(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return directory, c, p
}

func TestMCPNativeEndpointUsesRealHostAndRestrictedAuthority(t *testing.T) {
	_, c, p := nativeMCPFixture(t)
	if p.owner.session.Configuration.Model.Provider != "" || p.owner.session.Configuration.Model.Name != "" {
		t.Fatal("inherited configured model", p.owner.session.Configuration.Model)
	}
	var policy protocol.PermissionPolicy
	if err := c.Call(t.Context(), "permissions.policy", protocol.SessionParams{SessionID: p.owner.session.ID}, &policy); err != nil || policy.Mode != "prompt" || !policy.DenyInteractive {
		t.Fatal(policy, err)
	}
	path := filepath.Join(p.owner.session.WorkingDirectory, "fixture.txt")
	if err := os.WriteFile(path, []byte("safe workspace read"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("outside private bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(p.owner.session.WorkingDirectory, "escape")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	finished := make(chan error, 1)
	go func() { finished <- mcp.ServeTransport(ctx, "fixture", p, serverTransport) }()
	cs, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "fixture", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("MCP endpoint did not join")
		}
	})
	definitions, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions.Tools) != len(mcpAliases) {
		t.Fatal(definitions)
	}
	for _, definition := range definitions.Tools {
		if _, ok := mcpAliases[definition.Name]; !ok {
			t.Fatal("unrestricted tool", definition.Name)
		}
	}
	for _, request := range []struct {
		name   string
		args   map[string]any
		denied bool
	}{
		{"read", map[string]any{"path": "fixture.txt"}, false},
		{"read", map[string]any{"path": path}, false},
		{"read", map[string]any{"path": outside}, true},
		{"read", map[string]any{"path": "../secret.txt"}, true},
		{"read", map[string]any{"path": "escape"}, true},
		{"write", map[string]any{"path": "mutated", "content": "unsafe"}, true},
		{"edit", map[string]any{"path": "fixture.txt", "old_string": "safe", "new_string": "unsafe"}, true},
		{"bash", map[string]any{"command": "touch mutated"}, true},
	} {
		result, err := cs.CallTool(t.Context(), &sdkmcp.CallToolParams{Name: request.name, Arguments: request.args})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError != request.denied {
			t.Fatalf("%s %#v: %+v", request.name, request.args, result)
		}
		text := result.Content[0].(*sdkmcp.TextContent).Text
		if !request.denied && (!strings.Contains(text, "safe workspace read") || !strings.Contains(text, string(p.owner.session.ID)) || !strings.Contains(text, "operation_id")) {
			t.Fatal("lost canonical operation evidence", text)
		}
		if strings.Contains(text, "outside private bytes") {
			t.Fatal("workspace authority escape", text)
		}
	}
	if _, err := p.CallTool(t.Context(), "execute", json.RawMessage(`{"code":"print(1)"}`)); err == nil {
		t.Fatal("REPL exposed")
	}
	if _, err := os.Stat(filepath.Join(p.owner.session.WorkingDirectory, "mutated")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("mutating tool bypassed default denial", err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "safe workspace read" {
		t.Fatal(string(body), err)
	}
	var history protocol.HistoryResult
	if err := c.Call(t.Context(), "sessions.history", protocol.HistoryParams{SessionID: p.owner.session.ID, Limit: 100}, &history); err != nil || len(history.Items) != 0 {
		t.Fatal("fabricated conversation", history, err)
	}
	if err := cs.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	var owner protocol.Session
	if err := c.Call(t.Context(), "sessions.get", protocol.SessionParams{SessionID: p.owner.session.ID}, &owner); err == nil {
		t.Fatal("ephemeral endpoint owner retained")
	}
}

func TestMCPNativeObserverCancellationJoinsAcceptedCallBeforeDeletion(t *testing.T) {
	_, c, p := nativeMCPFixture(t)
	// A separate explicit human grant allows this one fixture action. Endpoint
	// construction itself never grants shell, write, browser or computer authority.
	var grant protocol.Grant
	if err := c.Call(t.Context(), "grants.create", protocol.CreateGrantParams{ID: "fixture_shell", SessionID: p.owner.session.ID, Capability: "shell.run", Resource: p.owner.session.WorkingDirectory}, &grant); err != nil {
		t.Fatal(err)
	}
	observing, cancel := context.WithCancel(t.Context())
	defer cancel()
	called := make(chan error, 1)
	go func() {
		text, err := p.CallTool(observing, "bash", json.RawMessage(`{"command":"sleep 0.15; printf joined"}`))
		if err == nil && !strings.Contains(text, "joined") {
			err = errors.New("missing accepted command output")
		}
		called <- err
	}()
	handle, _ := c.Session(p.owner.session.ID)
	deadline := time.Now().Add(5 * time.Second)
	for {
		activity, err := handle.Activity(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if activity.ActiveTurn != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("direct action never claimed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-called; err != nil {
		t.Fatal("observer cancellation stopped accepted execution", err)
	}
}

func TestMCPNativeLostAcknowledgementDoesNotResend(t *testing.T) {
	directory, c, p := nativeMCPFixture(t)
	if err := os.WriteFile(filepath.Join(p.owner.session.WorkingDirectory, "fixture"), []byte("lost acknowledgement"), 0o600); err != nil {
		t.Fatal(err)
	}
	socket, sends := mcpFaultProxy(t, filepath.Join(directory, "runtime.sock"), true)
	proxy, err := client.Connect(t.Context(), socket, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	p.owner.client = proxy
	text, err := p.CallTool(t.Context(), "read", json.RawMessage(`{"path":"fixture"}`))
	if err != nil || !strings.Contains(text, "lost acknowledgement") || sends.Load() != 1 {
		t.Fatal(text, err, sends.Load())
	}
	handle, _ := c.Session(p.owner.session.ID)
	inputs, err := handle.Inputs(t.Context(), "all", nil, 100)
	if err != nil || len(inputs.Items) != 1 {
		t.Fatal(inputs, err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMCPInputFramesBoundedBeforeDecode(t *testing.T) {
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{`{"jsonrpc":"2.0"}` + "\n", true},
		{"[{}]\n", true},
		{"[" + strings.Repeat("{},", 16) + "{}]\n", false},
		{`{"huge":"` + strings.Repeat("x", 1<<20) + `"}` + "\n", false},
	} {
		reader := io.NopCloser(strings.NewReader(tc.input))
		framed := &mcpLineReader{input: reader, scanner: bufio.NewScanner(reader)}
		framed.scanner.Buffer(make([]byte, 4096), (1<<20)+1)
		got, err := io.ReadAll(framed)
		if (err == nil) != tc.valid {
			t.Fatal(len(got), err)
		}
		if tc.valid && !bytes.Equal(got, []byte(tc.input)) {
			t.Fatal(string(got))
		}
	}
}

// Fault the acknowledgement only after the real private host has committed it.
func mcpFaultProxy(t *testing.T, target string, accepted bool) (string, *atomic.Int32) {
	t.Helper()
	return nativeControlFaultProxy(t, target, "tool.call", accepted)
}

func nativeControlFaultProxy(t *testing.T, target, method string, accepted bool) (string, *atomic.Int32) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "whip-mcp-proxy-") //nolint:usetesting // macOS socket bound.
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "host.sock")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	var workers sync.WaitGroup
	joined := make(chan struct{})
	var sends atomic.Int32
	go func() {
		defer close(joined)
		for {
			peer, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Go(func() {
				defer peer.Close()
				closePeer := context.AfterFunc(ctx, func() { _ = peer.Close() })
				defer closePeer()
				upstream, err := (&net.Dialer{}).DialContext(ctx, "unix", target)
				if err != nil {
					return
				}
				defer upstream.Close()
				closeUpstream := context.AfterFunc(ctx, func() { _ = upstream.Close() })
				defer closeUpstream()
				_ = peer.SetDeadline(time.Now().Add(10 * time.Second))
				_ = upstream.SetDeadline(time.Now().Add(10 * time.Second))
				incoming, response := bufio.NewScanner(peer), bufio.NewScanner(upstream)
				incoming.Buffer(make([]byte, 4096), protocol.MaxFrameBytes+1)
				response.Buffer(make([]byte, 4096), protocol.MaxFrameBytes+1)
				for incoming.Scan() {
					var request protocol.Request
					if json.Unmarshal(incoming.Bytes(), &request) != nil {
						return
					}
					drop := request.Method == method && sends.Add(1) == 1
					if drop && !accepted {
						return
					}
					if _, err := upstream.Write(append(incoming.Bytes(), '\n')); err != nil || !response.Scan() {
						return
					}
					if drop {
						return
					}
					if _, err := peer.Write(append(response.Bytes(), '\n')); err != nil {
						return
					}
				}
			})
		}
	}()
	t.Cleanup(func() { stop(); _ = listener.Close(); <-joined; workers.Wait(); _ = os.RemoveAll(dir) })
	return socket, &sends
}

func TestMCPNativeUnknownAdmissionPreservesOwnerAndNeverRetries(t *testing.T) {
	directory := useNativeAuth(t, nil)
	t.Chdir(t.TempDir())
	c, err := connectNativeRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p, err := newNativeMCPTools(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	socket, sends := mcpFaultProxy(t, filepath.Join(directory, "runtime.sock"), false)
	proxy, err := client.Connect(t.Context(), socket, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	p.owner.client = proxy
	if _, err := p.CallTool(t.Context(), "read", json.RawMessage(`{"path":"fixture"}`)); err == nil || !strings.Contains(err.Error(), "outcome unknown") {
		t.Fatal(err)
	}
	if _, err := p.CallTool(t.Context(), "read", json.RawMessage(`{"path":"fixture"}`)); err == nil || !strings.Contains(err.Error(), "unknown outcome") {
		t.Fatal(err)
	}
	if sends.Load() != 1 {
		t.Fatal("uncertain call replayed", sends.Load())
	}
	if err := p.Close(); err == nil || !strings.Contains(err.Error(), string(p.owner.session.ID)) {
		t.Fatal("unknown owner lost", err)
	}
	handle, _ := c.Session(p.owner.session.ID)
	if _, err := handle.Get(t.Context()); err != nil {
		t.Fatal("unknown owner deleted", err)
	}
	inputs, err := handle.Inputs(t.Context(), "all", nil, 100)
	if err != nil || len(inputs.Items) != 0 {
		t.Fatal(inputs, err)
	}
	// The fixture can now explicitly clean its known-unadmitted temporary owner.
	if err := deleteNativeRun(t.Context(), c, p.owner.session.ID); err != nil {
		t.Fatal(err)
	}
}
