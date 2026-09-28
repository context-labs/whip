package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/session"
)

type subscriptionAuthFixture struct {
	captured            openaiauth.CapturedCredentials
	captures, refreshes int
}

func (a *subscriptionAuthFixture) Capture(ctx context.Context) (openaiauth.CapturedCredentials, error) {
	a.captures++
	return a.captured, ctx.Err()
}

func (a *subscriptionAuthFixture) Check(ctx context.Context, captured openaiauth.CapturedCredentials) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if captured.Generation != a.captured.Generation || captured.Credentials.AccountID != a.captured.Credentials.AccountID {
		return openaiauth.ErrLoginChanged
	}
	return nil
}

func (a *subscriptionAuthFixture) RefreshCaptured(ctx context.Context, captured openaiauth.CapturedCredentials) (openaiauth.CapturedCredentials, error) {
	if err := a.Check(ctx, captured); err != nil {
		return openaiauth.CapturedCredentials{}, err
	}
	a.refreshes++
	a.captured.Credentials.AccessToken = "rotated-access"
	return a.captured, nil
}

func subscriptionFixture() (OpenAI, *subscriptionAuthFixture, Request) {
	auth := &subscriptionAuthFixture{captured: openaiauth.CapturedCredentials{Generation: 1, Credentials: openaiauth.Credentials{AccessToken: "private-access", RefreshToken: "private-refresh", AccountID: "private-account", ComputeResidency: "eu"}}}
	provider := OpenAI{Auth: auth, Resolve: func(context.Context, session.ModelSelection) (Route, error) {
		return Route{Kind: "openai-codex", TimeoutMillis: 1000, MaxAttempts: 3, ContextWindowTokens: new(int64(400000))}, nil
	}}
	request := chatRequest()
	request.Selection = session.ModelSelection{Provider: "openai-codex", Name: "gpt-6-astra", Effort: "medium"}
	return provider, auth, request
}

func subscriptionResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestSubscriptionFrozenRequestAndCredentialRefresh(t *testing.T) {
	provider, auth, request := subscriptionFixture()
	request.Tools = []Tool{executeTool()}
	request.Messages[0].Parts = append(request.Messages[0].Parts, session.Part{Type: "content", ReferenceID: "image"})
	request.Contents = map[string]Content{"image": {MediaType: "image/png", Data: []byte("image")}}
	price := int64(123)
	route := Route{Kind: "openai-codex", MaxOutputTokens: 200000, TimeoutMillis: 1000, MaxAttempts: 3, ContextWindowTokens: new(int64(400000)), Prices: session.ModelPrices{Input: &price}}
	resolves := 0
	provider.Resolve = func(context.Context, session.ModelSelection) (Route, error) { resolves++; return route, nil }
	var bodies []string
	provider.Client = &http.Client{Transport: contextLimitTransport(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		token := "private-access"
		if len(bodies) > 1 {
			token = "rotated-access"
		}
		if r.URL.String() != openaiauth.BaseURL+"/responses" || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Chatgpt-Account-Id") != "private-account" || r.Header.Get("Originator") != "whip" || r.Header.Get("User-Agent") != "whip" || r.Header.Get("X-Openai-Internal-Codex-Residency") != "eu" || r.Header.Get("Accept") != "text/event-stream" {
			t.Fatal("incorrect subscription route or private headers")
		}
		if len(bodies) == 1 {
			return subscriptionResponse(401, `{"error":{"message":"private-access"},"usage":{"input_tokens":7}}`), nil
		}
		return subscriptionResponse(200, responsesTerminal(responsesOutput, responsesUsage)), nil
	})}
	prepared, err := provider.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Messages[0].Parts[0].Text = "mutated"
	request.Contents["image"].Data[0] = 'X'
	price = 999
	route = Route{}
	if err := prepared.BeforeDispatch(t.Context()); err != nil {
		t.Fatal(err)
	}
	response, err := prepared.Execute(t.Context(), nil)
	failure, ok := errors.AsType[*CallError](err)
	if !ok || !failure.AuthRejected || failure.Retryable || failure.Uncertain || response.Usage.Input == nil || *response.Usage.Input != 7 || auth.refreshes != 0 {
		t.Fatalf("unauthorized evidence: %v", err)
	}
	fresh, err := prepared.RefreshCredentials(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	response, err = fresh.Execute(t.Context(), nil)
	if err != nil || len(response.Parts) != 2 || response.Continuation == nil {
		t.Fatalf("refreshed response: %v", err)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] || resolves != 1 || auth.captures != 1 || auth.refreshes != 1 || !reflect.DeepEqual(prepared.Snapshot, fresh.Snapshot) {
		t.Fatal("refresh changed the frozen request or hid another resolution")
	}
	digest := sha256.Sum256([]byte(bodies[0]))
	if prepared.Snapshot.RequestDigest != hex.EncodeToString(digest[:]) || prepared.Snapshot.MaxOutputTokens != 128000 || *prepared.Snapshot.Prices.Input != 123 || strings.Contains(bodies[0], "max_output_tokens") || !strings.Contains(bodies[0], "input_image") || !strings.Contains(bodies[0], "aW1hZ2U=") {
		t.Fatal("incorrect natural reservation or frozen body")
	}
	raw, _ := json.Marshal(prepared.Snapshot)
	for _, secret := range []string{"private-access", "rotated-access", "private-refresh", "private-account"} {
		if strings.Contains(string(raw), secret) || strings.Contains(bodies[0], secret) {
			t.Fatal("credential leaked into durable evidence/body")
		}
	}
}

