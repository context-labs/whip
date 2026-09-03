package main

// Coverage for the ACP CLI's pure helpers: vision resolution (catalog wins,
// config falls back) and whip's base MCP merge. The acpCLI entry point itself
// serves stdio and isn't unit-testable; its helpers are.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/mcp"
)

// acpCLI's config prologue runs before the serve loop: a broken config, an
// unknown model, or a provider with no key all error out instead of serving.
// These cover the entry point's front half without blocking on stdio.
func TestAcpCLIConfigErrors(t *testing.T) {
	// A config that doesn't parse → config.Load errors.
	t.Run("unparseable config", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("WHIP_HOME", home)
		writeConfig(t, home, `{ not json`)
		if err := acpCLI(nil); err == nil {
			t.Error("want config.Load parse error")
		}
	})

	// Valid config, but -m names a model that doesn't exist → Resolve fails.
	t.Run("unknown model", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("WHIP_HOME", home)
		writeConfig(t, home, `{
			"defaultModel": "test",
			"providers": {"testprov": {"baseUrl": "http://127.0.0.1:1", "api": "openai-completions", "apiKey": "k"}},
			"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
		}`)
		if err := acpCLI([]string{"-m", "ghost"}); err == nil {
			t.Error("want Resolve error for unknown model")
		}
	})

	// Model resolves, but the provider has no key anywhere → key error.
	t.Run("no api key", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("WHIP_HOME", home)
		writeConfig(t, home, `{
			"defaultModel": "test",
			"providers": {"testprov": {"baseUrl": "http://127.0.0.1:1", "api": "openai-completions"}},
			"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
		}`)
		err := acpCLI(nil)
		if err == nil || !strings.Contains(err.Error(), "no API key") {
			t.Errorf("want no-API-key error, got %v", err)
		}
	})
}

func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// With a working config, acpCLI wires the bridge and serves stdio. Driving a
// real initialize handshake over the stdio pipes exercises the serve loop and
// connection setup; closing stdin (no client) then ends the loop, so the
// whole wiring path runs without a live editor. Covers the prologue + serve
// path the error tests return early from.
func TestAcpCLIServeExitsOnEOF(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	writeConfig(t, home, `{
		"defaultModel": "test",
		"providers": {"testprov": {"baseUrl": "http://127.0.0.1:1", "api": "openai-completions", "apiKey": "k"}},
		"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
	}`)
	useTestDaemon(t)
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	previousCredentials := loadACPClientCredentials
	loadACPClientCredentials = func() (daemon.ClientCredentials, error) {
		return daemon.ClientCredentials{ClientID: "acp-test", PrivateKey: private}, nil
	}
	t.Cleanup(func() { loadACPClientCredentials = previousCredentials })

	// stdin/stdout become the ends of two pipes: the test acts as the ACP
	// client on the other side.
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	t.Cleanup(func() {
		os.Stdin, os.Stdout = oldIn, oldOut
		_ = inR.Close()
		_ = outW.Close()
		_ = outR.Close()
	})

	done := make(chan error, 1)
	go func() { done <- acpCLI(nil) }()

	// Send an initialize request and expect a response, proving the bridge is
	// up and serving before we hang up.
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}` + "\n"
	if _, err := inW.WriteString(initReq); err != nil {
		t.Fatal(err)
	}
	readLine := func() []byte {
		got := make(chan []byte, 1)
		go func() {
			buf := make([]byte, 64<<10)
			n, _ := outR.Read(buf)
			got <- buf[:n]
		}()
		select {
		case b := <-got:
			return b
		case <-time.After(10 * time.Second):
			t.Fatal("no response from acp serve loop")
			return nil
		}
	}
	if got := readLine(); !strings.Contains(string(got), `"protocolVersion"`) {
		t.Errorf("initialize response = %q", got)
	}

	// session/new drives the daemon-backed root factory without contacting the
	// provider, so it needs no network. Covers the wiring the error paths return
	// before.
	cwd, _ := os.Getwd()
	newReq := `{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":` + fmt.Sprintf("%q", cwd) + `,"mcpServers":[]}}` + "\n"
	if _, err := inW.WriteString(newReq); err != nil {
		t.Fatal(err)
	}
	if got := readLine(); !strings.Contains(string(got), `"sessionId"`) {
		t.Errorf("session/new response = %q", got)
	}

	// Hang up: stdin EOF ends the serve loop, conn.Done() fires, acpCLI exits.
	if err := inW.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("acpCLI = %v, want nil on stdin EOF", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("acpCLI did not exit after stdin EOF")
	}
}

