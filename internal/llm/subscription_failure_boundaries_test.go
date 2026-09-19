package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSubscriptionAttemptFailureBoundaries(t *testing.T) {
	for _, mode := range []string{"cancelled", "stalled", "transport", "missing credentials", "invalid body", "http error"} {
		t.Run(mode, func(t *testing.T) {
			client, store := subscriptionTestClient(t)
			credentials, err := store.Credentials(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if mode == "missing credentials" {
				credentials.AccessToken = ""
			}
			ctx, cancel := context.WithCancel(context.WithValue(t.Context(), subscriptionAuthKey{}, credentials))
			defer cancel()
			client.StallTimeout = 10 * time.Millisecond
			if mode == "cancelled" {
				cancel()
			}
			client.HTTP.Transport = subscriptionTransport(func(request *http.Request) (*http.Response, error) {
				switch mode {
				case "cancelled", "stalled":
					<-request.Context().Done()
					return nil, request.Context().Err()
				case "invalid body":
					return successfulSubscriptionResponse(), nil
				case "http error":
					return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"credential-leak"}}`))}, nil
				default:
					return nil, errors.New("credential-leak")
				}
			})
			body := []byte(`{"model":"gpt-5.4"}`)
			if mode == "invalid body" {
				body = []byte("not json")
			}
			_, _, err = client.subscriptionOnce(ctx, body, nil, nil, nil)
			if err == nil || strings.Contains(err.Error(), "credential-leak") {
				t.Fatalf("failure must be propagated without provider secrets: %v", err)
			}
			switch mode {
			case "cancelled":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("caller cancellation must survive: %v", err)
				}
			case "stalled":
				if _, ok := errors.AsType[stallError](err); !ok || !retryable(err) {
					t.Fatalf("stall must retain retryable classification: %v", err)
				}
			case "missing credentials", "invalid body":
				if retryable(err) {
					t.Fatalf("malformed local request must not retry: %v", err)
				}
			}
		})
	}
}

func TestSubscriptionRetryAfterDateAndErrorRedaction(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": {time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat)}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"credential-leak"}}`)),
	}
	err := subscriptionResponseError(response)
	httpErr, ok := errors.AsType[*HTTPError](err)
	if !ok || httpErr.RetryAfter != time.Minute || strings.Contains(err.Error(), "credential-leak") {
		t.Fatalf("untrusted retry date/error must be bounded and redacted: %+v", err)
	}
	for _, code := range []string{"model_not_found", "model_not_supported"} {
		err := subscriptionError(http.StatusBadRequest, []byte(`{"error":{"code":"`+code+`","message":"credential-leak"}}`))
		if !strings.Contains(err.Body, "model is unavailable") || strings.Contains(err.Error(), "credential-leak") {
			t.Fatalf("unavailable model diagnostics: %v", err)
		}
	}
}
