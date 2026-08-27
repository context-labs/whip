package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

// runFixture writes a config pointing the default model at an SSE test
// server that replies with reply (and records each request into reqs).
func runFixture(t *testing.T, reply string, reqs *[]llm.Request) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		if reqs != nil {
			*reqs = append(*reqs, req)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		body, _ := json.Marshal(reply)
		fmt.Fprintf(w, `data: {"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`+"\n\n", body)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	cfg := fmt.Sprintf(`{
		"defaultModel": "test",
		"providers": {"testprov": {"baseUrl": %q, "api": "openai-completions", "apiKey": "k"}},
		"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
	}`, srv.URL)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

// runCapture swaps stdout/stdin for the duration of runCLI and returns what
// the run printed on stdout. stdinData is piped in ("" still leaves a
// non-TTY empty stdin, like `whip run "…" < /dev/null`).
func runCapture(t *testing.T, stdinData string, args ...string) (string, error) {
	t.Helper()

	oldIn := os.Stdin
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inW.WriteString(stdinData); err != nil {
		t.Fatal(err)
	}
	inW.Close()
	os.Stdin = inR
	defer func() { os.Stdin = oldIn; inR.Close() }()

	oldOut := os.Stdout
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outW
	defer func() { os.Stdout = oldOut }()
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { io.Copy(&buf, outR); close(done) }()

	runErr := runCLI(args)

	outW.Close()
	<-done
	outR.Close()
	return buf.String(), runErr
}

// text mode streams the assistant reply to stdout.
func TestRunTextOutput(t *testing.T) {
	runFixture(t, "hello world", nil)

	out, err := runCapture(t, "", "say hi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello world") {
		t.Fatalf("stdout should stream the reply, got %q", out)
	}
}

// --format json emits newline-delimited events: a text event per delta and a
// final done event carrying the full reply.
func TestRunJSONStream(t *testing.T) {
	runFixture(t, "all done", nil)

	out, err := runCapture(t, "", "--format", "json", "go")
	if err != nil {
		t.Fatal(err)
	}
	var sawText, sawDone bool
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		var ev map[string]string
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line not JSON: %q: %v", line, err)
		}
		switch ev["type"] {
		case "text":
			sawText = true
		case "done":
			sawDone = true
			if ev["text"] != "all done" {
				t.Fatalf("done text: %q", ev["text"])
			}
		}
	}
	if !sawText || !sawDone {
		t.Fatalf("want a text event and a done event, got:\n%s", out)
	}
}

// Piped stdin is appended to the prompt argument in the user message.
func TestRunStdinAppendsToPrompt(t *testing.T) {
	var reqs []llm.Request
	runFixture(t, "ok", &reqs)

	if _, err := runCapture(t, "piped context\n", "summarize this"); err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("requests: %d", len(reqs))
	}
	var user string
	for _, m := range reqs[0].Messages {
		if m.Role == "user" {
			user = m.Content
		}
	}
	if !strings.Contains(user, "summarize this") || !strings.Contains(user, "piped context") {
		t.Fatalf("user message should combine the arg prompt and stdin, got %q", user)
	}
}

// -resume continues a persisted session instead of starting fresh; the
// resumed conversation's history precedes the new prompt.
func TestRunResume(t *testing.T) {
	var reqs []llm.Request
	runFixture(t, "first reply", &reqs)
	if _, err := runCapture(t, "", "first question"); err != nil {
		t.Fatal(err)
	}

	// find the session id from the store (same WHIP_HOME for both runs)
	dir, _ := configDir()
	st, err := sessionOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	metas, _ := st.Recent(10)
	if len(metas) != 1 {
		t.Fatalf("one session should exist, got %d", len(metas))
	}
	id := metas[0].ID
	st.Close()

	if _, err := runCapture(t, "", "-resume", id, "follow up"); err != nil {
		t.Fatal(err)
	}
	last := reqs[len(reqs)-1]
	var sawFirst bool
	for _, m := range last.Messages {
		if m.Role == "user" && strings.Contains(m.TextContent(), "first question") {
			sawFirst = true
		}
	}
	if !sawFirst {
		t.Fatal("a resumed run should carry the prior conversation")
	}
}