func TestACPDaemonBackendAndMCPToolsRoundTrip(t *testing.T) {
	var requests []llm.Request
	runFixture(t, "daemon reply", &requests)
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	backend := &acpDaemonBackend{
		clientID: "acp-round-trip", privateKey: private,
		model: "test", provider: "testprov",
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	disabled := false
	workingDirectory := t.TempDir()
	root, err := backend.NewRoot(ctx, workingDirectory, map[string]mcp.ServerConfig{
		"unused": {Command: []string{"false"}, Enabled: &disabled},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	// Exercise the content-parts path used by image-capable ACP clients.
	action, err := root.NewAction("submit", daemon.SubmitPayload{
		Text:  "remember this",
		Parts: []llm.ContentPart{{Type: "text", Text: "remember this"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := root.Command(ctx, action)
	if err != nil || result.Status != "succeeded" || result.Output != "daemon reply" {
		t.Fatalf("submit = %+v, %v", result, err)
	}
	if len(requests) != 1 || len(requests[0].Messages) == 0 {
		t.Fatalf("provider requests = %+v", requests)
	}

	// The MCP adapter sees schemas and invokes built-ins only through daemon
	// commands. Read is safe; write is rejected after the headless policy is
	// installed, proving side effects do not bypass daemon-owned permissions.
	configure, err := root.NewAction("tool.configure", map[string]bool{"deny_permissions": true})
	if err != nil {
		t.Fatal(err)
	}
	if configured, commandErr := root.Command(ctx, configure); commandErr != nil || configured.Status != "succeeded" {
		t.Fatalf("tool.configure = %+v, %v", configured, commandErr)
	}
	provider := daemonMCPTools{client: root}
	definitions, err := provider.ToolDefinitions(ctx)
	if err != nil || len(definitions) == 0 {
		t.Fatalf("tool definitions = %d, %v", len(definitions), err)
	}
	path := filepath.Join(workingDirectory, "note.txt")
	if err := os.WriteFile(path, []byte("daemon-owned tools\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	readArgs, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	output, err := provider.CallTool(ctx, "read", readArgs)
	if err != nil || !strings.Contains(output, "daemon-owned tools") {
		t.Fatalf("read tool = %q, %v", output, err)
	}
	writeArgs, err := json.Marshal(map[string]string{"path": path, "content": "changed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.CallTool(ctx, "write", writeArgs); err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("write tool should be denied, got %v", err)
	}
	if backend.Paired(ctx) {
		t.Fatal("an untrusted ACP identity should not report as paired")
	}

	// Pair the ACP identity, switch to external prompts, and resolve the real
	// daemon-owned permission. This covers the signed boundary rather than a
	// protocol mock: the tool worker stays blocked until the decision lands.
	identityConnection, err := connectDaemon(ctx, "acp", backend.clientID, nil)
	if err != nil {
		t.Fatal(err)
	}
	enroller, ok := identityConnection.(interface {
		EnrollIdentity(context.Context, ed25519.PrivateKey, bool, string, ed25519.PrivateKey) (daemon.IdentityResult, error)
	})
	if !ok {
		t.Fatal("daemon connection does not support identity enrollment")
	}
	if _, err := enroller.EnrollIdentity(ctx, private, true, "", nil); err != nil {
		t.Fatal(err)
	}
	_ = identityConnection.Close()
	if !backend.Paired(ctx) {
		t.Fatal("enrolled ACP identity should report as paired")
	}
	external, err := root.NewAction("permission.mode", map[string]bool{"external_permissions": true})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := root.SetPermissionMode(ctx, external, true); err != nil || result.Status != "succeeded" {
		t.Fatalf("external permission mode = %+v, %v", result, err)
	}
	beforeWrite, err := root.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	type toolResult struct {
		output string
		err    error
	}
	writeDone := make(chan toolResult, 1)
	go func() {
		output, callErr := provider.CallTool(ctx, "write", writeArgs)
		writeDone <- toolResult{output: output, err: callErr}
	}()
	permissionID := ""
	var eventKinds []string
	permissionDeadline := time.NewTimer(5 * time.Second)
	defer permissionDeadline.Stop()
	for permissionID == "" {
		select {
		case update := <-root.Updates():
			if update.Event == nil {
				continue
			}
			eventKinds = append(eventKinds, update.Event.Kind)
			if update.Event.Kind != "permission.pending" || update.Event.Seq <= beforeWrite.Cursor {
				continue
			}
			var pending struct {
				ID string `json:"permission_id"`
			}
			if err := json.Unmarshal(update.Event.Payload, &pending); err != nil {
				t.Fatal(err)
			}
			permissionID = pending.ID
		case result := <-writeDone:
			t.Fatalf("write finished before a permission decision: %q, %v", result.output, result.err)
		case <-permissionDeadline.C:
			snapshot, snapshotErr := root.Snapshot(context.Background())
			t.Fatalf("permission event was not delivered; events=%v snapshot permissions=%+v err=%v cursor=%d", eventKinds, snapshot.Permissions, snapshotErr, root.Cursor())
		case <-ctx.Done():
			t.Fatal("permission event was not delivered")
		}
	}
	decisionAction, err := root.NewAction("permission.decide", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := root.DecidePermission(ctx, decisionAction, permissionID, true, "approved in ACP")
	if err != nil || decision.OperationID == "" {
		t.Fatalf("permission decision = %+v, %v", decision, err)
	}
	select {
	case result := <-writeDone:
		if result.err != nil {
			t.Fatalf("approved write = %q, %v", result.output, result.err)
		}
	case <-ctx.Done():
		t.Fatal("approved write did not finish")
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != "changed" {
		t.Fatalf("written body = %q, %v", body, err)
	}
	automatic, err := root.NewAction("permission.mode", map[string]bool{"external_permissions": false})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := root.SetPermissionMode(ctx, automatic, false); err != nil || result.Status != "succeeded" {
		t.Fatalf("automatic permission mode = %+v, %v", result, err)
	}

	snapshot, err := root.Snapshot(ctx)
	if err != nil || snapshot.RootID != root.RootID() {
		t.Fatalf("snapshot = %+v, %v", snapshot, err)
	}
	metas, err := backend.ListSessions(ctx, 10)
	if err != nil || len(metas) != 1 || metas[0].ID != root.RootID() {
		t.Fatalf("sessions = %+v, %v", metas, err)
	}
	loaded, err := backend.LoadRoot(ctx, root.RootID(), "ignored", nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RootID() != root.RootID() {
		t.Fatalf("loaded root = %q, want %q", loaded.RootID(), root.RootID())
	}
	_ = loaded.Close()
}

// Provider-advertised input_modalities beat the config's per-model vision
// flag; without a catalog entry the config flag is the answer.
func TestAcpSupportsVision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	// Seed a catalog: "vis" advertises image input, "plain" advertises none.
	catalogs := `{"testprov": {"baseUrl": "http://x", "models": [
		{"id": "vis", "inputModalities": ["text", "image"]},
		{"id": "plain", "inputModalities": ["text"]}
	]}}`
	if err := os.WriteFile(filepath.Join(home, "models.json"), []byte(catalogs), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Models: map[string]config.Model{
		"cfgvis": {Providers: []string{"testprov"}, Vision: true},
		"cfgno":  {Providers: []string{"testprov"}},
	}}

	cases := []struct {
		name, modelName, modelID string
		want                     bool
	}{
		{"catalog says image", "anyname", "vis", true},
		{"catalog says text-only", "anyname", "plain", false},
		{"no catalog entry, config true", "cfgvis", "unknown-id", true},
		{"no catalog entry, config false", "cfgno", "unknown-id", false},
		{"no catalog entry, model unknown", "ghost", "unknown-id", false},
	}
	for _, c := range cases {
		if got := acpSupportsVision(cfg, c.modelName, c.modelID, "testprov"); got != c.want {
			t.Errorf("%s: acpSupportsVision = %v, want %v", c.name, got, c.want)
		}
	}
}

// With no catalog file at all, the config's per-model flag decides alone.
func TestAcpSupportsVisionNoCatalog(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir()) // empty — LoadCatalogs returns an empty map
	cfg := &config.Config{Models: map[string]config.Model{
		"m": {Providers: []string{"p"}, Vision: true},
	}}
	if !acpSupportsVision(cfg, "m", "any-id", "p") {
		t.Error("config vision=true should hold when there's no catalog")
	}
}

// whip's own MCP config is the per-session floor. An empty config merges to
// an empty (non-nil) map; a configured stdio server carries through.
func TestAcpBaseMCP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	// Disable claude/codex imports so discovery only sees whip's own config
	// (the test machine's ~/.codex/config.toml would otherwise leak in).
	off := false
	noImport := &config.MCPImport{
		Claude: &config.MCPImportSource{Enabled: &off},
		Codex:  &config.MCPImportSource{Enabled: &off},
	}

	empty := acpBaseMCP(&config.Config{MCPImport: noImport})
	if empty == nil || len(empty) != 0 {
		t.Errorf("empty config: got %v", empty)
	}

	cfg := &config.Config{
		MCPImport: noImport,
		MCPServers: map[string]config.MCPServer{
			"docs": {Command: []string{"docs-mcp", "--serve"}},
		},
	}
	got := acpBaseMCP(cfg)
	srv, ok := got["docs"]
	if !ok {
		t.Fatalf("docs server missing: %v", got)
	}
	if len(srv.Command) != 2 || srv.Command[0] != "docs-mcp" {
		t.Errorf("docs command = %v", srv.Command)
	}
}
