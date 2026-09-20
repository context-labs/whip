package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/session"
)

type testDesktopProvider struct {
	store     *session.Store
	scope     capability.BrowserScope
	grant     capability.Reference
	lifetime  context.Context
	backend   *fakeBackend
	resolves  atomic.Int64
	executes  atomic.Int64
	lists     atomic.Int64
	revokeErr error
}

func (p *testDesktopProvider) ListTabs(context.Context, browser.DesktopIdentity) (browser.DesktopResult, error) {
	p.lists.Add(1)
	return browser.DesktopResult{Availability: "available", Tabs: []browser.DesktopTab{}}, nil
}

func TestDesktopBrowserDiscoveryRequiresModuleButNoControlAdmission(t *testing.T) {
	s, ledger, p, authority := desktopTestServices(t)
	s.SetGate(func(context.Context, GateRequest) (GateDecision, string) {
		t.Error("discovery requested control permission")
		return GateReject, ""
	})
	result := callDesktop(t, s, "browser.list_tabs", "{}")
	if result.Error != nil || result.Availability != "available" || p.lists.Load() != 1 || p.resolves.Load() != 0 || p.executes.Load() != 0 || ledger.begins.Load() != 0 {
		t.Fatalf("discovery crossed admission boundary: %+v", result)
	}
	authority.AgentID = "unrelated-child"
	if err := s.BindDispatcher(ledger, p.store.Workspaces(), p.store.Processes(), authority); err != nil {
		t.Fatal(err)
	}
	result = callDesktop(t, s, "browser.list_tabs", "{}")
	if result.Error == nil || result.Error.Kind != "permission_denied" || p.lists.Load() != 1 {
		t.Fatalf("ungranted discovery reached provider: %+v", result)
	}
}

func (p *testDesktopProvider) Resolve(_ context.Context, _ browser.DesktopIdentity, op string, args browser.DesktopArguments) (capability.BrowserCall, error) {
	p.resolves.Add(1)
	data, _ := json.Marshal(args)
	ref := p.grant
	if op == "browser.open" || op == "browser.attach" {
		ref = capability.Reference{}
	}
	return capability.BrowserCall{Grant: ref, Scope: p.scope, Arguments: data}, nil
}

func (p *testDesktopProvider) CallContext(capability.BrowserCall) (context.Context, error) {
	return p.lifetime, nil
}

func (p *testDesktopProvider) Execute(ctx context.Context, r browser.DesktopRequest, run func(context.Context, browser.Backend) (string, error)) (browser.DesktopResult, error) {
	p.executes.Add(1)
	result := browser.DesktopResult{AttachmentID: p.scope.AttachmentID, TabID: p.scope.TabID, DocumentRevision: "doc-1"}
	if r.OperationID == "" {
		return result, errors.New("missing admitted operation ID")
	}
	if r.Operation == "browser.open" || r.Operation == "browser.attach" {
		var err error
		p.grant, err = p.store.IssueBrowserCapability(ctx, r.Identity.RootID, r.Identity.AgentID, "", p.scope, capability.Reference{})
		return result, err
	}
	if run != nil {
		var err error
		result.Output, err = run(ctx, p.backend)
		return result, err
	}
	return result, nil
}

func (p *testDesktopProvider) Transfer(context.Context, browser.DesktopIdentity, browser.DesktopIdentity, []string) ([]browser.DesktopResult, error) {
	return nil, nil
}

func (p *testDesktopProvider) Attachments(context.Context, browser.DesktopIdentity) []browser.DesktopResult {
	return nil
}

func (p *testDesktopProvider) RevokeAgent(context.Context, browser.DesktopIdentity) error {
	return p.revokeErr
}

func TestRevokeDesktopAttachmentsHandlesUnavailableProvider(t *testing.T) {
	s := NewServices()
	if err := s.RevokeDesktopAttachments(t.Context()); err != nil {
		t.Fatalf("no provider: %v", err)
	}
	s.SetDesktopBrowserProvider(func() browser.DesktopProvider { return nil })
	if err := s.RevokeDesktopAttachments(t.Context()); err != nil {
		t.Fatalf("unavailable provider: %v", err)
	}
	want := errors.New("revoke failed")
	p := &testDesktopProvider{revokeErr: want}
	s.SetDesktopBrowserProvider(func() browser.DesktopProvider { return p })
	if err := s.RevokeDesktopAttachments(t.Context()); !errors.Is(err, want) {
		t.Fatalf("revocation error: got %v, want %v", err, want)
	}
}

