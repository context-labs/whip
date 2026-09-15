package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/context-labs/whip/internal/capability"
)

// OTLP export renders a session's spans as one ExportTraceServiceRequest in
// the OTLP/JSON encoding: int64 fields as decimal strings, enums as integers,
// ids as lowercase hex. Attributes carry the OpenTelemetry GenAI semantic
// conventions and the OpenInference keys the inference.net and HALO viewers
// promote, plus whip.* provenance. Bodies come from the rows spans point at,
// never from the bounded excerpts in span attrs, and every transcript message
// appears exactly once across the export: each LLM span's input is the delta
// since that agent's previous model call, its output the message it produced.

const (
	otlpSpanKindInternal = 1
	otlpSpanKindClient   = 3
	otlpStatusUnset      = 0
	otlpStatusOK         = 1
	otlpStatusError      = 2
)

type otlpValue struct {
	String *string  `json:"stringValue,omitempty"`
	Int    *string  `json:"intValue,omitempty"`
	Double *float64 `json:"doubleValue,omitempty"`
	Bool   *bool    `json:"boolValue,omitempty"`
}

type otlpAttribute struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpStatus struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type otlpLink struct {
	TraceID string `json:"traceId"`
	SpanID  string `json:"spanId"`
}

type otlpSpan struct {
	TraceID      string          `json:"traceId"`
	SpanID       string          `json:"spanId"`
	ParentSpanID string          `json:"parentSpanId,omitempty"`
	Name         string          `json:"name"`
	Kind         int             `json:"kind"`
	Start        string          `json:"startTimeUnixNano"`
	End          string          `json:"endTimeUnixNano"`
	Attributes   []otlpAttribute `json:"attributes"`
	Status       otlpStatus      `json:"status"`
	Links        []otlpLink      `json:"links,omitempty"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}

type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

// OTLPExport is the ExportTraceServiceRequest body.
type OTLPExport struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

// ExportOptions selects what to export. An empty TraceID exports every trace
// of the session; ServiceVersion names the exporting build.
type ExportOptions struct {
	TraceID        string
	ServiceVersion string
}

// ExportSummary counts what an export contained.
type ExportSummary struct {
	Spans  int `json:"spans"`
	Traces int `json:"traces"`
}

// SplitOTLP re-batches one export into requests of at most maxBytes each,
// keeping the resource and scope on every batch, for endpoints that cap the
// request body (HALO and inference.net accept 4 MiB).
func SplitOTLP(data []byte, maxBytes int) ([][]byte, error) {
	var export OTLPExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, err
	}
	if len(export.ResourceSpans) != 1 || len(export.ResourceSpans[0].ScopeSpans) != 1 {
		return nil, errors.New("export does not have the single resource and scope this encoder writes")
	}
	if len(data) <= maxBytes {
		return [][]byte{data}, nil
	}
	template := export.ResourceSpans[0]
	spans := template.ScopeSpans[0].Spans
	encode := func(batch []otlpSpan) ([]byte, error) {
		copyTemplate := template
		copyTemplate.ScopeSpans = []otlpScopeSpans{{Scope: template.ScopeSpans[0].Scope, Spans: batch}}
		return json.Marshal(OTLPExport{ResourceSpans: []otlpResourceSpans{copyTemplate}})
	}
	var batches [][]byte
	var batch []otlpSpan
	size := 0
	for _, span := range spans {
		encoded, err := json.Marshal(span)
		if err != nil {
			return nil, err
		}
		if len(batch) > 0 && size+len(encoded)+1024 > maxBytes {
			body, err := encode(batch)
			if err != nil {
				return nil, err
			}
			batches = append(batches, body)
			batch, size = nil, 0
		}
		batch = append(batch, span)
		size += len(encoded) + 1
	}
	if len(batch) > 0 {
		body, err := encode(batch)
		if err != nil {
			return nil, err
		}
		batches = append(batches, body)
	}
	return batches, nil
}

// ExportOTLP renders the root's spans as OTLP/JSON.
func (s *Store) ExportOTLP(ctx context.Context, rootID string, options ExportOptions) ([]byte, ExportSummary, error) {
	if rootID == "" {
		return nil, ExportSummary{}, errors.New("export requires a root")
	}
	spans, err := s.SpansForTrace(ctx, rootID, "")
	if err != nil {
		return nil, ExportSummary{}, err
	}
	var runtimeID string
	_ = s.db.QueryRowContext(ctx, `SELECT runtime_id FROM runtime_schema WHERE id=1`).Scan(&runtimeID)
	exporter := &otlpExporter{store: s, ctx: ctx, rootID: rootID, agents: map[string]agentIdentity{}, transcripts: map[string]*agentTranscript{}}
	if err := exporter.loadAgents(); err != nil {
		return nil, ExportSummary{}, err
	}
	exporter.assignDeltas(spans)
	var encoded []otlpSpan
	traces := map[string]bool{}
	for _, span := range spans {
		if options.TraceID != "" && span.TraceID != options.TraceID {
			continue
		}
		out, err := exporter.span(span)
		if err != nil {
			return nil, ExportSummary{}, err
		}
		encoded = append(encoded, out)
		traces[span.TraceID] = true
	}
	if encoded == nil {
		encoded = []otlpSpan{}
	}
	resource := []otlpAttribute{
		stringAttr("service.name", "whipcode"),
		stringAttr("whip.session.id", rootID),
	}
	if options.ServiceVersion != "" {
		resource = append(resource, stringAttr("service.version", options.ServiceVersion))
	}
	if runtimeID != "" {
		resource = append(resource, stringAttr("whip.runtime_id", runtimeID))
	}
	export := OTLPExport{ResourceSpans: []otlpResourceSpans{{
		Resource:   otlpResource{Attributes: resource},
		ScopeSpans: []otlpScopeSpans{{Scope: otlpScope{Name: "whip", Version: options.ServiceVersion}, Spans: encoded}},
	}}}
	data, err := json.Marshal(export)
	if err != nil {
		return nil, ExportSummary{}, err
	}
	return data, ExportSummary{Spans: len(encoded), Traces: len(traces)}, nil
}

type agentIdentity struct{ name, definition string }

// transcriptRow is one raw transcript message read generically, so image
// parts and provider-specific fields never break the export.
type transcriptRow struct {
	Seq        int
	Role       string
	Content    string
	Name       string
	ToolCallID string
	CallID     string
	ToolCalls  []otlpToolCall
}

type otlpToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type,omitempty"`
	Function otlpToolFunction `json:"function"`
}

type otlpToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// otlpMessage is the OpenAI-shaped message both viewers parse.
type otlpMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []otlpToolCall `json:"tool_calls,omitempty"`
}

type agentTranscript struct {
	rows       []transcriptRow
	byCallID   map[string]int // model call id -> index of the assistant row it produced
	toolResult map[string]int // tool call id -> index of the tool row
	toolArgs   map[string]string
}

type otlpExporter struct {
	store       *Store
	ctx         context.Context
	rootID      string
	agents      map[string]agentIdentity
	transcripts map[string]*agentTranscript
	// deltas maps an LLM span id to the transcript rows that are its input.
	deltas map[string][]transcriptRow
}

func (e *otlpExporter) loadAgents() error {
	rows, err := e.store.db.QueryContext(e.ctx, `SELECT id,name,definition FROM agents WHERE root_id=?`, e.rootID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var identity agentIdentity
		if err := rows.Scan(&id, &identity.name, &identity.definition); err != nil {
			return err
		}
		e.agents[id] = identity
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var definition string
	_ = e.store.db.QueryRowContext(e.ctx, `SELECT definition FROM sessions WHERE id=?`, e.rootID).Scan(&definition)
	if identity, ok := e.agents[e.rootID]; ok && identity.definition == "" {
		identity.definition = definition
		e.agents[e.rootID] = identity
	}
	return nil
}

func (e *otlpExporter) transcript(agentID string) (*agentTranscript, error) {
	if transcript, ok := e.transcripts[agentID]; ok {
		return transcript, nil
	}
	query, args := `SELECT seq,content FROM transcript_messages WHERE root_id=? AND agent_id=? ORDER BY seq`, []any{e.rootID, agentID}
	if agentID == e.rootID {
		query, args = `SELECT seq,content FROM messages WHERE session_id=? ORDER BY seq`, []any{e.rootID}
	}
	rows, err := e.store.db.QueryContext(e.ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	transcript := &agentTranscript{byCallID: map[string]int{}, toolResult: map[string]int{}, toolArgs: map[string]string{}}
	for rows.Next() {
		var seq int
		var raw []byte
		if err := rows.Scan(&seq, &raw); err != nil {
			return nil, err
		}
		row, err := decodeTranscriptRow(seq, raw)
		if err != nil {
			return nil, err
		}
		index := len(transcript.rows)
		transcript.rows = append(transcript.rows, row)
		if row.Role == "assistant" && row.CallID != "" {
			transcript.byCallID[row.CallID] = index
		}
		if row.Role == "tool" && row.ToolCallID != "" {
			transcript.toolResult[row.ToolCallID] = index
		}
		for _, call := range row.ToolCalls {
			transcript.toolArgs[call.ID] = call.Function.Arguments
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	e.transcripts[agentID] = transcript
	return transcript, nil
}

func decodeTranscriptRow(seq int, raw []byte) (transcriptRow, error) {
	var wire struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		Name       string          `json:"name"`
		ToolCallID string          `json:"tool_call_id"`
		CallID     string          `json:"call_id"`
		ToolCalls  []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return transcriptRow{}, fmt.Errorf("decode transcript row %d: %w", seq, err)
	}
	row := transcriptRow{Seq: seq, Role: wire.Role, Name: wire.Name, ToolCallID: wire.ToolCallID, CallID: wire.CallID, Content: flattenContent(wire.Content)}
	for _, call := range wire.ToolCalls {
		row.ToolCalls = append(row.ToolCalls, otlpToolCall{ID: call.ID, Type: call.Type, Function: otlpToolFunction{Name: call.Function.Name, Arguments: call.Function.Arguments}})
	}
	return row, nil
}

// flattenContent turns a string or a typed-parts array into text; image
// parts become a placeholder so the message keeps its place.
func flattenContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return string(raw)
	}
	var b strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			b.WriteString(part.Text)
		} else if part.Type != "" {
			b.WriteString("[" + part.Type + "]")
		}
	}
	return b.String()
}

