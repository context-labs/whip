package model

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

const maxResponseItems = 512

func responseScope(route, credential, model string) string {
	digest := hmac.New(sha256.New, []byte(credential))
	_, _ = digest.Write([]byte("whip:openai-responses:v1\x00" + route + "\x00" + model))
	return hex.EncodeToString(digest.Sum(nil))
}

func encodeResponses(request Request, scope string, maxTokens int64) ([]byte, error) {
	if len(request.Messages) > 100 {
		return nil, errors.New("model message count exceeds limit")
	}
	remaining := session.MaxContentBytes - len(request.Instructions)
	tools, err := encodeTools(request.Tools, &remaining)
	if err != nil || remaining < 0 {
		return nil, errors.New("provider instructions or tools exceed context limit")
	}
	instructions := []string{request.Instructions}
	input := []any{}
	for _, message := range request.Messages {
		if err := session.ValidateMessage(message.Role, message.Parts); err != nil {
			return nil, err
		}
		if saved := message.Continuation; (request.Purpose == "" || request.Purpose == "turn") && message.Role == session.Assistant && saved != nil && saved.Scope == scope {
			if err := saved.Validate(); err != nil {
				return nil, errors.New("saved provider continuation is invalid")
			}
			parts, err := responseParts([]byte(saved.Data), nil)
			if err != nil {
				return nil, errors.New("saved provider continuation is invalid")
			}
			if reflect.DeepEqual(parts, message.Parts) {
				remaining -= len(saved.Data)
				if remaining < 0 {
					return nil, errors.New("provider context exceeds content limit")
				}
				var items []json.RawMessage
				_ = json.Unmarshal([]byte(saved.Data), &items) // Validated by responseParts.
				for _, item := range items {
					input = append(input, item)
				}
				continue
			}
		}
		content := []any{}
		flush := func() {
			if len(content) != 0 {
				input = append(input, map[string]any{"type": "message", "role": message.Role, "content": content})
				content = []any{}
			}
		}
		for _, part := range message.Parts {
			text := part.Text
			switch part.Type {
			case "tool_call":
				flush()
				remaining -= len(part.Call.Arguments)
				input = append(input, map[string]any{"type": "function_call", "call_id": part.Call.ID, "name": part.Call.Name, "arguments": string(part.Call.Arguments)})
				continue
			case "tool_result":
				remaining -= len(part.Result.Output)
				input = append(input, map[string]any{"type": "function_call_output", "call_id": part.Result.CallID, "output": part.Result.Output})
				continue
			case "content":
				value, ok := request.Contents[part.ReferenceID]
				if !ok {
					return nil, errors.New("content reference was not authorized and hydrated")
				}
				remaining -= len(value.Data)
				if remaining < 0 {
					return nil, errors.New("provider context exceeds content limit")
				}
				switch value.MediaType {
				case "text/plain":
					if !utf8.Valid(value.Data) {
						return nil, errors.New("text content is not valid UTF-8")
					}
					text = string(value.Data)
				case "image/png", "image/jpeg", "image/webp", "image/gif":
					if message.Role != session.User {
						return nil, errors.New("responses images require user messages")
					}
					content = append(content, map[string]string{"type": "input_image", "image_url": "data:" + value.MediaType + ";base64," + base64.StdEncoding.EncodeToString(value.Data)})
					continue
				default:
					return nil, errors.New("content media type is unsupported by the Responses provider")
				}
			case "text":
				remaining -= len(text)
			}
			if message.Role == session.System {
				instructions = append(instructions, text)
				continue
			}
			kind := "input_text"
			if message.Role == session.Assistant {
				kind = "output_text"
			}
			content = append(content, map[string]string{"type": kind, "text": text})
		}
		flush()
	}
	if remaining < 0 || len(input) > maxResponseItems {
		return nil, errors.New("provider context exceeds content limit")
	}
	if len(input) == 0 {
		return nil, errors.New("model request has no input messages")
	}
	functions := make([]any, 0, len(tools))
	for _, tool := range tools {
		functions = append(functions, map[string]any{"type": "function", "name": tool.Function.Name, "description": tool.Function.Description, "parameters": tool.Function.Parameters, "strict": false})
	}
	wire := map[string]any{
		"model": request.Selection.Name, "instructions": strings.Join(instructions, "\n\n"), "input": input,
		"tools": functions, "stream": true, "store": false, "include": []string{"reasoning.encrypted_content"},
	}
	if maxTokens > 0 {
		wire["max_output_tokens"] = maxTokens
	}
	if request.Selection.Effort != "" && request.Selection.Effort != "off" {
		wire["reasoning"] = map[string]string{"effort": request.Selection.Effort, "summary": "auto"}
	}
	if key := promptCacheKey(request.SessionID); key != "" {
		wire["prompt_cache_key"] = key
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	if len(body) > 8<<20 {
		return nil, errors.New("encoded model request exceeds size limit")
	}
	return body, nil
}

type responseItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Role      string `json:"role"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Content   []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	} `json:"content"`
}

func responseParts(raw json.RawMessage, allowed map[string]bool) ([]session.Part, error) {
	var items []json.RawMessage
	if !utf8.Valid(raw) || json.Unmarshal(raw, &items) != nil || len(items) > maxResponseItems {
		return nil, streamError("provider returned invalid output items")
	}
	parts := []session.Part{}
	for _, value := range items {
		var item responseItem
		if json.Unmarshal(value, &item) != nil {
			return nil, streamError("provider returned an invalid output item")
		}
		switch item.Type {
		case "reasoning":
			// Opaque fields are retained only in the private continuation.
		case "message":
			if item.Role != "assistant" {
				return nil, streamError("provider returned an invalid output role")
			}
			var text strings.Builder
			for _, part := range item.Content {
				switch part.Type {
				case "output_text":
					text.WriteString(part.Text)
				case "refusal":
					text.WriteString(part.Refusal)
				default:
					return nil, streamError("provider returned unsupported output content")
				}
			}
			if text.Len() > 0 {
				parts = append(parts, session.Part{Type: "text", Text: text.String()})
			}
		case "function_call":
			if allowed != nil && !allowed[item.Name] {
				return nil, streamError("provider returned an undeclared tool call")
			}
			parts = append(parts, session.Part{Type: "tool_call", Call: &session.ToolCall{ID: item.CallID, Name: item.Name, Arguments: json.RawMessage(item.Arguments)}})
		default:
			return nil, streamError("provider returned unsupported output items")
		}
	}
	if session.ValidateMessage(session.Assistant, parts) != nil {
		return nil, streamError("provider returned invalid assistant content")
	}
	return parts, nil
}

func decodeResponsesUsage(raw json.RawMessage) (session.ModelUsage, *int64, *string) {
	var fields map[string]json.RawMessage
	if len(raw) == 0 || string(raw) == "null" {
		return session.ModelUsage{}, nil, nil
	}
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return session.ModelUsage{}, nil, new("provider usage was not an object; accounting unavailable")
	}
	fields["prompt_tokens"] = fields["input_tokens"]
	fields["completion_tokens"] = fields["output_tokens"]
	fields["prompt_tokens_details"] = fields["input_tokens_details"]
	fields["completion_tokens_details"] = fields["output_tokens_details"]
	translated, _ := json.Marshal(fields)
	return decodeChatUsage(translated)
}

func executeResponses(ctx context.Context, client *http.Client, url string, auth responseAuth, scope string, body []byte, allowed map[string]bool, emit func(Chunk)) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, &CallError{Message: "provider request could not be constructed"}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream, application/json")
	auth.setHeaders(request)

	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, streamError("provider transport failed; outcome is unknown")
	}
	defer func() { _ = response.Body.Close() }()
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode >= 200 && response.StatusCode < 300 && strings.EqualFold(mediaType, "text/event-stream") {
		return decodeResponsesStream(ctx, response.Body, scope, allowed, emit, auth.accountID != "")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, streamError("provider response was incomplete or exceeded the size limit")
	}
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
		Error json.RawMessage `json:"error"`
	}
	decodeErr := json.Unmarshal(raw, &envelope)
	result := Response{}
	if decodeErr == nil {
		result.Usage, result.ReportedCostNanoUSD, result.UsageNote = decodeResponsesUsage(envelope.Usage)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status := response.StatusCode
		contextLimit := (status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge) &&
			!strings.EqualFold(mediaType, "text/event-stream") && decodeErr == nil && isContextLimitError(envelope.Error)
		message := fmt.Sprintf("provider returned HTTP %d", status)
		if contextLimit {
			message = fmt.Sprintf("provider rejected request context (HTTP %d)", status)
		}
		failure := &CallError{
			StatusCode: status, ContextLimit: contextLimit, Message: message, RetryAfter: retryAfter(response.Header.Get("Retry-After")),
			Retryable: status == http.StatusTooManyRequests || status == http.StatusInternalServerError || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout,
		}
		if auth.accountID != "" {
			failure.AuthRejected = status == http.StatusUnauthorized && !strings.EqualFold(mediaType, "text/event-stream")
			if message := subscriptionDiagnostic(raw); message != "" {
				failure.Message, failure.Retryable, failure.AuthRejected = message, false, false
			}
		}
		return result, failure
	}
	state := responsesStream{response: result, subscription: auth.accountID != "", scope: scope, allowed: allowed, emit: emit, calls: map[int]*responseStreamCall{}}
	if err := state.complete(raw); err != nil {
		return state.response, err
	}
	return state.response, nil
}
