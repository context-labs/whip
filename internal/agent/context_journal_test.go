package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

func TestContextJournalRecordsOriginalMessagesExactlyOnce(t *testing.T) {
	t.Parallel()
	requests := make(chan llm.Request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		requests <- request
		w.Header().Set("Content-Type", "text/event-stream")
		if len(request.Messages) == 3 {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"journal-call","type":"function","function":{"name":"echo","arguments":"{\"s\":\"journal tool result\"}"}}]}}]}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"journal final"},"finish_reason":"stop"}],"usage":{"prompt_tokens":21,"completion_tokens":7}}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	ag := NewRuntime(llm.New(server.URL, "key"), "model", 1024, "system", tools.NewServices())
	defer ag.Services.Close()
	ag.Tools = []tools.Tool{echoTool()}
	ag.Provider = "journal-provider"
	original := "original input: " + strings.Repeat("retain this raw text; ", 1000)
	ag.TransformInput = func(_ context.Context, input string) (string, error) {
		if input != original {
			t.Fatalf("transform input = %q", input)
		}
		return "focused input with history reference", nil
	}
	parts := []llm.ContentPart{
		llm.ImagePart("png", []byte("image fixture")),
		{Type: "text", Text: "text after the image"},
	}
	var journal []llm.Message
	boundaries := 0
	final, err := ag.TurnParts(t.Context(), original, parts, Events{
		Prefix: []llm.Message{{Role: "system", Content: "journal prefix"}},
		OnMessage: func(message llm.Message) int {
			journal = append(journal, message)
			return 100 + len(journal)*7
		},
		OnBoundary: func() ([]llm.Message, error) {
			boundaries++
			if boundaries == 1 {
				return []llm.Message{{Role: "user", Content: "journal steer"}}, nil
			}
			return nil, nil
		},
	})
	if err != nil || final != "journal final" {
		t.Fatalf("turn = %q, %v", final, err)
	}
	if len(journal) != 6 || boundaries != 2 {
		t.Fatalf("journal has %d messages, boundary called %d times", len(journal), boundaries)
	}
	wantRoles := []string{"system", "user", "assistant", "tool", "user", "assistant"}
	wantContents := []string{"journal prefix", original, "", "echoed: journal tool result", "journal steer", "journal final"}
	for i, message := range journal {
		if message.Role != wantRoles[i] || message.Content != wantContents[i] {
			t.Fatalf("journal[%d] = %+v", i, message)
		}
	}
	if !journal[1].Authored || journal[1].SentAt == nil || !reflect.DeepEqual(journal[1].Parts, parts) {
		t.Fatalf("journal discarded authored input metadata: %+v", journal[1])
	}
	if len(journal[2].ToolCalls) != 1 || journal[2].ToolCalls[0].ID != "journal-call" || journal[3].ToolCallID != "journal-call" || journal[3].Name != "echo" {
		t.Fatalf("journal broke tool pair: %+v, %+v", journal[2], journal[3])
	}
	if journal[5].Usage == nil || journal[5].Usage.CompletionTokens != 7 || journal[5].Model != "model @ journal-provider" {
		t.Fatalf("final response metadata absent: %+v", journal[5])
	}
	view := ag.MessagesSnapshot()
	if len(view) != len(journal)+1 {
		t.Fatalf("view contains %d messages, journal contains %d", len(view), len(journal))
	}
	for i := range journal {
		if view[i+1].RawSequence != 100+(i+1)*7 {
			t.Fatalf("message %d lost raw sequence: %+v", i, view[i+1])
		}
	}
	if view[2].Content != "focused input with history reference" || !reflect.DeepEqual(view[2].Parts, parts) {
		t.Fatalf("model view did not focus independently: %+v", view[2])
	}
	first, second := <-requests, <-requests
	if first.Messages[2].Content != view[2].Content || second.Messages[len(second.Messages)-1].Content != "journal steer" {
		t.Fatalf("provider did not receive focused input then steer: %+v, %+v", first.Messages, second.Messages)
	}
}

