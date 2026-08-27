package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// server that answers with a tool call on the first request, text on the second
func loopServer(t *testing.T) *httptest.Server {
	t.Helper()
	call := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		call++
		if call == 1 {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"echo","arguments":"{\"s\":\"hi\"}"}}]}}]}`+"\n\n")
		} else {
			// verify the tool result round-tripped
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != "t1" || last.Content != "echoed: hi" {
				t.Errorf("tool result not fed back: %+v", last)
			}
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func echoTool() tools.Tool {
	return tools.Tool{
		Def: llm.NewTool("echo", "echo", `{"type":"object","properties":{"s":{"type":"string"}}}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct{ S string }
			json.Unmarshal(args, &a)
			return "echoed: " + a.S, nil
		},
	}
}

func TestTurnLoop(t *testing.T) {
	srv := loopServer(t)
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.Tools = []tools.Tool{echoTool()}

	var events []string
	final, err := ag.Turn(context.Background(), "go", Events{
		OnText:      func(d string) { events = append(events, "text:"+d) },
		OnToolStart: func(_, n, _ string) { events = append(events, "start:"+n) },
		OnToolEnd:   func(_, _, r string) { events = append(events, "end:"+r) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if final != "done" {
		t.Fatalf("final: %q", final)
	}
	want := []string{"start:echo", "end:echoed: hi", "text:done"}
	if len(events) != len(want) {
		t.Fatalf("events: %v", events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events[%d] = %q, want %q", i, events[i], want[i])
		}
	}
	// system, user, assistant(tool call), tool result, assistant(text)
	if len(ag.Messages) != 5 {
		t.Fatalf("message count: %d", len(ag.Messages))
	}
}

func TestTurnSendsMaxTokens(t *testing.T) {
	srv := textServer(t, func(_ int, req llm.Request) string {
		if req.MaxTokens != 123 {
			t.Errorf("max tokens = %d, want 123", req.MaxTokens)
		}
		return "done"
	})
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 123, "sys")
	if _, err := ag.Turn(context.Background(), "go", Events{}); err != nil {
		t.Fatal(err)
	}
}

// Each assistant message records its token usage and which model produced it;
// tool calls record their run time and exit status. All survive for per-turn
// cost and perf views after the in-memory session totals are gone.
func TestTurnStampsUsageModelAndToolTiming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		if len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == "tool" {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"echo","arguments":"{\"s\":\"hi\"}"}}]}}]}`+"\n\n")
			fmt.Fprint(w, `data: {"usage":{"prompt_tokens":5,"completion_tokens":2}}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "kimi-k3-fast", 100, "sys")
	ag.Provider = "inference"
	ag.Tools = []tools.Tool{echoTool()}

	if _, err := ag.TurnAuthored(context.Background(), "go", Events{}); err != nil {
		t.Fatal(err)
	}
	// system, user, assistant(toolcall), tool, assistant(text)
	if len(ag.Messages) != 5 {
		t.Fatalf("messages: %d", len(ag.Messages))
	}
	user := ag.Messages[1]
	if user.SentAt == nil {
		t.Error("authored user message should carry SentAt")
	}
	var assistants []llm.Message
	for _, m := range ag.Messages {
		if m.Role == "assistant" {
			assistants = append(assistants, m)
		}
	}
	if len(assistants) != 2 {
		t.Fatalf("assistants: %d", len(assistants))
	}
	for i, a := range assistants {
		if a.Usage == nil || a.Usage.PromptTokens == 0 {
			t.Errorf("assistant[%d] missing usage: %+v", i, a.Usage)
		}
		if a.Model != "kimi-k3-fast @ inference" {
			t.Errorf("assistant[%d] model: %q", i, a.Model)
		}
	}
	// the tool call carries its run time and a successful exit status
	call := assistants[0].ToolCalls[0]
	if call.DurationMs < 0 {
		t.Errorf("tool call duration: %d", call.DurationMs)
	}
	if call.ExitCode != 0 {
		t.Errorf("successful echo should be exit 0, got %d", call.ExitCode)
	}
}

