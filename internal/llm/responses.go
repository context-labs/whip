package llm

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

const (
	maxResponseBytes = 32 << 20
	maxResponseItems = 512
)

// ResponseContinuation is immutable host-only model state. Its value fields
// make transcript/snapshot copies independent without another mutable cache.
// The custom Message codec persists it; public projections must clear it.
type ResponseContinuation struct {
	AccountID string `json:"account_id"`
	Model     string `json:"model"`
	Items     string `json:"items"`
}

type responseItem struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	Role      string `json:"role,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Content   []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	} `json:"content,omitempty"`
}

func encodeResponses(req Request, accountID string) ([]byte, error) {
	return encodeResponsesWithLimit(req, accountID, 0)
}

func encodeResponsesWithLimit(req Request, accountID string, maxOutput int) ([]byte, error) {
	var instructions []string
	input := []any{}
	for _, message := range req.Messages {
		if message.Role == "system" || message.Role == "developer" {
			instructions = append(instructions, message.TextContent())
			continue
		}
		if continuation := message.Continuation; message.Role == "assistant" &&
			continuation.AccountID == accountID && continuation.Model == req.Model && continuation.Items != "" {
			if len(continuation.Items) > maxResponseBytes {
				return nil, errors.New("saved OpenAI continuation exceeds the size limit")
			}
			var items []json.RawMessage
			if json.Unmarshal([]byte(continuation.Items), &items) != nil {
				return nil, errors.New("saved OpenAI continuation is malformed")
			}
			saved, err := responseMessage(items)
			if err != nil {
				return nil, err
			}
			if saved.Content == message.Content && sameResponseCalls(saved.ToolCalls, message.ToolCalls) {
				for _, item := range items {
					input = append(input, item)
				}
				continue
			}
		}
		switch message.Role {
		case "tool":
			input = append(input, map[string]any{
				"type": "function_call_output", "call_id": message.ToolCallID, "output": message.TextContent(),
			})
		case "user", "assistant":
			content := []any{}
			textType := "input_text"
			if message.Role == "assistant" {
				textType = "output_text"
			}
			if message.Content != "" {
				content = append(content, map[string]any{"type": textType, "text": message.Content})
			}
			for _, part := range message.Parts {
				switch part.Type {
				case "text":
					content = append(content, map[string]any{"type": textType, "text": part.Text})
				case "image_url":
					if part.ImageURL == nil || message.Role != "user" {
						return nil, errors.New("invalid image in OpenAI message")
					}
					content = append(content, map[string]any{"type": "input_image", "image_url": part.ImageURL.URL})
				default:
					return nil, errors.New("unsupported OpenAI message content")
				}
			}
			if len(content) > 0 {
				input = append(input, map[string]any{"type": "message", "role": message.Role, "content": content})
			}
			for _, call := range message.ToolCalls {
				input = append(input, map[string]any{
					"type": "function_call", "call_id": call.ID, "name": call.Function.Name, "arguments": call.Function.Arguments,
				})
			}
		default:
			return nil, fmt.Errorf("unsupported OpenAI message role %q", message.Role)
		}
	}
	tools := []any{}
	for _, tool := range req.Tools {
		if tool.Type != "function" {
			return nil, errors.New("OpenAI Responses requires function tools")
		}
		tools = append(tools, map[string]any{
			"type": "function", "name": tool.Function.Name, "description": tool.Function.Description,
			"parameters": tool.Function.Parameters, "strict": false,
		})
	}
	body := map[string]any{
		"model": req.Model, "instructions": strings.Join(instructions, "\n\n"), "input": input,
		"tools": tools, "stream": true, "store": false, "include": []string{"reasoning.encrypted_content"},
	}
	if maxOutput > 0 {
		body["max_output_tokens"] = maxOutput
	}
	if req.ReasoningEffort != "" {
		effort := req.ReasoningEffort
		if effort == "off" {
			effort = "none"
		}
		body["reasoning"] = map[string]string{"effort": effort, "summary": "auto"}
	}
	if req.PromptCacheKey != "" {
		body["prompt_cache_key"] = normalizeCacheKey(req.PromptCacheKey)
	}
	if req.Temperature != nil || req.TopP != nil {
		return nil, errors.New("temperature and top_p are unsupported by this OpenAI Responses route")
	}
	return json.Marshal(body)
}

func sameResponseCalls(a, b []ToolCall) bool {
	return slices.EqualFunc(a, b, func(a, b ToolCall) bool {
		return a.ID == b.ID && a.Type == b.Type && a.Function == b.Function
	})
}

func responseMessage(items []json.RawMessage) (Message, error) {
	message := Message{Role: "assistant"}
	if len(items) > maxResponseItems {
		return message, errors.New("OpenAI response has too many output items")
	}
	var text strings.Builder
	calls := []ToolCall{}
	seen := make(map[string]bool)
	bytes := 0
	for _, raw := range items {
		bytes += len(raw)
		if bytes > maxResponseBytes {
			return message, errors.New("OpenAI response exceeds the size limit")
		}
		var item responseItem
		if json.Unmarshal(raw, &item) != nil {
			return message, errors.New("OpenAI returned a malformed output item")
		}
		switch item.Type {
		case "reasoning":
			// Preserve the entire opaque item, including encrypted_content, for
			// exact replay. It is never interpreted as executable code or UI text.
		case "message":
			if item.Role != "assistant" {
				return message, errors.New("OpenAI returned an unexpected output role")
			}
			for _, part := range item.Content {
				switch part.Type {
				case "output_text":
					text.WriteString(part.Text)
				case "refusal":
					text.WriteString(part.Refusal)
				default:
					return message, errors.New("OpenAI returned unsupported output content")
				}
			}
		case "function_call":
			if item.CallID == "" || item.Name == "" || seen[item.CallID] || !validToolCallArgs(item.Arguments) ||
				strings.TrimSpace(item.Arguments) == "null" {
				return message, errors.New("OpenAI returned an invalid function call; tool calls were discarded")
			}
			seen[item.CallID] = true
			call := ToolCall{ID: item.CallID, Type: "function"}
			call.Function.Name, call.Function.Arguments = item.Name, item.Arguments
			calls = append(calls, call)
		default:
			return message, errors.New("OpenAI returned an unsupported output item; tool calls were discarded")
		}
	}
	message.Content, message.ToolCalls = text.String(), calls
	return message, nil
}

func responseUsage(data json.RawMessage) (Usage, error) {
	if len(data) == 0 || string(data) == "null" {
		return Usage{}, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return Usage{}, errors.New("OpenAI returned malformed token usage")
	}
	translated := make(map[string]json.RawMessage)
	for from, to := range map[string]string{ //nolint:gosec // G101: these are token-usage JSON field names, not credentials.
		"input_tokens": "prompt_tokens", "output_tokens": "completion_tokens",
		"input_tokens_details": "prompt_tokens_details", "output_tokens_details": "completion_tokens_details",
	} {
		if value, ok := fields[from]; ok {
			translated[to] = value
		}
	}
	raw, err := json.Marshal(translated)
	if err != nil {
		return Usage{}, err
	}
	return decodeUsage(raw)
}

// decodeResponses consumes bounded SSE frames, including multiline data fields.
// Only a completed response publishes executable tool calls or continuation.
func decodeResponses(
	reader io.Reader,
	accountID, model string,
	onText, onThink func(string),
	onTool func(string, string, string),
) (Message, Usage, error) {
	message := Message{Role: "assistant"}
	var usage Usage
	var frame strings.Builder
	var text strings.Builder
	calls := make(map[int]responseItem)
	var outputItems []json.RawMessage
	totalBytes := 0
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxResponseBytes)
	consume := func() (bool, error) {
		if frame.Len() == 0 {
			return false, nil
		}
		data := frame.String()
		frame.Reset()
		if strings.TrimSpace(data) == "[DONE]" {
			return false, nil // A transport marker is not a successful response.
		}
		var event struct {
			Type        string          `json:"type"`
			Delta       string          `json:"delta"`
			OutputIndex int             `json:"output_index"`
			Item        json.RawMessage `json:"item"`
			Response    json.RawMessage `json:"response"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			return false, nonRetryable{errors.New("OpenAI returned malformed stream data")}
		}
		switch event.Type {
		case "response.output_text.delta", "response.refusal.delta":
			text.WriteString(event.Delta)
			onText(event.Delta)
		case "response.reasoning_summary_text.delta":
			onThink(event.Delta)
		case "response.output_item.added", "response.output_item.done":
			if event.OutputIndex < 0 || event.OutputIndex >= maxResponseItems {
				return false, nonRetryable{errors.New("OpenAI output index exceeds the limit")}
			}
			var item responseItem
			if json.Unmarshal(event.Item, &item) != nil {
				return false, nonRetryable{errors.New("OpenAI returned a malformed output item")}
			}
			for len(outputItems) <= event.OutputIndex {
				outputItems = append(outputItems, nil)
			}
			if event.Type == "response.output_item.done" {
				outputItems[event.OutputIndex] = event.Item
			} else if item.Type == "function_call" {
				calls[event.OutputIndex] = item
				onTool(item.CallID, item.Name, item.Arguments)
			}
		case "response.function_call_arguments.delta":
			call, ok := calls[event.OutputIndex]
			if !ok {
				return false, nonRetryable{errors.New("OpenAI sent function arguments without a call")}
			}
			call.Arguments += event.Delta
			calls[event.OutputIndex] = call
			onTool(call.CallID, call.Name, call.Arguments)
		case "response.completed", "response.failed", "response.incomplete":
			var response struct {
				Status string            `json:"status"`
				Output []json.RawMessage `json:"output"`
				Usage  json.RawMessage   `json:"usage"`
			}
			if json.Unmarshal(event.Response, &response) != nil {
				return false, nonRetryable{errors.New("OpenAI returned malformed terminal response")}
			}
			var err error
			usage, err = responseUsage(response.Usage)
			if err != nil {
				return false, nonRetryable{err}
			}
			if event.Type != "response.completed" || response.Status != "completed" {
				if event.Type == "response.failed" {
					return false, subscriptionError(0, event.Response)
				}
				return false, nonRetryable{errors.New("OpenAI response did not complete; tool calls were discarded")}
			}
			// Codex sends completed items separately and may leave terminal
			// output empty. Keep their opaque fields and original output order.
			if len(response.Output) == 0 {
				response.Output = outputItems
			}
			completed, err := responseMessage(response.Output)
			if err != nil {
				return false, nonRetryable{err}
			}
			if !strings.HasPrefix(completed.Content, text.String()) {
				return false, nonRetryable{errors.New("OpenAI completed text disagrees with its stream")}
			}
			if remaining := strings.TrimPrefix(completed.Content, text.String()); remaining != "" {
				onText(remaining)
			}
			for _, call := range completed.ToolCalls {
				onTool(call.ID, call.Function.Name, call.Function.Arguments)
			}
			items, err := json.Marshal(response.Output)
			if err != nil {
				return false, nonRetryable{err}
			}
			completed.Continuation = ResponseContinuation{AccountID: accountID, Model: model, Items: string(items)}
			message = completed
			return true, nil
		case "error":
			return false, subscriptionError(0, []byte(data))
		}
		return false, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		totalBytes += len(line)
		if totalBytes > 2*maxResponseBytes {
			message.Content = text.String()
			return message, usage, nonRetryable{errors.New("OpenAI stream exceeds the size limit")}
		}
		if line == "" {
			done, err := consume()
			if done {
				return message, usage, nil
			}
			if err != nil {
				message.Content = text.String()
				return message, usage, err
			}
		} else if data, ok := strings.CutPrefix(line, "data:"); ok {
			frame.WriteString(strings.TrimPrefix(data, " "))
			frame.WriteByte('\n')
			if frame.Len() > maxResponseBytes {
				return Message{Role: "assistant", Content: text.String()}, usage,
					nonRetryable{errors.New("OpenAI stream frame exceeds the size limit")}
			}
		}
	}
	if done, err := consume(); done || err != nil {
		if err != nil {
			message.Content = text.String()
		}
		return message, usage, err
	}
	message.Content = text.String()
	if err := scanner.Err(); err != nil {
		return message, usage, err
	}
	return message, usage, io.ErrUnexpectedEOF
}
