package main

// Coverage for the ACP CLI's pure helpers: vision resolution (catalog wins,
// config falls back) and whip's base MCP merge. The acpCLI entry point itself
// serves stdio and isn't unit-testable; its helpers are.

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
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

	// session/new drives the factory closure: it builds the agent (skills,
	// memory, MCP manager) without contacting the provider, so it needs no
	// network. Covers the wiring the error paths return before.
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
