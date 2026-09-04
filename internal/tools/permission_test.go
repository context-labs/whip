package tools

import (
	"context"
	"encoding/json"
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
