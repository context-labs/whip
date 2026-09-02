package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

func TestEventReplayExpiresOldCursorsAndBoundsRetention(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, _ := st.Create(t.TempDir(), "model", "provider")
	authority, _ := st.EnsureClassicAuthority(context.Background(), rootID)
	tx, err := st.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for seq := int64(1); seq <= EventRetention; seq++ {
		if _, err := tx.ExecContext(context.Background(), `INSERT INTO events(root_id,seq,kind,created_at) VALUES(?,?,?,?)`, rootID, seq, "fixture", now()); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	sequence, err := st.EnqueueInbox(context.Background(), InboxEnqueue{
		RootID: rootID, AgentID: authority.AgentID, Kind: "submit", Payload: RuntimePayload{Data: []byte("prompt")},
	})
	if err != nil || sequence.EventSeq != EventRetention+1 {
		t.Fatalf("retention event sequence = %+v, %v", sequence, err)
	}
	if _, _, err := st.ReplayEvents(context.Background(), rootID, 0, 10); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("expired cursor error = %v", err)
	}
	events, latest, err := st.ReplayEvents(context.Background(), rootID, 1, MaxEventReplay)
	if err != nil || len(events) != MaxEventReplay || events[0].Seq != 2 || latest != EventRetention+1 {
		t.Fatalf("retained replay = %d events, first %+v, latest %d, err %v", len(events), events[0], latest, err)
	}
	if _, _, err := st.ReplayEvents(context.Background(), rootID, latest+1, 1); !errors.Is(err, ErrCursorAhead) {
		t.Fatalf("future cursor error = %v", err)
	}
}

func TestRootSnapshotAndActiveRootDiscovery(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, _ := st.Create(t.TempDir(), "model", "provider")
	authority, _ := st.EnsureClassicAuthority(context.Background(), rootID)
	if err := st.Save(rootID, 0, []llm.Message{{Role: "user", Content: "hello", Authored: true}}, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	sequence, err := st.EnqueueInbox(context.Background(), InboxEnqueue{
		RootID: rootID, AgentID: authority.AgentID, Kind: "submit", Payload: RuntimePayload{Data: []byte("queued")},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := st.SnapshotRoot(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Cursor != sequence.EventSeq || snapshot.Meta.ID != rootID || len(snapshot.Messages) != 1 || len(snapshot.Agents) != 1 || len(snapshot.Inbox) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if _, err := st.AddSchedule(rootID, "@every 1h", "wake", time.Now()); err != nil {
		t.Fatal(err)
	}
	roots, err := st.ActiveRootIDs(context.Background())
	if err != nil || fmt.Sprint(roots) != fmt.Sprint([]string{rootID}) {
		t.Fatalf("active roots = %v, %v", roots, err)
	}
}
