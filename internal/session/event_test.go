package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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

func TestRootSnapshotCarriesOnlyTheUncommittedPresentationTail(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	large := []byte(strings.Repeat("stream", 2_000))
	if _, err := st.AppendRootEvent(context.Background(), rootID, "stream.text", RuntimePayload{Data: large}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := st.SnapshotRoot(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Presentation) != 1 || snapshot.Presentation[0].Kind != "stream.text" || string(snapshot.Presentation[0].Payload) != string(large) {
		t.Fatalf("presentation tail = %+v", snapshot.Presentation)
	}
	if _, err := st.AppendRootEvent(context.Background(), rootID, "turn.succeeded", RuntimePayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendRootEvent(context.Background(), rootID, "stream.notice", RuntimePayload{Data: []byte(`{"text":"next"}`)}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = st.SnapshotRoot(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Presentation) != 1 || snapshot.Presentation[0].Kind != "stream.notice" {
		t.Fatalf("presentation after terminal turn = %+v", snapshot.Presentation)
	}
}

func TestReplayAndSnapshotRejectInvalidCoordinates(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, request := range []struct {
		root   string
		cursor int64
		limit  int
	}{{limit: 1}, {root: "root", cursor: -1, limit: 1}, {root: "root", limit: 0}, {root: "root", limit: MaxEventReplay + 1}} {
		if _, _, err := st.ReplayEvents(context.Background(), request.root, request.cursor, request.limit); err == nil {
			t.Fatalf("invalid replay was accepted: %+v", request)
		}
	}
	if _, err := st.SnapshotRoot(context.Background(), ""); err == nil {
		t.Fatal("empty snapshot root was accepted")
	}
	if _, err := st.SnapshotRoot(context.Background(), "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing snapshot root = %v", err)
	}
}

func TestReplayResolvesReferencedEventPayloadAndRejectsCorruptSnapshot(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(t.TempDir(), "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	large := []byte(strings.Repeat("event", 2_000))
	if _, err := st.CommitRuntime(context.Background(), RuntimeTransition{Event: &RuntimeEvent{
		RootID: rootID, Seq: 1, Kind: "large", Payload: RuntimePayload{Data: large, MediaType: "application/octet-stream"},
	}}); err != nil {
		t.Fatal(err)
	}
	events, _, err := st.ReplayEvents(context.Background(), rootID, 0, 1)
	if err != nil || len(events) != 1 || events[0].Payload.ReferenceID == "" {
		t.Fatalf("referenced replay = %+v, %v", events, err)
	}
	resolved, err := st.ResolveRuntimeValue(context.Background(), rootID, events[0].Payload)
	if err != nil || string(resolved) != string(large) {
		t.Fatalf("resolved event = %d bytes, %v", len(resolved), err)
	}
	authority, err := st.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnqueueInbox(context.Background(), InboxEnqueue{
		RootID: rootID, AgentID: authority.AgentID, Kind: "large", Payload: RuntimePayload{Data: large},
	}); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := st.SnapshotRoot(context.Background(), rootID); err != nil || len(snapshot.Inbox) != 1 || snapshot.Inbox[0].Payload.ReferenceID == "" {
		t.Fatalf("referenced inbox snapshot = %+v, %v", snapshot.Inbox, err)
	}
	if _, err := st.db.ExecContext(context.Background(), `UPDATE events SET payload_inline=?,payload_ref=NULL WHERE root_id=? AND seq=1`, make([]byte, InlineValueLimit+1), rootID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ReplayEvents(context.Background(), rootID, 0, 1); err == nil {
		t.Fatal("replay accepted oversized inline payload")
	}
	if _, err := st.db.ExecContext(context.Background(), `INSERT INTO messages(session_id,seq,role,content) VALUES(?,1,'user','not-json')`, rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SnapshotRoot(context.Background(), rootID); err == nil {
		t.Fatal("snapshot accepted corrupt durable message")
	}
}

func TestEventAPIsReturnClosedStoreErrors(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err := st.ReplayEvents(ctx, "root", 0, 1); err == nil {
		t.Fatal("closed store replayed events")
	}
	if _, err := st.SnapshotRoot(ctx, "root"); err == nil {
		t.Fatal("closed store created snapshot")
	}
	if _, err := st.ActiveRootIDs(ctx); err == nil {
		t.Fatal("closed store listed active roots")
	}
	if _, err := st.RootCursors(ctx); err == nil {
		t.Fatal("closed store listed cursors")
	}
}
