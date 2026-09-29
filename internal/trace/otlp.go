// Package trace encodes canonical execution projections as bounded OTLP/JSON.
// It owns no execution state and never sends telemetry to an external service.
package trace

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/context-labs/whip/internal/session"
)

var ErrLimit = errors.New("trace export exceeds its count or byte bound")

type value struct {
	String *string  `json:"stringValue,omitempty"`
	Int    *string  `json:"intValue,omitempty"`
	Bool   *bool    `json:"boolValue,omitempty"`
	Double *float64 `json:"doubleValue,omitempty"`
}
type (
	attribute struct {
		Key   string `json:"key"`
		Value value  `json:"value"`
	}
	status struct {
		Code int `json:"code"`
	}
	span struct {
		TraceID    string      `json:"traceId"`
		SpanID     string      `json:"spanId"`
		ParentID   *string     `json:"parentSpanId,omitempty"`
		Name       string      `json:"name"`
		Kind       int         `json:"kind"`
		Start      string      `json:"startTimeUnixNano"`
		End        string      `json:"endTimeUnixNano"`
		Attributes []attribute `json:"attributes"`
		Status     status      `json:"status"`
	}
)

// Export retains at most MaxContentBytes. Each Add checks encoded size before
// append. Open spans have zero exported duration and an explicit in-progress flag.
type Export struct {
	data   bytes.Buffer
	traces map[string]struct{}
	count  int
}

func NewExport(root session.SessionID, revision int64) *Export {
	result := &Export{traces: map[string]struct{}{}}
	// Values are encoded, never interpolated into JSON.
	attrs, _ := json.Marshal([]attribute{
		{Key: "service.name", Value: value{String: new("whip")}},
		{Key: "whip.root.id", Value: value{String: new(string(root))}},
		{Key: "whip.trace.revision", Value: value{Int: new(strconv.FormatInt(revision, 10))}},
	})
	result.data.WriteString(`{"resourceSpans":[{"resource":{"attributes":`)
	result.data.Write(attrs)
	result.data.WriteString(`},"scopeSpans":[{"scope":{"name":"whip.runtime"},"spans":[`)
	return result
}

const suffix = `]}]}]}`

func (e *Export) Add(row session.TraceRow, input, output *string) error {
	if row.Span == nil {
		return nil
	}
	if e.count >= 4096 {
		return ErrLimit
	}
	source := row.Span
	item := span{TraceID: source.TraceID, SpanID: row.SpanID, ParentID: source.ParentSpanID, Name: source.Name, Kind: 1, Start: strconv.FormatInt(source.StartNS, 10), End: strconv.FormatInt(source.StartNS, 10), Attributes: []attribute{}}
	if source.Kind == "llm" {
		item.Kind = 3
	}
	if source.EndNS != nil {
		item.End = strconv.FormatInt(*source.EndNS, 10)
		switch source.State {
		case "succeeded", "approved", "answered":
			item.Status.Code = 1
		default:
			item.Status.Code = 2
		}
	}
	addText := func(key, text string) {
		item.Attributes = append(item.Attributes, attribute{Key: key, Value: value{String: new(text)}})
	}
	addText("whip.root.id", string(row.RootID))
	addText("whip.session.id", string(row.SessionID))
	addText("whip.turn.id", string(row.TurnID))
	addText("whip.source.kind", row.SourceKind)
	addText("whip.source.id", row.SourceID)
	addText("whip.state", source.State)
	kind := "CHAIN"
	switch source.Kind {
	case "agent":
		kind = "AGENT"
	case "llm":
		kind = "LLM"
	case "tool", "host":
		kind = "TOOL"
	}
	addText("openinference.span.kind", kind)
	item.Attributes = append(item.Attributes, attribute{Key: "whip.span.in_progress", Value: value{Bool: new(source.EndNS == nil)}})
	var inputTokens, outputTokens *int64
	for _, a := range source.Attributes {
		v := value{String: a.Text, Bool: a.Flag}
		if a.Count != nil {
			v.Int = new(strconv.FormatInt(*a.Count, 10))
		}
		item.Attributes = append(item.Attributes, attribute{Key: a.Key, Value: v})
		alias := ""
		switch a.Key {
		case "gen_ai.request.model":
			alias = "llm.model_name"
		case "gen_ai.provider.name":
			alias = "llm.provider"
		case "gen_ai.usage.input_tokens":
			alias = "llm.token_count.prompt"
			inputTokens = a.Count
		case "gen_ai.usage.output_tokens":
			alias = "llm.token_count.completion"
			outputTokens = a.Count
		case "gen_ai.usage.cache_read.input_tokens":
			alias = "llm.token_count.prompt_details.cache_read"
		case "gen_ai.usage.reasoning_tokens":
			alias = "llm.token_count.completion_details.reasoning"
		case "whip.cost.nano_usd":
			if a.Count != nil {
				item.Attributes = append(item.Attributes, attribute{Key: "llm.cost.total", Value: value{Double: new(float64(*a.Count) / 1e9)}})
			}
		}
		if alias != "" {
			item.Attributes = append(item.Attributes, attribute{Key: alias, Value: v})
		}
	}
	if inputTokens != nil && outputTokens != nil && *inputTokens <= math.MaxInt64-*outputTokens {
		item.Attributes = append(item.Attributes, attribute{Key: "llm.token_count.total", Value: value{Int: new(strconv.FormatInt(*inputTokens+*outputTokens, 10))}})
	}
	if input != nil {
		addText("input.value", *input)
		addText("input.mime_type", "application/json")
	}
	if output != nil {
		addText("output.value", *output)
		addText("output.mime_type", "application/json")
		if row.SourceKind == "attempt" || row.SourceKind == "cell" {
			addText("whip.output.encoding", "session.parts")
		}
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return err
	}
	separator := 0
	if e.count > 0 {
		separator = 1
	}
	if e.data.Len()+len(raw)+separator+len(suffix) > session.MaxContentBytes {
		return ErrLimit
	}
	if separator != 0 {
		e.data.WriteByte(',')
	}
	e.data.Write(raw)
	e.count++
	e.traces[source.TraceID] = struct{}{}
	return nil
}

// Bytes returns a complete independent body; no wall-clock time enters exports.
func (e *Export) Bytes() []byte {
	result := make([]byte, 0, e.data.Len()+len(suffix))
	result = append(result, e.data.Bytes()...)
	return append(result, suffix...)
}
func (e *Export) Counts() (int, int) { return e.count, len(e.traces) }