// assignDeltas walks every agent's LLM spans in start order and gives each
// one the transcript rows written since the previous call that produced a
// message. A call that produced nothing leaves the delta for the next one.
func (e *otlpExporter) assignDeltas(spans []SpanRecord) {
	e.deltas = map[string][]transcriptRow{}
	byAgent := map[string][]SpanRecord{}
	for _, span := range spans {
		if span.Kind == SpanKindLLM {
			byAgent[span.AgentID] = append(byAgent[span.AgentID], span)
		}
	}
	for agentID, calls := range byAgent {
		transcript, err := e.transcript(agentID)
		if err != nil || transcript == nil {
			continue
		}
		sort.Slice(calls, func(i, j int) bool { return calls[i].StartNS < calls[j].StartNS || calls[i].StartNS == calls[j].StartNS && calls[i].ID < calls[j].ID })
		previous := -1 // index of the last assistant row already attributed
		for _, call := range calls {
			attrs := decodeAttrs(call.Attrs)
			callID, _ := attrs["model_call_id"].(string)
			index, produced := transcript.byCallID[callID]
			if !produced {
				continue
			}
			var delta []transcriptRow
			for i := previous + 1; i < index; i++ {
				delta = append(delta, transcript.rows[i])
			}
			e.deltas[call.ID] = delta
			previous = index
		}
	}
}

