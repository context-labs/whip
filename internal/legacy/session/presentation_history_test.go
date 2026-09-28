package session

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

func TestPresentationSurvivesBoundedPagesForkRewindCompactionAndReopen(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	p := &llm.TranscriptPresentation{Version: 1, TurnID: "turn", Parts: []llm.PresentationPart{
		{ID: "thought", Kind: "reasoning", Text: strings.Repeat("Retained reasoning 🌍. ", 1000)},
		{ID: "call", Kind: "tool", CallID: "tool", ToolName: "rlm_exec", Hosts: []llm.PresentationHost{{InvocationID: "host", Name: "files.read", Status: "completed", Display: &llm.OperationDisplay{Target: "a.ts"}}}},
	}}
	result := &llm.TranscriptPresentation{Version: 1, TurnID: "turn", Parts: []llm.PresentationPart{{ID: "result", Kind: "result", CallID: "tool", ToolName: "rlm_exec", Status: "completed"}}}
	transcriptCommit(t, store, root, root, 1, []llm.Message{
		{Role: "assistant", Content: strings.Repeat("Prose ", 10000), Presentation: p},
		{Role: "tool", ToolCallID: "tool", Name: "rlm_exec", Content: strings.Repeat("Output ", 10000), Presentation: result},
	})
	for _, budget := range []int{1024, 4096, 16384} {
		page, err := store.ReadTranscriptPage(t.Context(), root, root, TranscriptReadOptions{ThroughSeq: -1, Limit: 2, MaxBytes: budget})
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(page)
		if len(encoded) > budget || len(page.Messages) != 1 || page.Messages[0].Body == nil || page.Messages[0].Presentation == nil {
			t.Fatalf("page bound: %d %+v", len(encoded), page)
		}
		var data []byte
		for len(data) < int(page.Messages[0].Body.Size) {
			chunk, _, err := store.ReadContent(t.Context(), page.Messages[0].Body.ReferenceID, root, root, int64(len(data)), MaxContentRead)
			if err != nil || len(chunk) == 0 {
				t.Fatalf("read details: %v", err)
			}
			data = append(data, chunk...)
		}
		var message llm.Message
		if err != nil || json.Unmarshal(data, &message) != nil || !reflect.DeepEqual(message.Presentation, p) {
			t.Fatalf("explicit details lost: %v", err)
		}
		next, err := store.ReadTranscriptPage(t.Context(), root, root, TranscriptReadOptions{AfterSeq: page.NextSeq, ThroughSeq: page.ThroughSeq, Revision: &page.HistoryRevision, Limit: 2, MaxBytes: budget})
		if err != nil || len(next.Messages) != 1 || !reflect.DeepEqual(next.Messages[0].Presentation, result) {
			t.Fatalf("large result identity lost: %+v %v", next, err)
		}
	}
	fork, err := store.Fork(root, 2, "presentation fork")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRawCompaction(t.Context(), root, root, 2, "Summary", false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RewindHistory(t.Context(), fork, 2); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{root, fork} {
		page, err := store.ReadTranscript(t.Context(), id, id, 0, -1, 2)
		if err != nil || len(page.Messages) == 0 || !reflect.DeepEqual(page.Messages[0].Message.Presentation, p) {
			t.Fatalf("restored metadata: %+v %v", page, err)
		}
	}
}