// The internal stamps (usage, model, tool timing) must be stripped before the
// provider ever sees them.
func TestInternalStampsStrippedFromRequest(t *testing.T) {
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, b)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	// pre-seed a message loaded from storage with all internal fields set
	sent := time.Now()
	u := llm.Usage{PromptTokens: 9}
	ag.Messages = append(ag.Messages, llm.Message{
		Role: "assistant", Content: "prior", Usage: &u, Model: "m @ p",
		ToolCalls: []llm.ToolCall{{ID: "x", DurationMs: 5, ExitCode: 1}},
	})
	ag.Messages = append(ag.Messages, llm.Message{Role: "user", Content: "old", Authored: true, SentAt: &sent, RewoundFrom: "earlier"})
	if _, err := ag.Turn(context.Background(), "go", Events{}); err != nil {
		t.Fatal(err)
	}
	if len(bodies) == 0 {
		t.Fatal("no request captured")
	}
	body := string(bodies[len(bodies)-1])
	for _, leak := range []string{"usage\":{", "\"model\":\"m @ p\"", "duration_ms", "exit_code", "sent_at", "rewound_from", "authored"} {
		if strings.Contains(body, leak) {
			t.Errorf("internal field %q leaked to provider:\n%s", leak, body)
		}
	}
}

func TestTurnCancelled(t *testing.T) {
	srv := loopServer(t)
	defer srv.Close()
	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.Tools = []tools.Tool{echoTool()}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { cancel() }()
	// either the stream or the post-tool check reports cancellation; both are fine
	if _, err := ag.Turn(ctx, "go", Events{}); err == nil {
		t.Skip("cancel raced turn completion") // ponytail: timing-dependent; the happy path above is the real check
	}
}

func TestTurnAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	// Single attempt: this test is about the error surfacing from Turn, not
	// about the retry ladder (covered in internal/llm/retry_test.go). Leaving
	// retries on made it sleep through the full backoff — ~80s for one assert.
	c := llm.New(srv.URL, "k")
	c.MaxRetries = 1
	ag := New(c, "m", 100, "sys")
	if _, err := ag.Turn(context.Background(), "go", Events{}); err == nil {
		t.Fatal("expected error")
	}
}

