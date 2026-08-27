// Package llm is a minimal streaming client for OpenAI-compatible chat completions APIs.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Message is one chat message. Content is a string; ToolCalls set on assistant
// messages, ToolCallID on role "tool" results. A user message may also carry
// image Parts (multimodal/vision) — when Parts is non-empty it is sent as the
// content array and Content is mirrored as a text part so both stay in sync.
type Message struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	Parts      []ContentPart `json:"-"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	// Name is the function name on role "tool" messages. OpenAI ignores it,
	// but Moonshot/Kimi requires it ("tool messages need a resolvable tool
	// name") — without it every tool-using turn 400s.
	Name string `json:"name,omitempty"`
	// Authored marks a user message the human actually typed and submitted, as
	// opposed to one whip injected on their behalf (steered background-task
	// results, goal-check continuations). Internal only — never sent to the
	// provider. Used so input-history recall cycles only real submissions.
	Authored bool `json:"authored,omitempty"`
	// SentAt is when the human submitted the message (local time). Internal
	// only — never sent to the provider; used by the rewind picker's
	// per-message timestamp. A pointer so omitempty drops it for injected and
	// pre-field messages (a zero time.Time struct is never omitted).
	SentAt *time.Time `json:"sent_at,omitempty"`
	// Usage is the token accounting for the assistant response that produced
	// this message. Internal only — never sent to the provider; powers
	// per-turn cost display and survives session resume (the in-memory
	// session totals do not).
	Usage *Usage `json:"usage,omitempty"`
	// Model records which model produced an assistant message ("id @
	// provider"), so a /model switch mid-session doesn't rewrite history
	// silently. Internal only — never sent to the provider.
	Model string `json:"model,omitempty"`
	// ResponseID is the provider's output-message item ID. The Codex Responses
	// API requires it when a prior assistant message is replayed as history.
	// It is kept out of generic OpenAI-compatible requests.
	ResponseID string `json:"response_id,omitempty"`
	// ResponsePhase is the Codex output-message phase (for example,
	// "commentary"). Codex requires it to be preserved when it appears on a
	// prior assistant message.
	ResponsePhase string `json:"response_phase,omitempty"`
	// CodexReasoning holds opaque Responses reasoning items, including encrypted
	// content. It is replayed only to Codex; generic providers never receive it.
	CodexReasoning []json.RawMessage `json:"codex_reasoning,omitempty"`
	// RewoundFrom notes that this message replaced an earlier clipped one
	// (rewind + resubmit). Internal only — never sent to the provider.
	RewoundFrom string `json:"rewound_from,omitempty"`
}

// ContentPart is one element of a multimodal user message: either text or an
// image (as a data-URL). Kimi K3 and OpenAI vision models require `content`
// as an array of these parts rather than a plain string when images are
// attached. The wire shape is {"type":"text","text":...} and
// {"type":"image_url","image_url":{"url":"data:image/png;base64,..."}}.
type ContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

// TextContent returns the message's text, whether it was set directly
// (Content) or carried in a Parts array (multimodal messages mirror their
// text into both).
func (m Message) TextContent() string {
	if m.Content != "" {
		return m.Content
	}
	for _, p := range m.Parts {
		if p.Type == "text" {
			return p.Text
		}
	}
	return ""
}

// imageDataURL builds a base64 data URL for image bytes of the given format
// extension (png, jpg, gif, webp, bmp). jpg is emitted as image/jpeg.
func imageDataURL(ext string, data []byte) string {
	mime := "image/" + ext
	if ext == "jpg" {
		mime = "image/jpeg"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// ImagePart builds an image ContentPart from raw bytes and a format extension.
func ImagePart(ext string, data []byte) ContentPart {
	p := ContentPart{Type: "image_url"}
	p.ImageURL = &struct {
		URL string `json:"url"`
	}{URL: imageDataURL(ext, data)}
	return p
}

// messageWire is the JSON shape of a Message. Content is `any` so it can be a
// plain string (text-only) or a []ContentPart array (multimodal). The internal
// fields are omitempty and cleared by stripAuthored before a provider request,
// so they only ever appear in the persisted session store.
type messageWire struct {
	Role           string            `json:"role"`
	Content        any               `json:"content"`
	ToolCalls      []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallID     string            `json:"tool_call_id,omitempty"`
	Name           string            `json:"name,omitempty"`
	Authored       bool              `json:"authored,omitempty"`
	SentAt         *time.Time        `json:"sent_at,omitempty"`
	Usage          *Usage            `json:"usage,omitempty"`
	Model          string            `json:"model,omitempty"`
	ResponseID     string            `json:"response_id,omitempty"`
	ResponsePhase  string            `json:"response_phase,omitempty"`
	CodexReasoning []json.RawMessage `json:"codex_reasoning,omitempty"`
	RewoundFrom    string            `json:"rewound_from,omitempty"`
}

// MarshalJSON sends Content as a plain string for text-only messages and as a
// content-parts array (text + images) for multimodal ones.
func (m Message) MarshalJSON() ([]byte, error) {
	w := messageWire{
		Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID,
		Name: m.Name, Authored: m.Authored, SentAt: m.SentAt, Usage: m.Usage,
		Model: m.Model, ResponseID: m.ResponseID, ResponsePhase: m.ResponsePhase, CodexReasoning: m.CodexReasoning, RewoundFrom: m.RewoundFrom,
	}
	if len(m.Parts) > 0 {
		parts := m.Parts
		if m.Content != "" {
			// keep the text part first so the model reads it before the images
			parts = append([]ContentPart{{Type: "text", Text: m.Content}}, parts...)
		}
		w.Content = parts
	}
	return json.Marshal(w)
}

// UnmarshalJSON accepts both the plain-string and content-parts wire forms.
func (m *Message) UnmarshalJSON(data []byte) error {
	var raw struct {
		messageWire
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role, m.ToolCalls, m.ToolCallID, m.Name = raw.Role, raw.ToolCalls, raw.ToolCallID, raw.Name
	m.Authored, m.SentAt, m.Usage, m.Model, m.ResponseID, m.ResponsePhase, m.CodexReasoning, m.RewoundFrom = raw.Authored, raw.SentAt, raw.Usage, raw.Model, raw.ResponseID, raw.ResponsePhase, raw.CodexReasoning, raw.RewoundFrom
	if len(raw.Content) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw.Content, &s); err == nil {
		m.Content = s
		return nil
	}
	var parts []ContentPart
	if err := json.Unmarshal(raw.Content, &parts); err != nil {
		return err
	}
	for _, p := range parts {
		switch p.Type {
		case "text":
			m.Content = p.Text
		case "image_url":
			m.Parts = append(m.Parts, p)
		}
	}
	return nil
}

// ToolCall is a model-requested tool invocation. DurationMs and ExitCode are
// whip-internal execution bookkeeping (never sent to the provider): how long
// the tool ran and how it finished, for a future /tools perf view.
type ToolCall struct {
	ID string `json:"id"`
	// ItemID is the provider's function-call item ID. Codex requires it when
	// replaying a prior function call; generic providers never receive it.
	ItemID   string `json:"item_id,omitempty"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
	DurationMs int64 `json:"duration_ms,omitempty"`
	ExitCode   int   `json:"exit_code,omitempty"`
}

