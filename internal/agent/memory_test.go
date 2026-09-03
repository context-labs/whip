package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// SetSessionID gates the session memory scope: before it, remember(scope:
// session) errors; after it, entries land in the per-session markdown file.
func TestMemoryToolsSessionScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	ag := newTestAgent(llm.New("http://unused", "k"), "m", 100, "sys")
	ctx := context.Background()

	out := tools.Execute(ctx, ag.Tools, "remember", json.RawMessage(`{"text":"x","scope":"session"}`))
	if !strings.Contains(out, "no session yet") {
		t.Fatalf("session scope without a session id should refuse: %q", out)
	}
	out = tools.Execute(ctx, ag.Tools, "remember", json.RawMessage(`{"text":"x","scope":"bogus"}`))
	if !strings.Contains(out, "scope must be") {
		t.Fatalf("unknown scope should refuse: %q", out)
	}

	ag.SetSessionID("sess1")
	out = tools.Execute(ctx, ag.Tools, "remember", json.RawMessage(`{"text":"always pnpm, never npm","scope":"session"}`))
	if out != "Remembered (session memory)." {
		t.Fatalf("remember after SetSessionID: %q", out)
	}
	path := filepath.Join(home, "sessions", "sess1.memory.md")
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "always pnpm, never npm") {
		t.Fatalf("session memory file should hold the entry: %v %q", err, data)
	}

	// forget strikes the entry ("- [x]") instead of deleting it
	out = tools.Execute(ctx, ag.Tools, "forget", json.RawMessage(`{"n":1,"scope":"session"}`))
	if !strings.Contains(out, "Forgot entry 1") {
		t.Fatalf("forget: %q", out)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "- [x]") || !strings.Contains(string(data), "always pnpm") {
		t.Fatalf("forget should strike, not delete: %q", data)
	}

	// default scope is installation, independent of the session id
	out = tools.Execute(ctx, ag.Tools, "remember", json.RawMessage(`{"text":"likes go"}`))
	if out != "Remembered (installation memory)." {
		t.Fatalf("default scope: %q", out)
	}
	data, err = os.ReadFile(filepath.Join(home, "memory.md"))
	if err != nil || !strings.Contains(string(data), "likes go") {
		t.Fatalf("installation memory file: %v %q", err, data)
	}
}

// Bad arguments and store-level failures surface as tool errors, never as a
// false "Remembered".
func TestMemoryToolErrors(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	ag := newTestAgent(llm.New("http://unused", "k"), "m", 100, "sys")
	ctx := context.Background()

	for _, tc := range []struct{ tool, args, want string }{
		{"remember", `{"text":`, "unexpected end"},             // malformed arguments
		{"remember", `{"text":"   "}`, "text is required"},     // store rejects it
		{"forget", `{"n":`, "unexpected end"},                  // malformed arguments
		{"forget", `{"n":1,"scope":"bogus"}`, "scope must be"}, // bad scope
		{"forget", `{"n":99}`, "no memor"},                     // nothing to forget
	} {
		out := tools.Execute(ctx, ag.Tools, tc.tool, json.RawMessage(tc.args))
		if !strings.HasPrefix(out, "Error") || !strings.Contains(out, tc.want) {
			t.Errorf("%s %s: want error containing %q, got %q", tc.tool, tc.args, tc.want, out)
		}
	}
}