func decodeAttrs(raw json.RawMessage) map[string]any {
	attrs := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &attrs)
	}
	return attrs
}

func (e *otlpExporter) span(record SpanRecord) (otlpSpan, error) {
	attrs := decodeAttrs(record.Attrs)
	identity := e.agents[record.AgentID]
	out := otlpSpan{
		TraceID: record.TraceID, SpanID: record.ID, ParentSpanID: record.ParentID, Name: record.Name, Kind: otlpSpanKindInternal,
		Start: strconv.FormatInt(record.StartNS, 10), End: strconv.FormatInt(record.EndNS, 10),
	}
	out.Attributes = []otlpAttribute{
		stringAttr("session.id", e.rootID),
		stringAttr("agent.id", record.AgentID),
		stringAttr("agent.name", identity.name),
		stringAttr("gen_ai.agent.id", record.AgentID),
		stringAttr("gen_ai.agent.name", identity.name),
		stringAttr("whip.span.kind", record.Kind),
		stringAttr("whip.turn_id", record.TurnID),
		stringAttr("whip.agent.definition", identity.definition),
	}
	switch record.Status {
	case SpanStatusOK:
		out.Status = otlpStatus{Code: otlpStatusOK}
	case SpanStatusError, SpanStatusCancelled, SpanStatusInterrupted:
		message, _ := attrs["error"].(string)
		if message == "" {
			message = record.Status
		}
		out.Status = otlpStatus{Code: otlpStatusError, Message: message}
	default:
		out.Status = otlpStatus{Code: otlpStatusUnset}
	}
	if record.EndNS == 0 {
		// Decision 6: an open span exports zero-length and flagged; both
		// viewers draw a zero-length span as running.
		out.End = out.Start
		out.Attributes = append(out.Attributes, boolAttr("whip.span.in_progress", true))
	}
	if len(record.Links) > 0 {
		var links []SpanLink
		if json.Unmarshal(record.Links, &links) == nil {
			for _, link := range links {
				out.Links = append(out.Links, otlpLink{TraceID: link.TraceID, SpanID: link.SpanID})
			}
		}
	}
	var err error
	switch record.Kind {
	case SpanKindAgent:
		err = e.agentSpan(&out, record, attrs, identity)
	case SpanKindLLM:
		out.Kind = otlpSpanKindClient
		err = e.llmSpan(&out, record, attrs)
	case SpanKindTool:
		err = e.toolSpan(&out, record, attrs)
	case SpanKindHost:
		err = e.hostSpan(&out, record, attrs)
	case SpanKindWait:
		out.Attributes = append(out.Attributes, stringAttr("openinference.span.kind", "CHAIN"))
		for _, key := range []string{"permission_id", "question_id", "operation_id", "operation", "command", "rule", "path", "decision", "principal"} {
			if value, ok := attrs[key].(string); ok {
				out.Attributes = append(out.Attributes, stringAttr("whip.wait."+key, value))
			}
		}
		if input, ok := attrs["input"].(string); ok {
			out.Attributes = append(out.Attributes, stringAttr("input.value", input), stringAttr("input.mime_type", "text/plain"))
		}
		if output, ok := attrs["output"].(string); ok {
			out.Attributes = append(out.Attributes, stringAttr("output.value", output), stringAttr("output.mime_type", "text/plain"))
		}
	}
	out.Attributes = compactAttrs(out.Attributes)
	return out, err
}

// compactAttrs drops empty strings: an absent value is not the same as "".
func compactAttrs(attrs []otlpAttribute) []otlpAttribute {
	kept := attrs[:0]
	for _, attr := range attrs {
		if attr.Value.String != nil && *attr.Value.String == "" {
			continue
		}
		kept = append(kept, attr)
	}
	return kept
}