// stripAuthored returns a copy of msgs with the internal Authored marker and
// SentAt timestamp cleared — they're whip-local bookkeeping (input-history
// recall, the rewind picker) and must never reach the provider. It copies
// because req.Messages typically aliases the caller's conversation slice,
// which must keep the fields for storage/recall.
func stripAuthored(msgs []Message) []Message {
	return stripInternal(msgs, false)
}

// stripAuthoredForCodex keeps the response-item IDs that Codex needs to
// replay prior assistant output and function calls.
func stripAuthoredForCodex(msgs []Message) []Message {
	return stripInternal(msgs, true)
}

func stripInternal(msgs []Message, keepCodexIDs bool) []Message {
	out := make([]Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		out[i].Authored = false
		out[i].SentAt = nil
		out[i].Usage = nil
		out[i].Model = ""
		if !keepCodexIDs {
			out[i].ResponseID = ""
			out[i].ResponsePhase = ""
			out[i].CodexReasoning = nil
		}
		out[i].RewoundFrom = ""
		for j := range out[i].ToolCalls {
			out[i].ToolCalls[j].DurationMs = 0
			out[i].ToolCalls[j].ExitCode = 0
			if !keepCodexIDs {
				out[i].ToolCalls[j].ItemID = ""
			}
		}
	}
	// Backfill tool-message Name from the owning call (older sessions predate
	// the field; providers that require it only look at Name).
	names := map[string]string{}
	for _, m := range out {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				names[tc.ID] = tc.Function.Name
			}
		}
	}
	for i := range out {
		if out[i].Role == "tool" && out[i].Name == "" {
			out[i].Name = names[out[i].ToolCallID]
		}
	}
	return out
}

