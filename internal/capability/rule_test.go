package capability

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
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

func TestCommandRules(t *testing.T) {
	cases := []struct {
		in    string
		rules []string
		ok    bool
	}{
		{"ls -la", []string{"ls"}, true},
		// every command in a chain or pipeline carries its own rule
		{"git checkout main && rm -rf /", []string{"git checkout", "rm"}, true},
		{"ls; ls -la | head", []string{"ls", "head"}, true},
		{"make &\nwait", []string{"make", "wait"}, true},
		// harmless redirects are ignored, anything else that redirects never matches
		{"go test ./... 2>&1 | tail -50", []string{"go test", "tail"}, true},
		{"cmd >/dev/null 2>&1", []string{"cmd"}, true},
		{"echo hi > out.txt", nil, false},
		{"cat < in.txt", nil, false},
		{"cat <<EOF\nx\nEOF", nil, false},
		// command substitution runs anything
		{"ls $(rm -rf ~)", nil, false},
		{"echo `whoami`", nil, false},
		{"FOO=1; ls", []string{"ls"}, true},
		{"  ", nil, false},
	}
	for _, c := range cases {
		rules, ok := CommandRules(c.in)
		if !slices.Equal(rules, c.rules) || ok != c.ok {
			t.Errorf("CommandRules(%q) = (%q, %v), want (%q, %v)", c.in, rules, ok, c.rules, c.ok)
		}
	}
}

func TestPermissionRule(t *testing.T) {
	cases := []struct {
		operation, arguments, path string
		command                    string
		rules                      []string
		ok                         bool
	}{
		{"bash", `{"command":"git checkout main && rm -rf /"}`, "", "git checkout main && rm -rf /", []string{"git checkout", "rm"}, true},
		{"shell_start", `{"command":"npm run dev"}`, "", "npm run dev", []string{"npm run dev"}, true},
		{"workspace_process", `{"command":"  "}`, "", "  ", nil, false},
		{"bash", `{"command":"ls $(cat x)"}`, "", "ls $(cat x)", nil, false},
		{"write", `{"path":"alias/file"}`, "/workspace/file", "/workspace/file", []string{"/workspace/file"}, true},
		{"edit", `{"path":"x"}`, "", "", nil, false},
		{"read", `{"path":"x"}`, "/workspace/file", "/workspace/file", nil, false},
	}
	for _, c := range cases {
		command, rules, ok := PermissionRule(c.operation, json.RawMessage(c.arguments), c.path)
		if command != c.command || !slices.Equal(rules, c.rules) || ok != c.ok {
			t.Errorf("PermissionRule(%s, %s, %q) = (%q, %q, %v), want (%q, %q, %v)",
				c.operation, c.arguments, c.path, command, rules, ok, c.command, c.rules, c.ok)
		}
	}
	if got := RuleLabel([]string{"git checkout", "rm"}); got != "git checkout, rm" {
		t.Errorf("RuleLabel = %q", got)
	}
}

func TestMCPPermissionRuleBindsRawNamesAndStableDefinition(t *testing.T) {
	call := MCPCall{
		MCPSelector: MCPSelector{Server: "raw-server", Tool: "raw.tool", Definition: "opaque-definition"},
		Generation:  "ephemeral-generation", Source: "attached client", Arguments: json.RawMessage(`{"text":"concrete content"}`),
	}
	ruleFor := func(call MCPCall) (string, string) {
		t.Helper()
		envelope, err := json.Marshal(call)
		if err != nil {
			t.Fatal(err)
		}
		command, rules, ok := PermissionRule("mcp.call", envelope, "")
		if !ok || len(rules) != 1 {
			t.Fatalf("MCP rule=%q ok=%v", rules, ok)
		}
		return command, rules[0]
	}
	command, original := ruleFor(call)
	for _, value := range []string{"raw-server", "raw.tool", "attached client", `"text":"concrete content"`} {
		if !strings.Contains(command, value) {
			t.Errorf("consent omitted %q: %s", value, command)
		}
	}
	call.Generation = "reconnected"
	call.Arguments = json.RawMessage(`{"text":"another call"}`)
	if _, current := ruleFor(call); current != original {
		t.Fatal("connection generation or per-call arguments changed remembered tool rule")
	}
	for _, changed := range []MCPSelector{
		{Server: "raw_server", Tool: call.Tool, Definition: call.Definition},
		{Server: call.Server, Tool: "raw_tool", Definition: call.Definition},
		{Server: call.Server, Tool: call.Tool, Definition: "new-definition"},
	} {
		copy := call
		copy.MCPSelector = changed
		if _, current := ruleFor(copy); current == original {
			t.Errorf("different selector reused permission rule: %+v", changed)
		}
	}
	for _, raw := range []string{`{}`, `{"server":"s","tool":"t"}`, `{`} {
		if _, _, ok := PermissionRule("mcp.call", json.RawMessage(raw), ""); ok {
			t.Errorf("malformed identity produced permission rule: %q", raw)
		}
	}
}
