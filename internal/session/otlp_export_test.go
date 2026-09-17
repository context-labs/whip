package session

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

type exportedSpan struct {
	TraceID      string `json:"traceId"`
	SpanID       string `json:"spanId"`
	ParentSpanID string `json:"parentSpanId"`
	Name         string `json:"name"`
	Kind         int    `json:"kind"`
	Start        string `json:"startTimeUnixNano"`
	End          string `json:"endTimeUnixNano"`
	Attributes   []struct {
		Key   string `json:"key"`
		Value struct {
			String *string  `json:"stringValue"`
			Int    *string  `json:"intValue"`
			Double *float64 `json:"doubleValue"`
			Bool   *bool    `json:"boolValue"`
		} `json:"value"`
	} `json:"attributes"`
	Status struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
}

func (s exportedSpan) attr(key string) (string, bool) {
	for _, attr := range s.Attributes {
		if attr.Key != key {
			continue
		}
		switch {
		case attr.Value.String != nil:
			return *attr.Value.String, true
		case attr.Value.Int != nil:
			return *attr.Value.Int, true
		case attr.Value.Double != nil:
			return strings.TrimRight(strings.TrimRight(json.Number(formatFloat(*attr.Value.Double)).String(), "0"), "."), true
		case attr.Value.Bool != nil:
			if *attr.Value.Bool {
				return "true", true
			}
			return "false", true
		}
	}
	return "", false
}