func (e *otlpExporter) agentSpan(out *otlpSpan, record SpanRecord, attrs map[string]any, identity agentIdentity) error {
	if identity.name != "" {
		out.Name = identity.name
	}
	out.Attributes = append(out.Attributes,
		stringAttr("openinference.span.kind", "AGENT"),
		stringAttr("gen_ai.operation.name", "invoke_agent"),
	)
	if trigger, ok := attrs["trigger"].(string); ok {
		out.Attributes = append(out.Attributes, stringAttr("whip.turn.trigger", trigger))
	}
	// The full submission comes from the inbox row the turn claimed.
	if seq, ok := attrs["inbox_seq"].(float64); ok && seq > 0 {
		var inline []byte
		var reference sql.NullString
		if err := e.store.db.QueryRowContext(e.ctx, `SELECT payload_inline,payload_ref FROM inbox WHERE root_id=? AND agent_id=? AND seq=?`, e.rootID, record.AgentID, int64(seq)).Scan(&inline, &reference); err == nil {
			if payload, err := e.store.readRuntimeValue(e.ctx, inline, reference); err == nil && len(payload) > 0 {
				out.Attributes = append(out.Attributes, stringAttr("input.value", string(payload)), stringAttr("input.mime_type", "text/plain"))
			}
		}
	} else if input, ok := attrs["input"].(string); ok {
		out.Attributes = append(out.Attributes, stringAttr("input.value", input), stringAttr("input.mime_type", "text/plain"))
	}
	// The visible result is the last message the turn's model calls produced.
	output := e.turnOutput(record)
	if output == "" {
		output, _ = attrs["output"].(string)
	}
	if output != "" {
		out.Attributes = append(out.Attributes, stringAttr("output.value", output), stringAttr("output.mime_type", "text/plain"))
	}
	return nil
}

// turnOutput finds the full text of the last assistant message an agent's
// turn produced, through the LLM spans of that turn.
func (e *otlpExporter) turnOutput(turn SpanRecord) string {
	transcript, err := e.transcript(turn.AgentID)
	if err != nil || transcript == nil {
		return ""
	}
	rows, err := e.store.db.QueryContext(e.ctx, `SELECT attrs FROM spans WHERE root_id=? AND turn_id=? AND agent_id=? AND kind='llm' ORDER BY start_ns DESC`, e.rootID, turn.TurnID, turn.AgentID)
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return ""
		}
		callID, _ := decodeAttrs(json.RawMessage(raw))["model_call_id"].(string)
		if index, ok := transcript.byCallID[callID]; ok && transcript.rows[index].Content != "" {
			return transcript.rows[index].Content
		}
	}
	return ""
}