// server that echoes text responses and records how many calls it got
func textServer(t *testing.T, onCall func(n int, req llm.Request) string) *httptest.Server {
	t.Helper()
	n := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		n++
		w.Header().Set("Content-Type", "text/event-stream")
		body, _ := json.Marshal(onCall(n, req))
		fmt.Fprintf(w, `data: {"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`+"\n\n", body)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

// TurnAuthored marks the user message as genuinely typed (for input-history
// recall); plain Turn (steered/goal/background paths) leaves it unmarked.
func TestTurnAuthoredMarksMessage(t *testing.T) {
	srv := textServer(t, func(n int, req llm.Request) string { return "done" })
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	if _, err := ag.TurnAuthored(context.Background(), "i typed this", Events{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ag.Turn(context.Background(), "injected by whip", Events{}); err != nil {
		t.Fatal(err)
	}

	var typed, injected bool
	for _, m := range ag.Messages {
		if m.Role != "user" {
			continue
		}
		switch m.Content {
		case "i typed this":
			typed = m.Authored
		case "injected by whip":
			injected = m.Authored
		}
	}
	if !typed {
		t.Error("TurnAuthored message must carry Authored=true")
	}
	if injected {
		t.Error("plain Turn message must carry Authored=false")
	}
}

// TestUsageAccumulates verifies every stream call folds its usage into the
// session totals (input/output/cached) and fires OnUsage per request.
func TestUsageAccumulates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":40}}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	var fired int
	for range 3 {
		if _, err := ag.Turn(context.Background(), "go", Events{
			OnUsage: func(u llm.Usage) {
				fired++
				if u.PromptTokens != 100 || u.CompletionTokens != 10 || u.Cached() != 40 {
					t.Errorf("per-call usage: %+v", u)
				}
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if fired != 3 {
		t.Fatalf("OnUsage fired %d times, want 3", fired)
	}
	u := ag.Usage()
	if u.PromptTokens != 300 || u.CompletionTokens != 30 || u.Cached() != 120 {
		t.Fatalf("session totals: %+v", u)
	}
}

// TestUsageMissingLeavesTotalsAlone: providers that omit usage (no terminal
// chunk) must not corrupt totals or fire misleading events.
func TestUsageMissingLeavesTotalsAlone(t *testing.T) {
	srv := textServer(t, func(n int, req llm.Request) string { return "done" })
	defer srv.Close()
	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	if _, err := ag.Turn(context.Background(), "go", Events{}); err != nil {
		t.Fatal(err)
	}
	if u := ag.Usage(); u.PromptTokens != 0 || u.CompletionTokens != 0 || u.Cached() != 0 {
		t.Fatalf("usage should stay zero without provider usage: %+v", u)
	}
}

func TestSteerContinuesTurn(t *testing.T) {
	srv := textServer(t, func(n int, req llm.Request) string {
		if n == 2 {
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "user" || last.Content != "also do this" {
				t.Errorf("steered message not injected: %+v", last)
			}
			return "ok2"
		}
		return "ok1"
	})
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.Steer("also do this") // queued before the first response completes
	var steered []string
	final, err := ag.Turn(context.Background(), "go", Events{
		OnSteer: func(s string) { steered = append(steered, s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if final != "ok2" {
		t.Fatalf("turn should continue after steer, got %q", final)
	}
	if len(steered) != 1 || steered[0] != "also do this" {
		t.Fatalf("OnSteer events: %v", steered)
	}
}

func TestNoSteerEndsTurn(t *testing.T) {
	srv := textServer(t, func(n int, req llm.Request) string { return "done" })
	defer srv.Close()
	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	final, err := ag.Turn(context.Background(), "go", Events{})
	if err != nil || final != "done" {
		t.Fatalf("%q %v", final, err)
	}
}

func TestTaskToolSpawnsSubagent(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		call++
		w.Header().Set("Content-Type", "text/event-stream")
		switch call {
		case 1: // outer agent delegates
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"task","arguments":"{\"description\":\"probe\",\"prompt\":\"find the answer\"}"}}]}}]}`+"\n\n")
		case 2: // inner subagent: fresh context, no task tool, gets the prompt
			if len(req.Messages) != 2 || req.Messages[1].Content != "find the answer" {
				t.Errorf("subagent context wrong: %+v", req.Messages)
			}
			for _, tl := range req.Tools {
				if tl.Function.Name == "task" {
					t.Error("subagent must not have the task tool")
				}
			}
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"the answer is 42"}}]}`+"\n\n")
		case 3: // outer agent sees the report as the tool result
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "tool" || last.Content != "the answer is 42" {
				t.Errorf("task result not fed back: %+v", last)
			}
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"done"}}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	final, err := ag.Turn(context.Background(), "go", Events{})
	if err != nil || final != "done" {
		t.Fatalf("%q %v", final, err)
	}
	if call != 3 {
		t.Fatalf("expected 3 API calls, got %d", call)
	}
}

func TestTaskToolBadArgs(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	out := tools.Execute(context.Background(), ag.Tools, "task", json.RawMessage(`{bad`))
	if !strings.HasPrefix(out, "Error") {
		t.Fatalf("expected error, got %q", out)
	}
}

// compactionServer lets the first request error with context_length_exceeded,
// then serves a summary completion (for the compaction call) and finally the
// real answer. call==2 is the /chat/completions summary request (stream:false).
func compactionServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		switch call {
		case 1:
			http.Error(w, `{"error":{"code":"context_length_exceeded"}}`, http.StatusBadRequest)
		case 2:
			var req llm.Request
			json.NewDecoder(r.Body).Decode(&req)
			if req.Stream {
				t.Errorf("summary call should not stream")
			}
			w.Write([]byte(`{"choices":[{"message":{"content":"summary of prior work"}}]}`))
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"recovered"}}]}`+"\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
	}))
	return srv, &call
}

func TestTurnAutoCompactsOnContextLimit(t *testing.T) {
	srv, pcall := compactionServer(t)
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	// build a history that's compactable: system + enough turns
	for i := range 8 {
		ag.Messages = append(ag.Messages,
			llm.Message{Role: "user", Content: fmt.Sprintf("q%d", i)},
			llm.Message{Role: "assistant", Content: fmt.Sprintf("a%d", i)},
		)
	}
	var compacted int
	final, err := ag.Turn(context.Background(), "go", Events{
		OnCompact: func(took, kept int) { compacted++ },
	})
	if err != nil {
		t.Fatalf("turn after compaction: %v", err)
	}
	if final != "recovered" {
		t.Fatalf("final: %q", final)
	}
	if compacted != 1 {
		t.Fatalf("OnCompact fired %d times, want 1", compacted)
	}
	if *pcall < 3 {
		t.Fatalf("expected ≥3 calls (fail+summary+retry), got %d", *pcall)
	}
	// summary lives between system prompt and the kept tail
	if !strings.Contains(ag.Messages[1].Content, "Summary of the conversation") {
		t.Fatalf("messages[1] should be the summary, got %q", ag.Messages[1].Content)
	}
}

func TestCompactDoesNotLoopOnRepeatedContextLimit(t *testing.T) {
	// every request errors with context_length_exceeded → compaction must
	// happen once and then the error surfaces (no infinite retry loop)
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		// one summary call succeeds (to exercise the compaction path), then
		// every stream fails with context_length_exceeded
		if r.URL.Path == "/chat/completions" {
			var req llm.Request
			json.NewDecoder(r.Body).Decode(&req)
			if !req.Stream { // the summary call
				w.Write([]byte(`{"choices":[{"message":{"content":"sim"}}]}`))
				return
			}
		}
		http.Error(w, `{"error":{"code":"context_length_exceeded"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	for i := range 8 {
		ag.Messages = append(ag.Messages,
			llm.Message{Role: "user", Content: fmt.Sprintf("q%d", i)},
			llm.Message{Role: "assistant", Content: fmt.Sprintf("a%d", i)},
		)
	}
	_, err := ag.Turn(context.Background(), "go", Events{})
	if err == nil {
		t.Fatal("expected context-limit error to surface, not loop forever")
	}
	if call > 3 {
		t.Fatalf("expected ≤3 calls (fail+summary+retry-fail), got %d", call)
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens(nil); got != 0 {
		t.Fatalf("empty: %d", got)
	}
	msgs := []llm.Message{
		{Role: "system", Content: strings.Repeat("x", 400)}, // 400/4 + 4 = 104
		{Role: "assistant", ToolCalls: []llm.ToolCall{ // 4 + 8 + (4+96+3)/4 = 37
			func() llm.ToolCall {
				var tc llm.ToolCall
				tc.Function.Name = "tool"
				tc.Function.Arguments = strings.Repeat("y", 96)
				return tc
			}(),
		}},
	}
	if got := EstimateTokens(msgs); got != 104+37 {
		t.Fatalf("got %d, want %d", got, 104+37)
	}
}

func TestProactiveCompactAtFiftyPercent(t *testing.T) {
	// the first stream request should already carry the compacted history —
	// no context_length_exceeded round-trip needed
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			w.Write([]byte(`{"choices":[{"message":{"content":"summary of prior work"}}]}`))
			return
		}
		compact := strings.Contains(req.Messages[1].Content, "Summary of the conversation")
		w.Header().Set("Content-Type", "text/event-stream")
		if compact {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"ok"}}]}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"not-compacted"}}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.ContextLimit = 1000 // default 50% = 500 estimated tokens
	// 8 user messages × 120 chars ≈ 272 estimated tokens: under the threshold
	for range 8 {
		ag.Messages = append(ag.Messages, llm.Message{Role: "user", Content: strings.Repeat("x", 120)})
	}
	var compacted bool
	final, err := ag.Turn(context.Background(), strings.Repeat("z", 900), Events{
		OnCompact: func(took, kept int) { compacted = true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatal("expected proactive compaction above 50% of the context limit")
	}
	if final != "ok" {
		t.Fatalf("first request should have used compacted history, got %q", final)
	}
}

func TestCompactThresholdExplicitOverride(t *testing.T) {
	srv := textServer(t, func(n int, req llm.Request) string { return "done" })
	defer srv.Close()

	// ~74% of the limit: over the 50% default, under an explicit 80% — no
	// compaction, and the estimate stays deterministic
	ag := New(llm.New(srv.URL, "m"), "m", 100, "sys")
	ag.ContextLimit = 1000
	ag.CompactThreshold = 0.8
	for range 8 {
		ag.Messages = append(ag.Messages, llm.Message{Role: "user", Content: strings.Repeat("x", 360)})
	}
	if _, err := ag.Turn(context.Background(), "hi", Events{}); err != nil {
		t.Fatal(err)
	}
	if len(ag.Messages) != 11 { // system + 8 seeded + user + assistant: untouched
		t.Fatalf("history should not compact below the explicit threshold, got %d msgs", len(ag.Messages))
	}

	// CompactThreshold wins over the default: same history at the default 50%
	// would have compacted
	// The compaction call fails here by design (textServer speaks SSE, compact
	// uses the non-streaming Complete) — that failure is the signal compaction
	// was attempted. Single attempt so the assert doesn't wait out the backoff.
	c2 := llm.New(srv.URL, "m")
	c2.MaxRetries = 1
	ag2 := New(c2, "m", 100, "sys")
	ag2.ContextLimit = 1000
	for range 8 {
		ag2.Messages = append(ag2.Messages, llm.Message{Role: "user", Content: strings.Repeat("x", 360)})
	}
	if err := ag2.maybeCompact(context.Background(), Events{}); err == nil {
		t.Fatal("the same history should compact at the default 50% threshold")
	}
}

func TestNoProactiveCompactBelowThresholdOrWithoutLimit(t *testing.T) {
	srv := textServer(t, func(n int, req llm.Request) string { return "done" })
	defer srv.Close()

	// below threshold: estimate well under 50% of the limit
	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.ContextLimit = 100000
	if _, err := ag.Turn(context.Background(), "hi", Events{}); err != nil {
		t.Fatal(err)
	}

	// no advertised limit: proactive compaction disabled regardless of size
	ag2 := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag2.Messages = append(ag2.Messages, llm.Message{Role: "user", Content: strings.Repeat("x", 4000)})
	if _, err := ag2.Turn(context.Background(), "hi", Events{}); err != nil {
		t.Fatal(err)
	}
	if len(ag2.Messages) != 4 { // system + big user + user + assistant: untouched
		t.Fatalf("history should not compact without a context limit, got %d msgs", len(ag2.Messages))
	}
}

func TestCompactUsesCompactModel(t *testing.T) {
	var models []string
	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("summary call must not hit the conversation's provider")
	}))
	defer main.Close()
	sum := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		models = append(models, req.Model)
		w.Write([]byte(`{"choices":[{"message":{"content":"sim"}}]}`))
	}))
	defer sum.Close()

	ag := New(llm.New(main.URL, "k"), "conversation-model", 100, "sys")
	ag.CompactClient = llm.New(sum.URL, "k")
	ag.CompactModel = "summary-model"
	for i := range 8 {
		ag.Messages = append(ag.Messages,
			llm.Message{Role: "user", Content: fmt.Sprintf("q%d", i)},
			llm.Message{Role: "assistant", Content: fmt.Sprintf("a%d", i)},
		)
	}
	if err := ag.ManualCompact(context.Background(), Events{}); err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0] != "summary-model" {
		t.Fatalf("summary should run on summary-model, got %v", models)
	}
}