// repairToolHistory patches message-pairing defects that strict providers
// (Kimi K3, Gemini) reject with a 400 before the first token:
//
//   - assistant tool_calls with no following tool result (interrupted turn)
//     get a synthetic "(interrupted before execution)" result per call
//   - tool messages whose tool_call_id has no owning assistant tool_call
//     (compaction/rewind trimmed the caller) are flattened into plain user
//     context — the model loses the ID pairing but keeps the information
//
// Idempotent: a well-formed conversation comes through unchanged.
func repairToolHistory(msgs []Message) []Message {
	answered := make(map[string]bool, len(msgs))
	callName := make(map[string]string, len(msgs))
	for i, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			answered[tc.ID] = false
			callName[tc.ID] = tc.Function.Name
			for _, r := range msgs[i+1:] {
				if r.Role == "tool" && r.ToolCallID == tc.ID {
					answered[tc.ID] = true
					break
				}
				if r.Role == "assistant" || r.Role == "user" {
					break // results always immediately follow their call
				}
			}
		}
	}
	out := make([]Message, 0, len(msgs))
	var pending []string // unanswered call IDs from the last assistant message
	flush := func() {    // synthetics land after any real results in the run
		for _, id := range pending {
			out = append(out, Message{
				Role:       "tool",
				Content:    "(interrupted before execution)",
				ToolCallID: id,
				Name:       callName[id],
			})
		}
		pending = nil
	}
	for _, m := range msgs {
		if m.Role == "tool" {
			if _, ok := answered[m.ToolCallID]; !ok {
				flush()
				// orphan: flatten into user context rather than drop the info
				out = append(out, Message{
					Role:    "user",
					Content: "[earlier tool result]\n" + m.Content,
				})
				continue
			}
			out = append(out, m)
			continue
		}
		flush()
		out = append(out, m)
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				if !answered[tc.ID] {
					pending = append(pending, tc.ID)
				}
			}
		}
	}
	flush()
	return out
}

// Tool is a tool definition advertised to the model.
type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// NewTool builds a Tool from name, description, and a JSON Schema string.
func NewTool(name, desc, schema string) Tool {
	t := Tool{Type: "function"}
	t.Function.Name = name
	t.Function.Description = desc
	t.Function.Parameters = json.RawMessage(schema)
	return t
}

// Client is the provider contract consumed by the agent loop.
type Client interface {
	Models(context.Context) ([]ModelInfo, error)
	Stream(context.Context, Request, func(string), func(string)) (Message, Usage, error)
	Complete(context.Context, Request) (string, Usage, error)
}

// OpenAI talks to an OpenAI-compatible chat-completions endpoint.
type OpenAI struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	// MaxRetries caps retries of transient request failures. 0 uses
	// DefaultMaxAttempts; 1 disables retries (a single attempt).
	MaxRetries int
	// OnRetry, when set, is invoked before each retry of a transient request
	// failure. Optional — nil means silent retries.
	OnRetry func(RetryEvent)
}

// attempts returns the total try count (initial + retries) for this client.
func (c *OpenAI) attempts() int {
	if c.MaxRetries > 0 {
		return c.MaxRetries
	}
	return DefaultMaxAttempts
}

func New(baseURL, apiKey string) *OpenAI {
	return &OpenAI{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
	}
}

// SetOnRetry installs the optional retry reporter used by the agent's active
// turn. It keeps retry reporting outside the provider contract so providers
// that do not retry do not need a no-op implementation.
func (c *OpenAI) SetOnRetry(fn func(RetryEvent)) {
	c.OnRetry = fn
}

// Request is a chat completions request.
type Request struct {
	Model           string    `json:"model"`
	Messages        []Message `json:"messages"`
	Tools           []Tool    `json:"tools,omitempty"`
	MaxTokens       int       `json:"max_tokens,omitempty"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
	Stream          bool      `json:"stream"`
	StreamOptions   *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

// Usage is the token accounting the provider reports for one request
// (prompt = input, completion = output). CachedTokens counts the slice of
// the prompt served from the provider's prompt cache. Providers that omit
// usage leave all fields zero — the session totals just skip those calls.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// PromptTokensDetails nests the cache hit count (OpenAI-compatible).
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// Cached is the prompt-token count served from cache (0 when unreported).
func (u Usage) Cached() int {
	if u.PromptTokensDetails == nil {
		return 0
	}
	return u.PromptTokensDetails.CachedTokens
}

// Chunk delta payload from the SSE stream.
type delta struct {
	Content string `json:"content"`
	// ReasoningContent carries thinking tokens on reasoning models (deepseek,
	// grok, kimi, claude all emit it; claude also nests it in thinking_blocks).
	ReasoningContent string `json:"reasoning_content"`
	ToolCalls        []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type chunk struct {
	Choices []struct {
		Delta        delta  `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *apiError `json:"error"`
	Usage *Usage    `json:"usage"`
}

