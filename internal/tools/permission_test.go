package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestExternalPermissionsWaitForAuthenticatedDaemonDecision(t *testing.T) {
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
		waiting := services.permissionWaiters["pending"] != nil
		services.mu.RUnlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("permission decision did not begin waiting")
		}
		time.Sleep(time.Millisecond)
	}
	want := capability.Decision{Allow: true, PrincipalID: "paired-human", Reason: "approved"}
	if err := services.ResolvePermission("pending", want); err != nil {
		t.Fatal(err)
	}
	if got, err := <-decisionCh, <-errCh; err != nil || got != want {
		t.Fatalf("external decision = %+v, %v", got, err)
	}

	early := capability.Decision{PrincipalID: "paired-human", Reason: "rejected"}
	if err := services.ResolvePermission("early", early); err != nil {
		t.Fatal(err)
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
	if _, err := ledger.AddPermissionRule(t.Context(), authority.RootID, "write", path, "paired-human"); err != nil {
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
	previous.permissionEarly["early"] = capability.Decision{Allow: true, PrincipalID: "previous-client"}
	previous.permissionWaiters["pending"] = make(chan capability.Decision, 1)
	replacement := NewServices()
	replacement.CopyPermissionPolicyFrom(previous)
	if !replacement.ExternalPermissionsEnabled() || len(replacement.permissionEarly) != 0 || len(replacement.permissionWaiters) != 0 {
		t.Fatal("replacement copied pending decisions or lost external permission policy")
	}
	if len(previous.permissionEarly) != 1 || len(previous.permissionWaiters) != 1 {
		t.Fatal("copying permission policy changed the previous runtime's pending requests")
	}
	if err := replacement.ResolvePermission("replacement", capability.Decision{PrincipalID: "new-client"}); err != nil {
		t.Fatal(err)
	}
	if len(previous.permissionEarly) != 1 {
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
	if len(services.permissionWaiters) != 0 {
		t.Fatal("stale invocation registered a waiter after policy invalidation")
	}
}
