package runtime

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

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/mcpconfig"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func isolateMCP(t *testing.T) {
	t.Helper()
	directory := t.TempDir()
	// The package's retained discovery injection avoids changing HOME, which
	// would also relocate the real engine-worker build's Go module cache.
	previousCodex, previousClaude, previousOpenCode := mcp.CodexPath, mcp.ClaudeGlobalPath, mcp.OpenCodePaths
	mcp.CodexPath = func() string { return filepath.Join(directory, "codex.toml") }
	mcp.ClaudeGlobalPath = func() string { return filepath.Join(directory, "claude.json") }
	mcp.OpenCodePaths = func() []string { return []string{filepath.Join(directory, "opencode.json")} }
	t.Cleanup(func() {
		mcp.CodexPath, mcp.ClaudeGlobalPath, mcp.OpenCodePaths = previousCodex, previousClaude, previousOpenCode
	})
}

func mcpHTTPFixture(t *testing.T) (string, *sdkmcp.Server, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture"}, &sdkmcp.ServerOptions{Instructions: "private server usage"})
	for _, name := range []string{"visible", "hidden"} {
		sdkmcp.AddTool(server, &sdkmcp.Tool{Name: name, Description: "lookup " + name}, func(context.Context, *sdkmcp.CallToolRequest, map[string]any) (*sdkmcp.CallToolResult, any, error) {
			calls.Add(1)
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "done"}}}, nil, nil
		})
	}
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil))
	t.Cleanup(httpServer.Close)
	return httpServer.URL, server, calls
}

