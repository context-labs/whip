package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/openaiauth"
)

type subscriptionAuthKey struct{}

type subscriptionUnauthorized struct{ token string }

func (e *subscriptionUnauthorized) Error() string {
	return "OpenAI subscription access was rejected; sign in again"
}

func (e *subscriptionUnauthorized) Unwrap() error {
	return &HTTPError{Status: "401 Unauthorized", Body: e.Error()}
}

// NewSubscription shares the daemon's credential owner; it never imports an
// API key or sends credentials to a configurable endpoint.
func NewSubscription(auth *openaiauth.Manager) *Client {
	client := New(openaiauth.BaseURL, "")
	client.openAI = auth
	client.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}

// SubscriptionOutputLimit is a vetted natural model ceiling, not a request
// parameter. Codex's subscription endpoint does not expose max_output_tokens.
// Sources checked 2026-09-08: developers.openai.com/api/docs/models/{gpt,
// gpt-5.5,gpt-5.4,gpt-5.4-mini,gpt-5.4-nano,gpt-5.3-codex,gpt-5-codex}.
// Unknown models are refused until their bounds have been verified.
func SubscriptionOutputLimit(model string) int {
	switch model {
	case "gpt-6-astra", "gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna",
		"gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano", "gpt-5.3-codex", "gpt-5-codex":
		return 128000
	default:
		return 0
	}
}

func (c *Client) subscriptionRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	credentials, ok := ctx.Value(subscriptionAuthKey{}).(openaiauth.Credentials)
	if !ok || credentials.AccessToken == "" {
		return nil, openaiauth.ErrSignInRequired
	}
	request, err := http.NewRequestWithContext(ctx, method, openaiauth.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
	request.Header.Set("Chatgpt-Account-Id", credentials.AccountID)
	request.Header.Set("User-Agent", "whip")
	request.Header.Set("Originator", "whip")
	if credentials.ComputeResidency != "" {
		request.Header.Set("X-Openai-Internal-Codex-Residency", credentials.ComputeResidency)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	return request, nil
}

func (c *Client) subscriptionOnce(
	ctx context.Context,
	body []byte,
	onText, onThink func(string),
	onTool func(string, string, string),
) (Message, Usage, error) {
	request, err := c.subscriptionRequest(ctx, http.MethodPost, "/responses", body)
	if err != nil {
		return Message{}, Usage{}, nonRetryable{err}
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return Message{}, Usage{}, ctx.Err()
		}
		return Message{}, Usage{}, errors.New("could not reach OpenAI subscription endpoint")
	}
	defer func() { _ = response.Body.Close() }()
	credentials := ctx.Value(subscriptionAuthKey{}).(openaiauth.Credentials)
	if response.StatusCode == http.StatusUnauthorized {
		return Message{}, Usage{}, &subscriptionUnauthorized{token: credentials.AccessToken}
	}
	if response.StatusCode != http.StatusOK {
		return Message{}, Usage{}, subscriptionResponseError(response)
	}
	var input struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &input) != nil {
		return Message{}, Usage{}, nonRetryable{errors.New("invalid OpenAI request model")}
	}
	return decodeResponses(response.Body, credentials.AccountID, input.Model, onText, onThink, onTool)
}

func subscriptionResponseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	err := subscriptionError(response.StatusCode, body)
	if seconds, parseErr := strconv.Atoi(response.Header.Get("Retry-After")); parseErr == nil && seconds > 0 {
		err.RetryAfter = time.Duration(min(seconds, 60)) * time.Second
	} else if reset, parseErr := http.ParseTime(response.Header.Get("Retry-After")); parseErr == nil {
		err.RetryAfter = min(max(time.Until(reset), 0), time.Minute)
	}
	return err
}

type subscriptionFailure struct {
	Code     string `json:"code"`
	Type     string `json:"type"`
	ResetsAt int64  `json:"resets_at"`
}

