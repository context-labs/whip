package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/session"
)

type testMCPProvider struct {
	mu          sync.Mutex
	descriptor  capability.MCPCall
	validateErr error
	callErr     error
	lifetime    context.Context
	queued      chan struct{}
	proceed     chan struct{}
	effects     atomic.Int64
}

func (p *testMCPProvider) ResolveTool(server, tool string) (capability.MCPCall, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if server != p.descriptor.Server || tool != p.descriptor.Tool {
		return capability.MCPCall{}, errors.New("unknown exact tool")
	}
	return p.descriptor, nil
}

func (p *testMCPProvider) ValidateArguments(capability.MCPCall) error { return p.validateErr }

func (p *testMCPProvider) CallContext(capability.MCPCall) (context.Context, error) {
	if p.lifetime != nil {
		return p.lifetime, nil
	}
	return context.Background(), nil
}

func (p *testMCPProvider) CallChecked(ctx context.Context, call capability.MCPCall, before func(context.Context) error) (string, error) {
	if p.queued != nil {
		close(p.queued)
		select {
		case <-p.proceed:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if err := before(ctx); err != nil {
		return "", err
	}
	p.effects.Add(1)
	return "remote result", p.callErr
}

func newMCPServices(t *testing.T) (*Services, *countingLedger, *testMCPProvider, capability.Authority) {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID, err := store.Create(session.SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(t.Context(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	ledger := &countingLedger{Store: store}
	provider := &testMCPProvider{descriptor: capability.MCPCall{
		MCPSelector: capability.MCPSelector{Server: "my-server", Tool: "write.raw", Definition: "definition-1"},
		Generation:  "connection-1", Source: "imported Claude config",
	}}
	services := NewServices()
	services.SetMCPProvider(func() MCPProvider { return provider })
	if err := services.BindDispatcher(ledger, store.Workspaces(), store.Processes(), authority); err != nil {
		t.Fatal(err)
	}
	return services, ledger, provider, authority
}

func rememberMCP(t *testing.T, ledger *countingLedger, authority capability.Authority, call capability.MCPCall) {
	t.Helper()
	arguments, err := json.Marshal(call)
	if err != nil {
		t.Fatal(err)
	}
	_, rules, ok := capability.PermissionRule("mcp.call", arguments, "")
	if !ok || len(rules) != 1 {
		t.Fatalf("MCP rule = %q, ok=%v", rules, ok)
	}
	if _, err := ledger.AddPermissionRule(t.Context(), authority.RootID, "mcp.call", rules[0], "test-user"); err != nil {
		t.Fatal(err)
	}
}

func assertMCPSettled(t *testing.T, ledger *countingLedger, authority capability.Authority) {
	t.Helper()
	budgets, err := ledger.InspectBudgets(t.Context(), authority.RootID, authority.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range budgets {
		if budget.Kind == session.BudgetActiveOperations && (budget.Used != 0 || budget.Reserved != 0) {
			t.Errorf("active operation capacity leaked: %+v", budget)
		}
	}
	pending, err := ledger.ListPendingPermissions(t.Context(), authority.RootID)
	if err != nil || len(pending) != 0 {
		t.Errorf("pending permissions = %+v, error=%v", pending, err)
	}
}

func TestMCPConsentPolicy(t *testing.T) {
	for _, test := range []struct {
		name                                   string
		trusted, external, headless, automatic bool
		remembered, rejectingGate, localHuman  bool
		wantAllow                              bool
	}{
		{name: "native", trusted: true, external: true, wantAllow: true},
		{name: "imported nil gate"},
		{name: "direct human is not remote authority", localHuman: true},
		{name: "headless unapproved", external: true, headless: true},
		{name: "headless native", trusted: true, external: true, headless: true, wantAllow: true},
		{name: "headless saved rule", external: true, headless: true, remembered: true, wantAllow: true},
		{name: "headless explicit automatic", external: true, headless: true, automatic: true, wantAllow: true},
		{name: "automatic", automatic: true, wantAllow: true},
		{name: "native explicit veto", trusted: true, rejectingGate: true},
		{name: "saved rule explicit veto", remembered: true, rejectingGate: true},
		{name: "automatic explicit veto", automatic: true, rejectingGate: true},
		{name: "headless gate never called", trusted: true, headless: true, rejectingGate: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			services, ledger, provider, authority := newMCPServices(t)
			provider.descriptor.Trusted = test.trusted
			services.SetExternalPermissions(test.external)
			services.SetHeadlessPermissions(test.headless)
			services.SetMCPAutomatic(test.automatic)
			var gateCalls int
			if test.rejectingGate {
				services.SetGate(func(context.Context, GateRequest) (GateDecision, string) {
					gateCalls++
					return GateReject, "explicit veto"
				})
			}
			if test.remembered {
				rememberMCP(t, ledger, authority, provider.descriptor)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if test.localHuman {
				ctx = WithLocalHuman(ctx)
			}
			out, err := services.InvokeMCP(ctx, "my-server", "write.raw", json.RawMessage(`{"path":"remote.txt"}`))
			if test.wantAllow {
				if err != nil || out != "remote result" || provider.effects.Load() != 1 {
					t.Fatalf("call output=%q error=%v effects=%d", out, err, provider.effects.Load())
				}
			} else if !errors.Is(err, capability.ErrDenied) || provider.effects.Load() != 0 {
				t.Fatalf("denied call error=%v effects=%d", err, provider.effects.Load())
			}
			if test.headless && gateCalls != 0 {
				t.Fatal("headless call invoked an opaque gate")
			}
			admission := ledger.lastAdmission()
			if admission.Request.Operation != "mcp.call" || admission.Mutation != capability.MutationNone || admission.Request.WriterCapabilityID != "" || admission.Request.CapabilityID != authority.MCP.ID {
				t.Fatalf("MCP used wrong dispatcher authority: %+v", admission)
			}
			assertMCPSettled(t, ledger, authority)
		})
	}
}

func TestMCPRememberedConsentBindsDefinitionAcrossReconnects(t *testing.T) {
	services, ledger, provider, authority := newMCPServices(t)
	var requests []GateRequest
	services.SetGate(func(_ context.Context, request GateRequest) (GateDecision, string) {
		requests = append(requests, request)
		return GateAllowOnce, ""
	})
	if _, err := services.InvokeMCP(t.Context(), "my-server", "write.raw", json.RawMessage(`{"body":"approved content"}`)); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || !strings.Contains(requests[0].Command, "imported Claude config") || !strings.Contains(requests[0].Command, `"body":"approved content"`) {
		t.Fatalf("consent did not show source and inner arguments: %+v", requests)
	}
	rememberMCP(t, ledger, authority, provider.descriptor)
	rules, err := ledger.ListPermissionRules(t.Context(), authority.RootID)
	if err != nil || len(rules) != 1 || strings.Contains(rules[0].Rule, "connection-1") {
		t.Fatalf("remembered rules=%+v error=%v", rules, err)
	}
	services.SetGate(nil)
	services.SetHeadlessPermissions(true)
	provider.descriptor.Generation = "connection-2"
	if _, err := services.InvokeMCP(t.Context(), "my-server", "write.raw", json.RawMessage(`{"body":"next content"}`)); err != nil {
		t.Fatalf("unchanged tool on reconnect lost approval: %v", err)
	}
	provider.descriptor.Definition = "definition-2"
	if _, err := services.InvokeMCP(t.Context(), "my-server", "write.raw", nil); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("changed definition reused approval: %v", err)
	}
	provider.descriptor.Definition = "definition-1"
	provider.descriptor.Tool = "write_raw"
	if _, err := services.InvokeMCP(t.Context(), "my-server", "write_raw", nil); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("sanitized tool collision reused approval: %v", err)
	}
	if provider.effects.Load() != 2 {
		t.Fatalf("effects=%d, want 2", provider.effects.Load())
	}
	assertMCPSettled(t, ledger, authority)
}

func TestMCPRevalidatesAfterQueue(t *testing.T) {
	for _, change := range []string{"revoke", "gate", "headless", "automatic", "manager", "definition", "cancel"} {
		t.Run(change, func(t *testing.T) {
			services, ledger, provider, authority := newMCPServices(t)
			provider.descriptor.Trusted = true
			provider.queued, provider.proceed = make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, err := services.InvokeMCP(ctx, "my-server", "write.raw", nil)
				result <- err
			}()
			select {
			case <-provider.queued:
			case <-time.After(3 * time.Second):
				t.Fatal("call never reached server queue")
			}
			switch change {
			case "revoke":
				if err := ledger.RevokeCapability(t.Context(), authority.MCP.ID); err != nil {
					t.Fatal(err)
				}
			case "gate":
				services.SetGate(func(context.Context, GateRequest) (GateDecision, string) {
					t.Error("changed gate must not be prompted under server queue")
					return GateReject, "denied"
				})
			case "headless":
				services.SetHeadlessPermissions(true)
			case "automatic":
				services.SetMCPAutomatic(true)
			case "manager":
				replacement := &testMCPProvider{descriptor: provider.descriptor}
				replacement.descriptor.Generation = "replacement"
				services.SetMCPProvider(func() MCPProvider { return replacement })
			case "definition":
				provider.mu.Lock()
				provider.descriptor.Definition = "changed-definition"
				provider.mu.Unlock()
			case "cancel":
				cancel()
			}
			close(provider.proceed)
			select {
			case err := <-result:
				if err == nil || provider.effects.Load() != 0 {
					t.Fatalf("queued call error=%v effects=%d", err, provider.effects.Load())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("queued call did not terminate")
			}
			assertMCPSettled(t, ledger, authority)
		})
	}
}

func TestMCPRejectsForgedEnvelopeAndInvalidArgumentsBeforeAdmission(t *testing.T) {
	services, ledger, provider, _ := newMCPServices(t)
	if _, err := services.Invoke(t.Context(), "mcp.call", json.RawMessage(`{"trusted":true}`)); err == nil {
		t.Fatal("generic Invoke accepted a forged MCP envelope")
	}
	for _, arguments := range []string{`null`, `[]`, `{`, `{} {}`} {
		if _, err := services.InvokeMCP(t.Context(), "my-server", "write.raw", json.RawMessage(arguments)); err == nil {
			t.Errorf("accepted malformed arguments %q", arguments)
		}
	}
	provider.validateErr = errors.New("schema requires body")
	if _, err := services.InvokeMCP(t.Context(), "my-server", "write.raw", nil); !errors.Is(err, provider.validateErr) {
		t.Fatalf("schema validation error=%v", err)
	}
	if provider.effects.Load() != 0 || ledger.begins.Load() != 0 {
		t.Fatal("invalid call reached admission or transmission")
	}
}

func TestMCPChildWithoutCapabilityCannotCallTrustedTool(t *testing.T) {
	services, ledger, provider, authority := newMCPServices(t)
	provider.descriptor.Trusted = true
	if _, err := ledger.AdmitAgent(t.Context(), session.AgentAdmission{
		RootID: authority.RootID, ParentAgentID: authority.AgentID, ChildAgentID: "reader",
	}); err != nil {
		t.Fatal(err)
	}
	childAuthority := capability.Authority{
		RootID: authority.RootID, AgentID: "reader",
	}
	clone, err := services.CloneForAuthority(ledger, ledger.Workspaces(), ledger.Processes(), childAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clone.InvokeMCP(t.Context(), "my-server", "write.raw", nil); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("child lacking MCP authority error=%v", err)
	}
	if provider.effects.Load() != 0 {
		t.Fatal("child call reached the remote tool")
	}
}

func TestMCPOnlyRestoredAuthorityBindsWithoutLocalGrants(t *testing.T) {
	services, ledger, provider, authority := newMCPServices(t)
	provider.descriptor.Trusted = true
	if _, err := ledger.AdmitAgent(t.Context(), session.AgentAdmission{
		RootID: authority.RootID, ParentAgentID: authority.AgentID, ChildAgentID: "remote-worker",
		Capabilities: []session.CapabilityDelegation{{
			ID: "remote-worker-mcp", AgentID: "remote-worker", Issuer: authority.MCP, Operations: []string{"mcp.call"},
			MCP: []capability.MCPSelector{provider.descriptor.MCPSelector},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	restored, _, err := ledger.LoadAgentAuthority(t.Context(), authority.RootID, "remote-worker")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Files.ID != "" || restored.Shell.ID != "" || restored.MCP.ID == "" {
		t.Fatalf("restored capability set=%+v", restored)
	}
	clone, err := services.CloneForAuthority(ledger, ledger.Workspaces(), ledger.Processes(), restored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clone.InvokeMCP(t.Context(), "my-server", "write.raw", nil); err != nil {
		t.Fatalf("restored MCP-only child call error=%v", err)
	}
	for _, operation := range []string{"read", "write", "bash"} {
		if _, err := clone.Invoke(t.Context(), operation, json.RawMessage(`{"path":"missing","command":"printf unwanted"}`)); !errors.Is(err, capability.ErrDenied) {
			t.Errorf("MCP-only child %s error=%v", operation, err)
		}
	}
	if provider.effects.Load() != 1 {
		t.Fatalf("remote effects=%d", provider.effects.Load())
	}
	assertMCPSettled(t, ledger, authority)
}

func TestMCPExternalConsentSettlesPendingOperations(t *testing.T) {
	for _, action := range []string{"allow", "reject", "cancel", "changed gate", "external disabled", "headless", "automatic", "generation retired"} {
		t.Run(action, func(t *testing.T) {
			services, ledger, provider, authority := newMCPServices(t)
			services.SetExternalPermissions(true)
			lifetime, retire := context.WithCancel(t.Context())
			defer retire()
			provider.lifetime = lifetime
			ctx, cancel := context.WithCancel(WithLocalHuman(t.Context()))
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, err := services.InvokeMCP(ctx, "my-server", "write.raw", nil)
				result <- err
			}()
			var permissionID string
			deadline := time.After(3 * time.Second)
			tick := time.NewTicker(time.Millisecond)
			defer tick.Stop()
			for permissionID == "" {
				pending, err := ledger.ListPendingPermissions(t.Context(), authority.RootID)
				if err != nil {
					t.Fatal(err)
				}
				if len(pending) == 1 {
					permissionID = pending[0].ID
					break
				}
				select {
				case <-deadline:
					t.Fatal("external MCP consent was not persisted")
				case <-tick.C:
				}
			}
			if provider.effects.Load() != 0 {
				t.Fatal("pending remote call transmitted before consent")
			}
			for {
				services.mu.RLock()
				waiting := services.permissionWaiters[permissionID] != nil
				services.mu.RUnlock()
				if waiting {
					break
				}
				select {
				case <-deadline:
					t.Fatal("external MCP consent did not register its waiter")
				case <-tick.C:
				}
			}
			switch action {
			case "cancel":
				cancel()
			case "changed gate":
				services.SetGate(func(context.Context, GateRequest) (GateDecision, string) { return GateReject, "deny" })
			case "external disabled":
				services.SetExternalPermissions(false)
			case "headless":
				services.SetHeadlessPermissions(true)
			case "automatic":
				services.SetMCPAutomatic(true)
			case "generation retired":
				retire()
			default:
				if err := services.ResolvePermission(permissionID, capability.Decision{Allow: action != "reject", PrincipalID: "paired-client"}); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-result:
				if action == "allow" {
					if err != nil || provider.effects.Load() != 1 {
						t.Fatalf("approved call error=%v effects=%d", err, provider.effects.Load())
					}
				} else if err == nil || provider.effects.Load() != 0 {
					t.Fatalf("unapproved call error=%v effects=%d", err, provider.effects.Load())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("permission completion did not unblock call")
			}
			assertMCPSettled(t, ledger, authority)
			services.mu.RLock()
			waiters, early := len(services.permissionWaiters), len(services.permissionEarly)
			services.mu.RUnlock()
			if waiters != 0 || early != 0 {
				t.Fatalf("permission state leaked: waiters=%d early=%d", waiters, early)
			}
		})
	}
}

type pausedMCPLedger struct {
	*countingLedger
	after   bool
	entered chan context.Context
	resume  chan struct{}
}

func (l *pausedMCPLedger) Begin(ctx context.Context, admission capability.Admission) (capability.Ticket, error) {
	if !l.after {
		l.entered <- ctx
		<-l.resume
	}
	ticket, err := l.countingLedger.Begin(ctx, admission)
	if l.after && err == nil {
		l.entered <- ctx
		<-l.resume
	}
	return ticket, err
}

func TestMCPGenerationRetirementDuringAdmission(t *testing.T) {
	for _, stage := range []string{"before begin", "permission persisted", "operation running"} {
		t.Run(stage, func(t *testing.T) {
			_, ledger, provider, authority := newMCPServices(t)
			provider.descriptor.Trusted = stage == "operation running"
			lifetime, retire := context.WithCancel(t.Context())
			defer retire()
			provider.lifetime = lifetime
			paused := &pausedMCPLedger{
				countingLedger: ledger, after: stage != "before begin",
				entered: make(chan context.Context, 1), resume: make(chan struct{}),
			}
			services := NewServices()
			services.SetExternalPermissions(true)
			services.SetMCPProvider(func() MCPProvider { return provider })
			if err := services.BindDispatcher(paused, ledger.Workspaces(), ledger.Processes(), authority); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := services.InvokeMCP(t.Context(), "my-server", "write.raw", nil)
				result <- err
			}()
			var callCtx context.Context
			select {
			case callCtx = <-paused.entered:
			case <-time.After(3 * time.Second):
				close(paused.resume)
				t.Fatal("call never reached paused admission")
			}
			pending, err := ledger.ListPendingPermissions(t.Context(), authority.RootID)
			if err != nil || (len(pending) == 1) != (stage == "permission persisted") {
				close(paused.resume)
				t.Fatalf("wrong admission pause state: pending=%+v error=%v", pending, err)
			}
			// Simulate manager retirement after the owner's pending-request
			// snapshot. Before Begin there is no durable prompt to cancel yet.
			retire()
			select {
			case <-callCtx.Done():
			case <-time.After(3 * time.Second):
				close(paused.resume)
				t.Fatal("generation retirement did not cancel admission context")
			}
			close(paused.resume)
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) || provider.effects.Load() != 0 {
					t.Fatalf("retired generation error=%v effects=%d", err, provider.effects.Load())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("retired admission remained blocked")
			}
			assertMCPSettled(t, ledger, authority)
		})
	}
}

func TestMCPAlreadyRetiredGenerationNeverBegins(t *testing.T) {
	services, ledger, provider, _ := newMCPServices(t)
	lifetime, retire := context.WithCancel(t.Context())
	retire()
	provider.lifetime = lifetime
	if _, err := services.InvokeMCP(t.Context(), "my-server", "write.raw", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("already retired generation error=%v", err)
	}
	if ledger.begins.Load() != 0 || provider.effects.Load() != 0 {
		t.Fatal("already canceled lifetime reached admission or transmission")
	}
}

func TestMCPCopiedPermissionPolicyKeepsReplacementProvider(t *testing.T) {
	for _, mode := range []string{"explicit denial", "headless", "automatic"} {
		t.Run(mode, func(t *testing.T) {
			previous, ledger, oldProvider, authority := newMCPServices(t)
			switch mode {
			case "explicit denial":
				previous.SetGate(func(context.Context, GateRequest) (GateDecision, string) { return GateReject, "persistent veto" })
			case "headless":
				previous.SetExternalPermissions(true)
				previous.SetHeadlessPermissions(true)
			case "automatic":
				previous.SetMCPAutomatic(true)
			}
			replacementProvider := &testMCPProvider{descriptor: oldProvider.descriptor}
			replacementProvider.descriptor.Generation = "replacement"
			replacementProvider.descriptor.Trusted = mode == "explicit denial"
			replacement := NewServices()
			replacement.SetMCPProvider(func() MCPProvider { return replacementProvider })
			replacement.CopyPermissionPolicyFrom(previous)
			if err := replacement.BindDispatcher(ledger, ledger.Workspaces(), ledger.Processes(), authority); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_, err := replacement.InvokeMCP(ctx, "my-server", "write.raw", nil)
			if mode == "automatic" {
				if err != nil || replacementProvider.effects.Load() != 1 {
					t.Fatalf("copied automatic mode error=%v effects=%d", err, replacementProvider.effects.Load())
				}
			} else if !errors.Is(err, capability.ErrDenied) || replacementProvider.effects.Load() != 0 {
				t.Fatalf("copied restrictive mode error=%v effects=%d", err, replacementProvider.effects.Load())
			}
			if oldProvider.effects.Load() != 0 {
				t.Fatal("copying permission policy replaced the current MCP provider")
			}
			assertMCPSettled(t, ledger, authority)
		})
	}
}

func TestMCPRemoteFailureDoesNotRetryOrHoldBudget(t *testing.T) {
	services, ledger, provider, authority := newMCPServices(t)
	provider.descriptor.Trusted = true
	provider.callErr = errors.New("remote result uncertain after transmission")
	// Remote calls are independent of the local working directory and writer
	// capability; a cwd outside the root must not turn into filesystem authority.
	ctx := WithWorkingDirectory(t.Context(), t.TempDir())
	if _, err := services.InvokeMCP(ctx, "my-server", "write.raw", nil); !errors.Is(err, provider.callErr) {
		t.Fatalf("remote error=%v", err)
	}
	if provider.effects.Load() != 1 || ledger.begins.Load() != 1 {
		t.Fatal("uncertain remote call was retried")
	}
	if admission := ledger.lastAdmission(); admission.Request.WorkingDirectory != "" {
		t.Fatalf("MCP inherited filesystem cwd: %+v", admission)
	}
	assertMCPSettled(t, ledger, authority)
}