func (e *otlpExporter) llmSpan(out *otlpSpan, record SpanRecord, attrs map[string]any) error {
	model, _ := attrs["model"].(string)
	provider, _ := attrs["provider"].(string)
	out.Attributes = append(out.Attributes,
		stringAttr("openinference.span.kind", "LLM"),
		stringAttr("gen_ai.operation.name", "chat"),
		stringAttr("gen_ai.request.model", model),
		stringAttr("gen_ai.provider.name", provider),
		stringAttr("llm.model_name", model),
		stringAttr("llm.provider", provider),
	)
	for key, otelKey := range map[string]string{"purpose": "whip.model_call.purpose", "model_call_id": "whip.model_call.id", "logical_id": "whip.model_call.logical_id", "cost_source": "whip.cost.source", "usage_source": "whip.usage.source"} {
		if value, ok := attrs[key].(string); ok {
			out.Attributes = append(out.Attributes, stringAttr(otelKey, value))
		}
	}
	if attempt, ok := attrs["attempt"].(float64); ok {
		out.Attributes = append(out.Attributes, intAttr("whip.model_call.attempt", int64(attempt)))
	}
	prompt := intAttrValue(attrs, "prompt_tokens")
	completion := intAttrValue(attrs, "completion_tokens")
	cached := intAttrValue(attrs, "cached_tokens")
	reasoning := intAttrValue(attrs, "reasoning_tokens")
	if usage, _ := attrs["usage_source"].(string); usage == "reported" || prompt > 0 || completion > 0 {
		out.Attributes = append(out.Attributes,
			intAttr("gen_ai.usage.input_tokens", prompt),
			intAttr("gen_ai.usage.output_tokens", completion),
			intAttr("gen_ai.usage.total_tokens", prompt+completion),
			intAttr("llm.token_count.prompt", prompt),
			intAttr("llm.token_count.completion", completion),
			intAttr("llm.token_count.total", prompt+completion),
		)
		if cached > 0 {
			out.Attributes = append(out.Attributes, intAttr("gen_ai.usage.cache_read.input_tokens", cached), intAttr("llm.token_count.prompt_details.cache_read", cached))
		}
		if reasoning > 0 {
			out.Attributes = append(out.Attributes, intAttr("llm.token_count.completion_details.reasoning", reasoning))
		}
	}
	// Decision 13: cost only when Whip knows it; a zero total would read as
	// "producer could not price this" downstream.
	if source, _ := attrs["cost_source"].(string); source == "reported" || source == "estimated" {
		out.Attributes = append(out.Attributes, doubleAttr("llm.cost.total", microsToUSD(intAttrValue(attrs, "cost_micros"))))
		if intAttrValue(attrs, "cost_input_micros")+intAttrValue(attrs, "cost_cache_read_micros")+intAttrValue(attrs, "cost_output_micros") > 0 {
			out.Attributes = append(out.Attributes,
				doubleAttr("llm.cost.prompt_details.input", microsToUSD(intAttrValue(attrs, "cost_input_micros"))),
				doubleAttr("llm.cost.prompt_details.cache_read", microsToUSD(intAttrValue(attrs, "cost_cache_read_micros"))),
				doubleAttr("llm.cost.completion_details.output", microsToUSD(intAttrValue(attrs, "cost_output_micros"))),
				stringAttr("whip.cost.split_source", "estimated"),
			)
		}
	}
	// Messages: the delta since the previous call as input, the produced row
	// as output, in both the indexed OpenInference form and the JSON form.
	inputs := make([]otlpMessage, 0, len(e.deltas[record.ID]))
	for _, row := range e.deltas[record.ID] {
		inputs = append(inputs, messageFromRow(row))
	}
	if len(inputs) > 0 {
		out.Attributes = append(out.Attributes, boolAttr("whip.input.delta", true), intAttr("whip.input.through_seq", int64(e.deltas[record.ID][len(inputs)-1].Seq)))
		out.Attributes = append(out.Attributes, indexedMessages("llm.input_messages", inputs)...)
		if encoded, err := json.Marshal(inputs); err == nil {
			out.Attributes = append(out.Attributes, stringAttr("gen_ai.input.messages", string(encoded)), stringAttr("input.value", string(encoded)), stringAttr("input.mime_type", "application/json"))
		}
	}
	callID, _ := attrs["model_call_id"].(string)
	if transcript, err := e.transcript(record.AgentID); err == nil && transcript != nil {
		if index, ok := transcript.byCallID[callID]; ok {
			output := messageFromRow(transcript.rows[index])
			outputs := []otlpMessage{output}
			out.Attributes = append(out.Attributes, intAttr("whip.output.seq", int64(transcript.rows[index].Seq)))
			out.Attributes = append(out.Attributes, indexedMessages("llm.output_messages", outputs)...)
			if encoded, err := json.Marshal(outputs); err == nil {
				out.Attributes = append(out.Attributes, stringAttr("gen_ai.output.messages", string(encoded)), stringAttr("output.value", string(encoded)), stringAttr("output.mime_type", "application/json"))
			}
			if len(output.ToolCalls) == 1 {
				out.Attributes = append(out.Attributes, stringAttr("tool_call.id", output.ToolCalls[0].ID))
			}
		}
	}
	return nil
}

