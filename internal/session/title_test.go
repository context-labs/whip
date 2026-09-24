package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/llm"
)

func TestProvisionalTitle(t *testing.T) {
	long := strings.Repeat("界", 65)
	tests := []struct {
		name     string
		messages []llm.Message
		want     string
	}{
		{name: "empty"},
		{name: "ignores injected messages", messages: []llm.Message{
			{Role: "user", Content: "injected"},
			{Role: "user", Content: "  authored\n title  ", Authored: true},
		}, want: "authored title"},
		{name: "exact boundary", messages: []llm.Message{{Role: "user", Content: strings.Repeat("界", 64), Authored: true}}, want: strings.Repeat("界", 64)},
		{name: "truncates by rune", messages: []llm.Message{{Role: "user", Content: long, Authored: true}}, want: strings.Repeat("界", 63) + "…"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ProvisionalTitle(test.messages)
			if got != test.want {
				t.Fatalf("ProvisionalTitle() = %q, want %q", got, test.want)
			}
			if !utf8.ValidString(got) || utf8.RuneCountInString(got) > provisionalTitleRunes {
				t.Fatalf("invalid bounded title %q", got)
			}
		})
	}
}

func TestProvisionalTitlePersistsAcrossWritePaths(t *testing.T) {
	input := strings.Repeat("界", 65)
	want := strings.Repeat("界", 63) + "…"

	t.Run("Save", func(t *testing.T) {
		store, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		rootID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Save(rootID, 0, []llm.Message{{Role: "user", Content: input, Authored: true}}, "model", "provider"); err != nil {
			t.Fatal(err)
		}
		meta, _, err := store.Load(rootID)
		if err != nil || meta.Title != want || !utf8.ValidString(meta.Title) {
			t.Fatalf("persisted title = %q, err = %v", meta.Title, err)
		}
	})

	t.Run("CommitRootTurn", func(t *testing.T) {
		store, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		rootID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
		if err != nil {
			t.Fatal(err)
		}
		authority, err := store.EnsureAuthority(t.Context(), rootID)
		if err != nil {
			t.Fatal(err)
		}
		item, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: authority.AgentID, Kind: "submit", Payload: RuntimePayload{Data: []byte(input)}})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.StartRootTurn(t.Context(), rootID, authority.AgentID, item.InboxSeq); err != nil {
			t.Fatal(err)
		}
		if err := store.CommitRootTurn(context.Background(), RootTurnCommit{
			RootID: rootID, AgentID: authority.AgentID, InboxSeq: item.InboxSeq,
			Messages: []llm.Message{{Role: "user", Content: input, Authored: true}, {Role: "assistant", Content: "done"}},
			Model:    "model", Provider: "provider",
		}); err != nil {
			t.Fatal(err)
		}
		meta, _, err := store.Load(rootID)
		if err != nil || meta.Title != want || !utf8.ValidString(meta.Title) {
			t.Fatalf("persisted title = %q, err = %v", meta.Title, err)
		}
	})
}