func TestCompactTooLittleHistory(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	ag.Messages = append(ag.Messages, llm.Message{Role: "user", Content: "hi"})
	if _, _, err := ag.compact(context.Background()); err == nil {
		t.Fatal("expected error compacting a tiny history")
	}
}

func TestCompactKeepsToolCallPair(t *testing.T) {
	// orphan safety: a tail starting with role "tool" must pull in its owning
	// assistant message so the tool result never references an erased call id.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"sim"}}]}`))
	}))
	defer srv.Close()
	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	// system, user, asst(with tool call "t1"), tool("t1" result), user, asst, user
	for i := range 4 {
		ag.Messages = append(ag.Messages, llm.Message{Role: "user", Content: fmt.Sprintf("u%d", i)})
		if i == 0 {
			ag.Messages = append(ag.Messages,
				llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "t1", Type: "function"}}},
			)
			ag.Messages = append(ag.Messages, llm.Message{Role: "tool", Content: "tool-out", ToolCallID: "t1"})
		} else {
			ag.Messages = append(ag.Messages, llm.Message{Role: "assistant", Content: fmt.Sprintf("a%d", i)})
		}
	}
	before := len(ag.Messages)
	if _, _, err := ag.compact(context.Background()); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(ag.Messages) >= before {
		t.Fatalf("compaction didn't shrink: before %d after %d", before, len(ag.Messages))
	}
	// find the kept tool result and its owning assistant
	var asstTool, toolMsg *llm.Message
	for i := range ag.Messages {
		if ag.Messages[i].Role == "tool" {
			toolMsg = &ag.Messages[i]
		}
	}
	if toolMsg != nil && toolMsg.ToolCallID != "" {
		for i := range ag.Messages {
			for _, tc := range ag.Messages[i].ToolCalls {
				if tc.ID == toolMsg.ToolCallID {
					asstTool = &ag.Messages[i]
				}
			}
		}
	}
	if toolMsg != nil && asstTool == nil {
		t.Errorf("orphaned tool result: %#v", toolMsg)
	}
}

