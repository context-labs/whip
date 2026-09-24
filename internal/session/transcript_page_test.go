package session

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptPageBoundsRevisionAndRecent(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "runtime.db"))
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
