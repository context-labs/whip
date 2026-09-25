package session

import (
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

func TestReasoningSurvivesDatabaseReopenForkAndRewind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.Create(SessionKindAgent, t.TempDir(), "kimi-k3", "proxy")
	if err != nil {
		t.Fatal(err)
	}
	messages := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "answer", ReasoningContent: "Retain this hypothesis."},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "later", ReasoningContent: "Discarded future thought."},
	}
	if err := store.Save(root, 1, messages, "kimi-k3", "proxy"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, restored, err := store.Load(root)
	if err != nil || len(restored) != 4 || restored[1].ReasoningContent != messages[2].ReasoningContent ||
		restored[3].ReasoningContent != messages[4].ReasoningContent {
		t.Fatalf("database reopen lost reasoning: %+v, %v", restored, err)
	}
	fork, err := store.Fork(root, 2, "reasoning prefix")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RewindHistory(t.Context(), root, 3); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{root, fork} {
		_, kept, err := store.Load(id)
		if err != nil || len(kept) != 2 || kept[1].ReasoningContent != messages[2].ReasoningContent {
			t.Fatalf("fork/rewind did not retain exactly the selected prefix: %+v, %v", kept, err)
		}
	}
}