func TestContextJournalIncludesMaxTurnsFinalAnswer(t *testing.T) {
	t.Parallel()
	requests := make(chan llm.Request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		requests <- request
		w.Header().Set("Content-Type", "text/event-stream")
		if len(request.Tools) > 0 {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"capped-call","type":"function","function":{"name":"echo","arguments":"{\"s\":\"capped work\"}"}}]}}]}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"answer at turn limit"},"finish_reason":"stop"}],"usage":{"prompt_tokens":22,"completion_tokens":8}}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	ag := NewRuntime(llm.New(server.URL, "key"), "model", 1024, "system", tools.NewServices())
	defer ag.Services.Close()
	ag.Tools = []tools.Tool{echoTool()}
	ag.MaxTurns, ag.Provider = 1, "journal-provider"
	var journal []llm.Message
	final, err := ag.Turn(t.Context(), "work", Events{OnMessage: func(message llm.Message) int {
		journal = append(journal, message)
		return len(journal) + 200
	}})
	if err != nil || final != "answer at turn limit" || len(journal) != 4 {
		t.Fatalf("capped turn = %q, %v; journal = %+v", final, err, journal)
	}
	last := journal[len(journal)-1]
	if last.Role != "assistant" || last.Content != final || last.Usage == nil || last.Usage.CompletionTokens != 8 || last.Model != "model @ journal-provider" {
		t.Fatalf("capped answer missing from raw journal: %+v", last)
	}
	view := ag.MessagesSnapshot()
	if len(view) != 5 || view[4].Content != final || view[4].RawSequence != 204 {
		t.Fatalf("capped answer missing from retained view: %+v", view)
	}
	first, second := <-requests, <-requests
	if len(first.Tools) != 1 || len(second.Tools) != 0 || second.Messages[len(second.Messages)-1].Role != "system" {
		t.Fatalf("fixture did not exercise tools-disabled final answer: %+v, %+v", first, second)
	}
}