type apiError struct {
	Message string `json:"message"`
}

// HTTPError is returned when the API responds with a non-2xx status. Body is
// the ( capped ) response payload so callers can match against provider-
// specific reason strings; Error() keeps the "<status>: <body>" shape the
// existing tests assert ( e.g. "... 401 ..." ).
type HTTPError struct {
	Status string
	Body   string
}

func (e *HTTPError) Error() string { return e.Status + ": " + e.Body }

// DefaultMaxAttempts is the built-in retry budget for transient request
// failures (one initial try plus retries). Client.MaxRetries overrides it;
// exported so the UI can show "attempt N/M".
const DefaultMaxAttempts = 8

// RetryEvent describes one failed attempt that is about to be retried. It is
// passed to the Client.OnRetry hook so the UI can show "retrying in Ns"
// instead of looking hung.
type RetryEvent struct {
	Attempt int           // the attempt that just failed (1-based)
	Max     int           // total attempts the client will make (initial + retries)
	Delay   time.Duration // how long the client will sleep before retrying
	Err     error         // the transient error that caused the retry
}

// retryableStatus reports whether an HTTP status is worth retrying: rate
// limits and server/gateway errors are transient; 4xx client errors are not.
func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// nonRetryable wraps an error the retry loop must not repeat (mid-stream
// provider error chunks). errors.Is/As unwrap through it, so callers see the
// underlying error unchanged.
type nonRetryable struct{ err error }

func (n nonRetryable) Error() string { return n.err.Error() }
func (n nonRetryable) Unwrap() error { return n.err }

// retryable reports whether err is a transient request failure: a transport
// error (connection reset, DNS, timeout — but not caller cancellation) or a
// retryable HTTP status. Context-limit 4xxs are deliberately excluded so the
// agent's compaction retry path still sees them immediately.
func retryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if _, ok := errors.AsType[nonRetryable](err); ok {
		return false
	}
	if he, ok := errors.AsType[*HTTPError](err); ok {
		code, _ := strconv.Atoi(strings.Fields(he.Status)[0])
		return retryableStatus(code)
	}
	// Non-HTTPError here means the transport failed (c.HTTP.Do error or a
	// dropped stream). Context-limit plain-text errors are strings matched by
	// IsContextLimit, not transport errors, so no misclassification risk.
	return true
}

// backoff returns the sleep before the next attempt: 1s, 2s, 4s… capped at
// 20s, plus up to 25% jitter so concurrent sessions don't retry in lockstep.
func backoff(attempt int) time.Duration {
	d := min(time.Second<<(attempt-1), 20*time.Second)
	return d + time.Duration(rand.Int64N(int64(d/4)+1)) //nolint:gosec // G404: retry jitter, not a security token
}

// sleep blocks for d or returns ctx's error if the caller cancels first.
// It is a package-level var so tests can swap in a no-op — the retry tests
// would otherwise burn real seconds in backoff.
var sleep = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// contextLimitMarkers are substrings providers put in the error body when the
// conversation has grown past the model's context window. Anthropic and the
// OpenAI-compatible routers it backs onto both surface one of these.
var contextLimitMarkers = []string{
	"context_length_exceeded", // Anthropic / OpenAI error.code
	"maximum context length",  // OpenAI plain-text message
	"prompt_too_long",         // Anthropic error.type variant
}