func (e *otlpExporter) toolSpan(out *otlpSpan, record SpanRecord, attrs map[string]any) error {
	toolCallID, _ := attrs["tool_call_id"].(string)
	if summary, ok := attrs["summary"].(string); ok && summary != "" {
		out.Name = record.Name + ": " + summary
	}
	out.Attributes = append(out.Attributes,
		stringAttr("openinference.span.kind", "TOOL"),
		stringAttr("gen_ai.operation.name", "execute_tool"),
		stringAttr("tool.name", record.Name),
		stringAttr("gen_ai.tool.name", record.Name),
		stringAttr("tool_call.id", toolCallID),
		stringAttr("gen_ai.tool.call.id", toolCallID),
	)
	for key, otelKey := range map[string]string{"emitting_call_id": "whip.emitting_call_id", "execution_engine": "whip.execution_engine"} {
		if value, ok := attrs[key].(string); ok {
			out.Attributes = append(out.Attributes, stringAttr(otelKey, value))
		}
	}
	input, _ := attrs["input"].(string)
	output, _ := attrs["output"].(string)
	if transcript, err := e.transcript(record.AgentID); err == nil && transcript != nil {
		if arguments, ok := transcript.toolArgs[toolCallID]; ok {
			input = arguments
		}
		if index, ok := transcript.toolResult[toolCallID]; ok {
			output = transcript.rows[index].Content
		}
	}
	if input != "" {
		out.Attributes = append(out.Attributes, stringAttr("input.value", input), stringAttr("input.mime_type", "application/json"), stringAttr("gen_ai.tool.arguments", input))
	}
	if output != "" {
		out.Attributes = append(out.Attributes, stringAttr("output.value", output), stringAttr("output.mime_type", "text/plain"), stringAttr("gen_ai.tool.result", output))
	}
	return nil
}

func (e *otlpExporter) hostSpan(out *otlpSpan, record SpanRecord, attrs map[string]any) error {
	toolCallID, _ := attrs["tool_call_id"].(string)
	summary, _ := attrs["summary"].(string)
	if summary != "" {
		out.Name = record.Name + ": " + summary
	}
	out.Attributes = append(out.Attributes,
		stringAttr("openinference.span.kind", "TOOL"),
		stringAttr("gen_ai.operation.name", "execute_tool"),
		stringAttr("tool.name", record.Name),
		stringAttr("gen_ai.tool.name", record.Name),
		stringAttr("tool_call.id", toolCallID),
		stringAttr("gen_ai.tool.call.id", toolCallID),
	)
	for key, otelKey := range map[string]string{"invocation_id": "whip.invocation_id", "operation_id": "whip.operation_id"} {
		if value, ok := attrs[key].(string); ok {
			out.Attributes = append(out.Attributes, stringAttr(otelKey, value))
		}
	}
	if duration, ok := attrs["duration_ms"].(float64); ok {
		out.Attributes = append(out.Attributes, intAttr("whip.duration_ms", int64(duration)))
	}
	input, output := summary, ""
	inputMime := "text/plain"
	if operationID, ok := attrs["operation_id"].(string); ok && operationID != "" {
		arguments, result, err := e.operationBodies(operationID)
		if err == nil {
			if arguments != "" {
				input, inputMime = arguments, "application/json"
			}
			output = result
		}
	}
	if input != "" {
		out.Attributes = append(out.Attributes, stringAttr("input.value", input), stringAttr("input.mime_type", inputMime), stringAttr("gen_ai.tool.arguments", input))
	}
	if errorText, ok := attrs["error"].(string); ok && errorText != "" && output == "" {
		output = errorText
	}
	if output != "" {
		out.Attributes = append(out.Attributes, stringAttr("output.value", output), stringAttr("output.mime_type", "text/plain"), stringAttr("gen_ai.tool.result", output))
	}
	return nil
}

