package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
)

type recursiveBrowserProvider struct {
	mu              sync.Mutex
	operations      []string
	identities      []browser.DesktopIdentity
	attachments     map[string][]browser.DesktopResult
	revoked         []string
	transferStarted chan string
	transferRelease chan struct{}
	transferErr     error
}

func (p *recursiveBrowserProvider) Resolve(_ context.Context, identity browser.DesktopIdentity, operation string, _ browser.DesktopArguments) (capability.BrowserCall, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.operations = append(p.operations, operation)
	p.identities = append(p.identities, identity)
	return capability.BrowserCall{}, errors.New("desktop resolver reached")
}

func (p *recursiveBrowserProvider) ListTabs(_ context.Context, identity browser.DesktopIdentity) (browser.DesktopResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.operations = append(p.operations, "browser.list_tabs")
	p.identities = append(p.identities, identity)
	return browser.DesktopResult{}, errors.New("desktop inventory reached")
}

func (*recursiveBrowserProvider) CallContext(capability.BrowserCall) (context.Context, error) {
	return nil, errors.New("unexpected browser call context")
}

func (*recursiveBrowserProvider) Execute(context.Context, browser.DesktopRequest, func(context.Context, browser.Backend) (string, error)) (browser.DesktopResult, error) {
	return browser.DesktopResult{}, errors.New("unexpected browser execution")
}

func (p *recursiveBrowserProvider) Attachments(_ context.Context, identity browser.DesktopIdentity) []browser.DesktopResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.attachments[identity.AgentID])
}

func (p *recursiveBrowserProvider) RevokeAgent(_ context.Context, identity browser.DesktopIdentity) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revoked = append(p.revoked, identity.AgentID)
	delete(p.attachments, identity.AgentID)
	return nil
}

