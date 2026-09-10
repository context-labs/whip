package llm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
)

// Astra's tool protocol requires Responses. Keep custom compatible endpoints
// on their declared Chat Completions contract.
func (c *Client) apiResponses(model string) bool {
	return c.openAI == nil && c.BaseURL == "https://api.openai.com/v1" && model == "gpt-6-astra"
}

// Scope opaque reasoning history to this API credential, separate from ChatGPT
// account IDs. The key itself never enters persisted continuation state.
func (c *Client) apiResponseScope() string {
	digest := sha256.Sum256([]byte(c.APIKey))
	return "openai-api:" + hex.EncodeToString(digest[:])
}

func (c *Client) apiResponsesOnce(
	ctx context.Context,
	model string,
	body []byte,
	onText, onThink func(string),
	onTool func(string, string, string),
) (Message, Usage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return Message{}, Usage{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return Message{}, Usage{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		usage, err := responseError(response)
		return Message{}, usage, err
	}
	return decodeResponses(response.Body, c.apiResponseScope(), model, onText, onThink, onTool)
}
