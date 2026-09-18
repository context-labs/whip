package tools

import (
	"strings"
	"testing"
)

func TestCheckGate(t *testing.T) {
	old := Gate
	t.Cleanup(func() { Gate = old })

	Gate = nil
	if got := checkGate("bash", "git status"); got != "" {
		t.Fatalf("nil gate rejected: %q", got)
	}
	Gate = func(req GateRequest) (GateDecision, string) {
		if req.Tool != "bash" || req.Command != "git status" || req.Rule != "git status" {
			t.Fatalf("gate request = %+v", req)
		}
		return GateAllowOnce, ""
	}
	if got := checkGate("bash", "git status"); got != "" {
		t.Fatalf("allow gate rejected: %q", got)
	}
	Gate = func(GateRequest) (GateDecision, string) { return GateReject, "use read instead" }
	if got := checkGate("bash", "cat file"); !strings.Contains(got, "use read instead") {
		t.Fatalf("redirect rejection = %q", got)
	}
	Gate = func(GateRequest) (GateDecision, string) { return GateReject, "" }
	if got := checkGate("write", "file"); got != "Permission denied: the user rejected this action" {
		t.Fatalf("default rejection = %q", got)
	}
}

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
