package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

func TestCommandRule(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ls -la", "ls"},
		{"git checkout main", "git checkout"},
		{"git", "git"},
		{"npm run build --watch", "npm run build"},
		{"docker compose up -d", "docker compose up"},
		{"git submodule update --init", "git submodule update"},
		// only the first command of a chain/pipeline is the rule
		{"git checkout main && rm -rf /", "git checkout"},
		{"cat foo | grep bar", "cat"},
		{"echo hi > out.txt", "echo"},
		{"ls; rm -rf /", "ls"},
		// leading env assignments are stripped
		{"FOO=1 BAR=2 git status", "git status"},
		{"  ", ""},
		{"FOO=1", ""},
	}
	for _, c := range cases {
		if got := CommandRule(c.in); got != c.want {
			t.Errorf("CommandRule(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

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
