package llm

// Messages flavor: speak Anthropic Messages natively rather than through a
// chat-completions translation layer. The translation is lossy for thinking
// models (the translator enables thinking but whip's history cannot replay
// signature-carrying thinking blocks with tool_use, so the second tool round
// 400s opaque upstream errors) and for models that only expose a messages
// endpoint at all. Slice 1 deliberately does not send thinking; see PLAN.md
// (phase 2) for signature capture.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// imageMediaType extracts the media type from a data: URL image part;
// falls back to png for malformed values.
func imageMediaType(dataURL string) string {
	if !strings.HasPrefix(dataURL, "data:") {
		return "image/png"
	}
	rest := strings.TrimPrefix(dataURL, "data:")
	if i := strings.Index(rest, ";"); i > 0 {
		return rest[:i]
	}
	return "image/png"
}

// imageData strips a data: URL to its base64 payload.
func imageData(dataURL string) string {
	if i := strings.Index(dataURL, ","); i > 0 {
		return dataURL[i+1:]
	}
	return ""
}

const anthropicMessagesPath = "/messages"

// encodeMessages translates a Request into the Anthropic Messages body.
// thinkingBudgetTokens maps whip's reasoning-effort ladder to Anthropic
// thinking budget_tokens, floored at Anthropic's documented minimum (1024).
// Values are conservative: budgets cap thinking tokens per request and are
// subtracted from the max_tokens ceiling, so generous defaults risk truncating
// visible output on small max_tokens calls.
var thinkingBudgetTokens = map[string]int{
	"minimal": 1024,
	"low":     2048,
	"medium":  4096,
	"high":    8192,
	"xhigh":   16384,
	"max":     32768,
}

func encodeMessages(req Request) ([]byte, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	thinkingEnabled := req.ReasoningEffort != "" && req.ReasoningEffort != "off" && req.ReasoningEffort != "none"
	budget := 0
	if thinkingEnabled {
		if budgetTokens, ok := thinkingBudgetTokens[req.ReasoningEffort]; ok {
			// Anthropic requires max_tokens > budget_tokens; leave room for
			// visible output by capping the budget at half the ceiling.
			budget = min(budgetTokens, maxTokens/2)
		} else {
			// Unknown effort level: a wrong budget is a hard 400 upstream.
			// Fail loudly rather than guess.
			return nil, fmt.Errorf("anthropic-messages: unsupported reasoning effort %q", req.ReasoningEffort)
		}
		budget = max(budget, 1024) // documented minimum
	}
	// When thinking is on but history carries no thinking blocks (sessions
	// predating capture, or the turn itself), Anthropic still accepts the
	// request — thinking need only be replayed once emitted..
	var system []string
	blocks := []any{}
	for _, message := range req.Messages {
		switch message.Role {
		case "system", "developer":
			system = append(system, message.TextContent())
		case "user":
			content, err := anthropicUserContent(message)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, map[string]any{"role": "user", "content": content})
		case "assistant":
			content := []any{}
			for _, block := range message.Thinking {
				// Replay captured thinking ahead of tool_use, signature
				// verbatim — Anthropic validates the pairing and the
				// signature on every thinking-enabled tool round.
				content = append(content, map[string]any{
					"type": "thinking", "thinking": block.Thinking, "signature": block.Signature,
				})
			}
			if message.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": message.Content})
			}
			for _, call := range message.ToolCalls {
				var input any
				if err := json.Unmarshal([]byte(call.Function.Arguments), &input); err != nil {
					return nil, fmt.Errorf("anthropic-messages: tool call %q arguments: %w", call.Function.Name, err)
				}
				content = append(content, map[string]any{
					"type": "tool_use", "id": call.ID, "name": call.Function.Name, "input": input,
				})
			}
			blocks = append(blocks, map[string]any{"role": "assistant", "content": content})
		case "tool":
			blocks = append(blocks, map[string]any{
				"role": "user",
				"content": []any{map[string]any{
					"type": "tool_result", "tool_use_id": message.ToolCallID, "content": message.TextContent(),
				}},
			})
		default:
			return nil, fmt.Errorf("anthropic-messages: unsupported message role %q", message.Role)
		}
	}
	var tools []any
	for _, tool := range req.Tools {
		var schema any
		if err := json.Unmarshal(tool.Function.Parameters, &schema); err != nil {
			return nil, fmt.Errorf("anthropic-messages: tool %q schema: %w", tool.Function.Name, err)
		}
		tools = append(tools, map[string]any{
			"name": tool.Function.Name, "description": tool.Function.Description, "input_schema": schema,
		})
	}
	body := map[string]any{
		"model": req.Model, "messages": blocks, "stream": true,
		"anthropic_version": "2023-06-01",
	}
	if len(system) > 0 {
		body["system"] = strings.Join(system, "\n\n")
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if thinkingEnabled {
		body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
	}
	// prompt_cache_key and stream_options have no Messages meaning; omitted.
	return json.Marshal(body)
}

func anthropicUserContent(message Message) ([]any, error) {
	content := []any{}
	if message.Content != "" {
		content = append(content, map[string]any{"type": "text", "text": message.Content})
	}
	for _, part := range message.Parts {
		switch part.Type {
		case "text":
			content = append(content, map[string]any{"type": "text", "text": part.Text})
		case "image_url":
			if part.ImageURL == nil {
				return nil, errors.New("anthropic-messages: image part has no URL")
			}
			content = append(content, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type": "base64",
					// split a data: URL into media type and payload; seen as
					// source.base64 format in Messages
					"media_type": imageMediaType(part.ImageURL.URL),
					"data":       imageData(part.ImageURL.URL),
				},
			})
		default:
			return nil, errors.New("anthropic-messages: unsupported content part " + part.Type)
		}
	}
	if len(content) == 0 {
		return nil, errors.New("anthropic-messages: user message has no content")
	}
	return content, nil
}

