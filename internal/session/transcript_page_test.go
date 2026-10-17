package session

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
)

func TestTranscriptPagePreservesAssistantTimestamp(t *testing.T) {
	for _, name := range []string{"root", "child"} {
		t.Run(name, func(t *testing.T) {
			store, rootID, _ := newMailboxFixture(t)
			agentID := rootID
			if name == "child" {
				agentID = "child"
			}
			stamp := time.Date(2025, 6, 1, 14, 30, 0, 123, time.UTC)
			transcriptCommit(t, store, rootID, agentID, 1, []llm.Message{
				{Role: "assistant", Content: strings.Repeat("answer ", 1000), SentAt: &stamp},
				{Role: "assistant", Content: "legacy answer"},
			})
			for _, maxBytes := range []int{1024, 16384} {
				page, err := store.ReadTranscriptPage(t.Context(), rootID, agentID, TranscriptReadOptions{
					ThroughSeq: -1, Limit: 2, MaxBytes: maxBytes,
				})
				if err != nil || len(page.Messages) == 0 {
					t.Fatalf("page: %+v, %v", page, err)
				}
				entry := page.Messages[0]
				got := entry.SentAt
				if maxBytes == 1024 {
					if entry.Body == nil || entry.Message != nil || entry.Role != "assistant" {
						t.Fatalf("expected stored body metadata: %+v", entry)
					}
				} else {
					if entry.Message == nil || len(page.Messages) != 2 || page.Messages[1].Message.SentAt != nil {
						t.Fatalf("expected inline and unstamped legacy messages: %+v", page)
					}
					got = entry.Message.SentAt
				}
				if entry.Seq != 1 || got == nil || !got.Equal(stamp) {
					t.Fatalf("timestamp/provenance changed: %+v", entry)
				}
				encoded, err := json.Marshal(page)
				if err != nil || !strings.Contains(string(encoded), stamp.Format(time.RFC3339Nano)) {
					t.Fatalf("wire timestamp missing: %s, %v", encoded, err)
				}
			}
		})
	}
}

func TestTranscriptPageBoundsRevisionAndRecent(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "runtime.db"), capability.NewWorkspaces())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		text := strings.Repeat("a", i*1000)
		data, _ := json.Marshal(map[string]any{"role": "user", "content": text, "authored": true})
		if _, err := store.db.ExecContext(t.Context(), `INSERT INTO messages(session_id,seq,role,content) VALUES(?,?, 'user',?)`, root, i, string(data)); err != nil {
			t.Fatal(err)
		}
	}
	opts := TranscriptReadOptions{ThroughSeq: -1, Limit: 2, MaxBytes: 2048, Recent: true}
	page, err := store.ReadTranscriptPage(t.Context(), root, root, opts)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(page)
	if len(encoded) > opts.MaxBytes || len(page.Messages) != 1 || page.Messages[0].Body == nil || page.NextSeq != 5 || !page.HasMore {
		t.Fatalf("bounded page = %+v, %d bytes", page, len(encoded))
	}
	if page.Messages[0].Role != "user" || !page.Messages[0].Authored {
		t.Fatal("large body lost user metadata")
	}
	repeat, err := store.ReadTranscriptPage(t.Context(), root, root, opts)
	if err != nil || repeat.Messages[0].Body.ReferenceID != page.Messages[0].Body.ReferenceID {
		t.Fatalf("repeated page creates new grant: %+v, %v", repeat, err)
	}
	opts.BeforeSeq, opts.ThroughSeq, opts.Revision = page.NextSeq, page.ThroughSeq, &page.HistoryRevision
	next, err := store.ReadTranscriptPage(t.Context(), root, root, opts)
	if err != nil || next.NextSeq != 4 {
		t.Fatalf("next=%+v, %v", next, err)
	}
	if err := store.ClearMessages(root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadTranscriptPage(t.Context(), root, root, opts); !errors.Is(err, ErrHistoryRevision) {
		t.Fatalf("stale page error=%v", err)
	}
	snapshot, err := store.SnapshotRoot(t.Context(), root)
	if err != nil || snapshot.HistoryRevision != page.HistoryRevision+1 {
		t.Fatalf("snapshot revision=%d, %v", snapshot.HistoryRevision, err)
	}
}
