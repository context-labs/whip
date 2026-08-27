package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/codexauth"
)

// Codex talks to the ChatGPT Codex Responses endpoint using locally managed
// OAuth credentials.
type Codex struct {
	BaseURL string
	Source  *codexauth.Source
	HTTP    *http.Client
}

// codexModelsClientVersion is required by the subscription catalog endpoint.
const codexModelsClientVersion = "0.0.0"

var (
	_ Client = (*OpenAI)(nil)
	_ Client = (*Codex)(nil)
)

func NewCodex(baseURL string, source *codexauth.Source) *Codex {
	return &Codex{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Source:  source,
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
	}
}

// Models fetches the account-scoped catalog exposed by the Codex subscription
// backend. Unlike the public OpenAI /models response, its records use Codex
// names and include the models this ChatGPT account may actually select.
func (c *Codex) Models(ctx context.Context) ([]ModelInfo, error) {
	if c.Source == nil {
		return nil, codexauth.ErrLoginRequired
	}
	creds, err := c.Source.Credentials(ctx)
	if err != nil {
		return nil, err
	}
	modelsURL := c.BaseURL + "/codex/models?client_version=" + codexModelsClientVersion
	hr, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, err
	}
	setCodexHeaders(hr, creds)
	resp, err := c.httpClient().Do(hr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, httpError(resp)
	}

	var body codexModelsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&body); err != nil {
		return nil, err
	}
	infos := make([]ModelInfo, 0, len(body.Models))
	for _, model := range body.Models {
		if model.Slug == "" || !model.SupportedInAPI {
			continue
		}
		contextLength := model.ContextWindow
		if contextLength == 0 {
			contextLength = model.MaxContextWindow
		}
		modalities := model.InputModalities
		if len(modalities) == 0 {
			// Codex's model protocol defaults omitted modalities to text + image.
			modalities = []string{"text", "image"}
		}
		info := ModelInfo{
			ID:               model.Slug,
			ContextLength:    contextLength,
			ReasoningEfforts: model.ReasoningEfforts(),
			InputModalities:  modalities,
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// Stream maps the current conversation to a Responses API request and folds
// its SSE events back into the existing Whip message shape.
func (c *Codex) Stream(ctx context.Context, req Request, onText, onThink func(string)) (Message, Usage, error) {
	body, err := json.Marshal(codexRequest(req, true))
	if err != nil {
		return Message{}, Usage{}, err
	}
	resp, err := c.post(ctx, body, true)
	if err != nil {
		return Message{}, Usage{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Message{}, Usage{}, httpError(resp)
	}

	msg := Message{Role: "assistant"}
	var usage Usage
	calls := callCollector{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var event responseEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if event.Type == "error" || event.Type == "response.failed" {
			if event.Error.Message != "" {
				return Message{}, usage, fmt.Errorf("api error: %s", event.Error.Message)
			}
			return Message{}, usage, errors.New("codex response failed")
		}
		switch event.Type {
		case "response.output_text.delta":
			msg.Content += event.Delta
			if onText != nil && event.Delta != "" {
				onText(event.Delta)
			}
		case "response.reasoning_text.delta", "response.reasoning_summary_text.delta", "response.reasoning.delta":
			if onThink != nil && event.Delta != "" {
				onThink(event.Delta)
			}
		case "response.output_item.added":
			setResponseMessageID(&msg, event.Item)
			setCodexReasoning(&msg, event.Item)
			calls.add(event.Item, false)
		case "response.output_item.done", "response.function_call_arguments.done":
			setResponseMessageID(&msg, event.Item)
			setCodexReasoning(&msg, event.Item)
			calls.add(event.Item, true)
			if event.CallID != "" {
				calls.add(responseItem{CallID: event.CallID, Arguments: event.Arguments}, true)
			}
		case "response.function_call_arguments.delta":
			calls.delta(event.CallID, event.Delta)
		case "response.completed":
			usage = event.Response.Usage.usage()
			for _, item := range event.Response.Output {
				setResponseMessageID(&msg, item)
				setCodexReasoning(&msg, item)
			}
			calls.addAll(event.Response.Output)
		}
	}
	if err := scanner.Err(); err != nil {
		return Message{}, usage, err
	}
	msg.ToolCalls = calls.calls
	return msg, usage, nil
}

// Complete makes the non-streaming Responses request used by compaction and
// goal formulation.
func (c *Codex) Complete(ctx context.Context, req Request) (string, Usage, error) {
	body, err := json.Marshal(codexRequest(req, false))
	if err != nil {
		return "", Usage{}, err
	}
	resp, err := c.post(ctx, body, false)
	if err != nil {
		return "", Usage{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", Usage{}, httpError(resp)
	}
	var out response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&out); err != nil {
		return "", Usage{}, err
	}
	var text strings.Builder
	for _, item := range out.Output {
		for _, content := range item.Content {
			if content.Type == "output_text" || content.Type == "text" {
				text.WriteString(content.Text)
			}
		}
	}
	if text.Len() == 0 {
		return "", out.Usage.usage(), errors.New("no text in Codex completion response")
	}
	return text.String(), out.Usage.usage(), nil
}

func (c *Codex) post(ctx context.Context, body []byte, stream bool) (*http.Response, error) {
	if c.Source == nil {
		return nil, codexauth.ErrLoginRequired
	}
	creds, err := c.Source.Credentials(ctx)
	if err != nil {
		return nil, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/codex/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	setCodexHeaders(hr, creds)
	hr.Header.Set("Openai-Beta", "responses=experimental")
	hr.Header.Set("Content-Type", "application/json")
	if stream {
		hr.Header.Set("Accept", "text/event-stream")
	}
	return c.httpClient().Do(hr)
}

func setCodexHeaders(hr *http.Request, creds codexauth.Credentials) {
	hr.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	hr.Header.Set("Chatgpt-Account-Id", creds.AccountID)
	hr.Header.Set("Originator", "whip")
}

func (c *Codex) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func httpError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return &HTTPError{Status: resp.Status, Body: strings.TrimSpace(string(body))}
}

type codexRequestBody struct {
	Model        string         `json:"model"`
	Instructions string         `json:"instructions,omitempty"`
	Input        []any          `json:"input"`
	Tools        []responseTool `json:"tools,omitempty"`
	Reasoning    *struct {
		Effort string `json:"effort"`
	} `json:"reasoning,omitempty"`
	Include           []string `json:"include,omitempty"`
	Store             bool     `json:"store"`
	Stream            bool     `json:"stream"`
	ToolChoice        string   `json:"tool_choice"`
	ParallelToolCalls bool     `json:"parallel_tool_calls"`
}

type responseTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func codexRequest(req Request, stream bool) codexRequestBody {
	req.Messages = repairToolHistory(stripAuthoredForCodex(req.Messages))
	body := codexRequestBody{
		Model:             req.Model,
		Input:             []any{},
		Include:           []string{"reasoning.encrypted_content"},
		Store:             false,
		Stream:            stream,
		ToolChoice:        "auto",
		ParallelToolCalls: true,
	}
	var instructions []string
	messageIDIndex := 0
	for messageIndex := 0; messageIndex < len(req.Messages); messageIndex++ {
		msg := req.Messages[messageIndex]
		if msg.Role == "system" {
			instructions = append(instructions, msg.TextContent())
			continue
		}
		switch msg.Role {
		case "user":
			content := []responseInputContent{}
			if text := msg.TextContent(); text != "" {
				content = append(content, responseInputContent{Type: "input_text", Text: text})
			}
			for _, part := range msg.Parts {
				if part.Type == "image_url" && part.ImageURL != nil {
					content = append(content, responseInputContent{Type: "input_image", ImageURL: part.ImageURL.URL})
				}
			}
			if len(content) > 0 {
				body.Input = append(body.Input, responseInputMessage{Role: "user", Content: content})
			}
		case "assistant":
			for _, reasoning := range msg.CodexReasoning {
				body.Input = append(body.Input, reasoning)
			}
			if text := msg.TextContent(); text != "" {
				// Codex accepts previous assistant text only as a completed output
				// message item. A bare assistant input message with output_text is
				// rejected as an unknown content parameter.
				body.Input = append(body.Input, responseOutputMessage{
					Type:   "message",
					Role:   "assistant",
					ID:     codexMessageID(msg, messageIDIndex),
					Status: "completed",
					Phase:  msg.ResponsePhase,
					Content: []responseOutputText{{
						Type:        "output_text",
						Text:        text,
						Annotations: []any{},
					}},
				})
			}
			if hasLegacyCodexToolCall(msg.ToolCalls) {
				context, toolMessages := legacyCodexToolContext(msg.ToolCalls, req.Messages[messageIndex+1:])
				body.Input = append(body.Input, responseInputMessage{
					Role:    "user",
					Content: []responseInputContent{{Type: "input_text", Text: context}},
				})
				messageIndex += toolMessages
				messageIDIndex += toolMessages
				break
			}
			for _, call := range msg.ToolCalls {
				body.Input = append(body.Input, responseItem{
					Type:      "function_call",
					ID:        call.ItemID,
					CallID:    call.ID,
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
				})
			}
		case "tool":
			body.Input = append(body.Input, responseToolOutput{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: msg.Content,
			})
		}
		messageIDIndex++
	}
	body.Instructions = strings.Join(instructions, "\n\n")
	if req.ReasoningEffort != "" {
		body.Reasoning = &struct {
			Effort string `json:"effort"`
		}{Effort: req.ReasoningEffort}
	}
	for _, tool := range req.Tools {
		body.Tools = append(body.Tools, responseTool{
			Type:        tool.Type,
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		})
	}
	return body
}

// Codex will not replay a function call without its response-item ID. Sessions
// written before Whip started preserving those IDs cannot recreate it, so keep
// the useful tool activity as ordinary user context instead of sending an
// invalid native function_call/function_call_output pair.
func hasLegacyCodexToolCall(calls []ToolCall) bool {
	for _, call := range calls {
		if call.ItemID == "" {
			return true
		}
	}
	return false
}

func legacyCodexToolContext(calls []ToolCall, following []Message) (string, int) {
	var text strings.Builder
	text.WriteString("[Earlier tool activity]")
	for _, call := range calls {
		text.WriteString("\n\n[Tool call]\n")
		text.WriteString(call.Function.Name)
		text.WriteString("(")
		text.WriteString(call.Function.Arguments)
		text.WriteString(")")
	}
	consumed := 0
	for consumed < len(following) && following[consumed].Role == "tool" {
		result := following[consumed]
		text.WriteString("\n\n[Tool result]\n")
		text.WriteString(result.Content)
		consumed++
	}
	return text.String(), consumed
}

// codexModelsResponse is the subset of the Codex backend /models schema that
// Whip needs to build selectable model routes. The backend owns availability;
// unsupported or rollout-gated models are not inserted into this catalog.
type codexModelsResponse struct {
	Models []codexModel `json:"models"`
}

type codexModel struct {
	Slug                     string                `json:"slug"`
	SupportedInAPI           bool                  `json:"supported_in_api"`
	ContextWindow            int                   `json:"context_window"`
	MaxContextWindow         int                   `json:"max_context_window"`
	SupportedReasoningLevels []codexReasoningLevel `json:"supported_reasoning_levels"`
	InputModalities          []string              `json:"input_modalities"`
}

type codexReasoningLevel struct {
	Effort string `json:"effort"`
}

func (m codexModel) ReasoningEfforts() []string {
	efforts := make([]string, 0, len(m.SupportedReasoningLevels))
	for _, level := range m.SupportedReasoningLevels {
		if level.Effort != "" {
			efforts = append(efforts, level.Effort)
		}
	}
	return efforts
}

type responseInputMessage struct {
	Role    string                 `json:"role"`
	Content []responseInputContent `json:"content"`
}

type responseInputContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type responseOutputMessage struct {
	Type    string               `json:"type"`
	Role    string               `json:"role"`
	ID      string               `json:"id"`
	Status  string               `json:"status"`
	Phase   string               `json:"phase,omitempty"`
	Content []responseOutputText `json:"content"`
}

type responseOutputText struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
}

type responseContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type responseToolOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

type responseEvent struct {
	Type      string       `json:"type"`
	Delta     string       `json:"delta"`
	CallID    string       `json:"call_id"`
	Arguments string       `json:"arguments"`
	Item      responseItem `json:"item"`
	Response  response     `json:"response"`
	Error     struct {
		Message string `json:"message"`
	} `json:"error"`
}

type response struct {
	Output []responseItem `json:"output"`
	Usage  responseUsage  `json:"usage"`
}

type responseItem struct {
	Type      string            `json:"type"`
	ID        string            `json:"id,omitempty"`
	Phase     string            `json:"phase,omitempty"`
	CallID    string            `json:"call_id"`
	Name      string            `json:"name"`
	Arguments string            `json:"arguments"`
	Content   []responseContent `json:"content,omitempty"`
	Raw       json.RawMessage   `json:"-"`
}

func (i *responseItem) UnmarshalJSON(data []byte) error {
	type wire responseItem
	var item wire
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	*i = responseItem(item)
	i.Raw = append(i.Raw[:0], data...)
	return nil
}

type responseUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details,omitempty"`
}

func (u responseUsage) usage() Usage {
	usage := Usage{PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens}
	if u.InputTokensDetails != nil {
		usage.PromptTokensDetails = &struct {
			CachedTokens int `json:"cached_tokens"`
		}{CachedTokens: u.InputTokensDetails.CachedTokens}
	}
	return usage
}

type callCollector struct {
	calls   []ToolCall
	byID    map[string]int
	unnamed int
}

func (c *callCollector) add(item responseItem, replace bool) {
	if item.Type != "" && item.Type != "function_call" {
		return
	}
	if item.CallID == "" && item.Name == "" && item.Arguments == "" {
		return
	}
	if c.byID == nil {
		c.byID = map[string]int{}
	}
	id := item.CallID
	if id == "" {
		id = fmt.Sprintf("output-%d", c.unnamed)
		c.unnamed++
	}
	index, ok := c.byID[id]
	if !ok {
		index = len(c.calls)
		c.byID[id] = index
		c.calls = append(c.calls, ToolCall{ID: id, Type: "function"})
	}
	call := &c.calls[index]
	if item.ID != "" {
		call.ItemID = item.ID
	}
	if item.Name != "" {
		call.Function.Name = item.Name
	}
	if item.Arguments != "" {
		if replace {
			call.Function.Arguments = item.Arguments
		} else {
			call.Function.Arguments += item.Arguments
		}
	}
}

func codexMessageID(msg Message, index int) string {
	if msg.ResponseID != "" {
		return msg.ResponseID
	}
	return fmt.Sprintf("msg_%d", index)
}

func setResponseMessageID(msg *Message, item responseItem) {
	if item.Type != "message" {
		return
	}
	if msg.ResponseID == "" && item.ID != "" {
		msg.ResponseID = item.ID
	}
	if item.Phase != "" {
		msg.ResponsePhase = item.Phase
	}
}

func setCodexReasoning(msg *Message, item responseItem) {
	if item.Type != "reasoning" || len(item.Raw) == 0 {
		return
	}
	for index, prior := range msg.CodexReasoning {
		var existing responseItem
		if json.Unmarshal(prior, &existing) == nil && existing.ID == item.ID {
			msg.CodexReasoning[index] = append(json.RawMessage(nil), item.Raw...)
			return
		}
	}
	msg.CodexReasoning = append(msg.CodexReasoning, append(json.RawMessage(nil), item.Raw...))
}

func (c *callCollector) delta(callID, delta string) {
	if callID == "" || delta == "" {
		return
	}
	c.add(responseItem{CallID: callID, Arguments: delta}, false)
}

func (c *callCollector) addAll(items []responseItem) {
	for _, item := range items {
		c.add(item, true)
	}
}
