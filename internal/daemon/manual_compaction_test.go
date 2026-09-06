package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func TestManualCompactionUsesRawSequencesAfterFocusAndRestart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"retained obligations"}}]}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createRoot(t, store)
	raw := []llm.Message{{Role: "system", Content: "system"}}
	for turn := 1; turn <= 8; turn++ {
		call := llm.ToolCall{ID: fmt.Sprintf("call-%d", turn), Type: "function"}
		call.Function.Name, call.Function.Arguments = "exec", fmt.Sprintf(`{"turn":%d}`, turn)
		raw = append(raw,
			llm.Message{Role: "user", Content: fmt.Sprintf("turn %d: ", turn) + strings.Repeat("u", 6000)},
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
			llm.Message{Role: "tool", ToolCallID: call.ID, Name: "exec", Content: fmt.Sprintf("original result %d", turn)},
			llm.Message{Role: "assistant", Content: strings.Repeat("a", 6000)},
		)
	}
	if err := store.Save(rootID, 1, raw[:29], "model", "provider"); err != nil {
		t.Fatal(err)
	}
	openOwner := func() (*Daemon, *Session, *AgentSession) {
		t.Helper()
		var runner *AgentSession
		owner, err := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
			value := agent.NewRuntime(llm.New(server.URL, "key"), "model", 1024, "system", tools.NewServices())
			value.ContextLimit = 4000
			value.ModelName, value.Provider, value.WorkingDir = meta.Model, meta.Provider, meta.CWD
			value.ReplaceHistory(rlm.FocusedHistory(history))
			runner = &AgentSession{agent: value}
			return Components{Runner: runner}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = owner.Close() })
		root, err := owner.Open(rootID)
		if err != nil {
			t.Fatal(err)
		}
		return owner, root, runner
	}
	for generation, expectedCutoff := range []int{24, 28} {
		owner, root, runner := openOwner()
		before := runner.agent.MessagesSnapshot()
		if len(before) >= expectedCutoff || before[len(before)-1].RawSequence != expectedCutoff+4 {
			t.Fatalf("test requires sparse focused history: count=%d last=%+v", len(before), before[len(before)-1])
		}
		result := clientCommand(t, root, "tui", fmt.Sprintf("compact-%d", generation), "history.compact", map[string]string{})
		if result.Status != "succeeded" || !strings.Contains(result.Output, fmt.Sprintf(`"cutoff":%d`, expectedCutoff)) {
			t.Fatalf("manual compaction = %+v", result)
		}
		compactions := store.Compactions(rootID)
		if len(compactions) != generation+1 || compactions[generation].Cutoff != expectedCutoff {
			t.Fatalf("raw cutoff after focus = %+v", compactions)
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		store = openStore(t, path)
		_, restored, err := store.Load(rootID)
		if err != nil || len(restored) != 5 || restored[0].RawSequence != expectedCutoff || restored[1].RawSequence != expectedCutoff+1 || restored[3].Role != "tool" || restored[3].ToolCallID != restored[2].ToolCalls[0].ID {
			t.Fatalf("restored summary/raw tail = %+v, %v", restored, err)
		}
		page, err := store.ReadTranscript(t.Context(), rootID, rootID, 0, -1, 128)
		if err != nil || len(page.Messages) != expectedCutoff+4 || page.Messages[2].Message.Content != "original result 1" {
			t.Fatalf("manual compaction changed raw transcript: rows=%d, %v", len(page.Messages), err)
		}
		if generation == 0 {
			if err := store.Save(rootID, 29, raw, "model", "provider"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestHistoryReplacementDoesNotResurrectCommittedJournalAsProvisional(t *testing.T) {
	for _, from := range []int{1, 2} {
		t.Run(fmt.Sprintf("rewind=%d", from), func(t *testing.T) {
			store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
			rootID := createRoot(t, store)
			raw := []llm.Message{{}, {Role: "user", Content: "keep"}, {Role: "assistant", Content: "remove"}}
			if err := store.Save(rootID, 1, raw, "model", "provider"); err != nil {
				t.Fatal(err)
			}
			meta, history, err := store.Load(rootID)
			if err != nil {
				t.Fatal(err)
			}
			authority, err := store.EnsureAuthority(t.Context(), rootID)
			if err != nil {
				t.Fatal(err)
			}
			runner := &AgentSession{id: rootID, agent: agent.NewRuntime(nil, "model", 1024, "system", tools.NewServices()), turn: turnJournal{TurnID: "completed-turn", Messages: history}}
			runner.root = newSession(store, meta, authority, Components{Runner: runner})
			host := &recursiveHost{session: runner}
			replacement, err := store.RewindHistory(t.Context(), rootID, from)
			if err != nil {
				t.Fatal(err)
			}
			runner.ReplaceHistory(replacement)
			view, err := host.historyView(t.Context(), nil)
			if err != nil || view.through != from-1 || view.count != from-1 {
				t.Fatalf("replaced history includes removed journal: %+v, %v", view, err)
			}
			if message, ok, err := view.next(t.Context(), from-1); err != nil || ok {
				t.Fatalf("removed message returned as provisional: %+v, %t, %v", message, ok, err)
			}
		})
	}
}