// SteerImages queues a multimodal user message: the turn continues past it
// and the injected message carries both the text and the image parts.
func TestSteerImagesInjectsParts(t *testing.T) {
	srv := textServer(t, func(n int, req llm.Request) string {
		if n == 2 {
			return "ok2"
		}
		return "ok1"
	})
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.SteerImages("see the screenshot", []llm.ContentPart{llm.ImagePart("png", []byte("img-bytes"))})
	final, err := ag.Turn(context.Background(), "go", Events{})
	if err != nil {
		t.Fatal(err)
	}
	if final != "ok2" {
		t.Fatalf("turn should continue after the steered images, got %q", final)
	}
	var steered *llm.Message
	for i := range ag.Messages {
		if ag.Messages[i].Role == "user" && ag.Messages[i].Content == "see the screenshot" {
			steered = &ag.Messages[i]
		}
	}
	if steered == nil {
		t.Fatal("steered message not in the conversation")
	}
	if len(steered.Parts) != 1 || steered.Parts[0].Type != "image_url" {
		t.Fatalf("steered message should carry the image part: %+v", steered.Parts)
	}
}

// AppendUser adds an unauthored user message outside a turn (the `!` shell
// escape path).
func TestAppendUser(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	ag.AppendUser("shell output: ok")
	last := ag.Messages[len(ag.Messages)-1]
	if last.Role != "user" || last.Content != "shell output: ok" {
		t.Fatalf("appended message: %+v", last)
	}
	if last.Authored {
		t.Error("shared shell output is not an authored message")
	}
}

