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
		"rlm": {"enabled": false},
		"mcpImport": {"claude": {"enabled": false}, "codex": {"enabled": false}},
		"providers": {"testprov": {"baseUrl": %q, "api": "openai-completions", "apiKey": "k"}},
		"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
	}`, srv.URL)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	useTestDaemon(t)
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

func TestRunReasoningEffortIsSessionScoped(t *testing.T) {
	var requests []llm.Request
	runFixture(t, "done", &requests)
	if _, err := runCapture(t, "", "--effort", "high", "think carefully"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].ReasoningEffort != "high" {
		t.Fatalf("reasoning effort did not reach provider: %#v", requests)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultEffort != "" {
		t.Fatalf("one-off effort changed default: %q", cfg.DefaultEffort)
	}
	if _, err := runCapture(t, "", "--effort", "off", "reply directly"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[1].ReasoningEffort != "" {
		t.Fatalf("off leaked as an upstream reasoning effort: %#v", requests)
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

// -cache-key pins prompt_cache_key so runs share a provider prefix cache;
// without it the daemon keeps keying the cache by session id.
func TestRunCacheKey(t *testing.T) {
	var reqs []llm.Request
	runFixture(t, "ok", &reqs)
	if _, err := runCapture(t, "", "-cache-key", "repo/reviewer", "hi"); err != nil {
		t.Fatal(err)
	}
	if got := reqs[len(reqs)-1].PromptCacheKey; got != "repo/reviewer" {
		t.Fatalf("-cache-key should reach the provider request, got %q", got)
	}

	runFixture(t, "ok", &reqs)
	if _, err := runCapture(t, "", "hi"); err != nil {
		t.Fatal(err)
	}
	if got := reqs[len(reqs)-1].PromptCacheKey; got == "" || got == "repo/reviewer" {
		t.Fatalf("default should key the cache by session id, got %q", got)
	}
}

// -max-turns caps the tool loop; on the cap the model makes one final no-tools
// answer instead of erroring.
func TestRunMaxTurns(t *testing.T) {
	// Loops tool calls while tools are offered; answers with text once the
	// final (no-tools) request arrives — like a real model.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		if len(req.Tools) == 0 {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"final answer"},"finish_reason":"stop"}]}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"read","arguments":"{\"path\":\"/tmp/x\"}"}}]}}]}`+"\n\n")
			fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	cfg := fmt.Sprintf(`{
		"defaultModel": "test",
		"rlm": {"enabled": false},
		"providers": {"testprov": {"baseUrl": %q, "api": "openai-completions", "apiKey": "k"}},
		"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
	}`, srv.URL)
	os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600)
	useTestDaemon(t)

	out, err := runCapture(t, "", "-max-turns", "2", "-no-session", "loop forever")
	if err != nil {
		t.Fatalf("a capped run should finalize, not error: %v", err)
	}
	if !strings.Contains(out, "final answer") {
		t.Fatalf("capped run should return the forced final answer, got %q", out)
	}
}

