package session

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

func TestSessionMetadataReadsExactBoundedFields(t *testing.T) {
	store, root := collectionStore(t)
	title, cwd := strings.Repeat("Exact title 界 ", 32), "/"+strings.Repeat("directory/", 64)
	if _, err := store.db.ExecContext(t.Context(), `UPDATE sessions SET title=?,cwd=?,history_revision=9007199254740993 WHERE id=?`, title, cwd, root); err != nil {
		t.Fatal(err)
	}
	// Metadata must remain readable even if a transcript cannot be decoded.
	if _, err := store.db.ExecContext(t.Context(), `INSERT INTO messages VALUES(?,1,'user','invalid transcript JSON')`, root); err != nil {
		t.Fatal(err)
	}
	value, err := store.SessionMetadata(t.Context(), root)
	if err != nil || value.RootID != root || value.Title != title || value.CWD != cwd || value.HistoryRevision != 9007199254740993 || value.Archived {
		t.Fatalf("metadata %+v %v", value, err)
	}
	for _, invalid := range []string{"", strings.Repeat("x", 257), "missing", root[:4]} {
		if _, err := store.SessionMetadata(t.Context(), invalid); err == nil {
			t.Fatalf("invalid root accepted: %q", invalid)
		}
	}
	for _, test := range []struct{ name, title string }{
		{"raw byte limit", strings.Repeat("界", MaxSessionMetadataBytes)},
		{"JSON expansion", strings.Repeat("\x01", MaxSessionMetadataBytes/3)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := store.SetTitle(root, test.title); err != nil {
				t.Fatal(err)
			}
			if got, err := store.SessionMetadata(t.Context(), root); !errors.Is(err, ErrSessionMetadataTooLarge) || got.Title != "" {
				t.Fatalf("oversized metadata escaped budget: %+v %v", got, err)
			}
		})
	}
}

func TestSetArchivedPreservesSessionAndExplicitReads(t *testing.T) {
	store, root := collectionStore(t)
	if err := store.Save(root, 0, []llm.Message{{Role: "user", Content: "keep history"}}, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	before, _, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, archived := range []bool{true, false} {
		if err := store.SetArchived(t.Context(), root, archived); err != nil {
			t.Fatal(err)
		}
		meta, messages, err := store.Load(root)
		if err != nil || meta.Archived != archived || !meta.UpdatedAt.Equal(before.UpdatedAt) || len(messages) != 1 {
			t.Fatalf("archive changed session: %+v %+v %v", meta, messages, err)
		}
		snapshot, err := store.SnapshotRoot(t.Context(), root)
		if err != nil || snapshot.Meta.Archived != archived {
			t.Fatalf("snapshot archive state %+v %v", snapshot.Meta, err)
		}
		summaries, err := store.SessionSummaries(t.Context(), []string{root})
		if err != nil || len(summaries) != 1 || summaries[0].Missing || summaries[0].Archived != archived {
			t.Fatalf("archived root treated as missing: %+v %v", summaries, err)
		}
		attention, err := store.AttentionRoots(t.Context(), "", []string{root}, 10)
		if err != nil || len(attention) != 1 || attention[0].RootID != root {
			t.Fatalf("archive removed pending question from attention: %+v %v", attention, err)
		}
	}
	if err := store.SetArchived(t.Context(), "missing", true); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("archive missing root: %v", err)
	}
	if err := store.SetArchived(t.Context(), root, true); err != nil {
		t.Fatal(err)
	}
	forkID, err := store.Fork(root, 1, "fork")
	if err != nil {
		t.Fatal(err)
	}
	fork, err := store.SessionMetadata(t.Context(), forkID)
	if err != nil || fork.Archived || fork.CWD != before.CWD {
		t.Fatalf("fork inherited archive state: %+v %v", fork, err)
	}
}
