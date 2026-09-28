package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func chatRequest() Request {
	return Request{
		SessionID: "session", TurnID: "turn",
		Selection:    session.ModelSelection{Provider: "test", Name: "model", Effort: "low"},
		Instructions: "help accurately",
		Messages:     []Message{{Role: session.User, Parts: []session.Part{{Type: "text", Text: "question"}}}},
	}
}

func chatProvider(url string) OpenAI {
	return OpenAI{Resolve: func(context.Context, session.ModelSelection) (ChatRoute, error) {
		return ChatRoute{URL: url, Credential: "test-secret", MaxOutputTokens: 100, TimeoutMillis: 1000, MaxAttempts: 3}, nil
	}}
}

func TestPreparedChatFreezesWireBodyAndAccountingEvidence(t *testing.T) {
	var received []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("wrong provider route, verb or authentication")
		}
		var err error
		received, err = io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"completed"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":8},"completion_tokens_details":{"reasoning_tokens":2},"cost":0.0000000001}}`)
	}))
	defer server.Close()
	request := chatRequest()
	provider := chatProvider(server.URL + "/v1/")
	price := int64(123)
	resolve := provider.Resolve
	provider.Resolve = func(ctx context.Context, selection session.ModelSelection) (ChatRoute, error) {
		route, err := resolve(ctx, selection)
		route.Prices.Input = &price
		return route, err
	}
	prepared, err := provider.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Messages[0].Parts[0].Text = "changed after preparation"
	price = 999
	response, err := prepared.Execute(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(received)
	if prepared.Snapshot.RequestDigest != hex.EncodeToString(hash[:]) || *prepared.Snapshot.Prices.Input != 123 {
		t.Fatal("prepared body or prices were not frozen")
	}
	var body struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
		Limit    int           `json:"max_completion_tokens"`
		Effort   string        `json:"reasoning_effort"`
	}
	if err := json.Unmarshal(received, &body); err != nil {
		t.Fatal(err)
	}
	if body.Model != "model" || body.Limit != 100 || body.Effort != "low" || len(body.Messages) != 2 || body.Messages[1].Content != "question" {
		t.Fatalf("encoded request: %+v", body)
	}
	if response.Parts[0].Text != "completed" || *response.Usage.Input != 20 || *response.Usage.CachedInput != 8 || *response.Usage.Reasoning != 2 || response.Usage.CachedOutput != nil || *response.ReportedCostNanoUSD != 1 {
		t.Fatalf("response accounting: %+v", response)
	}
	raw, err := json.Marshal(prepared.Snapshot)
	if err != nil || strings.Contains(string(raw), "test-secret") {
		t.Fatal("credential entered durable evidence")
	}
}

func TestChatFailuresDoNotLeakBodiesOrRetryInsideAdapter(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		retryable bool
		uncertain bool
	}{
		{name: "throttled", status: 429, body: `{"error":"test-secret","usage":{"cost":0.25}}`, retryable: true},
		{name: "authentication", status: 401, body: `test-secret`},
		{name: "malformed", status: 200, body: `test-secret`, uncertain: true},
		{name: "malformed choices retain cost", status: 200, body: `{"choices":"test-secret","usage":{"cost":0.25}}`, uncertain: true},
		{name: "oversized", status: 200, body: strings.Repeat("x", maxResponseBytes+1), uncertain: true},
		{name: "truncated", status: 200, body: `{"choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"length"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			prepared, err := chatProvider(server.URL).Prepare(t.Context(), chatRequest())
			if err != nil {
				t.Fatal(err)
			}
			response, err := prepared.Execute(t.Context(), nil)
			failure, ok := errors.AsType[*CallError](err)
			if !ok || failure.Retryable != tc.retryable || failure.Uncertain != tc.uncertain || calls.Load() != 1 {
				t.Fatalf("response=%+v error=%+v calls=%d", response, err, calls.Load())
			}
			if strings.Contains(err.Error(), "test-secret") {
				t.Fatal("provider body leaked")
			}
			if tc.name == "throttled" && (response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000 || failure.RetryAfter != 2*time.Second) {
				t.Fatal("failure accounting or retry delay lost")
			}
			if tc.name == "malformed choices retain cost" && (response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000) {
				t.Fatal("invalid completion erased valid charge evidence")
			}
		})
	}
}

func TestChatRedirectDoesNotChangeRouteOrForwardCredential(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	prepared, err := chatProvider(server.URL).Prepare(t.Context(), chatRequest())
	if err != nil {
		t.Fatal(err)
	}
	_, err = prepared.Execute(t.Context(), nil)
	failure, ok := errors.AsType[*CallError](err)
	if !ok || failure.StatusCode != http.StatusTemporaryRedirect || failure.Retryable || redirected.Load() != 0 {
		t.Fatalf("redirect: %v calls=%d", err, redirected.Load())
	}
}

func TestChatCancellationReleasesTheRequest(t *testing.T) {
	entered, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	prepared, err := chatProvider(server.URL).Prepare(t.Context(), chatRequest())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := prepared.Execute(ctx, nil); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request cancellation hung")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("provider connection remained open")
	}
}

func TestChatAccountingPresenceAndMalformedFields(t *testing.T) {
	for _, tc := range []struct {
		name, raw           string
		input, output, cost *int64
		note                bool
	}{
		{name: "absent"},
		{name: "empty", raw: `{}`},
		{name: "known zero", raw: `{"prompt_tokens":0,"completion_tokens":0,"cost":0}`, input: new(int64(0)), output: new(int64(0)), cost: new(int64(0))},
		{name: "precise cost", raw: `{"cost":9.007199254740993}`, cost: new(int64(9007199255))},
		{name: "invalid input keeps output", raw: `{"prompt_tokens":-1,"completion_tokens":2,"cost":0.1}`, output: new(int64(2)), cost: new(int64(100000000)), note: true},
		{name: "invalid cost keeps usage", raw: `{"prompt_tokens":3,"cost":"secret"}`, input: new(int64(3)), note: true},
		{name: "overflow", raw: `{"cost":1e100}`, note: true},
		{name: "extreme exponent", raw: `{"cost":1e999999999}`, note: true},
		{name: "details contradict total", raw: `{"completion_tokens":1,"completion_tokens_details":{"reasoning_tokens":2},"cost":0}`, cost: new(int64(0)), note: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage, cost, note := decodeChatUsage(json.RawMessage(tc.raw))
			value := func(v *int64) string {
				if v == nil {
					return "unknown"
				}
				return strconv.FormatInt(*v, 10)
			}
			if value(usage.Input) != value(tc.input) || value(usage.Output) != value(tc.output) || value(cost) != value(tc.cost) || (note != nil) != tc.note {
				t.Fatalf("input=%s output=%s cost=%s note=%v", value(usage.Input), value(usage.Output), value(cost), note)
			}
		})
	}
}

func TestRepeatedContentCannotExceedProviderEncodingBudget(t *testing.T) {
	request := chatRequest()
	request.Contents = map[string]Content{"image": {MediaType: "image/png", Data: make([]byte, 3<<20)}}
	request.Messages[0].Parts = []session.Part{{Type: "content", ReferenceID: "image"}, {Type: "content", ReferenceID: "image"}}
	if _, err := chatProvider("https://provider.example").Prepare(t.Context(), request); err == nil {
		t.Fatal("repeated reference exceeded aggregate encoding budget")
	}
	request.Messages[0].Parts = []session.Part{{Type: "content", ReferenceID: "unresolved"}}
	if _, err := chatProvider("https://provider.example").Prepare(t.Context(), request); err == nil {
		t.Fatal("provider encoded an unauthorized reference")
	}
}
