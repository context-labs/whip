package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/openaiauth"
)

type subscriptionTransport func(*http.Request) (*http.Response, error)

func (f subscriptionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func subscriptionTestClient(t *testing.T) (*Client, *openaiauth.Manager) {
	t.Helper()
	auth := openaiauth.New(t.Context(), t.TempDir())
	t.Cleanup(auth.Close)
	if err := auth.Install(t.Context(), auth.Generation(), openaiauth.Credentials{
		AccessToken: "access-token", RefreshToken: "refresh-token", AccountID: "account",
		ExpiresAt: time.Now().Add(time.Hour), ComputeResidency: "eu",
	}); err != nil {
		t.Fatal(err)
	}
	client := NewSubscription(auth)
	client.MaxRetries = 1
	return client, auth
}

func successfulSubscriptionResponse() *http.Response {
	return &http.Response{Status: "200 OK", StatusCode: 200, Body: io.NopCloser(strings.NewReader(
		"data: " + responseFixture(`[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]`) + "\n\n",
	))}
}

type subscriptionBudget struct {
	attempts []ModelAttempt
	results  []ModelAttemptResult
	grant    int
	onBegin  func()
}

func (b *subscriptionBudget) BeginModelAttempt(_ context.Context, attempt ModelAttempt) (ModelPermit, error) {
	if b.onBegin != nil {
		b.onBegin()
	}
	b.attempts = append(b.attempts, attempt)
	grant := attempt.MaxTokens
	if b.grant > 0 {
		grant = b.grant
	}
	return ModelPermit{MaxTokens: grant, Timeout: time.Minute, Settle: func(result ModelAttemptResult) error {
		b.results = append(b.results, result)
		return nil
	}}, nil
}

func TestSubscriptionCompleteUsesStreamAndNaturalOutputReservation(t *testing.T) {
	client, _ := subscriptionTestClient(t)
	requests := 0
	client.HTTP.Transport = subscriptionTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.String() != openaiauth.BaseURL+"/responses" || request.Header.Get("Authorization") != "Bearer access-token" ||
			request.Header.Get("ChatGPT-Account-Id") != "account" || request.Header.Get("x-openai-internal-codex-residency") != "eu" ||
			request.Header.Get("originator") != "whip" {
			t.Fatal("incorrect subscription endpoint/headers")
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["stream"] != true || body["max_tokens"] != nil || body["max_output_tokens"] != nil {
			t.Fatal("helper did not use the streaming subscription profile")
		}
		return successfulSubscriptionResponse(), nil
	})
	budget := &subscriptionBudget{}
	req := Request{Model: "gpt-5.5", MaxTokens: 24, Messages: []Message{{Role: "user", Content: "title"}},
		Accounting: &CallAccounting{Budget: budget, Purpose: "title"}}
	text, usage, err := client.Complete(t.Context(), req)
	if err != nil || text != "ok" || !usage.HasUsage() || requests != 1 || budget.attempts[0].MaxTokens != 128000 || !budget.results[0].Dispatched {
		t.Fatalf("subscription helper/accounting failed: text=%s requests=%d err=%v", text, requests, err)
	}
	budget.grant = 100
	if _, _, err := client.Complete(t.Context(), req); !IsAccountingError(err) {
		t.Fatalf("reduced output grant was not refused: %v", err)
	}
	if requests != 1 || budget.results[1].Dispatched || !budget.results[1].Failed {
		t.Fatal("dispatched despite an unenforceable output grant")
	}
	req.OutputLimitExplicit = true
	if _, _, err := client.Complete(t.Context(), req); !IsPermanentRequestError(err) || requests != 1 {
		t.Fatalf("explicit output cap was silently ignored: %v", err)
	}
}

func TestSubscriptionAuthRetryUsesFreshCredentialAndSeparateAttempt(t *testing.T) {
	client, auth := subscriptionTestClient(t)
	requests := 0
	client.HTTP.Transport = subscriptionTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			// Another request already refreshed the account while this request
			// was in flight. The rejected token must not trigger another refresh.
			if err := auth.Install(t.Context(), auth.Generation(), openaiauth.Credentials{
				AccessToken: "rotated", RefreshToken: "rotated-refresh", AccountID: "account", ExpiresAt: time.Now().Add(time.Hour),
			}); err != nil {
				t.Fatal(err)
			}
			return &http.Response{StatusCode: 401, Status: "401 Unauthorized", Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		if request.Header.Get("Authorization") != "Bearer rotated" {
			t.Fatal("reused rejected token")
		}
		return successfulSubscriptionResponse(), nil
	})
	budget := &subscriptionBudget{}
	_, _, err := client.Stream(t.Context(), Request{Model: "gpt-5.5", Accounting: &CallAccounting{Budget: budget}}, nil, nil, nil)
	if err != nil || requests != 2 || len(budget.results) != 2 || budget.attempts[1].Number != 2 {
		t.Fatalf("auth retry was not separately accounted: requests=%d err=%v", requests, err)
	}
}

func TestSubscriptionNoRetryAfterReasoningWithNilCallbacks(t *testing.T) {
	client, _ := subscriptionTestClient(t)
	client.MaxRetries = 3
	requests := 0
	client.HTTP.Transport = subscriptionTransport(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(
			"data: " + `{"type":"response.reasoning_summary_text.delta","delta":"Checking"}` + "\n\n",
		))}, nil
	})
	_, _, err := client.Complete(t.Context(), Request{Model: "gpt-5.5"})
	if !errors.Is(err, io.ErrUnexpectedEOF) || requests != 1 {
		t.Fatalf("replayed after output with nil callbacks: requests=%d err=%v", requests, err)
	}
}

