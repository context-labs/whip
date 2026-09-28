package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func stateWrite(actor session.SessionID, scope session.StateScope, id, key, body string, revision int64) session.StateWrite {
	digest := sha256.Sum256([]byte(body))
	return session.StateWrite{ID: id, SessionID: actor, Scope: scope, Key: key, ExpectedRevision: revision, Digest: hex.EncodeToString(digest[:]), Size: int64(len(body)), SubmittedBytes: int64(len(body))}
}

func putStateTest(t *testing.T, s *Store, write session.StateWrite) session.StateValue {
	t.Helper()
	value, err := s.WriteState(t.Context(), write)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestStateVersionOwnershipHistoryAndAuthorDeletion(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	child := spawnChildTest(t, s, "child", childRequest(root.ID)).Session
	_, stranger := create(t, s, nil)
	private := putStateTest(t, s, stateWrite(child.ID, session.SessionState, "private", "same-key", `{"large":9007199254740993}`, 0))
	shared := putStateTest(t, s, stateWrite(child.ID, session.TreeState, "shared", "same-key", `"first"`, 0))
	for _, actor := range []session.SessionID{root.ID, stranger.ID} {
		if _, err := s.StateValue(t.Context(), actor, private.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("private handle escaped its owner: %v", err)
		}
	}
	if _, err := s.StateValue(t.Context(), stranger.ID, shared.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("shared handle escaped its tree: %v", err)
	}
	visible, err := s.State(t.Context(), root.ID, session.TreeState, "same-key")
	if err != nil || visible.ID != shared.ID || visible.SessionID != nil || visible.AuthorID != child.ID {
		t.Fatalf("shared value lost ownership: %+v %v", visible, err)
	}
	second := putStateTest(t, s, stateWrite(root.ID, session.TreeState, "shared_second", "same-key", `"second"`, 1))
	if second.Revision != 2 {
		t.Fatal("revision did not advance")
	}
	putStateTest(t, s, stateWrite(root.ID, session.TreeState, "another", "z-key", `true`, 0))
	page, err := s.ListState(t.Context(), child.ID, session.TreeState, "", 1)
	if err != nil || len(page) != 1 || page[0].ID != second.ID {
		t.Fatalf("latest-key page=%+v %v", page, err)
	}
	next, err := s.ListState(t.Context(), child.ID, session.TreeState, page[0].Key, 1)
	if err != nil || len(next) != 1 || next[0].Key != "z-key" {
		t.Fatalf("key cursor=%+v %v", next, err)
	}
	history, err := s.StateHistory(t.Context(), root.ID, session.TreeState, "same-key", 0, 100)
	if err != nil || len(history) != 2 || history[0].ID != shared.ID || history[1].ID != second.ID {
		t.Fatalf("immutable history=%+v %v", history, err)
	}
	if err := s.DeleteSubtree(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StateValue(t.Context(), root.ID, shared.ID); err != nil {
		t.Fatal("author deletion destroyed shared evidence", err)
	}
	if referenced, err := s.ContentReferenced(t.Context(), private.Digest); err != nil || referenced {
		t.Fatalf("deleted private value retained content: %v %v", referenced, err)
	}
	if err := s.PruneUnusedContent(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StateValue(t.Context(), root.ID, shared.ID); err != nil {
		t.Fatal("pruning removed retained state body metadata", err)
	}
}

func TestStateCASAcrossConnectionsAndIdempotentPriorVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	_, root := create(t, s, nil)
	initial := stateWrite(root.ID, session.TreeState, "initial", "key", `0`, 0)
	first := putStateTest(t, s, initial)
	var winners atomic.Int32
	var workers sync.WaitGroup
	for i := range 12 {
		workers.Go(func() {
			db := s
			if i%2 != 0 {
				db = other
			}
			_, err := db.WriteState(t.Context(), stateWrite(root.ID, session.TreeState, fmt.Sprintf("writer_%d", i), "key", strconv.Itoa(i+1), 1))
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, ErrConflict) {
				t.Errorf("write: %v", err)
			}
		})
	}
	workers.Wait()
	if winners.Load() != 1 || count(t, s, "state_versions") != 2 {
		t.Fatalf("CAS winners=%d versions=%d", winners.Load(), count(t, s, "state_versions"))
	}
	if retried := putStateTest(t, s, initial); retried.ID != first.ID || retried.Revision != 1 {
		t.Fatal("retry of an earlier write returned the current head")
	}
	changed := initial
	changed.Key = "other"
	if _, err := s.WriteState(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("write identity reused for another key: %v", err)
	}
}

func TestStateWriteRollsBackBodyMetadataAndCannotWidenContentAccess(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	write := stateWrite(root.ID, session.SessionState, "value", "key", `null`, 0)
	execTest(t, s, `CREATE TRIGGER fail_state BEFORE INSERT ON state_versions BEGIN SELECT RAISE(ABORT,'fault'); END`)
	if _, err := s.WriteState(t.Context(), write); err == nil {
		t.Fatal("injected failure ignored")
	}
	if count(t, s, "state_versions") != 0 || count(t, s, "content_bodies") != 0 {
		t.Fatal("failed state write left partial metadata")
	}
	execTest(t, s, "DROP TRIGGER fail_state")
	write.Size = session.MaxStateValueBytes
	value := putStateTest(t, s, write)
	if value.Size != session.MaxStateValueBytes {
		t.Fatal("large state metadata was truncated")
	}
	if _, err := s.RegisterContent(t.Context(), session.ContentReference{ID: "content", SessionID: root.ID, Digest: value.Digest, Size: value.Size, MediaType: "application/json"}); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("state capacity widened public content allowance: %v", err)
	}
	if _, err := s.ContentReference(t.Context(), root.ID, value.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("state handle became ordinary content authority: %v", err)
	}
	if err := s.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteState(t.Context(), write); !errors.Is(err, ErrNotFound) {
		t.Fatalf("write retry resurrected deleted owner: %v", err)
	}
}

func TestStateRetentionLimitsCoverPrivateAndSharedHistory(t *testing.T) {
	for _, kind := range []string{"versions", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			write := stateWrite(owner.ID, session.SessionState, "initial", "private", `0`, 0)
			copies := session.MaxStateVersions - 1
			if kind == "bytes" {
				// SQL accounting is tested independently of content publication: sixteen
				// retained 64 MiB versions consume the tree's logical byte allowance.
				write.Size = session.MaxStateValueBytes
				copies = session.MaxTreeStateBytes/session.MaxStateValueBytes - 1
			}
			first := putStateTest(t, s, write)
			_, err := s.db.ExecContext(t.Context(), `WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<?)
INSERT INTO state_versions SELECT 'shared_'||i,?,NULL,'shared',i,?,?,0 FROM n`, copies, owner.TreeID, owner.ID, first.Digest)
			if err != nil {
				t.Fatal(err)
			}
			before := count(t, s, "state_versions")
			if _, err := s.WriteState(t.Context(), stateWrite(owner.ID, session.TreeState, "overflow", "new", `1`, 0)); !errors.Is(err, ErrLimit) {
				t.Fatalf("%s budget not enforced: %v", kind, err)
			}
			if count(t, s, "state_versions") != before || count(t, s, "content_bodies") != 1 {
				t.Fatal("rejected write retained metadata")
			}
		})
	}
}
