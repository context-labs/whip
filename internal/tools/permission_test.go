package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

func TestDispatcherPermissionUsesCanonicalPath(t *testing.T) {
	services := NewServices()
	var got GateRequest
	services.SetGate(func(_ context.Context, request GateRequest) (GateDecision, string) {
		got = request
		return GateAllowOnce, ""
	})
	decision, err := services.Decide(context.Background(), capability.PermissionPrompt{
		Operation: "write", Arguments: json.RawMessage(`{"path":"alias/file"}`), CanonicalPath: "/workspace/file",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allow || got.Command != "/workspace/file" || got.Rule != "/workspace/file" {
		t.Fatalf("decision=%+v gate=%+v", decision, got)
	}
}

func TestLocalHumanPermissionBypassesGate(t *testing.T) {
	services := NewServices()
	services.SetGate(func(context.Context, GateRequest) (GateDecision, string) {
		return GateReject, "should not run"
	})
	decision, err := services.Decide(WithLocalHuman(context.Background()), capability.PermissionPrompt{})
	if err != nil || !decision.Allow || decision.PrincipalID != "local-human" {
		t.Fatalf("local human decision = %+v, %v", decision, err)
	}
}

func TestPermissionGateIsScopedToServices(t *testing.T) {
	allowed := NewServices()
	denied := NewServices()
	denied.SetGate(func(context.Context, GateRequest) (GateDecision, string) {
		return GateReject, "not this session"
	})

	if got := allowed.CheckGate(context.Background(), "bash", "pwd"); got != "" {
		t.Fatalf("ungated services denied command: %q", got)
	}
	if got := denied.CheckGate(context.Background(), "bash", "pwd"); got != "Permission denied: not this session" {
		t.Fatalf("denied services result = %q", got)
	}
}

func TestExternalPermissionsWaitForTrustedClientDecision(t *testing.T) {
	services := NewServices()
	services.SetExternalPermissions(true)
	if !services.ExternalPermissionsEnabled() {
		t.Fatal("external permission mode was not enabled")
	}

	decisionCh := make(chan capability.Decision, 1)
	errCh := make(chan error, 1)
	go func() {
		decision, err := services.Decide(t.Context(), capability.PermissionPrompt{ID: "pending"})
		decisionCh <- decision
		errCh <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		services.mu.RLock()
		waiting := services.permissions["pending"] != nil
		services.mu.RUnlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("permission decision did not begin waiting")
		}
		time.Sleep(time.Millisecond)
	}
	want := capability.Decision{Allow: true, PrincipalID: "trusted-client", Reason: "approved"}
	if err := services.ResolvePermission("pending", want); err != nil {
		t.Fatal(err)
	}
	if got, err := <-decisionCh, <-errCh; err != nil || got != want {
		t.Fatalf("external decision = %+v, %v", got, err)
	}

	if err := services.ResolvePermission("pending", capability.Decision{PrincipalID: "other-client"}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("competing decision after handoff error=%v", err)
	}

	early := capability.Decision{PrincipalID: "trusted-client", Reason: "rejected"}
	if err := services.ResolvePermission("early", early); err != nil {
		t.Fatal(err)
	}
	if err := services.ResolvePermission("early", want); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("competing early decision error=%v", err)
	}
	if got, err := services.Decide(t.Context(), capability.PermissionPrompt{ID: "early"}); err != nil || got != early {
		t.Fatalf("early external decision = %+v, %v", got, err)
	}
	services.SetExternalPermissions(false)
	if services.ExternalPermissionsEnabled() {
		t.Fatal("external permission mode was not disabled")
	}
	if err := services.ResolvePermission("disabled", want); err == nil {
		t.Fatal("disabled external permissions accepted a decision")
	}
}

