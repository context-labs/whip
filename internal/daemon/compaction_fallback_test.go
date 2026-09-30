package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func compactionFallbackHistory() []llm.Message {
	history := []llm.Message{{Role: "system", Content: "system"}}
	for range 8 {
		history = append(history,
			llm.Message{Role: "user", Authored: true, Content: strings.Repeat("history ", 1000)},
			llm.Message{Role: "assistant", Content: strings.Repeat("response ", 1000)},
		)
	}
	return history
}

func TestCompactionFallbackPersistsNoticeRouteAndBothCharges(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic=%t", automatic), func(t *testing.T) {
			var customCalls atomic.Int32
			store, root, _ := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
				request, ok := modelAccountingRequest(t, w, r)
				if !ok {
					return
				}
				if request.Model == "custom-model" {
					customCalls.Add(1)
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, "{\"error\":{\"message\":\"not supported\"},\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":0}}")
					return
				}
				modelAccountingReply(w, request, "retained summary", modelAccountingUsage())
			}, compactionFallbackHistory(), func(a *agent.Agent) {
				a.ContextLimit = 4000
				a.CompactClient, a.CompactModel, a.CompactProvider = a.Client, "custom-model", "custom-provider"
				a.CompactPricing = llm.Pricing{Prompt: "0.000007", Completion: "0.000011"}
			})
			if automatic {
				receipt, err := root.Submit(t.Context(), "continue")
				if err != nil {
					t.Fatal(err)
				}
				if done := waitReceipt(t, receipt); done.Err != nil {
					t.Fatal(done.Err)
				}
			} else if result := clientCommand(t, root, "tui", "compact-fallback", "history.compact", protocol.EmptyParams{}); result.Status != "succeeded" {
				t.Fatalf("manual compaction=%+v", result)
			}
			accounting := modelAccountingSummary(t, store, root, "", true)
			calls, cost := int64(2), int64(94) // custom: 7*7; conversation: 10*2 + 5*5
			if automatic {
				calls, cost = 3, 139
			}
			if customCalls.Load() != 1 || accounting.ReportedCalls != calls || accounting.EstimatedCostMicros != cost || accounting.UnknownCostCalls != 0 {
				t.Fatalf("accounting=%+v custom calls=%d", accounting, customCalls.Load())
			}
			spans, err := store.SpansForTrace(t.Context(), root.ID(), "")
			if err != nil {
				t.Fatal(err)
			}
			var folded, rejected int
			var reason string
			for _, span := range spans {
				var attrs map[string]any
				if err := json.Unmarshal(span.Attrs, &attrs); err != nil {
					t.Fatal(err)
				}
				if span.Kind != session.SpanKindLLM || attrs["purpose"] != "compaction" {
					continue
				}
				if span.Status == session.SpanStatusError {
					rejected++
					if attrs["provider"] != "custom-provider" || attrs["compaction_fallback"] != nil {
						t.Fatalf("rejected span attrs=%v", attrs)
					}
					continue
				}
				folded++
				reason, _ = attrs["compaction_fallback"].(string)
				ref, _ := attrs["output_ref"].(string)
				body, _, err := store.ReadContent(t.Context(), ref, root.ID(), root.AgentID(), 0, session.MaxContentRead)
				if err != nil || string(body) != "retained summary" || attrs["model"] != "root-model" || attrs["provider"] != "provider" || reason == "" {
					t.Fatalf("successful fallback span attrs=%v body=%q err=%v", attrs, body, err)
				}
			}
			if folded != 1 || rejected != 1 || len(store.Compactions(root.ID())) != 1 {
				t.Fatalf("folded=%d rejected=%d persisted=%d", folded, rejected, len(store.Compactions(root.ID())))
			}
			events, _, err := store.ReplayEvents(t.Context(), root.ID(), 0, 1000)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, event := range events {
				if event.Kind == "stream.notice" && strings.Contains(string(event.Payload.Inline), reason) {
					found = true
				}
			}
			if !found {
				t.Fatal("fallback diagnostic was not emitted as a stream.notice")
			}
		})
	}
}

func TestRecursiveCompactionAutoUsesChildRouteAndCustomIsInherited(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(fmt.Sprintf("custom=%t", custom), func(t *testing.T) {
			var model string
			var maxTokens int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request, ok := modelAccountingRequest(t, w, r)
				if !ok {
					return
				}
				model, maxTokens = request.Model, request.MaxTokens
				modelAccountingReply(w, request, "summary", modelAccountingUsage())
			}))
			defer server.Close()
			parent := agent.NewRuntime(llm.New("https://parent.invalid", "test"), "parent-model", 128, "system", tools.NewServices())
			defer parent.Services.Close()
			parent.ModelName, parent.Provider = "parent", "parent-provider"
			parent.ResolveModel = func(_, _ string) (agent.ModelRoute, error) {
				return agent.ModelRoute{Client: llm.New(server.URL, "test"), Model: "child-model", ModelName: "child", Provider: "child-provider", ContextLimit: 8192, MaxTokens: 1024}, nil
			}
			if custom {
				parent.CompactClient, parent.CompactModel, parent.CompactProvider = llm.New(server.URL, "test"), "custom-model", "custom-provider"
				parent.CompactContextLimit, parent.CompactMaxTokens = 64000, 256
				parent.CompactPricing = llm.Pricing{Prompt: "0.000007"}
			} else {
				// An unavailable custom route still behaves like Auto in a child.
				parent.CompactFallback = "Custom summarizer unavailable; using this conversation’s model."
			}
			child, _, _, err := cloneRuntimeAgent(parent, tools.NewServices(), map[string]any{"model": "child"})
			if err != nil {
				t.Fatal(err)
			}
			defer child.Services.Close()
			child.Messages = compactionFallbackHistory()
			_, _, info, err := child.CompactNow(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if custom {
				if model != parent.CompactModel || maxTokens != 256 || info.Provider != parent.CompactProvider || child.CompactClient != parent.CompactClient ||
					child.CompactContextLimit != 64000 || child.CompactMaxTokens != 256 || child.CompactPricing != parent.CompactPricing {
					t.Fatalf("custom inheritance: model=%s max=%d info=%+v", model, maxTokens, info)
				}
			} else if model != "child-model" || maxTokens <= 0 || maxTokens > 1024 || info.Provider != "child-provider" || info.Model != "child-model" ||
				child.CompactClient != nil || child.CompactModel != "" || info.Fallback != parent.CompactFallback {
				t.Fatalf("Auto used the wrong route: model=%s max=%d info=%+v", model, maxTokens, info)
			}
		})
	}
}