func awaitMCPReady(t *testing.T, r *Runtime, owner session.Session) *mcp.Manager {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entry, err := r.mcpRoot(t.Context(), owner, false)
		if err != nil {
			t.Fatal(err)
		}
		manager := r.mcpManager(entry)
		if manager != nil {
			for _, row := range manager.Statuses() {
				if row.Status == mcp.StatusReady {
					return manager
				}
				if row.Status == mcp.StatusFailed {
					t.Fatal(row)
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("MCP did not become ready")
	return nil
}

func configureMCPFixture(t *testing.T, r *Runtime, url string) {
	t.Helper()
	snapshot, err := r.MCPConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ConfigureMCP(t.Context(), MCPConfigurationPatch{ExpectedRevision: snapshot.Revision, Name: "fixture", Server: &mcpconfig.Server{URL: url}}); err != nil {
		t.Fatal(err)
	}
}

func TestMCPUntrustedDiscoveryRequiresExactApprovalAndRetryNeverReplays(t *testing.T) {
	isolateMCP(t)
	url, _, effects := mcpHTTPFixture(t)
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	path := mcp.ClaudeGlobalPath()
	if err := os.WriteFile(path, fmtJSONMCP(url), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := r.MCPStatus(t.Context(), owner.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != "not_started" {
		t.Fatal(rows, err)
	}
	entry, err := r.mcpRoot(t.Context(), owner, false)
	if err != nil || entry != nil {
		t.Fatal("metadata started a manager", err)
	}
	invocation := tool.Invocation{SessionID: owner.ID, CellID: cell.ID, RequestID: "refresh", Module: "mcp", Name: "refresh", Arguments: map[string]any{}}
	done := make(chan error, 1)
	go func() { _, _, err := r.tools.Call(t.Context(), invocation); done <- err }()
	var permission session.Permission
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		pending, err := r.store.Permissions(t.Context(), owner.ID, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) > 0 {
			permission = pending[0]
			break
		}
		time.Sleep(time.Millisecond)
	}
	if permission.OperationID == "" {
		t.Fatal("untrusted connection skipped approval")
	}
	operation, err := r.store.Operation(t.Context(), permission.OperationID)
	if err != nil || operation.Capability != "mcp.connect" {
		t.Fatal(operation, err)
	}
	if effects.Load() != 0 {
		t.Fatal("discovery caused tool effect")
	}
	if _, err := r.ResolvePermission(t.Context(), permission.OperationID, true); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	manager := awaitMCPReady(t, r, owner)
	call, err := manager.ResolveTool("fixture", "visible")
	if err != nil || call.Trusted {
		t.Fatal(call, err)
	}
	if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "call", SessionID: owner.ID, Capability: "mcp.call", Resource: mcpCallResource(owner.ID, call)}); err != nil {
		t.Fatal(err)
	}
	invocation.RequestID, invocation.Name, invocation.Arguments = "call", "call", map[string]any{"server": "fixture", "tool": "visible", "arguments": map[string]any{}}
	if _, _, err := r.tools.Call(t.Context(), invocation); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.tools.Call(t.Context(), invocation); err == nil {
		t.Fatal("effect replayed")
	}
	if effects.Load() != 1 {
		t.Fatal("wrong effect count", effects.Load())
	}
}

func fmtJSONMCP(url string) []byte {
	raw, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fixture": map[string]any{"url": url, "headers": map[string]string{"Authorization": "literal-secret"}}}})
	return raw
}

func TestMCPImportFingerprintCASAndSafeConfiguration(t *testing.T) {
	isolateMCP(t)
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner := createTest(t, r)
	path := mcp.ClaudeGlobalPath()
	if err := os.WriteFile(path, fmtJSONMCP("https://first.example/mcp"), 0o600); err != nil {
		t.Fatal(err)
	}
	discovery, err := r.MCPImportCandidates(t.Context(), owner.ID)
	if err != nil || len(discovery.Candidates) != 1 {
		t.Fatal(discovery, err)
	}
	raw, _ := json.Marshal(discovery)
	if strings.Contains(string(raw), "literal-secret") {
		t.Fatal("import metadata leaked credentials")
	}
	request := MCPImportRequest{SessionID: owner.ID, ExpectedRevision: discovery.Revision, Fingerprints: map[string]string{"fixture": discovery.Candidates[0].Fingerprint}}
	if err := os.WriteFile(path, fmtJSONMCP("https://changed.example/mcp"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ImportMCP(t.Context(), request); !errors.Is(err, store.ErrConflict) {
		t.Fatal("changed candidate imported", err)
	}
	discovery, err = r.MCPImportCandidates(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	request.Fingerprints["fixture"] = discovery.Candidates[0].Fingerprint
	result, err := r.ImportMCP(t.Context(), request)
	if err != nil || len(result.Added) != 1 || !result.Configuration.Imports.Offered {
		t.Fatal(result, err)
	}
	snapshot, err := r.configuration.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !mcp.NativeConfigs(snapshot.Host.MCP.Servers, config.FileName)["fixture"].Trusted {
		t.Fatal("explicit native import remained untrusted")
	}
	if _, err := r.ImportMCP(t.Context(), request); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatal("stale configuration update accepted", err)
	}
	if len(r.mcp.roots) != 0 {
		t.Fatal("configuration started resources")
	}
}

func TestMCPChildrenSeeOnlyCapturedValidDelegatedTools(t *testing.T) {
	isolateMCP(t)
	url, server, _ := mcpHTTPFixture(t)
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner := createTest(t, r)
	configureMCPFixture(t, r, url)
	if _, err := r.MCPRefresh(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	manager := awaitMCPReady(t, r, owner)
	call, err := manager.ResolveTool("fixture", "visible")
	if err != nil {
		t.Fatal(err)
	}
	grant, err := r.CreateGrant(t.Context(), session.Grant{ID: "delegated", SessionID: owner.ID, Capability: mcpCallCapability(call), Resource: mcpCallResource(owner.ID, call)})
	if err != nil {
		t.Fatal(err)
	}
	child, err := r.store.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child"}, store.ChildRequest{ParentID: owner.ID, Parts: []session.Part{{Type: "text", Text: "work"}}, GrantIDs: []session.GrantID{grant.ID}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := r.mcpRoot(t.Context(), *child.Session, false)
	if err != nil {
		t.Fatal(err)
	}
	visible, _, err := r.mcpVisibleCatalog(t.Context(), *child.Session, entry)
	if err != nil || len(visible["fixture"]) != 1 || visible["fixture"][0].Name != "visible" {
		t.Fatal(visible, err)
	}
	if _, err := r.MCPRefresh(t.Context(), child.Session.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatal("child refreshed shared owner", err)
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "future"}, func(context.Context, *sdkmcp.CallToolRequest, map[string]any) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{}, nil, nil
	})
	if _, err := r.MCPReconnect(t.Context(), owner.ID, "fixture"); err != nil {
		t.Fatal(err)
	}
	awaitMCPReady(t, r, owner)
	visible, _, err = r.mcpVisibleCatalog(t.Context(), *child.Session, entry)
	if err != nil || len(visible["fixture"]) != 1 {
		t.Fatal("refresh widened child", visible, err)
	}
	if _, err := r.mcpCatalog(t.Context(), *child.Session, session.MCPCatalogRequest{Action: "instructions", Server: "fixture"}); !errors.Is(err, store.ErrConflict) {
		t.Fatal("ungranted instructions leaked", err)
	}
	if _, err := r.RevokeGrant(t.Context(), grant.ID); err != nil {
		t.Fatal(err)
	}
	visible, _, err = r.mcpVisibleCatalog(t.Context(), *child.Session, entry)
	if err != nil || len(visible) != 0 {
		t.Fatal("revoked chain stayed visible", visible, err)
	}
}

func TestMCPRestartDoesNotReconnectAndAttachmentsCannotClaimTrust(t *testing.T) {
	isolateMCP(t)
	url, _, _ := mcpHTTPFixture(t)
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	owner := createTest(t, r)
	configureMCPFixture(t, r, url)
	result, err := r.MCPAttach(t.Context(), owner.ID, map[string]mcp.ServerConfig{"attached": {URL: url, Trusted: true, Origin: "whip", Source: "host"}, "fixture": {URL: "http://must-not-connect.invalid"}})
	if err != nil || len(result.Added) != 1 || result.Added[0] != "attached" || len(result.Blocked) != 1 {
		t.Fatal(result, err)
	}
	manager := awaitMCPReady(t, r, owner)
	call, err := manager.ResolveTool("attached", "visible")
	if err != nil || call.Trusted {
		t.Fatal("attachment claimed native trust", call, err)
	}
	if _, present := manager.Config("fixture"); present {
		t.Fatal("attachment implicitly connected unrequested configured server")
	}
	if _, err := r.MCPRefresh(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if len(manager.Blocked()) != 1 {
		t.Fatal("refresh forgot attachment refusal")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, directory, model.Scripted{})
	rows, err := reopened.MCPStatus(t.Context(), owner.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != "not_started" {
		t.Fatal(rows, err)
	}
	if len(reopened.mcp.roots) != 0 {
		t.Fatal("restart metadata recreated live resource")
	}
}

func TestMCPConfirmedFailurePublishesScopedLargeEvidence(t *testing.T) {
	isolateMCP(t)
	url, server, effects := mcpHTTPFixture(t)
	server.RemoveTools("visible")
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "visible"}, func(context.Context, *sdkmcp.CallToolRequest, map[string]any) (*sdkmcp.CallToolResult, any, error) {
		effects.Add(1)
		return &sdkmcp.CallToolResult{IsError: true, Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: strings.Repeat("failure evidence ", 6000)}, &sdkmcp.ImageContent{MIMEType: "image/png", Data: []byte("binary evidence")}}}, nil, nil
	})
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	configureMCPFixture(t, r, url)
	if _, err := r.MCPRefresh(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	manager := awaitMCPReady(t, r, owner)
	call, err := manager.ResolveTool("fixture", "visible")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "failed_call", SessionID: owner.ID, Capability: mcpCallCapability(call), Resource: mcpCallResource(owner.ID, call)}); err != nil {
		t.Fatal(err)
	}
	_, id, err := r.tools.Call(t.Context(), tool.Invocation{SessionID: owner.ID, CellID: cell.ID, RequestID: "failure", Module: "mcp", Name: "call", Arguments: map[string]any{"server": "fixture", "tool": "visible", "arguments": map[string]any{}}})
	if err == nil {
		t.Fatal("remote failure succeeded")
	}
	operation, err := r.store.Operation(t.Context(), id)
	if err != nil || operation.State != session.OperationFailed || operation.Result == nil {
		t.Fatal(operation, err)
	}
	var evidence struct {
		TextParts   []session.ContentReference `json:"text_parts"`
		Attachments []struct {
			Parts []session.ContentReference `json:"content_parts"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(operation.Result.Value, &evidence); err != nil {
		t.Fatal(err)
	}
	if len(evidence.TextParts) != 1 || len(evidence.Attachments) != 1 || len(evidence.Attachments[0].Parts) != 1 {
		t.Fatal("missing retained evidence", evidence)
	}
	other := createTest(t, r)
	for _, reference := range []session.ContentReference{evidence.TextParts[0], evidence.Attachments[0].Parts[0]} {
		if _, _, err := r.ReadContent(t.Context(), owner.ID, reference.ID, session.MaxContentBytes); err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.ReadContent(t.Context(), other.ID, reference.ID, session.MaxContentBytes); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("foreign evidence leaked", err)
		}
	}
	if effects.Load() != 1 {
		t.Fatal(effects.Load())
	}
}

func TestMCPBothEnginesRootAndDelegatedChildUseSameLedger(t *testing.T) {
	isolateMCP(t)
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			url, _, effects := mcpHTTPFixture(t)
			code := `print(mcp.list_tools(server="fixture"))
print(mcp.call(server="fixture",tool="visible",arguments={}))`
			if engine == session.QuickJS {
				code = `console.log(await mcp.list_tools({server:"fixture"})); console.log(await mcp.call({server:"fixture",tool:"visible",arguments:{}}));`
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"root_call": code, "child_call": code}))
			owner := createEngineSession(t, r, engine)
			configureMCPFixture(t, r, url)
			if _, err := r.MCPRefresh(t.Context(), owner.ID); err != nil {
				t.Fatal(err)
			}
			manager := awaitMCPReady(t, r, owner)
			call, err := manager.ResolveTool("fixture", "visible")
			if err != nil {
				t.Fatal(err)
			}
			grant, err := r.CreateGrant(t.Context(), session.Grant{ID: "engine_call", SessionID: owner.ID, Capability: mcpCallCapability(call), Resource: mcpCallResource(owner.ID, call)})
			if err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, owner.ID, "root_call")
			finished := waitTestWithin(t, r, "root_call", terminal, 30*time.Second)
			if finished.Turn.State != session.Succeeded {
				t.Fatal(finished.Turn)
			}
			child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child_call"}, store.ChildRequest{ParentID: owner.ID, Parts: []session.Part{{Type: "text", Text: "child_call"}}, GrantIDs: []session.GrantID{grant.ID}, Overrides: session.ConfigPatch{ReportMode: new(session.ReportNotice)}})
			if err != nil {
				t.Fatal(err)
			}
			admission := waitTestWithin(t, r, "child_call", terminal, 30*time.Second)
			if admission.Turn.State != session.Succeeded {
				t.Fatal(admission.Turn)
			}
			operations, err := r.Operations(t.Context(), admission.Turn.ID, "", 20)
			if err != nil {
				t.Fatal(err)
			}
			if len(operations) != 2 || operations[0].SessionID != child.Session.ID {
				t.Fatal(operations)
			}
			for _, operation := range operations {
				if operation.Capability == "mcp.catalog" && strings.Contains(string(operation.Result.Value), "hidden") {
					t.Fatal("child metadata leaked hidden tool")
				}
			}
			if effects.Load() != 2 {
				t.Fatal("root/child dispatches", effects.Load())
			}
		})
	}
}