func formatFloat(value float64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func decodeExport(t *testing.T, data []byte) []exportedSpan {
	t.Helper()
	var export struct {
		ResourceSpans []struct {
			Resource struct {
				Attributes []struct {
					Key string `json:"key"`
				} `json:"attributes"`
			} `json:"resource"`
			ScopeSpans []struct {
				Spans []exportedSpan `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal(data, &export); err != nil {
		t.Fatal(err)
	}
	if len(export.ResourceSpans) != 1 || len(export.ResourceSpans[0].ScopeSpans) != 1 {
		t.Fatalf("export shape: %s", data)
	}
	keys := map[string]bool{}
	for _, attr := range export.ResourceSpans[0].Resource.Attributes {
		keys[attr.Key] = true
	}
	if !keys["service.name"] || !keys["whip.session.id"] {
		t.Fatalf("resource attributes missing service identity: %v", keys)
	}
	return export.ResourceSpans[0].ScopeSpans[0].Spans
}

func insertTranscriptRow(t *testing.T, store *Store, root string, seq int, row map[string]any) {
	t.Helper()
	body, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, store, `INSERT INTO messages(session_id,seq,role,content) VALUES(?,?,?,?)`, root, seq, row["role"], string(body))
}

func TestExportOTLPCarriesEveryMessageOnceAndKeepsCostHonest(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	turnID, seq := startRootTurnForTest(t, store, root, agent, "fix it")
	trace := TraceIDForTurn(turnID)
	turnSpan := TurnSpanID(root, agent, turnID)
	base := time.Now().UnixNano()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	llmSpan := func(callID string, start int64, extra map[string]any) SpanRecord {
		attrs := map[string]any{"model": "kimi-k3-fast", "provider": "inference-net", "purpose": "turn", "model_call_id": callID, "logical_id": "L-" + callID, "attempt": 1}
		maps.Copy(attrs, extra)
		return SpanRecord{ID: ModelCallSpanID(root, callID), TraceID: trace, ParentID: turnSpan, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindLLM, Name: "inference-net/kimi-k3-fast", StartNS: start, Attrs: SpanAttrs(attrs)}
	}
	// First call: priced from catalog rates, emits a cell that reads a file.
	first := llmSpan("c1", base, nil)
	must(store.RecordSpanStart(context.Background(), first))
	first.EndNS = base + 2_000_000
	first.Attrs = SpanAttrs(map[string]any{"prompt_tokens": 12, "completion_tokens": 3, "cached_tokens": 2, "cost_micros": int64(40), "cost_input_micros": int64(10), "cost_cache_read_micros": int64(5), "cost_output_micros": int64(25), "cost_source": "estimated", "usage_source": "reported"})
	must(store.RecordSpanEnd(context.Background(), first))
	cell := SpanRecord{ID: ToolSpanID(root, agent, turnID, "tc1"), TraceID: trace, ParentID: turnSpan, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindTool, Name: "rlm_exec", StartNS: base + 3_000_000, Attrs: SpanAttrs(map[string]any{"tool_call_id": "tc1", "summary": "print(files.read(\"README.md\"))", "input": "{\"code\":\"…\"}"})}
	must(store.RecordSpanStart(context.Background(), cell))
	exec(t, store, `INSERT INTO operations(id,root_id,agent_id,status,payload_inline,result_inline,created_at,updated_at) VALUES(?,?,?,'succeeded',?,?,?,?)`,
		"op-1", root, agent, `{"request":{"operation":"read","arguments":{"path":"README.md"}}}`, `{"output":"# Whip","error":""}`, now(), now())
	host := SpanRecord{ID: HostSpanID(root, agent, turnID, "tc1", "1:1"), TraceID: trace, ParentID: cell.ID, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindHost, Name: "files.read", StartNS: base + 3_100_000, Attrs: SpanAttrs(map[string]any{"tool_call_id": "tc1", "invocation_id": "1:1", "summary": "path=README.md"})}
	must(store.RecordSpanStart(context.Background(), host))
	host.EndNS, host.Attrs = base+3_141_000, SpanAttrs(map[string]any{"operation_id": "op-1", "duration_ms": int64(41)})
	must(store.RecordSpanEnd(context.Background(), host))
	cell.EndNS = base + 3_500_000
	must(store.RecordSpanEnd(context.Background(), cell))
	// Second call: the provider reported nothing about cost, so no cost keys.
	second := llmSpan("c2", base+4_000_000, nil)
	must(store.RecordSpanStart(context.Background(), second))
	second.EndNS = base + 6_000_000
	second.Attrs = SpanAttrs(map[string]any{"prompt_tokens": 40, "completion_tokens": 5, "cost_micros": int64(0), "cost_source": "unknown", "usage_source": "reported"})
	must(store.RecordSpanEnd(context.Background(), second))
	// Third call is still running when the export happens.
	must(store.RecordSpanStart(context.Background(), llmSpan("c3", base+7_000_000, nil)))
	// A permission wait sits under the turn as a CHAIN span with its prompt.
	wait := SpanRecord{ID: WaitSpanID(root, "perm-1"), TraceID: trace, ParentID: turnSpan, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindWait, Name: "permission: bash", StartNS: base + 3_200_000, Attrs: SpanAttrs(map[string]any{"permission_id": "perm-1", "operation": "bash", "command": "rm -rf build", "input": "bash rm -rf build"})}
	must(store.RecordSpanStart(context.Background(), wait))
	wait.EndNS, wait.Status, wait.Attrs = base+3_300_000, SpanStatusOK, SpanAttrs(map[string]any{"decision": "approved", "principal": "human", "output": "approved"})
	must(store.RecordSpanEnd(context.Background(), wait))

	// A multimodal user message flattens to text with a placeholder per image part.
	insertTranscriptRow(t, store, root, 1, map[string]any{"role": "user", "content": []map[string]any{{"type": "text", "text": "fix it"}, {"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAAA"}}}})
	insertTranscriptRow(t, store, root, 2, map[string]any{"role": "assistant", "content": "", "call_id": "c1", "tool_calls": []map[string]any{{"id": "tc1", "type": "function", "function": map[string]any{"name": "rlm_exec", "arguments": `{"code":"print(files.read(\"README.md\"))"}`}}}})
	insertTranscriptRow(t, store, root, 3, map[string]any{"role": "tool", "tool_call_id": "tc1", "name": "rlm_exec", "content": "# Whip"})
	insertTranscriptRow(t, store, root, 4, map[string]any{"role": "assistant", "content": "Done.", "call_id": "c2"})
	must(store.CommitRootTurn(context.Background(), RootTurnCommit{RootID: root, AgentID: agent, InboxSeq: seq, Messages: []llm.Message{{Role: "assistant", Content: "Done."}}, Model: "kimi-k3-fast", Provider: "inference-net"}))

	data, summary, err := store.ExportOTLP(context.Background(), root, ExportOptions{ServiceVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	// WHIP_OTLP_DUMP=<path> keeps the document for manual oracle runs against
	// HALO desktop or inference.net; the assertions below are the automated part.
	if path := os.Getenv("WHIP_OTLP_DUMP"); path != "" {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if summary.Spans != 7 || summary.Traces != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	spans := decodeExport(t, data)
	byID := map[string]exportedSpan{}
	for _, span := range spans {
		byID[span.SpanID] = span
		if len(span.TraceID) != 32 || len(span.SpanID) != 16 || span.Start == "" || span.Start == "0" {
			t.Fatalf("span identity/timing invalid: %+v", span)
		}
		if session, ok := span.attr("session.id"); !ok || session != root {
			t.Fatalf("span %s lacks session.id", span.Name)
		}
		kind, _ := span.attr("whip.span.kind")
		if _, hasUsage := span.attr("gen_ai.usage.input_tokens"); hasUsage && kind != SpanKindLLM {
			t.Fatalf("usage on a non-LLM span %s", span.Name)
		}
	}
	turn := byID[turnSpan]
	if turn.ParentSpanID != "" || turn.Status.Code != otlpStatusOK {
		t.Fatalf("turn span=%+v", turn)
	}
	if kind, _ := turn.attr("openinference.span.kind"); kind != "AGENT" {
		t.Fatalf("turn kind=%q", kind)
	}
	if input, _ := turn.attr("input.value"); input != "fix it" {
		t.Fatalf("turn input=%q", input)
	}
	waitOut := byID[WaitSpanID(root, "perm-1")]
	if kind, _ := waitOut.attr("openinference.span.kind"); kind != "CHAIN" || waitOut.ParentSpanID != turnSpan {
		t.Fatalf("wait span=%+v", waitOut)
	}
	for key, want := range map[string]string{"whip.wait.operation": "bash", "whip.wait.decision": "approved", "input.value": "bash rm -rf build", "output.value": "approved"} {
		if got, _ := waitOut.attr(key); got != want {
			t.Fatalf("wait %s=%q want %q", key, got, want)
		}
	}
	if output, _ := turn.attr("output.value"); output != "Done." {
		t.Fatalf("turn output=%q", output)
	}

	c1 := byID[ModelCallSpanID(root, "c1")]
	if c1.Kind != otlpSpanKindClient {
		t.Fatalf("llm span kind=%d", c1.Kind)
	}
	for key, want := range map[string]string{
		"llm.model_name": "kimi-k3-fast", "gen_ai.request.model": "kimi-k3-fast", "llm.provider": "inference-net",
		"llm.token_count.prompt": "12", "llm.token_count.completion": "3", "llm.token_count.prompt_details.cache_read": "2",
		"gen_ai.usage.input_tokens": "12", "gen_ai.usage.output_tokens": "3",
		"llm.cost.total": "0.00004", "llm.cost.prompt_details.input": "0.00001", "llm.cost.prompt_details.cache_read": "0.000005", "llm.cost.completion_details.output": "0.000025",
		"whip.cost.source": "estimated", "whip.cost.split_source": "estimated",
		"llm.input_messages.0.message.role": "user", "llm.input_messages.0.message.content": "fix it[image_url]",
		"llm.output_messages.0.message.role": "assistant", "llm.output_messages.0.message.tool_calls.0.tool_call.id": "tc1",
		"llm.output_messages.0.message.tool_calls.0.tool_call.function.name": "rlm_exec",
		"tool_call.id": "tc1", "whip.input.delta": "true",
	} {
		if got, _ := c1.attr(key); got != want {
			t.Fatalf("c1 %s=%q want %q", key, got, want)
		}
	}
	c2 := byID[ModelCallSpanID(root, "c2")]
	if _, has := c2.attr("llm.cost.total"); has {
		t.Fatal("unknown cost must not export as zero")
	}
	if role, _ := c2.attr("llm.input_messages.0.message.role"); role != "tool" {
		t.Fatalf("c2 input delta should be the tool result, got %q", role)
	}
	if _, repeated := c2.attr("llm.input_messages.1.message.role"); repeated {
		t.Fatal("c2 repeated history instead of the delta")
	}
	if content, _ := c2.attr("llm.output_messages.0.message.content"); content != "Done." {
		t.Fatalf("c2 output=%q", content)
	}
	c3 := byID[ModelCallSpanID(root, "c3")]
	if c3.End != c3.Start || c3.Status.Code != otlpStatusUnset {
		t.Fatalf("open span must export zero-length and unset: %+v", c3)
	}
	if flag, _ := c3.attr("whip.span.in_progress"); flag != "true" {
		t.Fatal("open span missing in-progress flag")
	}

	tool := byID[cell.ID]
	if tool.Name != `rlm_exec: print(files.read("README.md"))` || tool.ParentSpanID != turnSpan {
		t.Fatalf("tool span=%+v", tool)
	}
	if input, _ := tool.attr("input.value"); input != `{"code":"print(files.read(\"README.md\"))"}` {
		t.Fatalf("tool input=%q", input)
	}
	if output, _ := tool.attr("output.value"); output != "# Whip" {
		t.Fatalf("tool output=%q", output)
	}
	hostOut := byID[host.ID]
	if hostOut.Name != "files.read: path=README.md" || hostOut.ParentSpanID != cell.ID {
		t.Fatalf("host span=%+v", hostOut)
	}
	if input, _ := hostOut.attr("input.value"); input != `{"path":"README.md"}` {
		t.Fatalf("host input=%q", input)
	}
	if output, _ := hostOut.attr("output.value"); output != "# Whip" {
		t.Fatalf("host output=%q", output)
	}
	if kind, _ := hostOut.attr("openinference.span.kind"); kind != "TOOL" {
		t.Fatalf("host kind=%q", kind)
	}

	// Every transcript message appears exactly once across LLM inputs and outputs.
	occurrences := map[string]int{}
	for _, span := range spans {
		for _, attr := range span.Attributes {
			if strings.HasPrefix(attr.Key, "llm.input_messages.") && strings.HasSuffix(attr.Key, ".message.role") || strings.HasPrefix(attr.Key, "llm.output_messages.") && strings.HasSuffix(attr.Key, ".message.role") {
				occurrences[span.SpanID+":"+attr.Key]++
			}
		}
	}
	total := 0
	for _, count := range occurrences {
		total += count
	}
	if total != 4 {
		t.Fatalf("expected the 4 transcript messages exactly once each, saw %d message slots: %v", total, occurrences)
	}

	// A single-trace export selects only that trace; splitting keeps each
	// batch a complete request.
	single, single_summary, err := store.ExportOTLP(context.Background(), root, ExportOptions{TraceID: trace})
	if err != nil || single_summary.Spans != 7 {
		t.Fatalf("single trace export summary=%+v err=%v", single_summary, err)
	}
	if _, _, err := store.ExportOTLP(context.Background(), root, ExportOptions{TraceID: "nope"}); err != nil {
		t.Fatal(err)
	}
	batches, err := SplitOTLP(single, 2048)
	if err != nil || len(batches) < 2 {
		t.Fatalf("split batches=%d err=%v", len(batches), err)
	}
	seen := 0
	for _, batch := range batches {
		batchSpans := decodeExport(t, batch)
		// A batch only exceeds the cap when one span alone is bigger than it.
		if len(batchSpans) > 1 && len(batch) > 2048 {
			t.Fatalf("batch of %d spans and %d bytes exceeds the cap", len(batchSpans), len(batch))
		}
		seen += len(batchSpans)
	}
	if seen != 7 {
		t.Fatalf("split lost spans: %d", seen)
	}
}

// A failed child turn, a tool that errored before it produced output, host
// operations whose results are an error or plain text, and a transcript row
// that no longer decodes must all still export: the failure lands on the
// span status and the export never aborts over one bad row.
func TestExportOTLPMapsFailuresLinksAndFallbacks(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	turnID, _ := startRootTurnForTest(t, store, root, agent, "fix it")
	trace := TraceIDForTurn(turnID)
	turnSpan := TurnSpanID(root, agent, turnID)
	base := time.Now().UnixNano()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	child := SpanRecord{
		ID: TurnSpanID(root, "child", "t-child"), TraceID: trace, ParentID: turnSpan, RootID: root, AgentID: "child", TurnID: "t-child", Kind: SpanKindAgent, Name: "child", StartNS: base,
		Attrs: SpanAttrs(map[string]any{"input": "sub task", "output": "gave up", "trigger": "spawn"}), Links: spanLinksJSON([]SpanLink{{TraceID: trace, SpanID: turnSpan}}),
	}
	must(store.RecordSpanStart(ctx, child))
	child.EndNS, child.Status = base+1_000, SpanStatusInterrupted
	must(store.RecordSpanEnd(ctx, child))
	call := SpanRecord{
		ID: ModelCallSpanID(root, "c9"), TraceID: trace, ParentID: turnSpan, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindLLM, Name: "inference-net/kimi-k3-fast", StartNS: base + 2_000,
		Attrs: SpanAttrs(map[string]any{"model": "kimi-k3-fast", "provider": "inference-net", "model_call_id": "c9"}),
	}
	must(store.RecordSpanStart(ctx, call))
	call.EndNS, call.Status = base+3_000, SpanStatusError
	call.Attrs = SpanAttrs(map[string]any{"prompt_tokens": "7", "completion_tokens": 2, "reasoning_tokens": 1, "error": "provider closed the stream", "usage_source": "reported", "cost_source": "unknown"})
	must(store.RecordSpanEnd(ctx, call))
	tool := SpanRecord{
		ID: ToolSpanID(root, agent, turnID, "tc9"), TraceID: trace, ParentID: turnSpan, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindTool, Name: "rlm_exec", StartNS: base + 4_000,
		Attrs: SpanAttrs(map[string]any{"tool_call_id": "tc9", "emitting_call_id": "c9", "execution_engine": "quickjs"}),
	}
	must(store.RecordSpanStart(ctx, tool))
	tool.EndNS, tool.Status, tool.Attrs = base+5_000, SpanStatusCancelled, SpanAttrs(map[string]any{"error": "cell cancelled"})
	must(store.RecordSpanEnd(ctx, tool))
	exec(t, store, `INSERT INTO operations(id,root_id,agent_id,status,payload_inline,result_inline,created_at,updated_at) VALUES(?,?,?,'failed',?,?,?,?)`,
		"op-denied", root, agent, `{"request":{"operation":"bash","arguments":{"command":"rm -rf build"}}}`, `{"output":"","error":"denied by policy"}`, now(), now())
	exec(t, store, `INSERT INTO operations(id,root_id,agent_id,status,payload_inline,result_inline,created_at,updated_at) VALUES(?,?,?,'succeeded',?,?,?,?)`,
		"op-text", root, agent, `not an admission`, `plain text result`, now(), now())
	// A missing operation row falls back to the host call's own error text.
	hosts := map[string]string{"op-denied": "denied by policy", "op-text": "plain text result", "op-missing": "operation vanished"}
	for index, operationID := range []string{"op-denied", "op-text", "op-missing"} {
		host := SpanRecord{
			ID: HostSpanID(root, agent, turnID, "tc9", operationID), TraceID: trace, ParentID: tool.ID, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindHost, Name: "host." + operationID, StartNS: base + 4_100 + int64(index),
			Attrs: SpanAttrs(map[string]any{"tool_call_id": "tc9", "invocation_id": operationID}),
		}
		must(store.RecordSpanStart(ctx, host))
		host.EndNS, host.Attrs = base+4_200+int64(index), SpanAttrs(map[string]any{"operation_id": operationID})
		if operationID == "op-missing" {
			host.Status, host.Attrs = SpanStatusError, SpanAttrs(map[string]any{"operation_id": operationID, "error": "operation vanished"})
		}
		must(store.RecordSpanEnd(ctx, host))
	}
	// The root transcript has one row that is not JSON at all; the export keeps
	// going without message bodies rather than failing the whole document.
	exec(t, store, `INSERT INTO messages(session_id,seq,role,content) VALUES(?,?,?,?)`, root, 1, "user", "{not json")

	data, summary, err := store.ExportOTLP(ctx, root, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Spans != 7 {
		t.Fatalf("summary=%+v", summary)
	}
	byID := map[string]exportedSpan{}
	for _, span := range decodeExport(t, data) {
		byID[span.SpanID] = span
	}
	childOut := byID[child.ID]
	if childOut.Status.Code != otlpStatusError || childOut.Status.Message != SpanStatusInterrupted {
		t.Fatalf("interrupted turn status=%+v", childOut.Status)
	}
	if input, _ := childOut.attr("input.value"); input != "sub task" {
		t.Fatalf("child input=%q", input)
	}
	if output, _ := childOut.attr("output.value"); output != "gave up" {
		t.Fatalf("child output=%q", output)
	}
	if !strings.Contains(string(data), `"links":[{"traceId":"`+trace+`","spanId":"`+turnSpan+`"}]`) {
		t.Fatalf("child links missing from %s", data)
	}
	callOut := byID[call.ID]
	if callOut.Status.Message != "provider closed the stream" {
		t.Fatalf("llm status=%+v", callOut.Status)
	}
	if prompt, _ := callOut.attr("llm.token_count.prompt"); prompt != "7" {
		t.Fatalf("string token count not parsed: %q", prompt)
	}
	if reasoning, _ := callOut.attr("llm.token_count.completion_details.reasoning"); reasoning != "1" {
		t.Fatalf("reasoning tokens=%q", reasoning)
	}
	toolOut := byID[tool.ID]
	if toolOut.Status.Message != "cell cancelled" {
		t.Fatalf("tool status=%+v", toolOut.Status)
	}
	if engine, _ := toolOut.attr("whip.execution_engine"); engine != "quickjs" {
		t.Fatalf("tool engine=%q", engine)
	}
	for operationID, want := range hosts {
		got, _ := byID[HostSpanID(root, agent, turnID, "tc9", operationID)].attr("output.value")
		if got != want {
			t.Fatalf("host %s output=%q want %q", operationID, got, want)
		}
	}
	if _, ok := byID[turnSpan].attr("output.value"); ok {
		t.Fatal("a turn whose transcript does not decode must not invent an output")
	}
}

func TestExportOTLPAndSplitRejectBadInputs(t *testing.T) {
	if _, err := SplitOTLP([]byte("nope"), 10); err == nil {
		t.Fatal("malformed export must not split")
	}
	if _, err := SplitOTLP([]byte(`{"resourceSpans":[]}`), 10); err == nil {
		t.Fatal("an export without the single resource/scope must not split")
	}
	store, root, _ := newSwarmFixture(t)
	if _, _, err := store.ExportOTLP(context.Background(), "", ExportOptions{}); err == nil {
		t.Fatal("export without a root must fail")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ExportOTLP(context.Background(), root, ExportOptions{}); err == nil {
		t.Fatal("export on a closed store must fail")
	}
}

// The bodies the transcript never holds ride on spans as content references:
// the system prompt appears on an agent's first call and again only when it
// changes, a fold's summary is the compaction span's output and the first
// message of the next call, and the raw cutoff tells a consumer which rows
// left the model's context. Transcript rows still appear exactly once.
func TestExportOTLPEmitsPromptsAndCompaction(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	intern := func(source, body string) string {
		t.Helper()
		value, err := store.InternContent(ctx, root, source, []byte(body))
		must(err)
		return value.ReferenceID
	}
	const summaryText = "The user wants the test fixed; README was read."
	prompt := intern("prompt.system", "You are the root agent.")
	notice := intern("prompt.ephemeral", "Model budgets: unlimited.")
	summary := intern("prompt.summary", summaryText)
	base := time.Now().UnixNano()
	call := func(callID, turnID, purpose string, start int64, promptRef, noticeRef string) SpanRecord {
		name := "inference-net/kimi-k3-fast"
		if purpose == "compaction" {
			name = "compaction"
		}
		attrs := map[string]any{"model": "kimi-k3-fast", "provider": "inference-net", "purpose": purpose, "model_call_id": callID, "logical_id": "L-" + callID, "attempt": 1, "system_prompt_ref": promptRef, "system_prompt_bytes": 23, "ephemeral_ref": noticeRef, "ephemeral_bytes": 25}
		return SpanRecord{ID: ModelCallSpanID(root, callID), TraceID: TraceIDForTurn(turnID), ParentID: TurnSpanID(root, agent, turnID), RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindLLM, Name: name, StartNS: start, Attrs: SpanAttrs(attrs)}
	}
	settle := func(record SpanRecord, end int64) {
		t.Helper()
		must(store.RecordSpanStart(ctx, record))
		record.EndNS, record.Attrs = end, SpanAttrs(map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "usage_source": "reported", "cost_source": "unknown"})
		must(store.RecordSpanEnd(ctx, record))
	}
	turn1, seq1 := startRootTurnForTest(t, store, root, agent, "fix it")
	settle(call("c1", turn1, "turn", base, prompt, notice), base+1)
	settle(call("k1", turn1, "compaction", base+2, prompt, ""), base+3)
	must(store.PatchSpanAttrs(ctx, root, ModelCallSpanID(root, "k1"), map[string]any{"output_ref": summary, "output_bytes": len(summaryText), "raw_cutoff": 2}))
	settle(call("c2", turn1, "turn", base+4, prompt, notice), base+5)
	insertTranscriptRow(t, store, root, 1, map[string]any{"role": "user", "content": "fix it"})
	insertTranscriptRow(t, store, root, 2, map[string]any{"role": "assistant", "content": "Reading.", "call_id": "c1"})
	insertTranscriptRow(t, store, root, 3, map[string]any{"role": "user", "content": "continue"})
	insertTranscriptRow(t, store, root, 4, map[string]any{"role": "assistant", "content": "Done.", "call_id": "c2"})
	must(store.CommitRootTurn(ctx, RootTurnCommit{RootID: root, AgentID: agent, InboxSeq: seq1, Model: "kimi-k3-fast", Provider: "inference-net"}))
	// The next turn composed a different prompt and a different notice, so
	// its first call carries both again.
	changed := intern("prompt.system", "You are the root agent. Skill: docs.")
	capped := intern("prompt.ephemeral", "Model budgets: finite cost.")
	turn2, _ := startRootTurnForTest(t, store, root, agent, "again")
	settle(call("c3", turn2, "turn", base+6, changed, capped), base+7)
	insertTranscriptRow(t, store, root, 5, map[string]any{"role": "user", "content": "again"})
	insertTranscriptRow(t, store, root, 6, map[string]any{"role": "assistant", "content": "Again done.", "call_id": "c3"})

	data, _, err := store.ExportOTLP(ctx, root, ExportOptions{})
	must(err)
	byID := map[string]exportedSpan{}
	for _, span := range decodeExport(t, data) {
		byID[span.SpanID] = span
	}
	expect := func(spanID string, want map[string]string) {
		t.Helper()
		for key, value := range want {
			if got, _ := byID[spanID].attr(key); got != value {
				t.Fatalf("%s %s=%q want %q", byID[spanID].Name, key, got, value)
			}
		}
	}
	absent := func(spanID, key string) {
		t.Helper()
		if _, has := byID[spanID].attr(key); has {
			t.Fatalf("%s must not carry %s", byID[spanID].Name, key)
		}
	}
	c1 := ModelCallSpanID(root, "c1")
	expect(c1, map[string]string{
		"llm.input_messages.0.message.role": "system", "llm.input_messages.0.message.content": "You are the root agent.",
		"llm.input_messages.1.message.role": "user", "llm.input_messages.1.message.content": "fix it",
		"llm.input_messages.2.message.role": "system", "llm.input_messages.2.message.content": "Model budgets: unlimited.",
		"whip.input.through_seq": "1", "llm.output_messages.0.message.content": "Reading.",
	})
	absent(c1, "whip.compaction.raw_cutoff")
	k1 := ModelCallSpanID(root, "k1")
	if byID[k1].Name != "compaction" {
		t.Fatalf("compaction span name=%q", byID[k1].Name)
	}
	expect(k1, map[string]string{
		"whip.model_call.purpose": "compaction", "whip.compaction.raw_cutoff": "2",
		"llm.output_messages.0.message.role": "assistant", "llm.output_messages.0.message.content": summaryText,
	})
	absent(k1, "llm.input_messages.0.message.role") // the same prompt as c1 and no notice: nothing new was sent
	absent(k1, "whip.output.seq")
	c2 := ModelCallSpanID(root, "c2")
	expect(c2, map[string]string{
		"llm.input_messages.0.message.role": "system", "llm.input_messages.0.message.content": SummaryPrefix + summaryText,
		"llm.input_messages.1.message.role": "user", "llm.input_messages.1.message.content": "continue",
		"whip.compaction.raw_cutoff": "2", "whip.input.through_seq": "3", "llm.output_messages.0.message.content": "Done.",
	})
	absent(c2, "llm.input_messages.2.message.role") // the notice is unchanged since c1, and the compaction call did not reset it
	c3 := ModelCallSpanID(root, "c3")
	expect(c3, map[string]string{
		"llm.input_messages.0.message.role": "system", "llm.input_messages.0.message.content": "You are the root agent. Skill: docs.",
		"llm.input_messages.1.message.content": "again",
		"llm.input_messages.2.message.role":    "system", "llm.input_messages.2.message.content": "Model budgets: finite cost.",
		"whip.input.through_seq": "5", "llm.output_messages.0.message.content": "Again done.",
	})
	absent(c3, "whip.compaction.raw_cutoff")
	// Six transcript rows once each, two prompts, two notices, one summary in and one out.
	slots := 0
	for _, span := range byID {
		for _, attr := range span.Attributes {
			if strings.HasSuffix(attr.Key, ".message.role") && (strings.HasPrefix(attr.Key, "llm.input_messages.") || strings.HasPrefix(attr.Key, "llm.output_messages.")) {
				slots++
			}
		}
	}
	if slots != 12 {
		t.Fatalf("message slots=%d want 12", slots)
	}
}