func TestSubscriptionQuotaIsPermanentAndSanitized(t *testing.T) {
	response := &http.Response{StatusCode: 429, Status: "429 Too Many Requests", Header: http.Header{"Retry-After": {"12"}},
		Body: io.NopCloser(strings.NewReader(`{"error":{"type":"usage_limit_reached","message":"secret","resets_at":1800000000}}`))}
	err := subscriptionResponseError(response)
	if retryable(err) || !IsPermanentRequestError(err) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("quota can be replayed or leaked upstream body: %v", err)
	}
	response.Body = io.NopCloser(strings.NewReader(`{"error":{"type":"rate_limit_exceeded","message":"secret"}}`))
	err = subscriptionResponseError(response)
	if !retryable(err) || IsPermanentRequestError(err) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("transient throttle was misclassified: %v", err)
	}
}

func TestSubscriptionCatalogUsesAccountAndVerifiedLimits(t *testing.T) {
	client, _ := subscriptionTestClient(t)
	client.HTTP.Transport = subscriptionTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method != "GET" || request.URL.Path != "/backend-api/codex/models" ||
			request.URL.Query().Get("client_version") != subscriptionCatalogVersion ||
			request.Header.Get("ChatGPT-Account-Id") != "account" {
			t.Fatal("catalog was not fetched with the subscription profile")
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"models":[
			{"slug":"gpt-5.5","visibility":"list","context_window":400000,"max_context_window":1000000,
			 "effective_context_window_percent":95,"input_modalities":["text","image"],
			 "supported_reasoning_levels":[{"effort":"none"},{"effort":"ultra"}]},
			{"slug":"unverified-model","visibility":"list","context_window":400000},
			{"slug":"gpt-5.4","visibility":"hide","context_window":400000}
		]}`))}, nil
	})
	models, err := client.Models(t.Context())
	if err != nil || len(models) != 1 {
		t.Fatalf("catalog normalization: %+v %v", models, err)
	}
	model := models[0]
	if model.ContextLength != 380000 || model.MaxCompletionTokens != 128000 ||
		len(model.ReasoningEfforts) != 2 || len(model.InputModalities) != 2 || model.Pricing != nil {
		t.Fatalf("catalog confused context, output or pricing metadata: %+v", model)
	}
}

func TestSubscriptionAccountChangeDuringAdmissionPreventsDispatch(t *testing.T) {
	for _, account := range []string{"", "different-account", "account"} {
		t.Run("replacement="+account, func(t *testing.T) {
			client, auth := subscriptionTestClient(t)
			client.HTTP.Transport = subscriptionTransport(func(*http.Request) (*http.Response, error) {
				t.Error("dispatched the previous account's history after its login changed")
				return successfulSubscriptionResponse(), nil
			})
			budget := &subscriptionBudget{onBegin: func() {
				if err := auth.Logout(); err != nil {
					t.Fatal(err)
				}
				if account != "" {
					if err := auth.Install(t.Context(), auth.Generation(), openaiauth.Credentials{
						AccessToken: "replacement", RefreshToken: "replacement-refresh", AccountID: account, ExpiresAt: time.Now().Add(time.Hour),
					}); err != nil {
						t.Fatal(err)
					}
				}
			}}
			_, _, err := client.Complete(t.Context(), Request{Model: "gpt-5.5", Accounting: &CallAccounting{Budget: budget}})
			if !errors.Is(err, openaiauth.ErrLoginChanged) || len(budget.results) != 1 || budget.results[0].Dispatched {
				t.Fatalf("account change was not settled as undispatched: %+v %v", budget.results, err)
			}
		})
	}
}

func TestSubscriptionStreamFailureClassification(t *testing.T) {
	for _, code := range []string{"usage_limit_reached", "server_error", "rate_limit_exceeded", "context_length_exceeded"} {
		for _, eventType := range []string{"error", "response.failed"} {
			t.Run(eventType+"/"+code, func(t *testing.T) {
				failure := map[string]any{"code": code, "message": "private error details"}
				event := map[string]any{"type": eventType, "code": code, "message": "private error details"}
				if eventType == "response.failed" {
					event = map[string]any{"type": eventType, "response": map[string]any{"status": "failed", "error": failure}}
				}
				data, err := json.Marshal(event)
				if err != nil {
					t.Fatal(err)
				}
				client, _ := subscriptionTestClient(t)
				client.HTTP.Transport = subscriptionTransport(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader("data: " + string(data) + "\n\n"))}, nil
				})
				message, _, err := client.Stream(t.Context(), Request{Model: "gpt-5.5"}, nil, nil, nil)
				transient := code == "server_error" || code == "rate_limit_exceeded"
				if err == nil || retryable(err) != transient || strings.Contains(err.Error(), "private error details") || len(message.ToolCalls) > 0 {
					t.Fatalf("incorrect stream error handling: %v", err)
				}
				if code == "usage_limit_reached" && !IsPermanentRequestError(err) {
					t.Fatal("hard quota could requeue a child turn")
				}
				if code == "context_length_exceeded" && !IsContextLimit(err) {
					t.Fatal("context error could not trigger compaction")
				}
			})
		}
	}
}