// messagesOnce performs one Messages streaming attempt, parsed from SSE.
func (c *Client) messagesOnce(ctx context.Context, body []byte, onText, onThink func(string), onToolCall func(id, name, args string)) (Message, Usage, error) {
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+anthropicMessagesPath, strings.NewReader(string(body)))
	if err != nil {
		return Message{}, Usage{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		// Bearer is the gateway's auth (inference.net validates it before
		// dispatching to the Anthropic backend); x-api-key is the native
		// Anthropic header, harmless for gateways that ignore it and
		// required by endpoints that speak Anthropic directly.
		hr.Header.Set("Authorization", "Bearer "+c.APIKey)
		hr.Header.Set("X-Api-Key", c.APIKey)
	}
	hr.Header.Set("Anthropic-Version", "2023-06-01")
	resp, err := c.do(hr, c.stallTimeout(chatStall))
	if err != nil {
		return Message{}, Usage{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		usage, err := responseError(resp)
		return Message{}, usage, err
	}
	return parseMessagesSSE(resp.Body, onText, onThink, onToolCall)
}

// parseMessagesSSE reduces an Anthropic event stream to the same callbacks
// as streamOnce: text deltas, thinking deltas (unused slice 1), tool-call
// argument accumulation keyed by block index.
func parseMessagesSSE(r io.Reader, onText, onThink func(string), onToolCall func(id, name, args string)) (Message, Usage, error) {
	msg := Message{Role: "assistant"}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxResponseBytes)
	calls := map[int]*ToolCall{}
	thinkingBlocks := map[int]*ThinkingBlock{}
	finish := ""
	done := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimPrefix(line, "data:")
		var ev struct {
			Type  string `json:"type"`
			Delta struct {
				Type       string `json:"type"`
				Text       string `json:"text"`
				Thinking   string `json:"thinking"`
				Signature  string `json:"signature"`
				Partial    string `json:"partial_json"`
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			ContentBlock struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				Name      string `json:"name"`
				Signature string `json:"signature"`
			} `json:"content_block"`
			Index int `json:"index"`
			Error *struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(payload)), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			msg.Usage = nil
		case "content_block_start":
			switch ev.ContentBlock.Type {
			case "tool_use":
				if calls[ev.Index] == nil {
					call := &ToolCall{Type: "function", ID: ev.ContentBlock.ID}
					call.Function.Name = ev.ContentBlock.Name
					calls[ev.Index] = call
				}
			case "thinking", "redacted_thinking":
				if thinkingBlocks[ev.Index] == nil {
					thinkingBlocks[ev.Index] = &ThinkingBlock{
						Signature: ev.ContentBlock.Signature,
						// redacted blocks carry no readable thinking text
						Thinking: "",
					}
				}
			}
		case "content_block_delta":
			switch ev.Delta.Type {
			case "text_delta":
				msg.Content += ev.Delta.Text
				if onText != nil {
					onText(ev.Delta.Text)
				}
			case "thinking_delta":
				if thinkingBlocks[ev.Index] == nil {
					thinkingBlocks[ev.Index] = &ThinkingBlock{}
				}
				thinkingBlocks[ev.Index].Thinking += ev.Delta.Thinking
				if onThink != nil {
					onThink(ev.Delta.Thinking)
				}
			case "signature_delta":
				if thinkingBlocks[ev.Index] == nil {
					thinkingBlocks[ev.Index] = &ThinkingBlock{}
				}
				thinkingBlocks[ev.Index].Signature += ev.Delta.Signature
			case "input_json_delta":
				call := calls[ev.Index]
				if call == nil {
					continue
				}
				call.Function.Arguments += ev.Delta.Partial
				if onToolCall != nil {
					onToolCall(call.ID, call.Function.Name, call.Function.Arguments)
				}
			}
		case "message_delta":
			if ev.Delta.StopReason != "" {
				finish = ev.Delta.StopReason
			}
			msg.Usage = nil
		case "message_stop":
			done = true
		case "error":
			if ev.Error != nil {
				return msg, Usage{}, providerError(ev.Error.Message, msg.Content != "" || len(calls) > 0)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return msg, Usage{}, err
	}
	if finish == "" && !done {
		return msg, Usage{}, errors.New("model stream ended without a completion marker")
	}
	// Ordered blocks: thinking indexes precede tool_use indexes for a
	// thinking-enabled tool round, preserving the pairing Anthropic validates
	// on replay. Iterate up to the max index seen; nil entries simply skip.
	maxIndex := 0
	for i := range calls {
		if i > maxIndex {
			maxIndex = i
		}
	}
	for i := range thinkingBlocks {
		if i > maxIndex {
			maxIndex = i
		}
	}
	for i := 0; i <= maxIndex; i++ {
		if block := thinkingBlocks[i]; block != nil {
			msg.Thinking = append(msg.Thinking, *block)
		}
		if call := calls[i]; call != nil {
			msg.ToolCalls = append(msg.ToolCalls, *call)
		}
	}
	if finish == "max_tokens" {
		msg.ToolCalls = nil
		msg.Content += "\n[response truncated by max_tokens; tool calls discarded]"
	}
	return msg, Usage{}, nil
}
