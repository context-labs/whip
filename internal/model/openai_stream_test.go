package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func streamEvent(raw string) string { return "data: " + raw + "\n\n" }

func streamText(value string) string {
	raw, _ := json.Marshal(value)
	return streamEvent(`{"choices":[{"index":0,"delta":{"content":` + string(raw) + `}}]}`)
}

func streamFinish(reason string) string {
	return streamEvent(`{"choices":[{"index":0,"delta":{},"finish_reason":"` + reason + `"}]}`)
}

func streamUsage() string {
	return streamEvent(`{"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":8},"completion_tokens_details":{"reasoning_tokens":2},"cost":0.25}}`)
}

func TestChatStreamEmitsBeforeCompletion(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Stream  bool              `json:"stream"`
			Options chatStreamOptions `json:"stream_options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !request.Stream || !request.Options.IncludeUsage || r.Header.Get("Accept") != "text/event-stream, application/json" {
			t.Errorf("stream request: %+v %v", request, err)
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(w, streamText("hel"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, streamText("lo")+streamFinish("stop")+streamUsage()+streamEvent("[DONE]"))
	}))
	defer server.Close()
	prepared, err := chatProvider(server.URL).Prepare(t.Context(), chatRequest())
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan Chunk, 1)
	var emitted []Chunk
	var response Response
	done := make(chan error, 1)
	go func() {
		var err error
		response, err = prepared.Execute(t.Context(), func(chunk Chunk) {
			emitted = append(emitted, chunk)
			if len(emitted) == 1 {
				first <- chunk
			}
		})
		done <- err
	}()
	select {
	case chunk := <-first:
		if chunk.Text != "hel" || chunk.Call != nil {
			t.Fatalf("first chunk: %+v", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback waited for complete response")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(emitted) != 2 || emitted[1].Text != "lo" || len(response.Parts) != 1 || response.Parts[0].Text != "hello" || *response.Usage.Input != 20 || *response.ReportedCostNanoUSD != 250000000 {
		t.Fatalf("stream response=%+v chunks=%+v", response, emitted)
	}
}

func TestChatStreamSSEFramingAndSparseToolFragments(t *testing.T) {
	stream := ": keepalive\r\nevent: message\r\nid: 1\r\n" +
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\r\n" +
		"data: \"content\":\"Checking.\"}}]}\r\n\r\n" +
		streamEvent(`{"choices":[{"delta":{"reasoning_content":"preview only"}}]}`) +
		streamEvent(`{"choices":[{"delta":{"tool_calls":[{"index":7,"id":"call_","type":"function","function":{"name":"exe","arguments":"{\"co"}}]}}]}`) +
		streamEvent(`{"choices":[{"delta":{"tool_calls":[{"index":2,"id":"second","function":{"name":"execute","arguments":"{\"code\":\"print(2)\"}"}}]}}]}`) +
		streamEvent(`{"choices":[{"delta":{"tool_calls":[{"index":7,"id":"first","function":{"name":"cute","arguments":"de\":\"print(1)\"}"}}]}}]}`) +
		streamFinish("tool_calls") + streamUsage() + "data: [DONE]"
	var chunks []Chunk
	response, err := decodeChatStream(t.Context(), strings.NewReader(stream), map[string]bool{"execute": true}, func(chunk Chunk) { chunks = append(chunks, chunk) })
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 5 || chunks[0].Text != "Checking." || chunks[1].Reasoning != "preview only" || chunks[2].Call.Index != 7 || chunks[2].Call.ID != "call_" || chunks[4].Call.ID != "first" || chunks[4].Call.Name != "cute" || chunks[4].Call.Arguments != `de":"print(1)"}` {
		t.Fatalf("callbacks were not raw incremental fragments: %+v", chunks)
	}
	if len(response.Parts) != 3 || response.Parts[1].Call.ID != "call_first" || response.Parts[1].Call.Name != "execute" || string(response.Parts[1].Call.Arguments) != `{"code":"print(1)"}` || response.Parts[2].Call.ID != "second" {
		t.Fatalf("sparse assembly: %+v", response.Parts)
	}
	chunks[2].Call.Name = "mutated callback"
	if response.Parts[1].Call.Name != "execute" {
		t.Fatal("callback aliases completed response")
	}
}

