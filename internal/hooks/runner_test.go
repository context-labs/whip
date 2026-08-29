package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunPayloadEnvironmentAndContext(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	root := t.TempDir()
	wd := t.TempDir()
	m := managerWith(action{
		event:      PreToolUse,
		source:     "test plugin",
		pluginRoot: root,
		command: `payload=$(cat)
printf '%s' "$payload" > "$PLUGIN_ROOT/payload.json"
printf '%s|%s|%s|%s' "$WHIP_HOOK_EVENT" "$WHIP_SESSION_ID" "$WHIP_TOOL_NAME" "$WHIP_PROJECT_DIR" > "$PLUGIN_ROOT/env.txt"
printf '{"decision":"allow","additionalContext":"from hook"}'`,
		timeout: time.Second,
	})
	req := Request{
		Event:          PreToolUse,
		SessionID:      "session-1",
		WorkingDir:     wd,
		MatcherContext: "bash",
		ToolName:       "bash",
		ToolInput:      json.RawMessage(`{"command":"go test ./..."}`),
		ToolCallID:     "call-1",
	}
	out := m.Run(t.Context(), req)
	if out.Blocked || out.Ran != 1 || out.AdditionalContext != "from hook" || len(out.Failures) != 0 {
		t.Fatalf("outcome = %+v", out)
	}
	data, err := os.ReadFile(filepath.Join(root, "payload.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("payload json: %v\n%s", err, data)
	}
	if got["version"] != float64(1) || got["event"] != "PreToolUse" || got["event_type"] != "PreToolUse" || got["working_dir"] != wd {
		t.Fatalf("payload fields = %v", got)
	}
	if got["session_id"] != "session-1" || got["tool_call_id"] != "call-1" {
		t.Fatalf("payload identity fields = %v", got)
	}
	env, err := os.ReadFile(filepath.Join(root, "env.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(env) != "PreToolUse|session-1|bash|"+wd {
		t.Fatalf("hook environment = %q", env)
	}
}

func TestRunDecisionAndFailureSemantics(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	tests := []struct {
		name           string
		command        string
		onFailureBlock bool
		blocked        bool
		failure        bool
		reason         string
	}{
		{name: "exit two blocks", command: `printf 'no deletes' >&2; exit 2`, blocked: true, reason: "no deletes"},
		{name: "json deny blocks", command: `printf '{"decision":"deny","reason":"policy"}'`, blocked: true, reason: "policy"},
		{name: "malformed fails open", command: `printf 'debug log'`, failure: true},
		{name: "malformed can fail closed", command: `printf 'debug log'`, onFailureBlock: true, blocked: true, failure: true},
		{name: "nonzero allow is failure", command: `printf '{"decision":"allow"}'; exit 1`, failure: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := managerWith(action{
				event:          PreToolUse,
				source:         "test",
				command:        tt.command,
				timeout:        time.Second,
				onFailureBlock: tt.onFailureBlock,
			})
			out := m.Run(t.Context(), Request{Event: PreToolUse, WorkingDir: t.TempDir()})
			if out.Blocked != tt.blocked {
				t.Fatalf("blocked = %v, want %v (%+v)", out.Blocked, tt.blocked, out)
			}
			if (len(out.Failures) > 0) != tt.failure {
				t.Fatalf("failures = %v, want failure=%v", out.Failures, tt.failure)
			}
			if tt.failure && !strings.HasPrefix(out.Failures[0], "test: ") {
				t.Fatalf("failure does not identify its hook source: %v", out.Failures)
			}
			if tt.reason != "" && !strings.Contains(out.Reason, tt.reason) {
				t.Fatalf("reason = %q, want %q", out.Reason, tt.reason)
			}
		})
	}
}

func TestRunStopFailureAlwaysFailsOpen(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := managerWith(action{
		event:          Stop,
		source:         "test",
		command:        `sleep 2`,
		timeout:        50 * time.Millisecond,
		onFailureBlock: true,
	})
	out := m.Run(t.Context(), Request{Event: Stop, WorkingDir: t.TempDir()})
	if out.Blocked || len(out.Failures) != 1 {
		t.Fatalf("stop failures must be visible but fail open: %+v", out)
	}
}

func TestRunCommandsStayOrdered(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	root := t.TempDir()
	logPath := filepath.Join(root, "order")
	m := managerWith(
		action{event: PostToolUse, source: "one", command: `printf '1' >> "$PLUGIN_ROOT/order"`, pluginRoot: root, timeout: time.Second},
		action{event: PostToolUse, source: "two", command: `printf '2' >> "$PLUGIN_ROOT/order"`, pluginRoot: root, timeout: time.Second},
	)
	out := m.Run(t.Context(), Request{Event: PostToolUse, WorkingDir: root})
	if out.Ran != 2 || len(out.Failures) != 0 {
		t.Fatalf("outcome = %+v", out)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "12" {
		t.Fatalf("command order = %q", data)
	}
}

func TestRunResourceLimitsAndCancellation(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	t.Run("payload", func(t *testing.T) {
		m := managerWith(action{event: UserPromptSubmit, source: "test", command: "exit 0", timeout: time.Second})
		out := m.Run(t.Context(), Request{
			Event:      UserPromptSubmit,
			WorkingDir: t.TempDir(),
			Message:    strings.Repeat("x", maxPayloadBytes),
		})
		if out.Ran != 1 || len(out.Failures) != 1 || !strings.Contains(out.Failures[0], "payload exceeds") {
			t.Fatalf("payload limit outcome = %+v", out)
		}
	})

	t.Run("output", func(t *testing.T) {
		m := managerWith(action{
			event:   PreToolUse,
			source:  "test",
			command: `yes x | head -c 70000`,
			timeout: time.Second,
		})
		out := m.Run(t.Context(), Request{Event: PreToolUse, WorkingDir: t.TempDir()})
		if len(out.Failures) != 1 || !strings.Contains(out.Failures[0], "output exceeds") {
			t.Fatalf("output limit outcome = %+v", out)
		}
	})

	t.Run("composed context", func(t *testing.T) {
		command := `printf '{"decision":"allow","additionalContext":"'; yes x | tr -d '\n' | head -c 40000; printf '"}'`
		m := managerWith(
			action{event: PreToolUse, source: "one", command: command, timeout: time.Second},
			action{event: PreToolUse, source: "two", command: command, timeout: time.Second},
		)
		out := m.Run(t.Context(), Request{Event: PreToolUse, WorkingDir: t.TempDir()})
		if out.Blocked || len(out.AdditionalContext) != 40000 {
			t.Fatalf("composed context outcome = blocked:%v context:%d", out.Blocked, len(out.AdditionalContext))
		}
		if len(out.Failures) != 1 || !strings.Contains(out.Failures[0], "additional context exceeds") {
			t.Fatalf("composed context failures = %v", out.Failures)
		}
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("WHIP_HOOK_TEST_OVERSIZE", strings.Repeat("x", maxEnvBytes))
		m := managerWith(action{event: PreToolUse, source: "test", command: "exit 0", timeout: time.Second})
		out := m.Run(t.Context(), Request{Event: PreToolUse, WorkingDir: t.TempDir()})
		if len(out.Failures) != 1 || !strings.Contains(out.Failures[0], "environment exceeds") {
			t.Fatalf("environment limit outcome = %+v", out)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		m := managerWith(action{event: PreToolUse, source: "test", command: "sleep 30", timeout: time.Minute})
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(50*time.Millisecond, cancel)
		start := time.Now()
		out := m.Run(ctx, Request{Event: PreToolUse, WorkingDir: t.TempDir()})
		cancel()
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("cancelled hook ran for %s", elapsed)
		}
		if len(out.Failures) != 1 || !strings.Contains(out.Failures[0], "cancelled") {
			t.Fatalf("cancellation outcome = %+v", out)
		}
	})
}

func managerWith(actions ...action) *Manager {
	m := &Manager{actions: make(map[Event][]action)}
	for _, a := range actions {
		m.actions[a.event] = append(m.actions[a.event], a)
	}
	return m
}
