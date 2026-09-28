package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/session"
)

// SubscriptionAuth is the host's existing credential owner. Captures remain
// private, ephemeral closure values, never request snapshots or transcript data.
type SubscriptionAuth interface {
	Capture(context.Context) (openaiauth.CapturedCredentials, error)
	Check(context.Context, openaiauth.CapturedCredentials) error
	RefreshCaptured(context.Context, openaiauth.CapturedCredentials) (openaiauth.CapturedCredentials, error)
}

// SubscriptionOutputLimit is the pinned natural ceiling used by the retained
// internal/llm/subscription.go adapter, whose model-documentation sources were
// checked on 2026-09-08. This package does not depend on that retired client.
// The subscription endpoint has no wire output cap; unknown models fail closed.
func SubscriptionOutputLimit(model string) int64 {
	switch model {
	case "gpt-6-astra", "gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna",
		"gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano", "gpt-5.3-codex", "gpt-5-codex":
		return 128000
	default:
		return 0
	}
}

func subscriptionRoute(route *Route, model string) error {
	ceiling := SubscriptionOutputLimit(model)
	if ceiling == 0 || route.MaxOutputTokens < 0 || route.MaxOutputTokens > 0 && route.MaxOutputTokens < ceiling {
		return fmt.Errorf("%w: ChatGPT subscription requires a verified natural output limit; smaller output caps are unsupported", session.ErrInvalid)
	}
	if route.URL != "" && route.URL != openaiauth.BaseURL || route.Credential != "" {
		return fmt.Errorf("%w: ChatGPT subscription requires its fixed host credential route", session.ErrInvalid)
	}
	route.URL, route.MaxOutputTokens = openaiauth.BaseURL, ceiling
	return nil
}

func (p OpenAI) captureSubscription(ctx context.Context) (openaiauth.CapturedCredentials, error) {
	if p.Auth == nil {
		return openaiauth.CapturedCredentials{}, openaiauth.ErrSignInRequired
	}
	return p.Auth.Capture(ctx)
}

func subscriptionScope(route, account, model string) string {
	// Account identity deliberately survives token rotation and same-account
	// re-login. Dispatch separately checks the ephemeral login generation.
	raw, _ := json.Marshal([]string{"whip:openai-codex:continuation:v1", route, account, model})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

type responseAuth struct {
	credential, accountID, residency string
}

func (a responseAuth) setHeaders(request *http.Request) {
	if a.credential != "" {
		request.Header.Set("Authorization", "Bearer "+a.credential)
	}
	if a.accountID != "" {
		request.Header.Set("Chatgpt-Account-Id", a.accountID)
		request.Header.Set("User-Agent", "whip")
		request.Header.Set("Originator", "whip")
		request.Header.Set("Accept", "text/event-stream")
		if a.residency != "" {
			request.Header.Set("X-Openai-Internal-Codex-Residency", a.residency)
		}
	}
}

func prepareSubscription(auth SubscriptionAuth, captured openaiauth.CapturedCredentials, client *http.Client, snapshot session.ModelRequestSnapshot, attempts int, window *int64, scope string, body []byte, tools map[string]bool, idle time.Duration) Prepared {
	return Prepared{
		Snapshot: snapshot, MaxAttempts: attempts, ContextWindowTokens: window,
		BeforeDispatch: func(ctx context.Context) error { return auth.Check(ctx, captured) },
		Execute: func(ctx context.Context, emit func(Chunk)) (Response, error) {
			// This proves authorization at the check, not an atomic transaction
			// with the subsequent HTTP request. No network precedes the check.
			if err := auth.Check(ctx, captured); err != nil {
				return Response{}, err
			}
			credentials := captured.Credentials
			return executeResponses(ctx, client, snapshot.Route, responseAuth{credential: credentials.AccessToken, accountID: credentials.AccountID, residency: credentials.ComputeResidency}, scope, body, tools, emit, idle)
		},
		RefreshCredentials: func(ctx context.Context) (Prepared, error) {
			fresh, err := auth.RefreshCaptured(ctx, captured)
			if err != nil {
				return Prepared{}, err
			}
			if fresh.Generation != captured.Generation || fresh.Credentials.AccountID != captured.Credentials.AccountID {
				return Prepared{}, openaiauth.ErrLoginChanged
			}
			if err := auth.Check(ctx, fresh); err != nil {
				return Prepared{}, err
			}
			return prepareSubscription(auth, fresh, client, snapshot, attempts, window, scope, body, tools, idle), nil
		},
	}
}

// subscriptionDiagnostic recognizes only structured retained categories. It
// never includes provider messages, account identifiers, tokens or raw bodies.
func subscriptionDiagnostic(raw []byte) string {
	type failure struct {
		Code     json.RawMessage `json:"code"`
		Type     json.RawMessage `json:"type"`
		ResetsAt json.RawMessage `json:"resets_at"`
	}
	var payload struct {
		failure
		Error *failure `json:"error"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	value := payload.failure
	if payload.Error != nil {
		value = *payload.Error
	}
	var code string
	if json.Unmarshal(value.Code, &code) != nil || code == "" {
		_ = json.Unmarshal(value.Type, &code)
	}
	switch code {
	case "usage_limit_reached", "insufficient_quota", "quota_exceeded":
		message := "ChatGPT subscription usage limit reached"
		var resetsAt int64
		if json.Unmarshal(value.ResetsAt, &resetsAt) == nil && resetsAt > 0 && resetsAt < time.Now().AddDate(1, 0, 0).Unix() {
			message += "; resets " + time.Unix(resetsAt, 0).UTC().Format(time.RFC3339)
		}
		return message
	case "model_not_found", "model_not_supported":
		return "model is unavailable for this ChatGPT account"
	}
	return ""
}

var _ SubscriptionAuth = (*openaiauth.Manager)(nil)