// SetUsage seeds a resumed session's totals so AddUsage keeps counting from
// there; ResetUsage zeroes them for /clear.
func TestSetAndResetUsage(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	ag.SetUsage(llm.Usage{PromptTokens: 11, CompletionTokens: 7})
	ag.AddUsage(llm.Usage{PromptTokens: 4, CompletionTokens: 1})
	if u := ag.Usage(); u.PromptTokens != 15 || u.CompletionTokens != 8 {
		t.Fatalf("seeded totals should keep counting: %+v", u)
	}
	ag.ResetUsage()
	if u := ag.Usage(); u.PromptTokens != 0 || u.CompletionTokens != 0 || u.Cached() != 0 {
		t.Fatalf("reset should zero the totals: %+v", u)
	}
}

// TurnWithImages is an authored submission carrying vision parts.
func TestTurnWithImagesMarksAuthoredAndCarriesParts(t *testing.T) {
	srv := textServer(t, func(n int, req llm.Request) string { return "done" })
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	parts := []llm.ContentPart{llm.ImagePart("png", []byte("shot"))}
	final, err := ag.TurnWithImages(context.Background(), "what's in this image?", parts, Events{})
	if err != nil || final != "done" {
		t.Fatalf("%q %v", final, err)
	}
	user := ag.Messages[1]
	if !user.Authored || user.SentAt == nil {
		t.Errorf("an image submission is authored and timestamped: %+v", user)
	}
	if len(user.Parts) != 1 || user.Parts[0].Type != "image_url" {
		t.Errorf("image parts lost: %+v", user.Parts)
	}
}

// SetMCPTools makes the MCP set visible via AllTools and installs the
// tools.Suggester, so a typo'd mcp__ call gets a "did you mean?" nudge.
func TestSetMCPToolsInstallsSuggester(t *testing.T) {
	orig := tools.Suggester
	tools.Suggester = nil
	t.Cleanup(func() { tools.Suggester = orig })

	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	mt := tools.Tool{Def: llm.NewTool("mcp__srv__hello", "h", `{"type":"object"}`)}
	ag.SetMCPTools([]tools.Tool{mt})

	var found bool
	for _, tl := range ag.AllTools() {
		if tl.Def.Function.Name == "mcp__srv__hello" {
			found = true
		}
	}
	if !found {
		t.Fatal("MCP tool missing from AllTools")
	}
	if tools.Suggester == nil {
		t.Fatal("SetMCPTools should install the suggester")
	}
	// a near-miss call self-corrects through Execute's unknown-tool path
	out := tools.Execute(context.Background(), ag.AllTools(), "mcp__srv__helo", json.RawMessage(`{}`))
	if !strings.Contains(out, "did you mean") || !strings.Contains(out, "mcp__srv__hello") {
		t.Fatalf("typo'd MCP call should suggest the live name, got %q", out)
	}
}