func TestSubscriptionPolicyRejectsBeforeAuth(t *testing.T) {
	for _, tc := range []struct {
		name            string
		model           string
		cap             int64
		url, credential string
	}{
		{"unknown model", "unverified", 0, "", ""},
		{"model lookalike", "gpt-6-astra-extra", 0, "", ""},
		{"lower cap", "gpt-6-astra", 127999, "", ""},
		{"negative cap", "gpt-6-astra", -1, "", ""},
		{"alternate URL", "gpt-6-astra", 0, "https://example.test", ""},
		{"API credential", "gpt-6-astra", 0, "", "api-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, auth, request := subscriptionFixture()
			request.Selection.Name = tc.model
			provider.Resolve = func(context.Context, session.ModelSelection) (Route, error) {
				return Route{Kind: "openai-codex", MaxAttempts: 2, TimeoutMillis: 1000, MaxOutputTokens: tc.cap, URL: tc.url, Credential: tc.credential}, nil
			}
			if _, err := provider.Prepare(t.Context(), request); !errors.Is(err, session.ErrInvalid) || auth.captures != 0 {
				t.Fatalf("policy did not reject before auth: %v", err)
			}
		})
	}
}

func TestSubscriptionContinuationScopeAndHelperIsolation(t *testing.T) {
	provider, auth, request := subscriptionFixture()
	request.Tools = []Tool{executeTool()}
	var body string
	provider.Client = &http.Client{Transport: contextLimitTransport(func(r *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		return subscriptionResponse(200, responsesTerminal(responsesOutput, responsesUsage)), nil
	})}
	prepared, err := provider.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	response, err := prepared.Execute(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Messages = append(request.Messages, Message{Role: session.Assistant, Parts: response.Parts, Continuation: response.Continuation})
	original := auth.captured
	for _, tc := range []struct {
		name           string
		account        string
		generation     uint64
		purpose, model string
		replay         bool
	}{
		{"rotated token", original.Credentials.AccountID, 1, "turn", "gpt-6-astra", true},
		{"same account re-login", original.Credentials.AccountID, 2, "turn", "gpt-6-astra", true},
		{"different account", "other", 3, "turn", "gpt-6-astra", false},
		{"different model", original.Credentials.AccountID, 4, "turn", "gpt-5.5", false},
		{"helper", original.Credentials.AccountID, 5, "compaction", "gpt-6-astra", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth.captured.Generation = tc.generation
			auth.captured.Credentials.AccountID = tc.account
			auth.captured.Credentials.AccessToken = "another-token"
			request.Purpose, request.Selection.Name = tc.purpose, tc.model
			next, err := provider.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := next.Execute(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(body, "opaque-secret") != tc.replay || tc.replay && (!strings.Contains(body, "9007199254740993") || !strings.Contains(body, `"phase":"commentary"`)) {
				t.Fatal("private replay scope or exact JSON changed")
			}
			if next.Snapshot.MaxOutputTokens != 128000 || next.Snapshot.Prices.Input != nil || next.Snapshot.Prices.Output != nil {
				t.Fatal("helper/profile natural bound or unknown prices changed")
			}
		})
	}
}

func TestSubscriptionChecksActualManagerAtDispatch(t *testing.T) {
	for _, change := range []string{"logout", "same account", "different account"} {
		t.Run(change, func(t *testing.T) {
			manager := openaiauth.New(t.Context(), t.TempDir())
			t.Cleanup(manager.Close)
			credentials := openaiauth.Credentials{AccessToken: "token", RefreshToken: "refresh", AccountID: "account", ExpiresAt: time.Now().Add(time.Hour)}
			if err := manager.Install(t.Context(), manager.Generation(), credentials); err != nil {
				t.Fatal(err)
			}
			provider, _, request := subscriptionFixture()
			provider.Auth = manager
			calls := 0
			provider.Client = &http.Client{Transport: contextLimitTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected HTTP") })}
			prepared, err := provider.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if err := prepared.BeforeDispatch(t.Context()); err != nil {
				t.Fatal(err)
			}
			if change == "logout" {
				err = manager.Logout()
			} else {
				if change == "different account" {
					credentials.AccountID = "other"
				}
				err = manager.Install(t.Context(), manager.Generation(), credentials)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := prepared.BeforeDispatch(t.Context()); !errors.Is(err, openaiauth.ErrLoginChanged) {
				t.Fatalf("stale predispatch: %v", err)
			}
			if _, err := prepared.Execute(t.Context(), nil); !errors.Is(err, openaiauth.ErrLoginChanged) {
				t.Fatalf("stale execution: %v", err)
			}
			if _, err := prepared.RefreshCredentials(t.Context()); !errors.Is(err, openaiauth.ErrLoginChanged) {
				t.Fatalf("stale refresh: %v", err)
			}
			if calls != 0 {
				t.Fatal("stale work contacted provider")
			}
		})
	}
}

func TestSubscriptionConfirmedFailuresAndRedirects(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		status                 int
		body, media            string
		auth, uncertain, retry bool
		diagnostic             string
	}{
		{"unauthorized", 401, `{"error":{"message":"private-debug"},"usage":{"input_tokens":7,"cost":0.1}}`, "application/json", true, false, false, "HTTP 401"},
		{"hard quota", 429, `{"error":{"code":"usage_limit_reached","message":"private-debug"},"usage":{"input_tokens":7}}`, "application/json", false, false, false, "subscription usage limit"},
		{"quota with invalid reset", 429, `{"error":{"code":"usage_limit_reached","resets_at":"private-debug"}}`, "application/json", false, false, false, "subscription usage limit"},
		{"quota overrides unauthorized", 401, `{"error":{"code":"insufficient_quota"}}`, "application/json", false, false, false, "subscription usage limit"},
		{"JSON quota", 200, `{"status":"failed","error":{"code":"quota_exceeded"},"usage":{"input_tokens":7}}`, "application/json", false, true, false, "subscription usage limit"},
		{"quota message alone", 429, `{"error":{"message":"usage_limit_reached private-debug"}}`, "application/json", false, false, true, "HTTP 429"},
		{"temporary rate", 429, `{"error":{"code":"rate_limit_exceeded"}}`, "application/json", false, false, true, "HTTP 429"},
		{"SSE unauthorized", 200, responsesEvent("error", `"code":"401","message":"private-debug"`), "text/event-stream", false, true, false, "stream error"},
		{"SSE quota", 200, responsesEvent("response.failed", `"response":{"error":{"type":"insufficient_quota","message":"private-debug"},"usage":{"input_tokens":7}}`), "text/event-stream", false, true, false, "subscription usage limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, auth, request := subscriptionFixture()
			calls := 0
			provider.Client = &http.Client{Transport: contextLimitTransport(func(*http.Request) (*http.Response, error) {
				calls++
				response := subscriptionResponse(tc.status, tc.body)
				response.Header.Set("Content-Type", tc.media)
				return response, nil
			})}
			prepared, err := provider.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			response, err := prepared.Execute(t.Context(), nil)
			failure, ok := errors.AsType[*CallError](err)
			if !ok || failure.AuthRejected != tc.auth || failure.Uncertain != tc.uncertain || failure.Retryable != tc.retry || !strings.Contains(err.Error(), tc.diagnostic) || strings.Contains(err.Error(), "private-debug") || len(response.Parts) != 0 || calls != 1 || auth.refreshes != 0 {
				t.Fatalf("failure classification: %v", err)
			}
			if strings.Contains(tc.body, `"input_tokens":7`) && (response.Usage.Input == nil || *response.Usage.Input != 7) {
				t.Fatal("known usage lost")
			}
		})
	}
	t.Run("redirect", func(t *testing.T) {
		provider, _, request := subscriptionFixture()
		calls := 0
		provider.Client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { t.Fatal("caller redirect policy used"); return nil }, Transport: contextLimitTransport(func(*http.Request) (*http.Response, error) {
			calls++
			response := subscriptionResponse(307, "")
			response.Header.Set("Location", "https://untrusted.test")
			return response, nil
		})}
		prepared, err := provider.Prepare(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		_, err = prepared.Execute(t.Context(), nil)
		failure, ok := errors.AsType[*CallError](err)
		if !ok || failure.StatusCode != http.StatusTemporaryRedirect || calls != 1 {
			t.Fatalf("redirect followed: %v calls=%d", err, calls)
		}
	})
}

func TestSubscriptionIncompleteRejectionNeverPermitsAuthRefresh(t *testing.T) {
	for _, mode := range []string{"body read failure", "oversized body", "transport failure", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			provider, auth, request := subscriptionFixture()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			provider.Client = &http.Client{Transport: contextLimitTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if mode == "transport failure" {
					return nil, errors.New("private transport detail")
				}
				response := subscriptionResponse(401, `{"usage":{"input_tokens":7}}`)
				switch mode {
				case "body read failure":
					response.Body = io.NopCloser(streamBrokenReader{strings.NewReader(`{"usage":{"input_tokens":7}}`)})
				case "oversized body":
					response.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxResponseBytes+1)))
				case "cancelled":
					cancel()
					response.Body = io.NopCloser(streamBrokenReader{strings.NewReader("")})
				}
				return response, nil
			})}
			prepared, err := provider.Prepare(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			response, err := prepared.Execute(ctx, nil)
			if err == nil || strings.Contains(err.Error(), "private") || auth.refreshes != 0 || calls != 1 || len(response.Parts) != 0 {
				t.Fatalf("incomplete rejection: %v", err)
			}
			if failure, ok := errors.AsType[*CallError](err); ok && (failure.AuthRejected || failure.Retryable) {
				t.Fatal("incomplete rejection enabled retry")
			}
		})
	}
}