// -resume with an unknown id errors clearly.
func TestRunResumeUnknown(t *testing.T) {
	runFixture(t, "x", nil)
	if _, err := runCapture(t, "", "-resume", "nosuchsession", "hi"); err == nil || !strings.Contains(err.Error(), "no session") {
		t.Fatalf("unknown session should error clearly, got %v", err)
	}
}

// -system overrides the prompt; -system-file wins over -system.
func TestRunSystemOverride(t *testing.T) {
	var reqs []llm.Request
	runFixture(t, "ok", &reqs)
	if _, err := runCapture(t, "", "-system", "You are a pirate.", "hi"); err != nil {
		t.Fatal(err)
	}
	if got := reqs[len(reqs)-1].Messages[0].Content; got != "You are a pirate." {
		t.Fatalf("-system should replace the prompt, got %q", got)
	}

	f := filepath.Join(t.TempDir(), "sys.md")
	os.WriteFile(f, []byte("You are a poet."), 0o644)
	runFixture(t, "ok", &reqs)
	if _, err := runCapture(t, "", "-system", "pirate", "-system-file", f, "hi"); err != nil {
		t.Fatal(err)
	}
	if got := reqs[len(reqs)-1].Messages[0].Content; got != "You are a poet." {
		t.Fatalf("-system-file should win over -system, got %q", got)
	}
}