// -timeout cancels an in-flight run and reports the timeout.
func TestRunTimeout(t *testing.T) {
	requested := make(chan struct{}, 1)
	canceled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		requested <- struct{}{}
		<-r.Context().Done()
		canceled <- struct{}{}
	}))
	defer srv.Close()
	defer srv.CloseClientConnections()
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	cfg := fmt.Sprintf(`{
		"defaultModel": "test",
		"rlm": {"enabled": false},
		"providers": {"testprov": {"baseUrl": %q, "api": "openai-completions", "apiKey": "k"}},
		"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
	}`, srv.URL)
	os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600)
	useTestDaemon(t)
	// Start the owner before timing a model turn so instrumentation and a cold
	// build cannot turn this into a test of daemon startup time instead.
	warm, err := connectDaemon(t.Context(), "automation", "timeout-warmup", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = warm.Close()

	// Allow a cold per-session worker to start; the provider remains blocked
	// until cancellation, so this still exercises an in-flight timeout.
	_, err = runCapture(t, "", "-timeout", "5s", "-no-session", "hi")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("a timed-out run should say so, got %v", err)
	}
	select {
	case <-requested:
	default:
		t.Fatal("timeout test never reached the provider")
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("timed-out run did not cancel its provider request")
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

func configDir() (string, error) { return os.Getenv("WHIP_HOME"), nil }

func sessionOpen(dir string) (*session.Store, error) { return session.Open(runtimeDBPath(dir)) }

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
	targetFile, err := os.CreateTemp(".", "run-tool-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	target := targetFile.Name()
	t.Cleanup(func() { _ = os.Remove(target) })
	if _, err := targetFile.WriteString("file body"); err != nil {
		t.Fatal(err)
	}
	if err := targetFile.Close(); err != nil {
		t.Fatal(err)
	}
	// Answers with a read tool call while tools are offered; once -max-turns
	// forces a no-tools call, answers with text.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		if len(req.Tools) == 0 {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		args, _ := json.Marshal(map[string]string{"code": fmt.Sprintf("files.read(path=%q)", target)})
		call, _ := json.Marshal(string(args))
		fmt.Fprintf(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"rlm_exec","arguments":%s}}]}}]}`+"\n\n", call)
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	cfg := fmt.Sprintf(`{
		"defaultModel": "test",
		"rlm": {"enabled": false},
		"providers": {"testprov": {"baseUrl": %q, "api": "openai-completions", "apiKey": "k"}},
		"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
	}`, srv.URL)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	useTestDaemon(t)

	out, err := runCapture(t, "", "-format", "json", "-max-turns", "2", "-quiet", "-no-session", "read it")
	if err != nil {
		t.Fatalf("a capped run should finalize, not error: %v", err)
	}
	seen := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		var ev map[string]string
		if uerr := json.Unmarshal([]byte(line), &ev); uerr != nil {
			t.Fatalf("stdout should be NDJSON, got %q: %v", line, uerr)
		}
		seen[ev["type"]] = ev["name"] + ev["result"] + ev["error"] + ev["text"]
	}
	if seen["tool_start"] != "rlm_exec" {
		t.Errorf("a tool call should emit tool_start for the tool, got %q", seen["tool_start"])
	}
	if !strings.Contains(seen["tool_end"], "file body") {
		t.Errorf("tool_end should carry the tool result, got %q", seen["tool_end"])
	}
	if _, ok := seen["done"]; !ok {
		t.Errorf("a finalized run should end with a done event, got %v", seen)
	}
}

// --format json surfaces provider reasoning tokens as reasoning events, ahead
// of the reply text, so downstream tools can show thinking activity live.
func TestRunJSONReasoning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"reasoning_content":"let me think"}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"answer"},"finish_reason":"stop"}]}`+"\n\n")
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
	useTestDaemon(t)

	out, err := runCapture(t, "", "--format", "json", "go")
	if err != nil {
		t.Fatal(err)
	}
	var reasoning string
	var sawText bool
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		var ev map[string]string
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line not JSON: %q: %v", line, err)
		}
		switch ev["type"] {
		case "reasoning":
			reasoning += ev["delta"]
		case "text":
			sawText = true
		}
	}
	if reasoning != "let me think" {
		t.Fatalf("want reasoning event with thinking tokens, got %q", reasoning)
	}
	if !sawText {
		t.Fatalf("want a text event too, got:\n%s", out)
	}
}

func TestRunExecutionEngineSelectionAndResume(t *testing.T) {
	runFixture(t, "done", nil)
	_, err := runCapture(t, "", "--rlm-engine", "quickjs", "--permission-mode", "automatic", "--max-tokens", "10000", "select language")
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.Open(filepath.Join(os.Getenv("WHIP_HOME"), "runtime-v2", "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sessions, err := store.Recent(10)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions=%+v %v", sessions, err)
	}
	root := sessions[0]
	if root.ExecutionEngine != "quickjs" {
		t.Fatalf("engine=%s", root.ExecutionEngine)
	}
	if _, err := runCapture(t, "", "--resume", root.ID, "--rlm-engine", "starlark", "conflict"); err == nil || !strings.Contains(err.Error(), "cannot resume") {
		t.Fatalf("resume conflict=%v", err)
	}
	if _, err := runCapture(t, "", "--resume", root.ID, "--rlm-engine", "quickjs", "matching assertion"); err != nil {
		t.Fatal(err)
	}
}

