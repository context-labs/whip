package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDiscoveryOrderAndFormats(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeHookConfig(t, filepath.Join(home, ".agents", "plugins", "z-last", "hooks", "hooks.json"), `{
  "hooks": {"PreToolUse": [{"matcher":"^bash$","hooks":[{"command":"echo user-z"}]}]}
}`)
	writeHookConfig(t, filepath.Join(home, ".agents", "plugins", "a-first", "hooks", "hooks.json"), `{
  "hooks": {"PreToolUse": [{"hooks":[{"command":"echo user-a"}]}]}
}`)
	writeHookConfig(t, filepath.Join(project, ".agents", "plugins", "project", "hooks", "hooks.json"), `{
  "hooks": {"PreToolUse": [{"hooks":[{"command":"echo project"}]}]}
}`)
	writeHookConfig(t, filepath.Join(project, ".whip", "hooks.json"), `{
	  "pre_tool_use": [{"matcher":"*","hooks":[{"command":"echo project-file"}]}]
}`)

	m := Load(LoadOptions{WorkingDir: project, HomeDir: home, IncludeProject: true})
	entries := m.Entries()
	if len(entries) != 4 {
		t.Fatalf("entries = %d, want 4; warnings=%v", len(entries), m.Warnings())
	}
	want := []string{"echo user-a", "echo user-z", "echo project", "echo project-file"}
	for i := range want {
		if entries[i].Command != want[i] {
			t.Fatalf("entry %d command = %q, want %q", i, entries[i].Command, want[i])
		}
	}
	if !entries[1].EventMatches("bash") || entries[1].EventMatches("read") {
		t.Fatal("plugin matcher should use its regular expression")
	}
	if !entries[3].EventMatches("anything") {
		t.Fatal("project '*' matcher should match every tool")
	}
}

func TestLoadTrustDisableAndWarnings(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeHookConfig(t, filepath.Join(home, ".agents", "plugins", "user", "hooks", "hooks.json"), `{
  "hooks": {"Stop": [{"hooks":[{"command":"echo user"}]}]}
}`)
	writeHookConfig(t, filepath.Join(project, ".agents", "plugins", "project", "hooks", "hooks.json"), `{
  "hooks": {
    "PreToolUse": [
      {"matcher":"[","hooks":[{"command":"echo bad matcher"}]},
      {"hooks":[{"command":"echo valid"},{"command":"echo async","async":true}]}
    ],
    "FutureEvent": [{"hooks":[{"command":"echo future"}]}]
  }
}`)

	userOnly := Load(LoadOptions{WorkingDir: project, HomeDir: home})
	if userOnly.Count() != 1 || userOnly.ProjectEnabled() {
		t.Fatalf("untrusted load = %d entries, project=%v", userOnly.Count(), userOnly.ProjectEnabled())
	}

	trusted := Load(LoadOptions{WorkingDir: project, HomeDir: home, IncludeProject: true})
	if trusted.Count() != 2 {
		t.Fatalf("valid entries should survive neighboring errors: count=%d warnings=%v", trusted.Count(), trusted.Warnings())
	}
	warnings := strings.Join(trusted.Warnings(), "\n")
	for _, want := range []string{"invalid matcher", "action 2 is invalid", "unsupported event"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings missing %q:\n%s", want, warnings)
		}
	}

	t.Setenv("WHIP_DISABLE_HOOKS", "1")
	disabled := Load(LoadOptions{WorkingDir: project, HomeDir: home, IncludeProject: true})
	if !disabled.Disabled() || disabled.Count() != 0 {
		t.Fatalf("disabled manager = disabled:%v count:%d", disabled.Disabled(), disabled.Count())
	}
}

func TestPluginRootExpandsFromEnvironment(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	home := t.TempDir()
	root := filepath.Join(home, ".agents", "plugins", "portable plugin")
	writeHookConfig(t, filepath.Join(root, "hooks", "hooks.json"), `{
  "hooks": {"Stop": [{"hooks":[{"command":"printf '%s' \"$PLUGIN_ROOT\" > \"$PLUGIN_ROOT/seen\""}]}]}
}`)
	m := Load(LoadOptions{HomeDir: home})
	out := m.Run(t.Context(), Request{Event: Stop, WorkingDir: home})
	if out.Ran != 1 || len(out.Failures) != 0 {
		t.Fatalf("plugin-root hook outcome = %+v, warnings = %v", out, m.Warnings())
	}
	seen, err := os.ReadFile(filepath.Join(root, "seen"))
	if err != nil {
		t.Fatal(err)
	}
	if string(seen) != root {
		t.Fatalf("PLUGIN_ROOT = %q, want %q", seen, root)
	}
}

func TestNormalizeActionClampsTimeoutBeforeDurationConversion(t *testing.T) {
	a, ok := normalizeAction(
		rawAction{Command: "exit 0", Timeout: int(maxTimeout/time.Second) + 1},
		PreToolUse,
		matcher{},
		"test",
		t.TempDir(),
		flavorPlugin,
	)
	if !ok || a.timeout != maxTimeout {
		t.Fatalf("normalized timeout = %s, ok=%v", a.timeout, ok)
	}
}

func TestCompileProjectMatchers(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		match   []string
		reject  []string
	}{
		{
			name:    "wildcard",
			pattern: "*",
			match:   []string{"bash", "mcp__server__tool"},
		},
		{
			name:    "exact native tool name",
			pattern: "bash",
			match:   []string{"bash"},
			reject:  []string{"bashful", "prefix-bash"},
		},
		{
			name:    "slash regular expression",
			pattern: `/^(read|write)$/`,
			match:   []string{"read", "write"},
			reject:  []string{"edit", "reader"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compileMatcher(&tt.pattern, flavorProject)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range tt.match {
				if !got.Match(value) {
					t.Errorf("matcher %q did not match %q", tt.pattern, value)
				}
			}
			for _, value := range tt.reject {
				if got.Match(value) {
					t.Errorf("matcher %q unexpectedly matched %q", tt.pattern, value)
				}
			}
		})
	}
}

func writeHookConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// EventMatches is a test-only adapter that keeps matcher internals private in
// production while making discovery normalization observable.
func (e Entry) EventMatches(context string) bool {
	pattern := e.Matcher
	if pattern == "<all>" || pattern == "*" {
		return true
	}
	m, err := compileMatcher(&pattern, flavorPlugin)
	if err != nil {
		panic(fmt.Sprintf("compiled entry matcher became invalid: %v", err))
	}
	return m.Match(context)
}