func TestChatStreamRequiresBothCompletionSignals(t *testing.T) {
	for _, tc := range []struct {
		name, tail string
		valid      bool
	}{
		{"neither", "", false},
		{"finish only", streamFinish("stop"), false},
		{"done only", streamEvent("[DONE]"), false},
		{"length", streamFinish("length") + streamUsage() + streamEvent("[DONE]"), false},
		{"content filter", streamFinish("content_filter") + streamEvent("[DONE]"), false},
		{"payload after finish", streamFinish("stop") + streamText("late") + streamEvent("[DONE]"), false},
		{"both", streamFinish("stop") + streamUsage() + streamEvent("[DONE]"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := decodeChatStream(t.Context(), strings.NewReader(streamText("partial")+streamUsage()+tc.tail), nil, nil)
			if (err == nil) != tc.valid || (len(response.Parts) != 0) != tc.valid {
				t.Fatalf("boundary response=%+v error=%v", response, err)
			}
			if !tc.valid {
				failure, ok := errors.AsType[*CallError](err)
				if !ok || !failure.Uncertain || failure.Retryable {
					t.Fatalf("incomplete stream was retryable or definite: %v", err)
				}
			}
			if response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000 {
				t.Fatal("completion boundary erased known charge")
			}
		})
	}
}

