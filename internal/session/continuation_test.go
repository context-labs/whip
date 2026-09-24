package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

func TestContinuationStaysDurableAndOutOfPublicHistory(t *testing.T) {
	for _, child := range []bool{false, true} {
		name := "root"
		if child {
			name = "child"
		}
		t.Run(name, func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			root, err := store.Create(SessionKindAgent, t.TempDir(), "gpt-5.5", "openai-codex")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.EnsureAuthority(t.Context(), root); err != nil {
				t.Fatal(err)
			}
			agent := root
			if child {
				agent = "child"
				admitTestChild(t, store, root, root, agent)
			}
			continuation := llm.ResponseContinuation{
				AccountID: "private-account", Model: "gpt-5.5",
				Items: `[{"type":"reasoning","encrypted_content":"` + strings.Repeat("opaque", 10000) + `"}]`,
			}
			transcriptCommit(t, store, root, agent, 1, []llm.Message{
				{Role: "assistant", Content: "visible", Continuation: continuation},
				{Role: "assistant", Content: strings.Repeat("large visible text ", 1000), Continuation: continuation},
			})
			raw, err := store.ReadTranscript(t.Context(), root, agent, 0, -1, 2)
			if err != nil || len(raw.Messages) != 2 || raw.Messages[0].Message.Continuation != continuation {
				t.Fatalf("durable model state was lost: %v", err)
			}
			page, err := store.ReadTranscriptPage(t.Context(), root, agent, TranscriptReadOptions{
				ThroughSeq: -1, Limit: 2, MaxBytes: 4096,
			})
			if err != nil || len(page.Messages) != 1 || page.Messages[0].Message == nil || page.Messages[0].Message.Content != "visible" {
				t.Fatalf("opaque size incorrectly displaced visible history: %+v %v", page, err)
			}
			assertNoContinuation(t, page)
			large, err := store.ReadTranscriptPage(t.Context(), root, agent, TranscriptReadOptions{
				AfterSeq: page.NextSeq, ThroughSeq: page.ThroughSeq, Revision: &page.HistoryRevision, Limit: 2, MaxBytes: 4096,
			})
			if err != nil || len(large.Messages) != 1 || large.Messages[0].Body == nil {
				t.Fatalf("large visible body was not referenced: %+v %v", large, err)
			}
			data, _, err := store.ReadContent(t.Context(), large.Messages[0].Body.ReferenceID, root, agent, 0, MaxContentRead)
			if err != nil || strings.Contains(string(data), "opaque") || strings.Contains(string(data), "continuation") {
				t.Fatalf("private state reached a downloadable reference: %v", err)
			}
			if !child {
				view, err := store.SnapshotRootView(t.Context(), root, SnapshotViewOptions{RecentMessages: 2, CollectionLimit: 2, MaxBytes: 512 << 10})
				if err != nil {
					t.Fatal(err)
				}
				assertNoContinuation(t, view)
			}
		})
	}
}

func assertNoContinuation(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil || strings.Contains(string(data), "continuation") || strings.Contains(string(data), "private-account") || strings.Contains(string(data), "opaque") {
		t.Fatalf("private continuation reached public history: %v", err)
	}
}

func TestContinuationFollowsForkAndRewindPrefix(t *testing.T) {
	store, root := seeded(t)
	continuation := llm.ResponseContinuation{AccountID: "account", Model: "gpt-5.5", Items: `[{"type":"reasoning","encrypted_content":"opaque"}]`}
	_, messages, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	messages[1].Continuation = continuation
	messages[3].Continuation = llm.ResponseContinuation{Items: "discarded-future"}
	messages = append([]llm.Message{{Role: "system", Content: "sys"}}, messages...)
	if err := store.Save(root, 1, messages, "gpt-5.5", "openai-codex"); err != nil {
		t.Fatal(err)
	}
	fork, err := store.Fork(root, 2, "subscription fork")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RewindHistory(t.Context(), root, 3); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{root, fork} {
		_, kept, err := store.Load(id)
		if err != nil || len(kept) != 2 || kept[1].Continuation != continuation {
			t.Fatalf("fork/rewind lost the selected continuation: messages=%d err=%v", len(kept), err)
		}
	}
}
