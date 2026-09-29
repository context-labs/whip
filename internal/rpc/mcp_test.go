package rpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/protocol"
)

func TestMCPRPCHostPublicationExplicitConnectionsAndScopedMetadata(t *testing.T) {
	directory := t.TempDir()
	oldCodex, oldClaude, oldOpenCode := mcp.CodexPath, mcp.ClaudeGlobalPath, mcp.OpenCodePaths
	mcp.CodexPath = func() string { return filepath.Join(directory, "codex") }
	mcp.ClaudeGlobalPath = func() string { return filepath.Join(directory, "claude.json") }
	mcp.OpenCodePaths = func() []string { return []string{filepath.Join(directory, "opencode")} }
	t.Cleanup(func() { mcp.CodexPath, mcp.ClaudeGlobalPath, mcp.OpenCodePaths = oldCodex, oldClaude, oldOpenCode })
	var requests atomic.Int32
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture"}, &sdkmcp.ServerOptions{Instructions: strings.Repeat("large instructions\n", 5000)})
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "visible", Description: "delegated"}, func(context.Context, *sdkmcp.CallToolRequest, map[string]any) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{}, nil, nil
	})
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); handler.ServeHTTP(w, r) }))
	t.Cleanup(remote.Close)
	_, c := fixture(t)
	tree := create(t, c)
	if !tree.Root.Configuration.MCPServers.All || tree.Root.Configuration.MCPServers.Servers == nil {
		t.Fatal("resolved selection missing", tree.Root.Configuration.MCPServers)
	}
	configuration := call[protocol.MCPConfiguration](t, c, "mcp.configuration", protocol.EmptyParams{})
	declaration := protocol.MCPServerInput{URL: remote.URL, Command: []string{}, Env: map[string]string{"PRIVATE": "private-secret"}, Headers: map[string]string{}, Note: "fixture"}
	change := protocol.ConfigureMCPParams{Revision: configuration.Revision, Name: "fixture", Server: &declaration, BrandIcons: new(false)}
	configuration = call[protocol.MCPConfiguration](t, c, "mcp.configure", change)
	checkError := func(method string, params any, kind string) {
		t.Helper()
		var result json.RawMessage
		err := c.Call(t.Context(), method, params, &result)
		var wire *client.Error
		if kind == "INVALID" && err != nil && !errors.As(err, &wire) {
			return
		}
		if !errors.As(err, &wire) || wire.Kind != kind {
			t.Fatalf("%s error %v expected %s", method, err, kind)
		}
	}
	checkError("mcp.configure", change, "CONFLICT")
	forged := map[string]any{"revision": configuration.Revision, "name": "forged", "remove": false, "server": map[string]any{"trusted": true}, "imports": nil, "brand_icons": nil}
	checkError("mcp.configure", forged, "INVALID")
	raw, _ := json.Marshal(configuration)
	for _, secret := range []string{"private-secret", "PRIVATE", remote.URL, "command", "headers", "cwd"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("declaration leak", string(raw))
		}
	}
	status := call[protocol.MCPStatusResult](t, c, "mcp.status", protocol.SessionParams{SessionID: tree.Root.ID})
	if len(status.Items) != 1 || status.Items[0].State != "not_started" || requests.Load() != 0 {
		t.Fatal(status, requests.Load())
	}
	checkError("mcp.tools", protocol.MCPServerParams{SessionID: tree.Root.ID, Server: "fixture"}, "MCP_UNAVAILABLE")
	icons := call[protocol.MCPBrandIconsResult](t, c, "mcp.brand.icons", protocol.MCPBrandIconsParams{Keys: []string{"example.com"}})
	if len(icons.Icons) != 0 {
		t.Fatal(icons)
	}
	for _, key := range []string{"127.0.0.1", "host.local", "example.ts.net", "user:secret@example.com", "example.com/path"} {
		checkError("mcp.brand.icons", protocol.MCPBrandIconsParams{Keys: []string{key}}, "INVALID")
	}
	refresh := call[protocol.MCPRefreshResult](t, c, "mcp.refresh", protocol.SessionParams{SessionID: tree.Root.ID})
	if len(refresh.Added) != 1 {
		t.Fatal(refresh)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		status = call[protocol.MCPStatusResult](t, c, "mcp.status", protocol.SessionParams{SessionID: tree.Root.ID})
		if status.Items[0].State == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(status)
		}
		time.Sleep(time.Millisecond)
	}
	tools := call[protocol.MCPToolsResult](t, c, "mcp.tools", protocol.MCPServerParams{SessionID: tree.Root.ID, Server: "fixture"})
	if len(tools.Items) != 1 || tools.Items[0].Capability != "mcp.call.trusted" {
		t.Fatal(tools)
	}
	instructions := call[protocol.MCPInstructionsResult](t, c, "mcp.instructions", protocol.MCPServerParams{SessionID: tree.Root.ID, Server: "fixture"})
	if instructions.Text != "" || len(instructions.ContentParts) != 1 || instructions.Bytes != 94999 {
		t.Fatal(instructions)
	}
	ref := instructions.ContentParts[0]
	call[protocol.ReadContentResult](t, c, "content.read", protocol.ReadContentParams{SessionID: tree.Root.ID, ReferenceID: ref.ID})
	other := create(t, c)
	checkError("content.read", protocol.ReadContentParams{SessionID: other.Root.ID, ReferenceID: ref.ID}, "NOT_FOUND")
	call[protocol.MCPRefreshResult](t, c, "mcp.disable", protocol.MCPServerParams{SessionID: tree.Root.ID, Server: "fixture"})
	refresh = call[protocol.MCPRefreshResult](t, c, "mcp.refresh", protocol.SessionParams{SessionID: tree.Root.ID})
	if refresh.Servers[0].State != "disabled" {
		t.Fatal("refresh enabled disabled server", refresh)
	}
	// Candidate approval pins private source contents, and publication alone does
	// not connect to the newly imported endpoint.
	if err := os.WriteFile(mcp.ClaudeGlobalPath(), []byte(`{"mcpServers":{"candidate":{"url":"`+remote.URL+`","headers":{"Secret":"do-not-project"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	candidates := call[protocol.MCPImportCandidatesResult](t, c, "mcp.import.candidates", protocol.MCPImportCandidatesParams{})
	if len(candidates.Candidates) != 1 {
		t.Fatal(candidates)
	}
	raw, _ = json.Marshal(candidates)
	if strings.Contains(string(raw), "do-not-project") {
		t.Fatal("candidate leaked secret")
	}
	before := requests.Load()
	imported := call[protocol.MCPImportResult](t, c, "mcp.import.apply", protocol.MCPImportParams{Revision: candidates.Revision, Fingerprints: map[string]string{"candidate": candidates.Candidates[0].Fingerprint}})
	if len(imported.Added) != 1 || !imported.Configuration.Imports.Offered || requests.Load() != before {
		t.Fatal(imported, requests.Load(), before)
	}
	checkError("mcp.import.apply", protocol.MCPImportParams{Revision: candidates.Revision, Fingerprints: map[string]string{"candidate": candidates.Candidates[0].Fingerprint}}, "CONFLICT")
}