func desktopTestServices(t *testing.T) (*Services, *countingLedger, *testDesktopProvider, capability.Authority) {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root, err := store.Create(session.SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	ledger := &countingLedger{Store: store}
	provider := &testDesktopProvider{store: store, lifetime: t.Context(), scope: capability.BrowserScope{ProviderID: "provider", ProviderEpoch: "epoch", TabID: "tab", TabGeneration: "tab-generation", ProfileID: "profile", AttachmentID: "attachment", AttachmentGeneration: "attachment-generation", Rights: []string{"create", "control"}}, backend: &fakeBackend{mode: browser.Mode("desktop"), eval: `"hello"`, shot: []byte("jpeg")}}
	services := NewServices()
	services.SetDesktopBrowserProvider(func() browser.DesktopProvider { return provider })
	services.SetGate(func(context.Context, GateRequest) (GateDecision, string) { return GateAllowOnce, "" })
	if err := services.BindDispatcher(ledger, store.Workspaces(), store.Processes(), authority); err != nil {
		t.Fatal(err)
	}
	return services, ledger, provider, authority
}

func callDesktop(t *testing.T, s *Services, operation, arguments string) browser.DesktopResult {
	t.Helper()
	result, err := s.RunDesktopBrowser(t.Context(), operation, json.RawMessage(arguments))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDesktopBrowserServicesAdmitResolvedScopeAndRunHelpers(t *testing.T) {
	s, ledger, p, authority := desktopTestServices(t)
	var prompts int
	s.SetGate(func(_ context.Context, request GateRequest) (GateDecision, string) {
		prompts++
		if p.executes.Load() != 0 {
			t.Error("native effect before approval")
		}
		for _, detail := range []string{"epoch", "tab-generation", "profile", "https://example.com"} {
			if !strings.Contains(request.Command, detail) {
				t.Errorf("permission summary omitted %s: %s", detail, request.Command)
			}
		}
		if strings.Contains(request.Command, `"grant"`) {
			t.Fatal("permission summary exposed raw grant envelope")
		}
		return GateAllowOnce, ""
	})
	opened := callDesktop(t, s, "open", `{"url":"https://example.com"}`)
	if opened.Error != nil || opened.AttachmentID != "attachment" {
		t.Fatalf("open: %+v", opened)
	}
	admission := ledger.lastAdmission()
	if admission.Request.Operation != "browser.open" || admission.Request.CapabilityID != authority.Shell.ID {
		t.Fatalf("wrong creation authority: %+v", admission)
	}
	var admittedCall capability.BrowserCall
	if err := json.Unmarshal(admission.Request.Arguments, &admittedCall); err != nil {
		t.Fatal(err)
	}
	if admittedCall.Scope.ProviderEpoch != "epoch" || admittedCall.Scope.TabGeneration != "tab-generation" {
		t.Fatalf("admission omitted immutable scope: %+v", admittedCall.Scope)
	}
	var shots int
	s.SetScreenshotSink(func(data [][]byte) { shots += len(data) })
	s.SetMCPAttachmentStore(func(_ context.Context, mime string, data []byte) (string, error) {
		if mime != "image/jpeg" || string(data) != "jpeg" {
			t.Error("bad media")
		}
		return "artifact:scoped-image", nil
	})
	result := callDesktop(t, s, "run", `{"attachment_id":"attachment","code":"print(js(\"document.title\")); screenshot()"}`)
	if result.Error != nil || !strings.Contains(result.Output, "hello") || len(result.Media) != 1 || shots != 1 {
		t.Fatalf("helper run/media: %+v shots=%d", result, shots)
	}
	if ledger.begins.Load() != 2 || prompts != 1 || p.executes.Load() != 2 {
		t.Fatalf("duplicate/missing admissions or prompt: %d/%d/%d", ledger.begins.Load(), prompts, p.executes.Load())
	}
	if ledger.lastAdmission().Request.CapabilityID != p.grant.ID {
		t.Fatal("run used module grant instead of browser resource")
	}
}

func TestDesktopBrowserServicesDenyWithoutNativeEffect(t *testing.T) {
	for _, mode := range []string{"deny", "always", "headless", "no-ui", "module-revoked", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			s, ledger, p, authority := desktopTestServices(t)
			switch mode {
			case "deny":
				s.SetGate(func(context.Context, GateRequest) (GateDecision, string) { return GateReject, "no" })
			case "always":
				s.SetGate(func(context.Context, GateRequest) (GateDecision, string) { return GateAllowAlways, "" })
			case "headless":
				s.SetHeadlessPermissions(true)
			case "no-ui":
				s.SetGate(nil)
			case "module-revoked":
				if err := ledger.RevokeCapability(t.Context(), authority.Shell.ID); err != nil {
					t.Fatal(err)
				}
			case "unavailable":
				s.SetDesktopBrowserProvider(nil)
			}
			result := callDesktop(t, s, "open", `{"url":"https://example.com"}`)
			if result.Error == nil || p.executes.Load() != 0 {
				t.Fatalf("not denied before native effect: %+v", result)
			}
			if (mode == "module-revoked" || mode == "unavailable") && p.resolves.Load() != 0 {
				t.Fatal("resolved after module denial or unavailability")
			}
		})
	}
}