func TestRunAgentSelectionAndResume(t *testing.T) {
	runFixture(t, "done", nil)
	if _, err := runCapture(t, "", "--agent", "junior-developer", "--permission-mode", "automatic", "--max-tokens", "10000", "select agent"); err != nil {
		t.Fatal(err)
	}
	store, err := session.Open(filepath.Join(os.Getenv("WHIP_HOME"), "runtime-v2", "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sessions, err := store.Recent(10)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions=%+v %v", sessions, err)
	}
	root := sessions[0]
	if root.Definition != "junior-developer" {
		t.Fatalf("definition=%s", root.Definition)
	}
	if _, err := runCapture(t, "", "--resume", root.ID, "--agent", "coding", "conflict"); err == nil || !strings.Contains(err.Error(), "cannot resume") {
		t.Fatalf("resume conflict=%v", err)
	}
	if _, err := runCapture(t, "", "--resume", root.ID, "--agent", "junior-developer", "matching assertion"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCapture(t, "", "--agent", "architect", "prompt"); err == nil || !strings.Contains(err.Error(), "junior-developer") {
		t.Fatalf("unknown agent accepted: %v", err)
	}
}

func TestRunRejectsInvalidEngineAndLimits(t *testing.T) {
	for _, args := range [][]string{{"--rlm-engine", "node"}, {"--permission-mode", "yes"}, {"--max-cost", "NaN"}, {"--max-cost", "-1"}, {"--max-tokens", "-1"}} {
		if _, err := runCapture(t, "", append(args, "prompt")...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestRunRejectsUnrepresentableCostCaps(t *testing.T) {
	for _, test := range []struct {
		value, want string
	}{
		{"0.0000001", "--max-cost must be at least"},
		{"0.0000009", "--max-cost must be at least"},
		{"9223372036854.775808", "--max-cost is too large"},
		{"1e100", "--max-cost is too large"},
	} {
		t.Run(test.value, func(t *testing.T) {
			_, err := runCapture(t, "", "--max-cost", test.value, "prompt")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("cost %s: got %v, want %q", test.value, err, test.want)
			}
		})
	}
}

func TestRunAutomaticHeadlessHonorsSavedPermission(t *testing.T) {
	for _, engine := range []string{"starlark", "quickjs"} {
		for _, mode := range []string{"prompt", "automatic"} {
			t.Run(engine+"/"+mode, func(t *testing.T) {
				target := filepath.Join(t.TempDir(), "proof.txt")
				code := fmt.Sprintf("files.write(path=%q, content=\"written\")", target)
				if engine == "quickjs" {
					code = fmt.Sprintf("await files.write({path:%q, content:'written'});", target)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req llm.Request
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Error(err)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					if req.Messages[len(req.Messages)-1].Role == "tool" || len(req.Tools) == 0 {
						fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\n")
						return
					}
					args, _ := json.Marshal(map[string]string{"code": code})
					call, _ := json.Marshal(string(args))
					fmt.Fprintf(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"write","type":"function","function":{"name":"rlm_exec","arguments":%s}}]},"finish_reason":"tool_calls"}]}`+"\n\n", call)
				}))
				defer server.Close()
				home := t.TempDir()
				t.Setenv("WHIP_HOME", home)
				cfg := fmt.Sprintf(`{"defaultModel":"test","mcpImport":{"claude":{"enabled":false},"codex":{"enabled":false}},"providers":{"testprov":{"baseUrl":%q,"api":"openai-completions","apiKey":"k"}},"models":{"test":{"providers":["testprov"],"maxOut":100}}}`, server.URL)
				if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600); err != nil {
					t.Fatal(err)
				}
				useTestDaemon(t)
				if _, err := runCapture(t, "", "--rlm-engine", engine, "--permission-mode", mode, "--max-turns", "2", "--timeout", "20s", "write proof"); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(target)
				if mode == "automatic" && (err != nil || string(data) != "written") {
					t.Fatalf("automatic headless write=%q err=%v", data, err)
				}
				if mode == "prompt" && !os.IsNotExist(err) {
					t.Fatalf("prompt mode wrote without authorization: %q %v", data, err)
				}
			})
		}
	}
}