func (p *recursiveBrowserProvider) Transfer(ctx context.Context, parent, child browser.DesktopIdentity, ids []string) ([]browser.DesktopResult, error) {
	if p.transferStarted != nil {
		p.transferStarted <- child.AgentID
	}
	if p.transferRelease != nil {
		select {
		case <-p.transferRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.transferErr != nil {
		return nil, p.transferErr
	}
	result := []browser.DesktopResult{{AttachmentID: "child-" + ids[0], TabID: "tab", DocumentRevision: "doc", Title: "page title", URL: "https://example.test", SupportedOperations: []string{"browser.run", "browser.detach"}}}
	p.attachments[child.AgentID] = result
	delete(p.attachments, parent.AgentID)
	return slices.Clone(result), nil
}

func TestRecursiveBrowserRoutesBothEnginesWithoutFallback(t *testing.T) {
	for _, engine := range []string{rlm.EngineStarlark, rlm.EngineQuickJS} {
		t.Run(engine, func(t *testing.T) {
			_, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.test", "key"), 2, engine)
			provider := &recursiveBrowserProvider{}
			runtime.rootNode.agent.Services.SetDesktopBrowserProvider(func() browser.DesktopProvider { return provider })
			for _, tc := range []struct{ op, starlark, javascript string }{
				{"open", `browser.open(url="https://example.test")`, `await browser.open({url:"https://example.test"})`},
				{"attach", `browser.attach(tab_id="tab")`, `await browser.attach({tab_id:"tab"})`},
				{"run", `browser.run(attachment_id="attachment",code="info()")`, `await browser.run({attachment_id:"attachment",code:"info()"})`},
				{"detach", `browser.detach(attachment_id="attachment")`, `await browser.detach({attachment_id:"attachment"})`},
				{"allow_preview_port", `browser.allow_preview_port(attachment_id="attachment",port=3000)`, `await browser.allow_preview_port({attachment_id:"attachment",port:3000})`},
			} {
				code := tc.starlark
				if engine == rlm.EngineQuickJS {
					code = tc.javascript
				}
				result, err := runtime.rootNode.kernel.Exec(t.Context(), "print("+code+")")
				if err != nil || !strings.Contains(result.Output, "desktop resolver reached") {
					t.Fatalf("%s did not route to desktop resolver: %+v %v", tc.op, result, err)
				}
			}
			code := `print(browser.list_tabs())`
			if engine == rlm.EngineQuickJS {
				code = `print(await browser.list_tabs())`
			}
			result, err := runtime.rootNode.kernel.Exec(t.Context(), code)
			if err != nil || !strings.Contains(result.Output, "desktop inventory reached") {
				t.Fatalf("list_tabs did not route to desktop inventory: %+v %v", result, err)
			}
			provider.mu.Lock()
			if len(provider.operations) != 6 {
				t.Fatalf("operations=%v", provider.operations)
			}
			for _, identity := range provider.identities {
				if identity.RootID != root.ID() || identity.AgentID != root.AgentID() {
					t.Errorf("wrong identity: %+v", identity)
				}
			}
			provider.mu.Unlock()
			if _, err := runtime.rootNode.host.Call(t.Context(), "browser", "run", map[string]any{"session": "legacy", "attachment_id": "attachment", "code": "info()"}); err == nil || !strings.Contains(err.Error(), "never both") {
				t.Fatalf("mixed selectors allowed: %v", err)
			}
			if _, err := runtime.rootNode.host.Call(t.Context(), "browser", "run", map[string]any{"session": "legacy", "code": "info()"}); err == nil || strings.Contains(err.Error(), "desktop resolver reached") {
				t.Fatalf("legacy run routed to Desktop: %v", err)
			}
		})
	}
}

func TestRecursiveBrowserTransferBeforeChildWorkAndRollback(t *testing.T) {
	for _, engine := range []string{rlm.EngineStarlark, rlm.EngineQuickJS} {
		for _, fail := range []bool{false, true} {
			name := engine + "/success"
			if fail {
				name = engine + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { streamText(w, "done") }))
				defer server.Close()
				_, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 3, engine)
				provider := &recursiveBrowserProvider{attachments: map[string][]browser.DesktopResult{}, transferStarted: make(chan string, 1), transferRelease: make(chan struct{})}
				if fail {
					provider.transferErr = errors.New("native handoff refused")
				}
				runtime.rootNode.agent.Services.SetDesktopBrowserProvider(func() browser.DesktopProvider { return provider })
				runs := &sync.Map{}
				runtime.setRunTurnHook(observeRunTurn(runs))
				type outcome struct {
					result any
					err    error
				}
				done := make(chan outcome, 1)
				go func() {
					result, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "browser-child", "prompt": "inspect page", "browser_attachments": []any{"parent-attachment"}, "report": "message"})
					done <- outcome{result, err}
				}()
				var childID string
				select {
				case childID = <-provider.transferStarted:
				case <-time.After(5 * time.Second):
					t.Fatal("transfer never started")
				}
				if runTurnCount(runs, childID) != 0 {
					t.Fatal("child ran before native transfer acknowledgment")
				}
				runtime.mu.RLock()
				published := runtime.agents[childID] != nil
				runtime.mu.RUnlock()
				if published {
					t.Fatal("tentative child published before native transfer")
				}
				close(provider.transferRelease)
				var got outcome
				select {
				case got = <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("spawn did not settle")
				}
				if fail {
					if got.err == nil || !strings.Contains(got.err.Error(), "native handoff refused") {
						t.Fatalf("spawn=%+v", got)
					}
					relatives, err := root.ListAgentRelatives(t.Context(), root.AgentID())
					if err != nil {
						t.Fatal(err)
					}
					for _, child := range relatives.Children {
						if child.ID == childID && child.Status != "deleted" {
							t.Fatalf("tentative child survived: %+v", child)
						}
					}
					if runTurnCount(runs, childID) != 0 {
						t.Fatal("failed transfer launched child")
					}
					return
				}
				if got.err != nil {
					t.Fatal(got.err)
				}
				metadata := got.result.(map[string]any)["browser_attachments"].([]browser.DesktopResult)
				if len(metadata) != 1 || metadata[0].AttachmentID != "child-parent-attachment" {
					t.Fatalf("metadata=%+v", metadata)
				}
				waitRunTurn(t, runs, childID, 1)
				runtime.mu.RLock()
				child := runtime.agents[childID]
				runtime.mu.RUnlock()
				input, err := child.host.focusInput(t.Context(), "inspect")
				if err != nil || input != "inspect" {
					t.Fatalf("input=%q err=%v", input, err)
				}
				inspection, err := runtime.inspect(t.Context(), runtime.rootNode, childID, false)
				if err != nil || len(inspection.(map[string]any)["browser_attachments"].([]browser.DesktopResult)) != 1 {
					t.Fatalf("inspection=%+v err=%v", inspection, err)
				}
				if _, err := runtime.terminalize(t.Context(), runtime.rootNode, childID, "stop"); err != nil {
					t.Fatal(err)
				}
				provider.mu.Lock()
				revoked := slices.Contains(provider.revoked, childID)
				provider.mu.Unlock()
				if !revoked {
					t.Fatal("stopped child attachments were not revoked")
				}
			})
		}
	}
}