// IsContextLimit reports whether err is a context-length-exceeded style
// error: an HTTP 4xx whose body names context length, or the older "context
// window"-free provider error code. It is the signal to auto-compact.
func IsContextLimit(err error) bool {
	if err == nil {
		return false
	}
	if he, ok := errors.AsType[*HTTPError](err); ok {
		if strings.HasPrefix(he.Status, "400") || strings.HasPrefix(he.Status, "413") {
			b := strings.ToLower(he.Body)
			for _, m := range contextLimitMarkers {
				if strings.Contains(b, m) {
					return true
				}
			}
		}
		return false
	}
	s := strings.ToLower(err.Error())
	for _, m := range contextLimitMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// ModelInfo is one entry from the provider's GET /models list. Fields beyond
// the OpenAI spec (context_length, reasoning_efforts, pricing) are omitted
// by APIs that don't supply them.
type ModelInfo struct {
	ID                  string   `json:"id"`
	ContextLength       int      `json:"context_length,omitempty"`
	MaxCompletionTokens int      `json:"max_completion_tokens,omitempty"`
	ReasoningEfforts    []string `json:"reasoning_efforts,omitempty"`
	Pricing             *Pricing `json:"pricing,omitempty"`
	// InputModalities lists the input types the model accepts (OpenRouter
	// shape: ["text","image"]). Nil when the provider doesn't advertise it.
	InputModalities []string `json:"input_modalities,omitempty"`
}

// SupportsVision reports whether the model advertises image input.
func (mi ModelInfo) SupportsVision() bool {
	return slices.Contains(mi.InputModalities, "image")
}

// Pricing is the provider's per-token USD rates as decimal strings
// (inference.net / OpenRouter shape). Nil Pricing on ModelInfo means the
// provider doesn't advertise prices.
type Pricing struct {
	Prompt         string `json:"prompt"`
	Completion     string `json:"completion"`
	InputCacheRead string `json:"input_cache_read,omitempty"`
}

// Rates parses the decimal-string prices into floats (0 for missing or
// unparseable fields).
func (p Pricing) Rates() (in, out, cacheRead float64) {
	in, _ = strconv.ParseFloat(p.Prompt, 64)
	out, _ = strconv.ParseFloat(p.Completion, 64)
	cacheRead, _ = strconv.ParseFloat(p.InputCacheRead, 64)
	return in, out, cacheRead
}

// SessionCost returns the USD spend for cumulative usage u at per-token
// rates. Cached prompt tokens are billed at the cache-read rate when
// advertised, else at the full input rate (pi models.ts calculateCost has
// the same shape, plus a cache-write term OpenAI-compatible usage lacks).
func SessionCost(u Usage, in, out, cacheRead float64) float64 {
	cached := u.Cached()
	if cacheRead == 0 {
		cacheRead = in
	}
	return float64(u.PromptTokens-cached)*in +
		float64(cached)*cacheRead +
		float64(u.CompletionTokens)*out
}

// Models fetches GET /models from the provider.
func (c *OpenAI) Models(ctx context.Context) ([]ModelInfo, error) {
	hr, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.HTTP.Do(hr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &HTTPError{Status: resp.Status, Body: strings.TrimSpace(string(b))}
	}
	var list struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list.Data, nil
}

// Stream sends the request and invokes onText for each content delta and
// onThink for each reasoning_content delta (both may be nil). It returns the
// final assistant message (with any accumulated tool calls) plus the usage
// the provider reports on the terminal chunk (stream_options:include_usage).
//
// Transient failures (transport errors, 429, 5xx) are retried with backoff —
// but only until the first visible delta has been handed to onText/onThink.
// After that point a retry would replay text the caller already rendered, so
// the error is surfaced instead. A retry regenerates the whole assistant
// message server-side; nothing in the request messages is mutated by a failed
// attempt, so retrying is idempotent.
func (c *OpenAI) Stream(ctx context.Context, req Request, onText, onThink func(string)) (Message, Usage, error) {
	req.Stream = true
	req.StreamOptions = &struct {
		IncludeUsage bool `json:"include_usage"`
	}{IncludeUsage: true}
	req.Messages = repairToolHistory(stripAuthored(req.Messages))
	body, err := json.Marshal(req)
	if err != nil {
		return Message{}, Usage{}, err
	}
	var last error
	for attempt := 1; attempt <= c.attempts(); attempt++ {
		emitted := false // true once any visible delta reached the caller
		wrapText, wrapThink := onText, onThink
		if onText != nil {
			wrapText = func(s string) { emitted = true; onText(s) }
		}
		if onThink != nil {
			wrapThink = func(s string) { emitted = true; onThink(s) }
		}
		msg, usage, err := c.streamOnce(ctx, body, wrapText, wrapThink)
		if err == nil {
			return msg, usage, nil
		}
		last = err
		// Retry only transient failures the caller hasn't seen output from.
		if emitted || !retryable(err) || attempt == c.attempts() {
			break
		}
		delay := backoff(attempt)
		if c.OnRetry != nil {
			c.OnRetry(RetryEvent{Attempt: attempt, Max: c.attempts(), Delay: delay, Err: err})
		}
		if serr := sleep(ctx, delay); serr != nil {
			return Message{}, Usage{}, serr
		}
	}
	return Message{}, Usage{}, last
}

// streamOnce performs a single streaming request attempt; the Stream retry
// wrapper calls it per attempt and reads its own `emitted` flag (set by the
// wrapped callbacks) to decide whether a retry would replay visible output.
func (c *OpenAI) streamOnce(ctx context.Context, body []byte, onText, onThink func(string)) (Message, Usage, error) {
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, Usage{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.HTTP.Do(hr)
	if err != nil {
		return Message{}, Usage{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Message{}, Usage{}, &HTTPError{Status: resp.Status, Body: strings.TrimSpace(string(b))}
	}

	msg := Message{Role: "assistant"}
	var usage Usage      // from the terminal chunk (include_usage); zero if omitted
	var calls []ToolCall // indexed by stream tool_call index
	finish := ""
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ch chunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			continue
		}
		if ch.Error != nil {
			// The provider accepted the request (200) then failed mid-stream.
			// These are provider-logic errors (content filter, model faults),
			// not transport blips — surface them, don't retry.
			return Message{}, usage, nonRetryable{fmt.Errorf("api error: %s", ch.Error.Message)}
		}
		if ch.Usage != nil {
			usage = *ch.Usage // the terminal usage chunk carries empty choices
		}
		if len(ch.Choices) == 0 {
			continue
		}
		if fr := ch.Choices[0].FinishReason; fr != "" {
			finish = fr
		}
		d := ch.Choices[0].Delta
		if d.ReasoningContent != "" {
			if onThink != nil {
				onThink(d.ReasoningContent)
			}
		}
		if d.Content != "" {
			msg.Content += d.Content
			if onText != nil {
				onText(d.Content)
			}
		}
		for _, tc := range d.ToolCalls {
			for len(calls) <= tc.Index {
				calls = append(calls, ToolCall{Type: "function"})
			}
			cur := &calls[tc.Index]
			if tc.ID != "" {
				cur.ID = tc.ID
			}
			if tc.Function.Name != "" {
				cur.Function.Name += tc.Function.Name
			}
			cur.Function.Arguments += tc.Function.Arguments
		}
	}
	if err := sc.Err(); err != nil {
		return Message{}, usage, err
	}
	// Never execute tool calls from a max_tokens-truncated response: the
	// streamed JSON arguments may be silently incomplete.
	if finish == "length" && len(calls) > 0 {
		calls = nil
		msg.Content += "\n[response truncated by max_tokens; tool calls discarded]"
	}
	msg.ToolCalls = calls
	return msg, usage, nil
}

// Complete sends a non-streaming chat request and returns the assistant text
// content plus the reported usage. It's used internally by compaction's
// summary call, where streaming would just add UI noise for a one-shot
// synthesis.
func (c *OpenAI) Complete(ctx context.Context, req Request) (string, Usage, error) {
	req.Stream = false
	req.Messages = stripAuthored(req.Messages)
	body, err := json.Marshal(req)
	if err != nil {
		return "", Usage{}, err
	}
	var last error
	for attempt := 1; attempt <= c.attempts(); attempt++ {
		var text string
		var usage Usage
		text, usage, err = c.completeOnce(ctx, body)
		if err == nil {
			return text, usage, nil
		}
		last = err
		if !retryable(err) || attempt == c.attempts() {
			break
		}
		delay := backoff(attempt)
		if c.OnRetry != nil {
			c.OnRetry(RetryEvent{Attempt: attempt, Max: c.attempts(), Delay: delay, Err: err})
		}
		if serr := sleep(ctx, delay); serr != nil {
			return "", Usage{}, serr
		}
	}
	return "", Usage{}, last
}

// completeOnce performs one non-streaming request attempt.
func (c *OpenAI) completeOnce(ctx context.Context, body []byte) (string, Usage, error) {
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", Usage{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.HTTP.Do(hr)
	if err != nil {
		return "", Usage{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", Usage{}, &HTTPError{Status: resp.Status, Body: strings.TrimSpace(string(b))}
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *Usage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", Usage{}, err
	}
	if len(out.Choices) == 0 {
		return "", Usage{}, errors.New("no choices in completion response")
	}
	var usage Usage
	if out.Usage != nil {
		usage = *out.Usage
	}
	return out.Choices[0].Message.Content, usage, nil
}