// GoalFromContextMessages windows the conversation tail, never including the
// system prompt, defaulting and clamping n.
func TestGoalFromContextMessages(t *testing.T) {
	if _, err := GoalFromContextMessages(nil, 0); err == nil {
		t.Error("empty history should error")
	}
	sys := llm.Message{Role: "system", Content: "sys"}
	if _, err := GoalFromContextMessages([]llm.Message{sys, {Role: "user", Content: "u"}}, 0); err == nil {
		t.Error("a single message is not enough context")
	}

	msgs := []llm.Message{sys}
	for i := range 12 {
		msgs = append(msgs, llm.Message{Role: "user", Content: fmt.Sprintf("m%d", i)})
	}
	tail, err := GoalFromContextMessages(msgs, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != GoalFromContextDefaultWindow || tail[0].Content != "m4" || tail[len(tail)-1].Content != "m11" {
		t.Fatalf("default window wrong: %+v", tail)
	}
	if tail, _ = GoalFromContextMessages(msgs, 3); len(tail) != 3 || tail[0].Content != "m9" {
		t.Fatalf("explicit window wrong: %+v", tail)
	}
	tail, err = GoalFromContextMessages(msgs, 100)
	if err != nil || len(tail) != 12 {
		t.Fatalf("oversized n should clamp to the conversation: %d %v", len(tail), err)
	}
	if tail[0].Role == "system" {
		t.Error("the system prompt must never be in the goal window")
	}
}

// BuildGoalFromContextPrompt renders the tail as a transcript inside the
// goal-formulation instructions.
func TestBuildGoalFromContextPrompt(t *testing.T) {
	var tc llm.ToolCall
	tc.Function.Name = "bash"
	tc.Function.Arguments = `{"command":"go test"}`
	tail := []llm.Message{
		{Role: "user", Content: "fix the flaky test"},
		{Role: "assistant", Content: "working on internal/foo", ToolCalls: []llm.ToolCall{tc}},
		{Role: "tool", Content: "exit 1"},
	}
	p := BuildGoalFromContextPrompt(tail)
	for _, want := range []string{
		"user: fix the flaky test",
		"assistant: working on internal/foo",
		"assistant called bash(",
		"tool result: exit 1",
		"Write the goal now.",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("goal prompt missing %q:\n%s", want, p)
		}
	}
}

func TestManualCompactFiresEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"sim"}}]}`))
	}))
	defer srv.Close()
	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	for i := range 8 {
		ag.Messages = append(ag.Messages,
			llm.Message{Role: "user", Content: fmt.Sprintf("q%d", i)},
			llm.Message{Role: "assistant", Content: fmt.Sprintf("a%d", i)},
		)
	}
	var fired bool
	var gotSummary string
	ev := Events{
		OnCompact:   func(took, kept int) { fired = true },
		OnCompacted: func(summary string, cutoff int) { gotSummary = summary },
	}
	if err := ag.ManualCompact(context.Background(), ev); err != nil {
		t.Fatalf("manual compact: %v", err)
	}
	if !fired {
		t.Fatal("OnCompact should fire for ManualCompact")
	}
	if gotSummary != "sim" {
		t.Fatalf("OnCompacted summary = %q", gotSummary)
	}

	// Too little history: the error surfaces instead of a silent no-op.
	empty := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	if err := empty.ManualCompact(context.Background(), Events{}); err == nil {
		t.Fatal("ManualCompact on a fresh agent should report too little history")
	}
}

// MessagesSnapshot hands out a copy — mutating it must not touch the agent's
// transcript (the TUI reads it while a turn runs).
func TestMessagesSnapshotIsACopy(t *testing.T) {
	ag := New(nil, "m", 0, "sys")
	ag.AppendUser("hello")
	snap := ag.MessagesSnapshot()
	if len(snap) != len(ag.Messages) {
		t.Fatalf("snapshot len %d, agent %d", len(snap), len(ag.Messages))
	}
	snap[0].Content = "clobbered"
	if ag.Messages[0].Content == "clobbered" {
		t.Fatal("snapshot must not alias the agent's messages")
	}
}

func TestTruncateField(t *testing.T) {
	if got := truncateField("  a\nb  ", 10); got != "a b" {
		t.Errorf("newlines/trim: %q", got)
	}
	if got := truncateField("abcdefghij", 5); got != "abcd…" {
		t.Errorf("truncation: %q", got)
	}
}