func TestHeadlessBuiltinsRequirePreauthorization(t *testing.T) {
	services, ledger, _, authority := newMCPServices(t)
	services.SetExternalPermissions(true)
	services.SetHeadlessPermissions(true)
	path := filepath.Join(services.ProcessOptions().Cwd, "result.txt")
	arguments, err := json.Marshal(map[string]string{"path": path, "content": "approved"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := services.Invoke(t.Context(), "write", arguments); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("headless write without approval error=%v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unapproved file exists: %v", err)
	}
	if _, err := ledger.AddPermissionRule(t.Context(), authority.RootID, "write", path, "trusted-client"); err != nil {
		t.Fatal(err)
	}
	if _, err := services.Invoke(t.Context(), "write", arguments); err != nil {
		t.Fatalf("preapproved headless write error=%v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "approved" {
		t.Fatalf("preapproved file=%q, error=%v", data, err)
	}
	assertMCPSettled(t, ledger, authority)
}

func TestHeadlessBuiltinDecisionDoesNotConsultGate(t *testing.T) {
	services := NewServices()
	services.SetHeadlessPermissions(true)
	services.SetGate(func(context.Context, GateRequest) (GateDecision, string) {
		t.Fatal("headless permission decision called an interactive gate")
		return GateReject, ""
	})
	decision, err := services.Decide(t.Context(), capability.PermissionPrompt{Operation: "bash", Arguments: json.RawMessage(`{"command":"pwd"}`)})
	if err != nil || decision.Allow {
		t.Fatalf("headless decision=%+v, error=%v", decision, err)
	}
}

func TestCopyPermissionPolicyDoesNotTransferPendingDecisions(t *testing.T) {
	previous := NewServices()
	previous.SetExternalPermissions(true)
	previous.permissions["early"] = &permissionResolution{decision: capability.Decision{Allow: true, PrincipalID: "previous-client"}, resolved: true}
	previous.permissions["pending"] = &permissionResolution{waiter: make(chan capability.Decision, 1)}
	replacement := NewServices()
	replacement.CopyPermissionPolicyFrom(previous)
	if !replacement.ExternalPermissionsEnabled() || len(replacement.permissions) != 0 {
		t.Fatal("replacement copied pending decisions or lost external permission policy")
	}
	if len(previous.permissions) != 2 {
		t.Fatal("copying permission policy changed the previous runtime's pending requests")
	}
	if err := replacement.ResolvePermission("replacement", capability.Decision{PrincipalID: "new-client"}); err != nil {
		t.Fatal(err)
	}
	if len(previous.permissions) != 2 {
		t.Fatal("replacement permission state aliases the previous runtime")
	}
}

func TestMCPPolicyChangeBeforeWaiterRegistrationFailsClosed(t *testing.T) {
	services := NewServices()
	services.SetExternalPermissions(true)
	ctx := context.WithValue(t.Context(), mcpConsentKey{}, &mcpConsent{revision: services.permissionRevision})
	services.SetGate(func(context.Context, GateRequest) (GateDecision, string) { return GateReject, "deny" })
	if _, err := services.waitPermission(ctx, "stale"); !errors.Is(err, capability.ErrStaleAdmission) {
		t.Fatalf("policy changed before registration error=%v", err)
	}
	if len(services.permissions) != 0 {
		t.Fatal("stale invocation registered a waiter after policy invalidation")
	}
}

// Hold the durable commit after the dispatcher has consumed the first answer.
// The client handoff must stay claimed for both built-ins and MCP calls.
func TestExternalPermissionDecisionClaimSurvivesHandoff(t *testing.T) {
	for _, operation := range []string{"write", "mcp.call"} {
		for _, allow := range []bool{false, true} {
			name := operation + "/deny"
			if allow {
				name = operation + "/allow"
			}
			t.Run(name, func(t *testing.T) {
				_, ledger, provider, authority := newMCPServices(t)
				paused := &pausedMCPDecisionLedger{
					countingLedger: ledger,
					before:         make(chan struct{}), commit: make(chan struct{}),
					after: make(chan struct{}), finish: make(chan struct{}),
				}
				var commitOnce, finishOnce sync.Once
				commit := func() { commitOnce.Do(func() { close(paused.commit) }) }
				finish := func() { finishOnce.Do(func() { close(paused.finish) }) }
				defer finish()
				defer commit()
				services := NewServices()
				services.SetExternalPermissions(true)
				services.SetMCPProvider(func() MCPProvider { return provider })
				if err := services.BindDispatcher(paused, ledger.Workspaces(), ledger.Processes(), authority); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(services.ProcessOptions().Cwd, "decision.txt")
				arguments, err := json.Marshal(map[string]string{"path": path, "content": "first decision won"})
				if err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() {
					var err error
					if operation == "mcp.call" {
						_, err = services.InvokeMCP(t.Context(), "my-server", "write.raw", nil)
					} else {
						_, err = services.Invoke(t.Context(), operation, arguments)
					}
					done <- err
				}()
				var id string
				deadline := time.After(3 * time.Second)
				tick := time.NewTicker(time.Millisecond)
				defer tick.Stop()
				for id == "" {
					services.mu.RLock()
					for pendingID, resolution := range services.permissions {
						if resolution.waiter != nil {
							id = pendingID
							break
						}
					}
					services.mu.RUnlock()
					if id != "" {
						break
					}
					select {
					case <-deadline:
						t.Fatal("operation did not register a permission waiter")
					case <-tick.C:
					}
				}
				first := capability.Decision{Allow: allow, PrincipalID: "first-client"}
				if err := services.ResolvePermission(id, first); err != nil {
					t.Fatal(err)
				}
				select {
				case <-paused.before:
				case <-time.After(3 * time.Second):
					t.Fatal("decision did not reach durable commit")
				}
				competing := capability.Decision{Allow: !allow, PrincipalID: "other-client", Remember: "tree"}
				if err := services.ResolvePermission(id, competing); !errors.Is(err, capability.ErrDenied) {
					t.Fatalf("opposing decision accepted before durable commit: %v", err)
				}
				commit()
				finish()
				select {
				case err := <-done:
					if allow && err != nil {
						t.Fatalf("approved operation failed: %v", err)
					}
					if !allow && !errors.Is(err, capability.ErrDenied) {
						t.Fatalf("denied operation error=%v", err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("operation did not settle")
				}
				services.mu.RLock()
				retained := len(services.permissions)
				services.mu.RUnlock()
				if retained != 0 {
					t.Fatalf("settled operation retained %d permission resolutions", retained)
				}
				if err := services.ResolvePermission(id, competing); !errors.Is(err, capability.ErrDenied) {
					t.Fatalf("settled operation accepted a decision: %v", err)
				}
				if operation == "write" {
					data, err := os.ReadFile(path)
					if allow && (err != nil || string(data) != "first decision won") {
						t.Fatalf("approved output=%q err=%v", data, err)
					}
					if !allow && !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("denied write produced output=%q err=%v", data, err)
					}
				} else {
					expected := int64(0)
					if allow {
						expected = 1
					}
					if got := provider.effects.Load(); got != expected {
						t.Fatalf("MCP effects=%d want=%d", got, expected)
					}
				}
				assertMCPSettled(t, ledger, authority)
			})
		}
	}
}

func TestExternalPermissionClaimsSurvivePolicyInvalidation(t *testing.T) {
	services := NewServices()
	services.SetExternalPermissions(true)
	first := capability.Decision{Allow: true, PrincipalID: "first-client"}
	if err := services.ResolvePermission("early", first); err != nil {
		t.Fatal(err)
	}
	services.SetMCPAutomatic(true)
	if err := services.ResolvePermission("early", capability.Decision{PrincipalID: "other-client"}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("policy change accepted a second decision: %v", err)
	}
	if _, err := services.Decide(t.Context(), capability.PermissionPrompt{ID: "early"}); !errors.Is(err, capability.ErrStaleAdmission) {
		t.Fatalf("policy change reused stale early consent: %v", err)
	}
}
