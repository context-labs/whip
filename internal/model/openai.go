package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/session"
)

const maxResponseBytes = 4 << 20

// Route is a resolved host route. Credentials are ephemeral and deliberately
// excluded from the durable request snapshot. The URL is an API base URL.
type Route struct {
	Kind            string
	URL             string
	Credential      string
	Prices          session.ModelPrices
	MaxOutputTokens int64
	TimeoutMillis   int64
	MaxAttempts     int
	// ContextWindowTokens is the host-declared, provider-enforced maximum.
	// Nil is unknown; this is not a token count for the encoded request.
	ContextWindowTokens *int64
}

// OpenAI implements streaming Chat Completions and Responses. Retry
// policy is enforced by the runner so every request has its own durable attempt.
type OpenAI struct {
	Resolve func(context.Context, session.ModelSelection) (Route, error)
	Client  *http.Client
	Auth    SubscriptionAuth
}

// CallError reports safe diagnostic and retry evidence without retaining a
// provider body, request credentials or transport error text in persisted errors.
type CallError struct {
	StatusCode int
	RetryAfter time.Duration
	Retryable  bool
	Uncertain  bool
	// ContextLimit is a confirmed rejection that may permit a smaller request,
	// never an automatic retry of this prepared request.
	ContextLimit bool
	// AuthRejected confirms a completed HTTP 401, not stream or transport uncertainty.
	AuthRejected bool
	Message      string
}

func (e *CallError) Error() string { return e.Message }

