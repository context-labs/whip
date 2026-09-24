package session

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestSnapshotRootViewBoundsHistoryAndKeepsCursor(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 1000; i++ {
		if _, err := store.db.ExecContext(t.Context(), `INSERT INTO messages(session_id,seq,role,content) VALUES(?,?,'user','{"role":"user","content":"hello"}')`, root, i); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.AppendRootEvent(t.Context(), root, "stream.text", RuntimePayload{Data: []byte(`{"text":"active output"}`)}); err != nil {
		t.Fatal(err)
	}
	view, err := store.SnapshotRootView(t.Context(), root, SnapshotViewOptions{RecentMessages: 3, CollectionLimit: 4, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(view)
	if len(encoded) > 4096 || len(view.Messages) != 3 || view.FirstMessageSeq != 998 || !view.Omitted["messages"] || view.Cursor != 1 || len(view.Presentation) != 1 {
		t.Fatalf("bounded snapshot=%+v, size=%d", view, len(encoded))
	}
	full, err := store.SnapshotRoot(t.Context(), root)
	if err != nil || len(full.Messages) != 1000 || full.Cursor != view.Cursor {
		t.Fatalf("complete snapshot count=%d cursor=%d error=%v", len(full.Messages), full.Cursor, err)
	}
}

func TestSnapshotPresentationBudgetExcludesAccounting(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		kind := "stream.accounting"
		if i == 0 {
			kind = "stream.text"
		}
		if _, err := store.AppendRootEvent(t.Context(), root, kind, RuntimePayload{Data: []byte(`{"text":"Still working"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	view, err := store.SnapshotRootView(t.Context(), root, SnapshotViewOptions{RecentMessages: 3, CollectionLimit: 4, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if view.Cursor != 20 || len(view.Presentation) != 1 || view.Presentation[0].Seq != 1 {
		t.Fatalf("accounting crowded out visible activity: %+v", view)
	}
}
