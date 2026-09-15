package llm

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClassifyProviderWording(t *testing.T) {
	cases := map[string]errorClass{
		"Inference stream timed out: No next token received for 30000ms": classTransient,
		"Rate limit exceeded, retry later":                               classTransient,
		"upstream connect error or disconnect/reset before headers":      classTransient,
		"Provider returned error: 502 Bad Gateway":                       classTransient,
		"The model is overloaded. Please try again later.":               classTransient,
		"stream ended before message_stop":                               classTransient,
		"insufficient quota for this request":                            classPermanent,
		"Invalid API key provided":                                       classPermanent,
		"rate limit exceeded for this billing plan":                      classPermanent, // permanent wins
		"This model's maximum context length is 131072 tokens":           classPermanent,
		"boom": classUnknown,
		"":     classUnknown,
	}
	for message, want := range cases {
		if got := classify(message); got != want {
			t.Errorf("classify(%q) = %v, want %v", message, got, want)
		}
	}
	if err := providerError("boom", false); !retryable(err) {
		t.Fatal("an unclassified message before any delta is worth one repeat")
	}
	if err := providerError("boom", true); retryable(err) {
		t.Fatal("an unclassified message after a delta is not regenerated")
	}
	if err := providerError("overloaded", true); !retryable(err) {
		t.Fatal("transient wording after a delta is regenerated")
	}
	if err := providerError("invalid api key", false); retryable(err) || !errors.Is(err, err) {
		t.Fatal("permanent wording is never repeated")
	}
	if got := providerError("overloaded", false).Error(); got != "api error: overloaded" {
		t.Fatalf("message %q", got)
	}
}

func TestRetryAfterHeaderIsHonouredAndCapped(t *testing.T) {
	noSleep(t)
	var delays []time.Duration
	for _, header := range []string{"3", "600", "garbage", ""} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if header != "" {
				w.Header().Set("Retry-After", header)
			}
			http.Error(w, "busy", http.StatusTooManyRequests)
		}))
		client := New(server.URL, "test-key")
		client.MaxRetries = 2
		client.OnRetry = func(event RetryEvent) { delays = append(delays, event.Delay) }
		_, _, err := client.Stream(t.Context(), Request{Model: "m"}, nil, nil, nil)
		server.Close()
		if err == nil {
			t.Fatalf("header %q: expected the 429 to surface", header)
		}
	}
	if len(delays) != 4 || delays[0] < 3*time.Second || delays[1] != time.Minute || delays[2] > 2*time.Second || delays[3] > 2*time.Second {
		t.Fatalf("delays %v: want at least the header, capped at one minute, and plain backoff otherwise", delays)
	}
}
