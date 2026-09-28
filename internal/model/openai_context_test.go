package model

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestChatContextLimitRequiresConfirmedStructuredRejection(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		detail       string
		contextLimit bool
	}{
		{"code 400", 400, `{"code":"context_length_exceeded"}`, true},
		{"code 413", 413, `{"code":"prompt_too_long"}`, true},
		{"type 400", 400, `{"type":"prompt_too_long"}`, true},
		{"type 413", 413, `{"type":"context_length_exceeded"}`, true},
		{"unrelated type", 400, `{"code":"context_length_exceeded","type":"invalid_request_error"}`, true},
		{"unrelated code", 413, `{"code":"invalid_request","type":"prompt_too_long"}`, true},
		{"unknown code", 400, `{"code":"invalid_request"}`, false},
		{"message only", 400, `{"message":"context_length_exceeded: maximum context length"}`, false},
		{"message substring", 413, `{"message":"prompt_too_long"}`, false},
		{"code substring", 400, `{"code":"wrapped_context_length_exceeded"}`, false},
		{"code case", 400, `{"code":"CONTEXT_LENGTH_EXCEEDED"}`, false},
		{"nested code", 400, `{"metadata":{"code":"context_length_exceeded"}}`, false},
		{"error string", 413, `"prompt_too_long"`, false},
		{"error array", 400, `[{"code":"context_length_exceeded"}]`, false},
		{"error null", 400, `null`, false},
		{"code number", 400, `{"code":400}`, false},
		{"unauthorized", 401, `{"code":"context_length_exceeded"}`, false},
		{"not found", 404, `{"type":"prompt_too_long"}`, false},
		{"success envelope", 200, `{"code":"context_length_exceeded"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"error":`+tc.detail+`,"provider_debug":"provider-secret","choices":"provider-secret","usage":{"prompt_tokens":17,"completion_tokens":2,"cost":0.25}}`)
			}))
			defer server.Close()
			prepared, err := chatProvider(server.URL).Prepare(t.Context(), chatRequest())
			if err != nil {
				t.Fatal(err)
			}
			response, err := prepared.Execute(t.Context(), nil)
			failure, ok := errors.AsType[*CallError](err)
			if !ok || failure.ContextLimit != tc.contextLimit || failure.Retryable || calls.Load() != 1 || len(response.Parts) != 0 {
				t.Fatalf("classification=%+v response=%+v calls=%d", failure, response, calls.Load())
			}
			if strings.Contains(failure.Message, "rejected request context") != tc.contextLimit {
				t.Fatalf("persisted diagnostic lost the confirmed rejection category: %q", failure.Message)
			}
			if tc.contextLimit && failure.Uncertain {
				t.Fatal("confirmed rejection was marked uncertain")
			}
			if response.Usage.Input == nil || *response.Usage.Input != 17 || response.Usage.Output == nil || *response.Usage.Output != 2 || response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000 {
				t.Fatalf("independent accounting lost: %+v", response)
			}
			if strings.Contains(err.Error(), "provider-secret") || strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), tc.detail) {
				t.Fatalf("provider details leaked: %v", err)
			}
		})
	}
}

type contextLimitTransport func(*http.Request) (*http.Response, error)

func (f contextLimitTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestChatContextLimitNeverPromotesStreamOrUncertainFailure(t *testing.T) {
	const rejection = `{"error":{"code":"context_length_exceeded","message":"provider-secret"}}`
	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		body        string
		readFailure bool
		transport   bool
		uncertain   bool
	}{
		{name: "plain marker", status: 400, body: "context_length_exceeded provider-secret"},
		{name: "plain prompt marker", status: 413, body: "prompt_too_long provider-secret"},
		{name: "truncated json", status: 400, body: rejection[:len(rejection)-1]},
		{name: "trailing json", status: 413, body: rejection + `{}`},
		{name: "partial read", status: 400, body: rejection, readFailure: true, uncertain: true},
		{name: "oversized body", status: 413, body: rejection + strings.Repeat(" ", maxResponseBytes), uncertain: true},
		{name: "SSE error", status: 200, contentType: "text/event-stream", body: streamEvent(rejection), uncertain: true},
		{name: "SSE partial output", status: 200, contentType: "text/event-stream", body: streamText("partial") + streamUsage() + streamEvent(rejection), uncertain: true},
		{name: "SSE HTTP rejection", status: 400, contentType: "text/event-stream; charset=utf-8", body: rejection},
		{name: "transport message", transport: true, uncertain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			provider := chatProvider("https://example.test")
			provider.Client = &http.Client{Transport: contextLimitTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if tc.transport {
					return nil, errors.New("context_length_exceeded provider-secret test-secret")
				}
				var body io.Reader = strings.NewReader(tc.body)
				if tc.readFailure {
					body = streamBrokenReader{body}
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{tc.contentType}}, Body: io.NopCloser(body)}, nil
			})}
			prepared, err := provider.Prepare(t.Context(), chatRequest())
			if err != nil {
				t.Fatal(err)
			}
			response, err := prepared.Execute(t.Context(), nil)
			failure, ok := errors.AsType[*CallError](err)
			if !ok || failure.ContextLimit || failure.Retryable || failure.Uncertain != tc.uncertain || calls != 1 || len(response.Parts) != 0 {
				t.Fatalf("classification=%+v response=%+v calls=%d", failure, response, calls)
			}
			if strings.Contains(err.Error(), "provider-secret") || strings.Contains(err.Error(), "test-secret") {
				t.Fatalf("provider details leaked: %v", err)
			}
			if tc.name == "SSE partial output" && (response.Usage.Input == nil || *response.Usage.Input != 20 || response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000) {
				t.Fatalf("uncertain stream lost known accounting: %+v", response)
			}
		})
	}
}