func (p OpenAI) Prepare(ctx context.Context, request Request) (Prepared, error) {
	if request.Purpose == "" {
		request.Purpose = "turn"
	}
	if p.Resolve == nil {
		return Prepared{}, errors.New("model route resolver is required")
	}
	if err := request.Selection.Validate(); err != nil {
		return Prepared{}, err
	}
	route, err := p.Resolve(ctx, request.Selection)
	if err != nil {
		return Prepared{}, err
	}
	if route.MaxAttempts < 1 || route.MaxAttempts > 5 {
		return Prepared{}, fmt.Errorf("%w: invalid provider attempt limit", session.ErrInvalid)
	}
	if route.Kind == "openai-codex" {
		if err := subscriptionRoute(&route, request.Selection.Name); err != nil {
			return Prepared{}, err
		}
	}
	var inputBound, contextWindow *int64
	if route.ContextWindowTokens != nil {
		if *route.ContextWindowTokens < 1 || *route.ContextWindowTokens > 1000000000 || route.MaxOutputTokens > *route.ContextWindowTokens {
			return Prepared{}, fmt.Errorf("%w: context window must be 1–1000000000 tokens and at least the output limit", session.ErrInvalid)
		}
		inputBound = new(*route.ContextWindowTokens)
		contextWindow = new(*route.ContextWindowTokens)
	}
	if strings.ContainsAny(route.Credential, "\r\n\x00") {
		return Prepared{}, errors.New("provider credential contains invalid header characters")
	}
	baseURL := strings.TrimRight(route.URL, "/")
	adapter, path := route.Kind, "/chat/completions"
	if adapter == "" {
		adapter = "openai-chat"
	}
	var body []byte
	scope := ""
	var captured openaiauth.CapturedCredentials
	switch adapter {
	case "openai-chat":
		body, err = encodeChat(request, baseURL, route.MaxOutputTokens)
	case "openai-responses":
		path = "/responses"
		scope = responseScope(baseURL+path, route.Credential, request.Selection.Name)
		body, err = encodeResponses(request, scope, route.MaxOutputTokens)
	case "openai-codex":
		path = "/responses"
		captured, err = p.captureSubscription(ctx)
		if err == nil {
			scope = subscriptionScope(baseURL+path, captured.Credentials.AccountID, request.Selection.Name)
			body, err = encodeResponses(request, scope, 0)
		}
	default:
		return Prepared{}, fmt.Errorf("%w: unsupported provider adapter", session.ErrInvalid)
	}
	if err != nil {
		return Prepared{}, err
	}
	allowedTools := make(map[string]bool, len(request.Tools))
	for _, tool := range request.Tools {
		allowedTools[tool.Name] = true
	}
	hash := sha256.Sum256(body)
	snapshot := session.ModelRequestSnapshot{
		Purpose: request.Purpose, Model: request.Selection,
		Route: baseURL + path, Adapter: adapter,
		RequestDigest: hex.EncodeToString(hash[:]), Prices: route.Prices.Clone(),
		MaxOutputTokens: route.MaxOutputTokens, TimeoutMillis: route.TimeoutMillis,
		InputTokenBound: inputBound,
	}
	if err := snapshot.Validate(); err != nil {
		return Prepared{}, err
	}
	// Copy the client policy: a redirect must never silently change the recorded
	// route or forward credentials. Transport connections can still be shared.
	client := http.Client{}
	if p.Client != nil {
		client = *p.Client
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if adapter == "openai-codex" {
		return prepareSubscription(p.Auth, captured, &client, snapshot, route.MaxAttempts, contextWindow, scope, body, allowedTools), nil
	}
	return Prepared{
		Snapshot: snapshot, MaxAttempts: route.MaxAttempts, ContextWindowTokens: contextWindow,
		Execute: func(ctx context.Context, emit func(Chunk)) (Response, error) {
			if adapter == "openai-responses" {
				return executeResponses(ctx, &client, snapshot.Route, responseAuth{credential: route.Credential}, scope, body, allowedTools, emit)
			}
			return executeChat(ctx, &client, snapshot.Route, route.Credential, body, allowedTools, emit)
		},
	}, nil
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []chatCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type chatPart struct {
	Type     string     `json:"type"`
	Text     string     `json:"text,omitempty"`
	ImageURL *chatImage `json:"image_url,omitempty"`
}
type chatImage struct {
	URL string `json:"url"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatThinking struct {
	Type string `json:"type"`
}

func encodeChat(request Request, baseURL string, maxTokens int64) ([]byte, error) {
	if len(request.Messages) > 100 {
		return nil, errors.New("model message count exceeds limit")
	}
	messages := make([]chatMessage, 0, len(request.Messages)+1)
	remaining := session.MaxContentBytes - len(request.Instructions)
	if remaining < 0 {
		return nil, errors.New("provider instructions exceed context limit")
	}
	tools, err := encodeTools(request.Tools, &remaining)
	if err != nil {
		return nil, err
	}
	if request.Instructions != "" {
		messages = append(messages, chatMessage{Role: "system", Content: request.Instructions})
	}
	for _, message := range request.Messages {
		if err := session.ValidateMessage(message.Role, message.Parts); err != nil {
			return nil, err
		}
		if message.Role == session.Tool {
			result := message.Parts[0].Result
			remaining -= len(result.Output)
			if remaining < 0 {
				return nil, errors.New("provider context exceeds content limit")
			}
			messages = append(messages, chatMessage{Role: "tool", ToolCallID: result.CallID, Content: result.Output})
			continue
		}
		parts := make([]chatPart, 0, len(message.Parts))
		var calls []chatCall
		var text strings.Builder
		hasImage := false
		for _, part := range message.Parts {
			if part.Type == "tool_call" {
				remaining -= len(part.Call.Arguments)
				if remaining < 0 {
					return nil, errors.New("provider context exceeds content limit")
				}
				calls = append(calls, chatCall{
					ID: part.Call.ID, Type: "function",
					Function: chatFunctionCall{Name: part.Call.Name, Arguments: string(part.Call.Arguments)},
				})
				continue
			}
			if part.Type == "text" {
				remaining -= len(part.Text)
				if remaining < 0 {
					return nil, errors.New("provider context exceeds content limit")
				}
				text.WriteString(part.Text)
				parts = append(parts, chatPart{Type: "text", Text: part.Text})
				continue
			}
			content, ok := request.Contents[part.ReferenceID]
			if !ok {
				return nil, errors.New("content reference was not authorized and hydrated")
			}
			// Count each occurrence, including repeats of a reference, before
			// base64 allocation. A small cache cannot permit an enormous wire body.
			remaining -= len(content.Data)
			if remaining < 0 {
				return nil, errors.New("provider context exceeds content limit")
			}
			switch content.MediaType {
			case "text/plain":
				if !utf8.Valid(content.Data) {
					return nil, errors.New("text content is not valid UTF-8")
				}
				text.Write(content.Data)
				parts = append(parts, chatPart{Type: "text", Text: string(content.Data)})
			case "image/png", "image/jpeg", "image/webp", "image/gif":
				hasImage = true
				parts = append(parts, chatPart{Type: "image_url", ImageURL: &chatImage{
					URL: "data:" + content.MediaType + ";base64," + base64.StdEncoding.EncodeToString(content.Data),
				}})
			default:
				return nil, errors.New("content media type is unsupported by the chat provider")
			}
		}
		var encoded any = text.String()
		if hasImage {
			encoded = parts
		} else if text.Len() == 0 && len(calls) > 0 {
			encoded = nil
		}
		messages = append(messages, chatMessage{Role: string(message.Role), Content: encoded, ToolCalls: calls})
	}
	if len(messages) == 0 {
		return nil, errors.New("model request has no messages")
	}
	effort := request.Selection.Effort
	if effort == "off" {
		effort = ""
	}
	cacheKey := promptCacheKey(request.SessionID)
	// Match complete preset roots, not hostnames: custom proxies retain the
	// generic wire contract. These presets omit the explicit cache-key field.
	switch baseURL {
	case "https://api.cerebras.ai/v1", "https://api.groq.com/openai/v1", "https://api.deepseek.com",
		"https://api.fireworks.ai/inference/v1", "https://api.together.ai/v1", "https://api.deepinfra.com/v1/openai":
		cacheKey = ""
	}
	var thinking *chatThinking
	if baseURL == "https://api.deepseek.com" && strings.HasPrefix(request.Selection.Name, "deepseek-v4-") {
		// Thinking mode requires reasoning_content replay, which this transcript
		// does not retain. Preserve the selected effort in the request snapshot.
		effort = ""
		thinking = &chatThinking{Type: "disabled"}
	}
	body, err := json.Marshal(struct {
		Model               string            `json:"model"`
		Messages            []chatMessage     `json:"messages"`
		MaxCompletionTokens int64             `json:"max_completion_tokens"`
		ReasoningEffort     string            `json:"reasoning_effort,omitempty"`
		PromptCacheKey      string            `json:"prompt_cache_key,omitempty"`
		Thinking            *chatThinking     `json:"thinking,omitempty"`
		Tools               []chatTool        `json:"tools,omitempty"`
		Stream              bool              `json:"stream"`
		StreamOptions       chatStreamOptions `json:"stream_options"`
	}{Model: request.Selection.Name, Messages: messages, MaxCompletionTokens: maxTokens, ReasoningEffort: effort, PromptCacheKey: cacheKey, Thinking: thinking, Tools: tools, Stream: true, StreamOptions: chatStreamOptions{IncludeUsage: true}})
	if err != nil {
		return nil, err
	}
	if len(body) > 8<<20 {
		return nil, errors.New("encoded model request exceeds size limit")
	}
	return body, nil
}

func promptCacheKey(id session.SessionID) string {
	key := string(id)
	if len(key) > 64 {
		digest := sha256.Sum256([]byte(key))
		key = hex.EncodeToString(digest[:])
	}
	return key
}

func executeChat(ctx context.Context, client *http.Client, url, credential string, body []byte, allowedTools map[string]bool, emit func(Chunk)) (Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, errors.New("could not construct provider request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream, application/json")
	if credential != "" {
		request.Header.Set("Authorization", "Bearer "+credential)
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, &CallError{Uncertain: true, Message: "provider transport failed; outcome is unknown"}
	}
	defer func() {
		// The read result determines completion; closing only releases resources.
		_ = response.Body.Close()
	}()
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode >= 200 && response.StatusCode < 300 && strings.EqualFold(mediaType, "text/event-stream") {
		return decodeChatStream(ctx, response.Body, allowedTools, emit)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, &CallError{Uncertain: true, Message: "provider response was incomplete or exceeded the size limit"}
	}
	var envelope struct {
		Choices json.RawMessage `json:"choices"`
		Usage   json.RawMessage `json:"usage"`
		Error   json.RawMessage `json:"error"`
	}
	decodeErr := json.Unmarshal(raw, &envelope)
	result := Response{}
	if decodeErr == nil {
		result.Usage, result.ReportedCostNanoUSD, result.UsageNote = decodeChatUsage(envelope.Usage)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusInternalServerError || response.StatusCode == http.StatusBadGateway || response.StatusCode == http.StatusServiceUnavailable || response.StatusCode == http.StatusGatewayTimeout
		contextLimit := (response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusRequestEntityTooLarge) &&
			!strings.EqualFold(mediaType, "text/event-stream") && decodeErr == nil && isContextLimitError(envelope.Error)
		message := fmt.Sprintf("provider returned HTTP %d", response.StatusCode)
		if contextLimit {
			message = fmt.Sprintf("provider rejected request context (HTTP %d)", response.StatusCode)
		}
		return result, &CallError{
			StatusCode: response.StatusCode, Retryable: retryable, RetryAfter: retryAfter(response.Header.Get("Retry-After")),
			ContextLimit: contextLimit, Message: message,
		}
	}
	var choices []struct {
		Message struct {
			Role      string          `json:"role"`
			Content   *string         `json:"content"`
			ToolCalls json.RawMessage `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}
	if decodeErr != nil || json.Unmarshal(envelope.Choices, &choices) != nil || len(choices) != 1 {
		return result, &CallError{Uncertain: true, Message: "provider returned an invalid completion response"}
	}
	choice := choices[0]
	if choice.Message.Role != "assistant" {
		return result, &CallError{Message: "provider returned no assistant message"}
	}
	if choice.FinishReason != "stop" && choice.FinishReason != "tool_calls" {
		return result, &CallError{Message: "provider did not complete the response"}
	}
	parts, err := decodeChatParts(choice.Message.Content, choice.Message.ToolCalls, choice.FinishReason, allowedTools)
	if err != nil {
		return result, err
	}
	if err := session.ValidateMessage(session.Assistant, parts); err != nil {
		return result, &CallError{Message: "provider returned invalid assistant content"}
	}
	result.Parts = parts
	emitResponse(result, emit)
	return result, nil
}

func isContextLimitError(raw json.RawMessage) bool {
	var detail struct {
		Code string `json:"code"`
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &detail) != nil {
		return false
	}
	for _, value := range []string{detail.Code, detail.Type} {
		if value == "context_length_exceeded" || value == "prompt_too_long" {
			return true
		}
	}
	return false
}

func retryAfter(value string) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		return time.Duration(min(seconds, 60)) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		return min(max(time.Until(date), 0), time.Minute)
	}
	return 0
}