func TestDesktopBrowserServicesRecheckResourceAndModule(t *testing.T) {
	for _, revoke := range []string{"resource", "module", "scope"} {
		t.Run(revoke, func(t *testing.T) {
			s, ledger, p, authority := desktopTestServices(t)
			if result := callDesktop(t, s, "open", `{"url":"https://example.com"}`); result.Error != nil {
				t.Fatal(result.Error)
			}
			switch revoke {
			case "resource":
				if err := ledger.RevokeCapability(t.Context(), p.grant.ID); err != nil {
					t.Fatal(err)
				}
			case "module":
				if err := ledger.RevokeCapability(t.Context(), authority.Shell.ID); err != nil {
					t.Fatal(err)
				}
			case "scope":
				p.scope.ProviderEpoch = "replacement"
			}
			result := callDesktop(t, s, "run", `{"attachment_id":"attachment","code":"print(info())"}`)
			if result.Error == nil || result.Error.Kind != "permission_denied" || p.executes.Load() != 1 {
				t.Fatalf("stale authority executed: %+v", result)
			}
		})
	}
}

func TestDesktopBrowserServicesDisconnectCancelsPermission(t *testing.T) {
	s, _, p, _ := desktopTestServices(t)
	lifetime, cancel := context.WithCancel(t.Context())
	defer cancel()
	p.lifetime = lifetime
	prompted := make(chan struct{})
	s.SetGate(func(ctx context.Context, _ GateRequest) (GateDecision, string) {
		close(prompted)
		<-ctx.Done()
		return GateAllowOnce, "late"
	})
	done := make(chan browser.DesktopResult, 1)
	go func() {
		result, _ := s.RunDesktopBrowser(t.Context(), "open", json.RawMessage(`{"url":"https://example.com"}`))
		done <- result
	}()
	select {
	case <-prompted:
	case <-time.After(3 * time.Second):
		t.Fatal("permission did not start")
	}
	cancel()
	select {
	case result := <-done:
		if result.Error == nil || p.executes.Load() != 0 {
			t.Fatalf("late permission survived disconnect: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("disconnect did not cancel permission")
	}
}

func TestDesktopBrowserMediaStoreFailureIsExplicit(t *testing.T) {
	s, _, _, _ := desktopTestServices(t)
	if result := callDesktop(t, s, "open", `{"url":"https://example.com"}`); result.Error != nil {
		t.Fatal(result.Error)
	}
	s.SetMCPAttachmentStore(func(context.Context, string, []byte) (string, error) {
		return "", errors.New("content store unavailable")
	})
	result := callDesktop(t, s, "run", `{"attachment_id":"attachment","code":"screenshot()"}`)
	if result.Error == nil || !strings.Contains(result.Error.Message, "content store unavailable") {
		t.Fatalf("lost content error: %+v", result)
	}
}

func TestDesktopBrowserMCPToolSurfaceAndStrictSelectors(t *testing.T) {
	s, _, p, _ := desktopTestServices(t)
	defs, err := s.ToolDefinitions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, def := range defs {
		names[def.Function.Name] = true
	}
	for name := range desktopToolOperations {
		if !names[name] {
			t.Fatalf("missing MCP tool %s", name)
		}
	}
	data, err := s.CallTool(t.Context(), "browser_open", json.RawMessage(`{"url":"https://example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	var result browser.DesktopResult
	if err := json.Unmarshal([]byte(data), &result); err != nil || result.Error != nil {
		t.Fatalf("MCP open %s: %v", data, err)
	}
	for _, args := range []string{`{"attachment_id":"attachment","session":"legacy","code":"print(info())"}`, `{"attachment_id":"attachment","provider_id":"forged","code":"print(info())"}`, `null`, `{} {}`, `{"attachment_id":"attachment","code":""}`} {
		result := callDesktop(t, s, "run", args)
		if result.Error == nil || result.Error.Kind != "invalid_arguments" {
			t.Fatalf("accepted selector injection %s: %+v", args, result)
		}
	}
	if p.executes.Load() != 1 {
		t.Fatal("invalid invocation reached provider")
	}
}