// HTTP bodies and SSE failures carry the same error, either nested or inline.
// SSE has no HTTP failure status, so derive only known retryable categories.
func subscriptionError(status int, body []byte) *HTTPError {
	var payload struct {
		Error *subscriptionFailure `json:"error"`
		subscriptionFailure
	}
	_ = json.Unmarshal(body, &payload)
	failure := payload.subscriptionFailure
	if payload.Error != nil {
		failure = *payload.Error
	}
	code := failure.Code
	if code == "" {
		code = failure.Type
	}
	if status == 0 {
		switch code {
		case "rate_limit_exceeded", "usage_limit_reached", "insufficient_quota", "quota_exceeded":
			status = http.StatusTooManyRequests
		case "server_error", "internal_error":
			status = http.StatusInternalServerError
		default:
			status = http.StatusBadRequest
		}
	}
	err := &HTTPError{Status: strconv.Itoa(status) + " " + http.StatusText(status), Body: "OpenAI subscription request failed"}
	switch code {
	case "usage_limit_reached", "insufficient_quota", "quota_exceeded":
		err.Permanent = true
		err.Body = "ChatGPT subscription usage limit reached"
		if failure.ResetsAt > 0 && failure.ResetsAt < time.Now().AddDate(1, 0, 0).Unix() {
			err.Body += "; resets " + time.Unix(failure.ResetsAt, 0).UTC().Format(time.RFC3339)
		}
	case "context_length_exceeded":
		err.Body = "context_length_exceeded: shorten or compact the conversation"
	case "model_not_found", "model_not_supported":
		err.Body = "model is unavailable for this ChatGPT account; refresh models and select another"
	default:
		switch status {
		case http.StatusUnauthorized:
			err.Body = "OpenAI subscription access was rejected; sign in again"
		case http.StatusTooManyRequests:
			err.Body = "OpenAI subscription requests are temporarily rate limited"
		case http.StatusForbidden:
			err.Body = "this ChatGPT account does not have access to the requested model"
		case http.StatusBadRequest:
			err.Body = "OpenAI rejected the subscription request parameters"
		}
	}
	return err
}

// Catalog compatibility version from the stable upstream release checked on
// 2026-09-08: github.com/openai/codex/releases/tag/rust-v0.153.4. This is not
// WHIP's version or a claim to be the Codex client.
const subscriptionCatalogVersion = "0.153.4"

func (c *Client) subscriptionModels(ctx context.Context) ([]ModelInfo, error) {
	credentials, err := c.openAI.Credentials(ctx)
	if err != nil {
		return nil, err
	}
	for attempt := range 2 {
		authCtx := context.WithValue(ctx, subscriptionAuthKey{}, credentials)
		request, err := c.subscriptionRequest(authCtx, http.MethodGet, "/models?client_version="+subscriptionCatalogVersion, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/json")
		response, err := c.HTTP.Do(request)
		if err != nil {
			return nil, errors.New("could not fetch OpenAI subscription models")
		}
		if response.StatusCode == http.StatusUnauthorized && attempt == 0 {
			_ = response.Body.Close()
			credentials, err = c.openAI.Refresh(ctx, credentials.AccessToken)
			if err != nil {
				return nil, err
			}
			continue
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusOK {
			return nil, subscriptionResponseError(response)
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		if err != nil || len(data) > maxResponseBytes {
			return nil, errors.New("could not read bounded OpenAI model catalog")
		}
		var catalog struct {
			Models []struct {
				Slug             string   `json:"slug"`
				Visibility       string   `json:"visibility"`
				Context          int      `json:"context_window"`
				MaxContext       int      `json:"max_context_window"`
				EffectivePercent int      `json:"effective_context_window_percent"`
				Modalities       []string `json:"input_modalities"`
				Efforts          []struct {
					Effort string `json:"effort"`
				} `json:"supported_reasoning_levels"`
			} `json:"models"`
		}
		if json.Unmarshal(data, &catalog) != nil || len(catalog.Models) > 512 {
			return nil, errors.New("OpenAI returned a malformed model catalog")
		}
		models := []ModelInfo{}
		for _, model := range catalog.Models {
			if model.Visibility != "list" || SubscriptionOutputLimit(model.Slug) == 0 {
				continue
			}
			contextLimit := model.Context
			if contextLimit <= 0 {
				contextLimit = model.MaxContext
			}
			if contextLimit <= 0 || contextLimit > 16<<20 {
				continue
			}
			percent := model.EffectivePercent
			if percent <= 0 || percent > 100 {
				percent = 95
			}
			info := ModelInfo{
				ID: model.Slug, ContextLength: contextLimit * percent / 100,
				MaxCompletionTokens: SubscriptionOutputLimit(model.Slug), InputModalities: model.Modalities,
			}
			for _, effort := range model.Efforts {
				if effort.Effort != "" && len(effort.Effort) <= 64 && !strings.ContainsAny(effort.Effort, "\r\n") {
					info.ReasoningEfforts = append(info.ReasoningEfforts, effort.Effort)
				}
			}
			models = append(models, info)
		}
		if len(models) == 0 {
			return nil, errors.New("OpenAI returned no models with supported visibility and verified limits; check this account's Codex access")
		}
		return models, nil
	}
	return nil, openaiauth.ErrSignInRequired
}