func TestChatStreamRejectsMalformedDataWithoutPublishingParts(t *testing.T) {
	call := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call","type":"function","function":{"name":"execute","arguments":"{}"}}]}}]}`
	for name, bad := range map[string]string{
		"malformed json":      `not-json-provider-secret`,
		"provider error":      `{"error":{"message":"provider-secret"},"usage":{"cost":0.25}}`,
		"malformed choices":   `{"choices":"provider-secret","usage":{"cost":0.25}}`,
		"multiple choices":    `{"choices":[{"delta":{"content":"a"}},{"delta":{"content":"b"}}]}`,
		"nonzero choice":      `{"choices":[{"index":1,"delta":{"content":"a"}}]}`,
		"wrong role":          `{"choices":[{"delta":{"role":"user","content":"a"}}]}`,
		"null delta":          `{"choices":[{"delta":null}]}`,
		"missing delta":       `{"choices":[{"finish_reason":"stop"}]}`,
		"invalid content":     `{"choices":[{"delta":{"content":true}}]}`,
		"invalid UTF8":        `{"choices":[{"delta":{"content":"` + string([]byte{255}) + `"}}]}`,
		"negative index":      strings.Replace(call, `"index":0`, `"index":-1`, 1),
		"oversized index":     strings.Replace(call, `"index":0`, `"index":16`, 1),
		"fractional index":    strings.Replace(call, `"index":0`, `"index":0.5`, 1),
		"missing index":       strings.Replace(call, `"index":0,`, "", 1),
		"unsupported call":    strings.Replace(call, `"type":"function"`, `"type":"custom"`, 1),
		"invalid ID":          strings.Replace(call, `"id":"call"`, `"id":"../call"`, 1),
		"undeclared tool":     strings.Replace(call, `"name":"execute"`, `"name":"other"`, 1),
		"invalid arguments":   strings.Replace(call, `"arguments":"{}"`, `"arguments":"{"`, 1),
		"empty arguments":     strings.Replace(call, `"arguments":"{}"`, `"arguments":""`, 1),
		"nonobject arguments": strings.Replace(call, `"arguments":"{}"`, `"arguments":"[]"`, 1),
		"duplicate IDs":       `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"same","function":{"name":"execute","arguments":"{}"}},{"index":1,"id":"same","function":{"name":"execute","arguments":"{}"}}]}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			stream := streamText("partial") + streamUsage() + streamEvent(bad)
			response, err := decodeChatStream(t.Context(), strings.NewReader(stream+streamFinish("tool_calls")+streamEvent("[DONE]")), map[string]bool{"execute": true}, nil)
			failure, ok := errors.AsType[*CallError](err)
			if !ok || !failure.Uncertain || failure.Retryable || len(response.Parts) != 0 || strings.Contains(err.Error(), "provider-secret") {
				t.Fatalf("invalid stream result=%+v error=%v", response, err)
			}
			if response.Usage.Input == nil || *response.Usage.Input != 20 || response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000 {
				t.Fatalf("malformed completion erased accounting: %+v", response)
			}
		})
	}
}

func TestChatStreamOpenRouterUsageFooter(t *testing.T) {
	// OpenRouter documents this duplicate finish choice at
	// https://openrouter.ai/docs/api_reference/streaming#the-final-usage-chunk-chat-completions
	// The same shape was captured by a ledger-recorded live execution smoke.
	call := streamEvent(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_fixture","type":"function","function":{"name":"execute","arguments":"{\"code\":\"print(42)\"}"}}]}}]}`)
	finish := `{"choices":[{"index":0,"delta":{"content":"","role":"assistant"},"finish_reason":"tool_calls","native_finish_reason":"completed"}]}`
	footer := strings.TrimSuffix(finish, "}") + `,"usage":{"prompt_tokens":269,"completion_tokens":28,"cost":0.0001524}}`
	chunks := 0
	response, err := decodeChatStream(t.Context(), strings.NewReader(call+streamEvent(finish)+streamEvent(footer)+streamEvent("[DONE]")), map[string]bool{"execute": true}, func(Chunk) { chunks++ })
	if err != nil || len(response.Parts) != 1 || response.Parts[0].Call.ID != "call_fixture" || response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 152400 || *response.Usage.Input != 269 || *response.Usage.Output != 28 || chunks != 1 {
		t.Fatalf("valid accounting footer: %+v error=%v callbacks=%d", response, err, chunks)
	}
	for name, invalid := range map[string]string{
		"new text":           strings.Replace(footer, `"content":""`, `"content":"late"`, 1),
		"new reasoning":      strings.Replace(footer, `"content":""`, `"content":"","reasoning_content":"late"`, 1),
		"new tool payload":   strings.Replace(footer, `"content":""`, `"content":"","tool_calls":[{"index":0,"function":{"arguments":"late"}}]`, 1),
		"unknown payload":    strings.Replace(footer, `"content":""`, `"content":"","future_payload":{"value":"late"}`, 1),
		"conflicting finish": strings.Replace(footer, `"finish_reason":"tool_calls"`, `"finish_reason":"stop"`, 1),
		"no usage":           finish,
		"null usage":         strings.TrimSuffix(finish, "}") + `,"usage":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			response, err := decodeChatStream(t.Context(), strings.NewReader(call+streamEvent(finish)+streamEvent(invalid)+streamEvent("[DONE]")), map[string]bool{"execute": true}, nil)
			failure, ok := errors.AsType[*CallError](err)
			if !ok || !failure.Uncertain || len(response.Parts) != 0 {
				t.Fatalf("invalid footer published response: %+v %v", response, err)
			}
		})
	}
}

func TestChatStreamUsageSnapshotsPreserveIndependentEvidence(t *testing.T) {
	stream := streamText("answer") + streamUsage() +
		streamEvent(`{"choices":[],"usage":{"prompt_tokens":22,"cost":"provider-secret"}}`) +
		streamEvent(`{"choices":[],"usage":{"completion_tokens":"bad","cost":0.3}}`) +
		streamEvent(`{"choices":[],"usage":{}}`) + streamFinish("stop") + streamEvent("[DONE]")
	response, err := decodeChatStream(t.Context(), strings.NewReader(stream), nil, nil)
	if err != nil || *response.Usage.Input != 22 || *response.Usage.Output != 5 || *response.Usage.CachedInput != 8 || *response.Usage.Reasoning != 2 || *response.ReportedCostNanoUSD != 300000000 || response.UsageNote == nil {
		t.Fatalf("snapshot accounting: %+v %v", response, err)
	}
	if strings.Contains(*response.UsageNote, "provider-secret") {
		t.Fatal("usage diagnostic leaked provider content")
	}
	stream = streamText("answer") + streamUsage() + streamEvent(`{"choices":[],"usage":{"prompt_tokens":1}}`) + streamFinish("stop") + streamEvent("[DONE]")
	response, err = decodeChatStream(t.Context(), strings.NewReader(stream), nil, nil)
	if err != nil || *response.Usage.Input != 20 || *response.Usage.CachedInput != 8 || response.UsageNote == nil {
		t.Fatalf("contradictory partial snapshot corrupted accounting: %+v %v", response, err)
	}
}

func TestChatStreamCancellationRetainsUsageAndClosesTransport(t *testing.T) {
	serverStopped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, streamUsage()+streamText("partial"))
		_ = http.NewResponseController(w).Flush()
		<-r.Context().Done()
		close(serverStopped)
	}))
	defer server.Close()
	prepared, err := chatProvider(server.URL).Prepare(t.Context(), chatRequest())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	response, err := prepared.Execute(ctx, func(Chunk) { cancel() })
	failure, ok := errors.AsType[*CallError](err)
	if !errors.Is(err, context.Canceled) || !ok || !failure.Uncertain || len(response.Parts) != 0 || response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000 {
		t.Fatalf("cancel result=%+v err=%v", response, err)
	}
	select {
	case <-serverStopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stream connection survived cancellation")
	}
}

type streamBrokenReader struct{ io.Reader }

func (r streamBrokenReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if errors.Is(err, io.EOF) {
		return n, errors.New("transport-provider-secret")
	}
	return n, err
}

func TestChatStreamTransportFailureAndNoAdapterRetries(t *testing.T) {
	response, err := decodeChatStream(t.Context(), streamBrokenReader{strings.NewReader(streamUsage() + streamText("partial"))}, nil, nil)
	if err == nil || strings.Contains(err.Error(), "transport-provider-secret") || len(response.Parts) != 0 || response.ReportedCostNanoUSD == nil {
		t.Fatalf("transport failure: %+v %v", response, err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, streamText("partial")+streamUsage())
	}))
	defer server.Close()
	prepared, err := chatProvider(server.URL).Prepare(t.Context(), chatRequest())
	if err != nil {
		t.Fatal(err)
	}
	response, err = prepared.Execute(t.Context(), nil)
	failure, ok := errors.AsType[*CallError](err)
	if requests.Load() != 1 || !ok || failure.Retryable || !failure.Uncertain || len(response.Parts) != 0 || response.ReportedCostNanoUSD == nil {
		t.Fatalf("failed stream repeated or lost accounting: %+v %v calls=%d", response, err, requests.Load())
	}
}

func TestChatStreamBoundsBeforePublishingParts(t *testing.T) {
	callFragment := `{"index":0,"function":{"arguments":" "}}`
	var calls []string
	for i := range session.MaxToolCalls {
		calls = append(calls, strings.Replace(callFragment, `"index":0`, fmt.Sprintf(`"index":%d`, i), 1))
	}
	multi := streamEvent(`{"choices":[{"delta":{"tool_calls":[` + strings.Join(calls, ",") + `]}}]}`)
	for name, stream := range map[string]string{
		"physical lines":  strings.Repeat(":\n", maxStreamLines+1),
		"event count":     strings.Repeat(streamEvent(`{"choices":[{"delta":{}}]}`), maxStreamEvents+1),
		"total bytes":     strings.Repeat(":"+strings.Repeat("x", 4094)+"\n", maxResponseBytes/4096+1),
		"single line":     "data: " + strings.Repeat("x", maxResponseBytes+1),
		"output bytes":    streamText(strings.Repeat("x", session.MaxDocumentBytes+1)),
		"callback count":  strings.Repeat(multi, maxStreamChunks/session.MaxToolCalls+1),
		"call ID bytes":   streamEvent(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"` + strings.Repeat("x", 129) + `"}]}}]}`),
		"call name bytes": streamEvent(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"` + strings.Repeat("x", 65) + `"}}]}}]}`),
	} {
		t.Run(name, func(t *testing.T) {
			chunks := 0
			response, err := decodeChatStream(t.Context(), strings.NewReader(streamUsage()+stream+streamFinish("stop")+streamEvent("[DONE]")), nil, func(Chunk) { chunks++ })
			failure, ok := errors.AsType[*CallError](err)
			if !ok || !failure.Uncertain || len(response.Parts) != 0 || chunks > maxStreamChunks || response.ReportedCostNanoUSD == nil {
				t.Fatalf("unbounded stream: parts=%d error=%v chunks=%d", len(response.Parts), err, chunks)
			}
		})
	}
}

func TestCompleteFallbackAndScriptedEmitThroughSameCallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done","tool_calls":[{"id":"call","type":"function","function":{"name":"execute","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()
	request := chatRequest()
	request.Tools = []Tool{executeTool()}
	prepared, err := chatProvider(server.URL).Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	var chunks []Chunk
	response, err := prepared.Execute(t.Context(), func(chunk Chunk) { chunks = append(chunks, chunk) })
	if err != nil || len(chunks) != 2 || chunks[0].Text != "done" || chunks[1].Call.Index != 0 || chunks[1].Call.Arguments != "{}" || len(response.Parts) != 2 {
		t.Fatalf("JSON fallback: %+v %+v %v", response, chunks, err)
	}
	request.Selection = session.ModelSelection{Provider: "scripted", Name: "scripted"}
	prepared, err = (Scripted{}).Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Messages[0].Parts[0].Text = "changed after preparation"
	chunks = nil
	response, err = prepared.Execute(t.Context(), func(chunk Chunk) { chunks = append(chunks, chunk) })
	if err != nil || !reflect.DeepEqual(chunks, []Chunk{{Text: "ack: question"}}) || response.Parts[0].Text != "ack: question" {
		t.Fatalf("scripted request was not frozen: %+v %+v %v", response, chunks, err)
	}
}
