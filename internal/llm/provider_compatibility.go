package llm

import (
	"encoding/json"
	"strings"
)

// encodeChatRequest limits provider-specific changes to exact preset roots.
// A custom proxy keeps the generic wire contract, even with a familiar hostname.
func hasThinking(msgs []Message) bool {
	for _, m := range msgs {
		if len(m.Thinking) > 0 {
			return true
		}
	}
	return false
}

func (c *Client) encodeChatRequest(req Request) ([]byte, error) {
	// "off" is the explicit no-reasoning level everywhere else in whip; the
	// wire contract expresses it by omitting the parameter.
	if req.ReasoningEffort == "off" {
		req.ReasoningEffort = ""
	}
	// Chat-completions has no thinking-block shape; replaying captured
	// Anthropic thinking there is at best ignored and at worst a strict
	// provider's rejection. Only the messages flavor replays thinking.
	// req.Messages aliases the caller's conversation slice — copy first so
	// the captured thinking survives in storage.
	if hasThinking(req.Messages) {
		msgs := make([]Message, len(req.Messages))
		copy(msgs, req.Messages)
		for i := range msgs {
			msgs[i].Thinking = nil
		}
		req.Messages = msgs
	}
	switch c.BaseURL {
	case "https://api.cerebras.ai/v1", "https://api.groq.com/openai/v1", "https://api.deepseek.com",
		"https://api.fireworks.ai/inference/v1", "https://api.together.ai/v1", "https://api.deepinfra.com/v1/openai":
		// These compatible APIs do not document OpenAI's explicit cache-key
		// parameter. Automatic prefix caching remains provider-controlled.
		req.PromptCacheKey = ""
	}
	if c.BaseURL == "https://api.deepseek.com" && strings.HasPrefix(req.Model, "deepseek-v4-") {
		// DeepSeek V4 defaults to thinking, whose tool round trips require
		// replaying reasoning_content. Our history stores final text/tool calls.
		// Use its documented non-thinking mode until that protocol is supported.
		req.ReasoningEffort = ""
		return json.Marshal(struct {
			Request
			Thinking struct {
				Type string `json:"type"`
			} `json:"thinking"`
		}{Request: req, Thinking: struct {
			Type string `json:"type"`
		}{Type: "disabled"}})
	}
	return json.Marshal(req)
}
