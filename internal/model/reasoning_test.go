package model

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func reasoningEvent(kind, value string) string {
	quoted, _ := json.Marshal(value)
	if kind == "openai-chat" {
		return streamEvent(`{"choices":[{"index":0,"delta":{"reasoning_content":` + string(quoted) + `}}]}`)
	}
	return responsesEvent("response.reasoning_summary_text.delta", `"delta":`+string(quoted))
}

func reasoningCompletion(kind string) string {
	if kind == "openai-chat" {
		return streamText("Answer") + streamFinish("stop") + streamUsage() + streamEvent("[DONE]")
	}
	return responsesEvent("response.completed", `"response":`+responsesTerminal(`[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Answer"}]}]`, responsesUsage))
}

func decodeReasoning(ctx context.Context, kind, stream string, emit func(Chunk)) (Response, error) {
	if kind == "openai-chat" {
		return decodeChatStream(ctx, strings.NewReader(stream), map[string]bool{"execute": true}, emit)
	}
	return decodeResponsesStream(ctx, strings.NewReader(stream), strings.Repeat("a", 64), map[string]bool{"execute": true}, emit, false)
}

func TestReasoningPreviewInterleavesWithoutBecomingOutput(t *testing.T) {
	for _, kind := range []string{"openai-chat", "openai-responses", "openai-codex"} {
		t.Run(kind, func(t *testing.T) {
			stream := reasoningEvent(kind, "Planning ")
			if kind == "openai-chat" {
				stream += streamText("Answer") + reasoningEvent(kind, "checked") +
					streamEvent(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call","type":"function","function":{"name":"execute","arguments":"{}"}}]}}]}`) +
					streamFinish("tool_calls") + streamUsage() + streamEvent("[DONE]")
			} else {
				stream += responsesEvent("response.output_text.delta", `"delta":"Answer"`) + reasoningEvent(kind, "checked") +
					responsesEvent("response.output_item.added", `"output_index":2,"item":{"type":"function_call","call_id":"call","name":"execute","arguments":""}`) +
					responsesEvent("response.function_call_arguments.delta", `"output_index":2,"delta":"{}"`) +
					responsesEvent("response.output_item.done", `"output_index":0,"item":{"type":"reasoning","encrypted_content":"opaque-secret","summary":[{"type":"summary_text","text":"private-summary"}]}`) +
					responsesEvent("response.completed", `"response":`+responsesTerminal(`[{"type":"reasoning","encrypted_content":"opaque-secret","summary":[{"type":"summary_text","text":"private-summary"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Answer"}]},{"type":"function_call","call_id":"call","name":"execute","arguments":"{}"}]`, responsesUsage))
			}
			provider, request := idleProvider(kind)
			request.Tools = []Tool{executeTool()}
			calls := 0
			provider.Client = &http.Client{Transport: contextLimitTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}, nil
			})}
			prepared, err := provider.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			var chunks []Chunk
			response, err := prepared.Execute(t.Context(), func(chunk Chunk) { chunks = append(chunks, chunk) })
			if err != nil || calls != 1 || len(chunks) < 4 || chunks[0].Reasoning != "Planning " || chunks[1].Text != "Answer" || chunks[2].Reasoning != "checked" || chunks[3].Call == nil {
				t.Fatalf("interleaved preview=%+v error=%v calls=%d", chunks, err, calls)
			}
			want := []session.Part{{Type: "text", Text: "Answer"}, {Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{}`)}}}
			if !reflect.DeepEqual(response.Parts, want) || response.Usage.Input == nil || *response.Usage.Input != 20 || response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000 {
				t.Fatalf("reasoning changed final output/accounting: %+v", response)
			}
			for _, chunk := range chunks {
				if chunk.Reasoning != "" && (chunk.Text != "" || chunk.Call != nil) {
					t.Fatal("reasoning preview mixed with executable output")
				}
			}
			public, _ := json.Marshal(chunks)
			if strings.Contains(string(public), "opaque-secret") || strings.Contains(string(public), "private-summary") {
				t.Fatal("opaque continuation was exposed as reasoning preview")
			}
			if kind != "openai-chat" && (response.Continuation == nil || !strings.Contains(response.Continuation.Data, "opaque-secret")) {
				t.Fatal("private continuation was changed by preview handling")
			}
			// Helpers may discard previews completely; their final response is
			// identical and the callback is not required for parser accounting.
			discarded, err := prepared.Execute(t.Context(), nil)
			if err != nil || !reflect.DeepEqual(discarded, response) {
				t.Fatalf("nil callback changed completed response: %+v %v", discarded, err)
			}
		})
	}
}

func TestReasoningPreviewKeepsFramingBoundsSeparateFromFinalParts(t *testing.T) {
	for _, kind := range []string{"openai-chat", "openai-responses"} {
		t.Run(kind, func(t *testing.T) {
			// Disposable reasoning may exceed the final-message size limit. Only
			// raw framing and preview limits bound it; it is never accumulated.
			preview := strings.Repeat("x", session.MaxDocumentBytes+1)
			bytes := 0
			response, err := decodeReasoning(t.Context(), kind, reasoningEvent(kind, preview)+reasoningCompletion(kind), func(chunk Chunk) { bytes += len(chunk.Reasoning) })
			if err != nil || bytes != len(preview) || len(response.Parts) != 1 || response.Parts[0].Text != "Answer" {
				t.Fatalf("reasoning consumed final-message allowance: bytes=%d parts=%+v err=%v", bytes, response.Parts, err)
			}
			for name, stream := range map[string]string{
				"raw bytes": reasoningEvent(kind, strings.Repeat("x", maxResponseBytes)),
				// Event traffic also consumes physical lines; whichever existing
				// framing bound is reached first must stop these previews.
				"event traffic": strings.Repeat(reasoningEvent(kind, "x"), maxStreamEvents+1),
			} {
				t.Run(name, func(t *testing.T) {
					count := 0
					response, err := decodeReasoning(t.Context(), kind, stream+reasoningCompletion(kind), func(Chunk) { count++ })
					failure, ok := errors.AsType[*CallError](err)
					if !ok || !failure.Uncertain || failure.Retryable || len(response.Parts) != 0 || response.Continuation != nil || count > maxStreamEvents {
						t.Fatalf("unbounded preview count=%d response=%+v error=%v", count, response, err)
					}
					if _, err := decodeReasoning(t.Context(), kind, stream, nil); err == nil {
						t.Fatal("nil callback bypassed framing bounds")
					}
				})
			}
		})
	}
}

func TestReasoningPreviewsConsumeSharedChunkAllowance(t *testing.T) {
	// Three callbacks per event reach the chunk bound before raw/event limits.
	fragment := streamEvent(`{"choices":[{"delta":{"content":"x","reasoning_content":"y","tool_calls":[{"index":0,"function":{"arguments":" "}}]}}]}`)
	chunks := 0
	response, err := decodeReasoning(t.Context(), "openai-chat", strings.Repeat(fragment, maxStreamChunks/3+1), func(Chunk) { chunks++ })
	failure, ok := errors.AsType[*CallError](err)
	if !ok || !failure.Uncertain || !strings.Contains(failure.Message, "chunk limit") || chunks != maxStreamChunks || len(response.Parts) != 0 {
		t.Fatalf("reasoning did not share chunk allowance: count=%d response=%+v err=%v", chunks, response, err)
	}
}

func TestReasoningMalformedAndInterruptedStreamsPreserveEvidence(t *testing.T) {
	for _, kind := range []string{"openai-chat", "openai-responses"} {
		for _, bad := range []string{`17`, `{}`, `[]`} {
			stream := reasoningEvent(kind, "preview")
			if kind == "openai-chat" {
				stream = streamUsage() + stream + streamEvent(`{"choices":[{"delta":{"reasoning_content":`+bad+`}}]}`)
			} else {
				stream += responsesEvent("response.reasoning_summary_text.delta", `"delta":`+bad)
			}
			response, err := decodeReasoning(t.Context(), kind, stream, nil)
			failure, ok := errors.AsType[*CallError](err)
			if !ok || !failure.Uncertain || failure.Retryable || len(response.Parts) > 0 || response.Continuation != nil {
				t.Fatalf("malformed %s reasoning=%s response=%+v err=%v", kind, bad, response, err)
			}
			if kind == "openai-chat" && (response.Usage.Input == nil || *response.Usage.Input != 20 || response.ReportedCostNanoUSD == nil) {
				t.Fatal("malformed reasoning erased known usage")
			}
		}
		t.Run(kind+" cancellation", func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stream := reasoningEvent(kind, "preview") + reasoningCompletion(kind)
			if kind == "openai-chat" {
				stream = streamUsage() + stream
			}
			count := 0
			response, err := decodeReasoning(ctx, kind, stream, func(chunk Chunk) {
				count++
				if chunk.Reasoning != "preview" {
					t.Error("unexpected non-reasoning callback")
				}
				cancel()
			})
			if !errors.Is(err, context.Canceled) || count != 1 || len(response.Parts) > 0 || response.Continuation != nil {
				t.Fatalf("reasoning cancellation leaked output: %+v %v callbacks=%d", response, err, count)
			}
			if kind == "openai-chat" && response.Usage.Input == nil || kind != "openai-chat" && response.Usage.Input != nil {
				t.Fatal("interruption changed known/unknown usage")
			}
		})
	}
	for _, state := range []string{"failed", "incomplete"} {
		stream := reasoningEvent("openai-responses", "preview") + responsesEvent("response."+state, `"response":{"status":"`+state+`","usage":`+responsesUsage+`}`)
		response, err := decodeReasoning(t.Context(), "openai-responses", stream, nil)
		failure, ok := errors.AsType[*CallError](err)
		if !ok || !failure.Uncertain || len(response.Parts) > 0 || response.Usage.Input == nil || *response.Usage.Input != 20 || response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000 {
			t.Fatalf("Responses reasoning failure lost terminal accounting: %+v %v", response, err)
		}
	}
}