// -max-turns caps the tool loop; a capped run errors non-zero.
func TestRunMaxTurns(t *testing.T) {
	// a server that always calls a tool (never finishes)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"read","arguments":"{\"path\":\"/tmp/x\"}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	cfg := fmt.Sprintf(`{
		"defaultModel": "test",
		"providers": {"testprov": {"baseUrl": %q, "api": "openai-completions", "apiKey": "k"}},
		"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
	}`, srv.URL)
	os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600)

	_, err := runCapture(t, "", "-max-turns", "2", "-no-session", "loop forever")
	if err == nil || !strings.Contains(err.Error(), "max turns") {
		t.Fatalf("a capped run should error with 'max turns', got %v", err)
	}
}

// -timeout cancels an in-flight run and reports the timeout.
func TestRunTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second) // hang past the timeout
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	cfg := fmt.Sprintf(`{
		"defaultModel": "test",
		"providers": {"testprov": {"baseUrl": %q, "api": "openai-completions", "apiKey": "k"}},
		"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
	}`, srv.URL)
	os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600)

	_, err := runCapture(t, "", "-timeout", "200ms", "-no-session", "hi")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("a timed-out run should say so, got %v", err)
	}
}

// -no-session leaves no row in the session store.
func TestRunNoSession(t *testing.T) {
	runFixture(t, "ok", nil)
	if _, err := runCapture(t, "", "-no-session", "one-off"); err != nil {
		t.Fatal(err)
	}
	dir, _ := configDir()
	st, _ := sessionOpen(dir)
	defer st.Close()
	metas, _ := st.Recent(10)
	if len(metas) != 0 {
		t.Fatalf("-no-session should leave no sessions, got %d", len(metas))
	}
}

// -quiet -format json: clean NDJSON on stdout, nothing on stderr.
func TestRunQuietJSON(t *testing.T) {
	runFixture(t, "quiet reply", nil)
	out, err := runCapture(t, "", "-quiet", "-format", "json", "go")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		var ev map[string]string
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("stdout should be clean NDJSON, got line %q: %v", line, err)
		}
	}
}

func TestRunClientForCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"tokens":{"access_token":"access","refresh_token":"refresh","account_id":"account"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	client, err := runClientForProvider(config.Provider{
		BaseURL: config.CodexBaseURL,
		API:     "openai-codex-responses",
		Auth:    "codex",
	}, config.CodexProviderName, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.(*llm.Codex); !ok {
		t.Fatalf("client = %T, want *llm.Codex", client)
	}
}

func configDir() (string, error) { return os.Getenv("WHIP_HOME"), nil }

func sessionOpen(dir string) (*session.Store, error) { return session.Open(dir + "/sessions.db") }

// Bad flags, an unknown --format, and a missing prompt all fail before any
// provider is contacted.
func TestRunArgValidation(t *testing.T) {
	runFixture(t, "never used", nil)

	for _, c := range []struct {
		name, want string
		args       []string
	}{
		{"unknown flag", "not defined", []string{"-nosuchflag"}},
		{"bad format", "unknown --format", []string{"--format", "xml", "hi"}},
		{"no prompt", "no prompt given", nil},
	} {
		_, err := runCapture(t, "", c.args...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want an error containing %q", c.name, err, c.want)
		}
	}
}

// Routing and system-prompt failures are reported before the turn starts.
func TestRunResolveErrors(t *testing.T) {
	runFixture(t, "never used", nil)

	if _, err := runCapture(t, "", "-m", "nosuchmodel", "hi"); err == nil {
		t.Error("an unroutable model should error")
	}
	missing := filepath.Join(t.TempDir(), "absent.md")
	_, err := runCapture(t, "", "-system-file", missing, "hi")
	if err == nil || !strings.Contains(err.Error(), "-system-file") {
		t.Errorf("a missing -system-file should name the flag, got %v", err)
	}

	// a provider with no key at all: nothing to authenticate with
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	cfg := `{
		"defaultModel": "test",
		"providers": {"testprov": {"baseUrl": "https://example.invalid", "api": "openai-completions"}},
		"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
	}`
	if werr := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600); werr != nil {
		t.Fatal(werr)
	}
	if _, err := runCapture(t, "", "hi"); err == nil || !strings.Contains(err.Error(), "no API key") {
		t.Errorf("a keyless provider should error, got %v", err)
	}
}

// An unreadable config dir fails the run instead of falling back to defaults
// that would reach the network.
func TestRunUnreadableConfig(t *testing.T) {
	unusableHome(t)
	if _, err := runCapture(t, "", "hi"); err == nil {
		t.Error("an unusable WHIP_HOME should error")
	}
}

// In --format json the tool calls are events too, and a failed run ends with
// an error event rather than a done event.
func TestRunJSONToolEvents(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(target, []byte("file body"), 0o600); err != nil {
		t.Fatal(err)
	}
	// a server that always answers with a read tool call: only -max-turns ends it
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		args, _ := json.Marshal(map[string]string{"path": target})
		call, _ := json.Marshal(string(args))
		fmt.Fprintf(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"read","arguments":%s}}]}}]}`+"\n\n", call)
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	cfg := fmt.Sprintf(`{
		"defaultModel": "test",
		"providers": {"testprov": {"baseUrl": %q, "api": "openai-completions", "apiKey": "k"}},
		"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
	}`, srv.URL)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runCapture(t, "", "-format", "json", "-max-turns", "2", "-quiet", "-no-session", "read it")
	if err == nil {
		t.Fatal("a capped run should error")
	}
	seen := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		var ev map[string]string
		if uerr := json.Unmarshal([]byte(line), &ev); uerr != nil {
			t.Fatalf("stdout should be NDJSON, got %q: %v", line, uerr)
		}
		seen[ev["type"]] = ev["name"] + ev["result"] + ev["error"]
	}
	if seen["tool_start"] != "read" {
		t.Errorf("a tool call should emit tool_start for the tool, got %q", seen["tool_start"])
	}
	if !strings.Contains(seen["tool_end"], "file body") {
		t.Errorf("tool_end should carry the tool result, got %q", seen["tool_end"])
	}
	if !strings.Contains(seen["error"], "max turns") {
		t.Errorf("a failed run should end with an error event, got %q", seen["error"])
	}
}