// operationBodies reads the full arguments and result of an admitted host
// operation.
func (e *otlpExporter) operationBodies(operationID string) (arguments, result string, err error) {
	var payloadInline, resultInline []byte
	var payloadRef, resultRef sql.NullString
	if err := e.store.db.QueryRowContext(e.ctx, `SELECT payload_inline,payload_ref,result_inline,result_ref FROM operations WHERE root_id=? AND id=?`, e.rootID, operationID).Scan(&payloadInline, &payloadRef, &resultInline, &resultRef); err != nil {
		return "", "", err
	}
	payload, err := e.store.readRuntimeValue(e.ctx, payloadInline, payloadRef)
	if err != nil {
		return "", "", err
	}
	var admission capability.Admission
	if json.Unmarshal(payload, &admission) == nil && len(admission.Request.Arguments) > 0 {
		arguments = string(admission.Request.Arguments)
	}
	if resultInline != nil || resultRef.Valid {
		raw, err := e.store.readRuntimeValue(e.ctx, resultInline, resultRef)
		if err == nil {
			var settled struct {
				Output string `json:"output"`
				Error  string `json:"error"`
			}
			if json.Unmarshal(raw, &settled) == nil {
				result = settled.Output
				if result == "" {
					result = settled.Error
				}
			} else {
				result = string(raw)
			}
		}
	}
	return arguments, result, nil
}

func messageFromRow(row transcriptRow) otlpMessage {
	return otlpMessage{Role: row.Role, Content: row.Content, Name: row.Name, ToolCallID: row.ToolCallID, ToolCalls: row.ToolCalls}
}

// indexedMessages is the OpenInference flat form:
// llm.input_messages.0.message.role, .content, .tool_call_id,
// .tool_calls.0.tool_call.id / .function.name / .function.arguments.
func indexedMessages(prefix string, messages []otlpMessage) []otlpAttribute {
	var attrs []otlpAttribute
	for i, message := range messages {
		base := fmt.Sprintf("%s.%d.message.", prefix, i)
		attrs = append(attrs, stringAttr(base+"role", message.Role))
		if message.Content != "" {
			attrs = append(attrs, stringAttr(base+"content", message.Content))
		}
		if message.ToolCallID != "" {
			attrs = append(attrs, stringAttr(base+"tool_call_id", message.ToolCallID))
		}
		for j, call := range message.ToolCalls {
			callBase := fmt.Sprintf("%stool_calls.%d.tool_call.", base, j)
			attrs = append(attrs,
				stringAttr(callBase+"id", call.ID),
				stringAttr(callBase+"function.name", call.Function.Name),
				stringAttr(callBase+"function.arguments", call.Function.Arguments),
			)
		}
	}
	return attrs
}

func intAttrValue(attrs map[string]any, key string) int64 {
	switch value := attrs[key].(type) {
	case float64:
		return int64(value)
	case string:
		parsed, _ := strconv.ParseInt(value, 10, 64)
		return parsed
	}
	return 0
}

func microsToUSD(micros int64) float64 { return float64(micros) / 1e6 }

func stringAttr(key, value string) otlpAttribute {
	return otlpAttribute{Key: key, Value: otlpValue{String: &value}}
}

func intAttr(key string, value int64) otlpAttribute {
	text := strconv.FormatInt(value, 10)
	return otlpAttribute{Key: key, Value: otlpValue{Int: &text}}
}

func doubleAttr(key string, value float64) otlpAttribute {
	return otlpAttribute{Key: key, Value: otlpValue{Double: &value}}
}

func boolAttr(key string, value bool) otlpAttribute {
	return otlpAttribute{Key: key, Value: otlpValue{Bool: &value}}
}
