package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestSubscriptionRetriesAttemptCeilingBeforeHeaders(t *testing.T) {
	noSleep(t)
	client, _ := subscriptionTestClient(t)
	client.MaxRetries = 2
	client.AttemptCeiling = 20 * time.Millisecond
	calls := 0
	client.HTTP.Transport = subscriptionTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}
		return successfulSubscriptionResponse(), nil
	})
	var retried []RetryEvent
	client.OnRetry = func(event RetryEvent) { retried = append(retried, event) }
	message, _, err := client.Stream(t.Context(), Request{Model: "gpt-5.4"}, nil, nil, nil)
	if err != nil || message.Content != "ok" || calls != 2 || len(retried) != 1 {
		t.Fatalf("content=%q calls=%d retries=%d err=%v", message.Content, calls, len(retried), err)
	}
	if _, ok := errors.AsType[ceilingError](retried[0].Err); !ok {
		t.Fatalf("retry cause %v, want attempt ceiling", retried[0].Err)
	}
}

func TestModelsBoundsHeadersAndBody(t *testing.T) {
	for _, subscription := range []bool{false, true} {
		name := "api"
		if subscription {
			name = "subscription"
		}
		t.Run(name, func(t *testing.T) {
			for _, phase := range []string{"headers", "body"} {
				t.Run(phase, func(t *testing.T) {
					client := New("https://example.test/v1", "")
					if subscription {
						client, _ = subscriptionTestClient(t)
					}
					client.AttemptCeiling = 20 * time.Millisecond
					client.HTTP.Transport = subscriptionTransport(func(request *http.Request) (*http.Response, error) {
						deadline, ok := request.Context().Deadline()
						if !ok || time.Until(deadline) > client.AttemptCeiling {
							t.Fatal("catalog request has no end-to-end ceiling")
						}
						if phase == "headers" {
							<-request.Context().Done()
							return nil, request.Context().Err()
						}
						reader, writer := io.Pipe()
						go func() {
							<-request.Context().Done()
							_ = writer.CloseWithError(request.Context().Err())
						}()
						return &http.Response{StatusCode: http.StatusOK, Body: reader}, nil
					})
					if _, err := client.Models(context.Background()); err == nil {
						t.Fatal("stalled model catalog returned success")
					}
				})
			}
		})
	}
}
