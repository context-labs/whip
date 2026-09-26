package llm

import (
	"encoding/json"
	"slices"
	"strings"
)

// encodeChatRequest limits provider-specific changes to exact preset roots.
// A custom proxy keeps the generic wire contract, even with a familiar hostname.
func (c *Client) encodeChatRequest(req Request) ([]byte, error) {
	if c.BaseURL == "https://api.openai.com/v1" {
		// Exposed reasoning_content is a compatible-provider extension, not
		// an OpenAI chat message field. Keep it durable across provider switches
		// without sending it to this endpoint. Custom proxies keep their contract.
		req.Messages = slices.Clone(req.Messages)
		for i := range req.Messages {
			req.Messages[i].ReasoningContent = ""
		}
	}
	switch c.BaseURL {
	case "https://api.cerebras.ai/v1", "https://api.groq.com/openai/v1", "https://api.deepseek.com",
		"https://api.fireworks.ai/inference/v1", "https://api.together.ai/v1", "https://api.deepinfra.com/v1/openai":
		// These compatible APIs do not document OpenAI's explicit cache-key
		// parameter. Automatic prefix caching remains provider-controlled.
		req.PromptCacheKey = ""
	}
	if c.BaseURL == "https://api.deepseek.com" && strings.HasPrefix(req.Model, "deepseek-v4-") {
		// Keep this preset's existing non-thinking default. Older saved tool
		// rounds can predate reasoning_content retention; do not implicitly
		// enable a mode that requires reasoning those sessions never stored.
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