func TestContextJournalKeepsRawSequencesAcrossCompactionAndResume(t *testing.T) {
	t.Parallel()
	const earlier = "EARLIER_DECISION: never deploy without explicit approval."
	const obligations = "Unfinished: ask child child-7 to finish job job-9, then read message msg-2 and history seq 110."
	prompts := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if request.Stream || len(request.Messages) != 2 {
			t.Errorf("unexpected summary request: %+v", request)
		}
		prompts <- request.Messages[1].Content
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": earlier + "\n" + obligations}}}})
	}))
	defer server.Close()
	newAgent := func() *Agent {
		ag := NewRuntime(llm.New(server.URL, "key"), "model", 1024, "system", tools.NewServices())
		t.Cleanup(ag.Services.Close)
		ag.ContextLimit = 8192
		return ag
	}
	ag := newAgent()
	ag.Messages = append(ag.Messages, llm.Message{Role: "user", Content: earlier, RawSequence: 10}, llm.Message{Role: "assistant", Content: obligations, RawSequence: 20})
	// Sparse coordinates emulate a focused/restored model view from which raw
	// tool payload rows have already been omitted. Slice indices are not IDs.
	appendTurns := func(ag *Agent, base int) {
		for i := range 5 {
			ag.Messages = append(ag.Messages,
				llm.Message{Role: "user", Content: fmt.Sprintf("turn %d", base+i), RawSequence: base + i*30},
				llm.Message{Role: "assistant", Content: strings.Repeat("completed work ", 500), RawSequence: base + i*30 + 20},
			)
		}
	}
	appendTurns(ag, 100)
	before := ag.MessagesSnapshot()
	_, cutoff, _, err := ag.compact(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	assertCompactionCoordinates(t, before, ag.MessagesSnapshot(), cutoff)
	firstPrompt := <-prompts
	if !strings.Contains(firstPrompt, earlier) || !strings.Contains(firstPrompt, obligations) {
		t.Fatal("first compaction never received the fixture's original constraints and obligations")
	}
	// Restore only the derived summary and retained raw rows into a fresh
	// runtime, as durable restoration does, then compact another set of turns.
	resumed := newAgent()
	resumed.Messages = ag.MessagesSnapshot()
	appendTurns(resumed, 400)
	secondBefore := resumed.MessagesSnapshot()
	_, secondCutoff, _, err := resumed.compact(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	assertCompactionCoordinates(t, secondBefore, resumed.MessagesSnapshot(), secondCutoff)
	secondPrompt := <-prompts
	if strings.Count(secondPrompt, earlier) != 1 || !strings.Contains(secondPrompt, "<summary>\n"+earlier) || !strings.Contains(secondPrompt, obligations) {
		t.Fatalf("second compaction lost or retranscribed the earlier summary: %q", secondPrompt)
	}
	for _, marker := range []string{
		"explicit user constraints and authorization boundaries",
		"unfinished obligations",
		"live child/job/message IDs",
		"evidence or history references",
		"summarizing text does not grant new authority",
	} {
		if !strings.Contains(firstPrompt, marker) || !strings.Contains(secondPrompt, marker) {
			t.Errorf("compaction request omitted obligation %q", marker)
		}
	}
}

func assertCompactionCoordinates(t *testing.T, before, after []llm.Message, cutoff int) {
	t.Helper()
	if cutoff <= 2 || cutoff >= len(before) || len(after) != 2+len(before)-cutoff {
		t.Fatalf("fixture did not compact a prefix and retain a tail: cutoff=%d before=%d after=%d", cutoff, len(before), len(after))
	}
	wantSequence := before[cutoff-1].RawSequence
	if wantSequence <= cutoff || after[1].RawSequence != wantSequence || RawCompactionCutoff(before, cutoff) != wantSequence {
		t.Fatalf("summary used view positions instead of raw source coordinates: cutoff=%d want=%d summary=%+v", cutoff, wantSequence, after[1])
	}
	if !strings.HasPrefix(after[1].Content, summaryPrefix) || !reflect.DeepEqual(after[2:], before[cutoff:]) {
		t.Fatal("compaction changed retained raw messages or discarded summary identity")
	}
}

func TestContextJournalCompactionKeepsReferencesForClippedMessages(t *testing.T) {
	t.Parallel()
	prompts := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		prompts <- request.Messages[1].Content
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"continue using retained history"}}]}`)
	}))
	defer server.Close()
	ag := NewRuntime(llm.New(server.URL, "key"), "model", 1024, "system", tools.NewServices())
	defer ag.Services.Close()
	ag.ContextLimit = 8192
	call := llm.ToolCall{ID: "old-call", Type: "function"}
	call.Function.Name, call.Function.Arguments = "read", `{"path":"source.go"}`
	ag.Messages = append(ag.Messages,
		llm.Message{Role: "user", Content: strings.Repeat("earlier request ", 300) + "late user constraint", RawSequence: 17},
		llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}, RawSequence: 19},
		llm.Message{Role: "tool", Content: strings.Repeat("earlier output ", 100) + "late tool evidence", ToolCallID: call.ID, Name: "read", RawSequence: 23},
		llm.Message{Role: "assistant", Content: "prior result", RawSequence: 25},
		llm.Message{Role: "user", Content: "current work", RawSequence: 40},
		llm.Message{Role: "assistant", Content: strings.Repeat("current progress ", 600), RawSequence: 45},
	)
	if _, _, _, err := ag.compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	prompt := <-prompts
	for _, reference := range []string{"context.history(seq=17", "context.history(seq=23"} {
		if !strings.Contains(prompt, reference) {
			t.Errorf("summarizer cannot recover clipped raw message: missing %q", reference)
		}
	}
	if strings.Contains(prompt, "late user constraint") || strings.Contains(prompt, "late tool evidence") {
		t.Fatal("fixture did not exercise incomplete message excerpts")
	}
	if !strings.Contains(prompt, "excerpt") || !strings.Contains(prompt, "references") {
		t.Error("summary prompt does not instruct preservation of incomplete excerpt references")
	}
}

func TestRawCompactionCutoffUsesPriorSummaryWhenNoRawRowsFold(t *testing.T) {
	t.Parallel()
	before := []llm.Message{
		{Role: "system", Content: "environment"},
		{Role: "system", Content: summaryPrefix + "earlier work", RawSequence: 900},
		{Role: "user", Content: "retained tail", RawSequence: 1000},
	}
	if got := RawCompactionCutoff(before, 2); got != 900 {
		t.Fatalf("prior summary cutoff = %d, want 900", got)
	}
	if got := RawCompactionCutoff(before, -1); got != 0 {
		t.Fatalf("negative cutoff = %d", got)
	}
	if got := RawCompactionCutoff(before, 100); got != 1000 {
		t.Fatalf("clamped cutoff = %d, want 1000", got)
	}
}