func TestRecursiveBrowserAttachmentsAreNotImplicitlyInherited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { streamText(w, "done") }))
	defer server.Close()
	_, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 2)
	provider := &recursiveBrowserProvider{attachments: map[string][]browser.DesktopResult{
		root.AgentID(): {{AttachmentID: "parent-only", TabID: "tab"}},
	}}
	runtime.rootNode.agent.Services.SetDesktopBrowserProvider(func() browser.DesktopProvider { return provider })
	result, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "plain-child", "prompt": "work", "report": "message"})
	if err != nil {
		t.Fatal(err)
	}
	if attachments := result.(map[string]any)["browser_attachments"].([]browser.DesktopResult); len(attachments) != 0 {
		t.Fatalf("child inherited attachments: %+v", attachments)
	}
	if attachments := runtime.rootNode.desktopAttachments(t.Context()); len(attachments) != 1 || attachments[0].AttachmentID != "parent-only" {
		t.Fatalf("parent lost undelegated attachment: %+v", attachments)
	}
}

func TestToolRunnerDesktopLifecycleUsesBoundIdentity(t *testing.T) {
	_, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.test", "key"), 2)
	provider := &recursiveBrowserProvider{}
	runtime.rootNode.agent.Services.SetDesktopBrowserProvider(func() browser.DesktopProvider { return provider })
	runner := &toolRunner{services: runtime.rootNode.agent.Services}
	runner.DenyToolPermissions()
	definitions, err := runner.ToolDefinitions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, definition := range definitions {
		names = append(names, definition.Function.Name)
	}
	for _, name := range []string{"browser_open", "browser_attach", "browser_run", "browser_detach", "browser_allow_preview_port"} {
		if !slices.Contains(names, name) {
			t.Errorf("MCP schema omitted %s", name)
		}
	}
	result, err := runner.CallTool(t.Context(), "browser_open", json.RawMessage(`{"url":"https://example.test"}`))
	if err != nil || !strings.Contains(result, "desktop resolver reached") {
		t.Fatalf("MCP browser route: %q %v", result, err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.identities) != 1 || provider.identities[0].AgentID != root.AgentID() || provider.identities[0].RootID != root.ID() {
		t.Fatalf("tool host used an unbound identity: %+v", provider.identities)
	}
}

func TestSpawnBrowserAttachmentsBoundedAndExplicit(t *testing.T) {
	parent := &AgentSession{definition: agentdef.Coding(), capabilities: []string{"read", "write", "shell", "browser", "computer", "mcp"}}
	for _, ids := range [][]any{{"same", "same"}, {""}, {"1", "2", "3", "4", "5"}} {
		request, err := parseSpawnRequest("child", "task", map[string]any{"browser_attachments": ids})
		if err == nil {
			_, err = resolveSpawn(parent, request)
		}
		if err == nil {
			t.Fatalf("invalid browser attachments accepted: %v", ids)
		}
	}
	request, err := parseSpawnRequest("child", "task", map[string]any{"browser_attachments": []any{"attachment"}, "capabilities": []any{"read"}})
	if err == nil {
		_, err = resolveSpawn(parent, request)
	}
	if err == nil {
		t.Fatal("browser transfer to read-only child accepted")
	}
	request, err = parseSpawnRequest("child", "task", nil)
	if err != nil || request.BrowserAttachments != nil {
		t.Fatalf("attachments implicitly inherited: %+v %v", request, err)
	}
}

func TestBrowserAttachmentContextIsBounded(t *testing.T) {
	values := make([]browser.DesktopResult, 10)
	for i := range values {
		values[i] = browser.DesktopResult{AttachmentID: "attachment", Title: strings.Repeat("x", 10000), URL: strings.Repeat("u", 10000), Output: strings.Repeat("secret page contents", 1000), Media: []string{"large media"}}
	}
	bounded := boundedDesktopAttachments(values)
	raw, err := json.Marshal(bounded)
	if err != nil || len(bounded) != 4 || len(raw) > 16<<10 || strings.Contains(string(raw), "secret page contents") {
		t.Fatalf("unbounded context: entries=%d bytes=%d err=%v", len(bounded), len(raw), err)
	}
}
