package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func openTest(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func fresh(t *testing.T) *Store {
	t.Helper()
	return openTest(t, filepath.Join(t.TempDir(), "runtime.db"))
}

func create(t *testing.T, s *Store, resources []session.ResourceLimit) (session.Tree, session.Session) {
	t.Helper()
	_, _, ref, err := session.CanonicalDefinition(session.Builtins()[0])
	if err != nil {
		t.Fatal(err)
	}
	tree, root, err := s.CreateTree(t.Context(), CreateTree{
		Engine: session.Starlark, Resources: resources, Definition: ref, WorkingDirectory: t.TempDir(),
		Defaults: session.Configuration{Model: session.ModelSelection{Provider: "test", Name: "scripted"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree, root
}

func submit(t *testing.T, s *Store, id session.SessionID, key string) Admission {
	t.Helper()
	result, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key}, Submission{
		SessionID: id, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: key}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func claim(t *testing.T, s *Store, id session.SessionID) Claim {
	t.Helper()
	result, err := s.Claim(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func count(t *testing.T, s *Store, table string) int {
	t.Helper()
	var count int
	// Table names are test constants, never request input.
	if err := s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func execTest(t *testing.T, s *Store, statement string, args ...any) {
	t.Helper()
	if _, err := s.db.ExecContext(t.Context(), statement, args...); err != nil {
		t.Fatal(err)
	}
}

func mustFail(t *testing.T, s *Store, statement string, args ...any) {
	t.Helper()
	if _, err := s.db.ExecContext(t.Context(), statement, args...); err == nil {
		t.Fatal("invalid SQL write succeeded")
	}
}

func TestFreshIdentityAndForeignSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	identity := s.Identity()
	reopened := openTest(t, path)
	if identity == "" || reopened.Identity() != identity || fresh(t).Identity() == identity {
		t.Fatal("runtime identity is not stable and distinct")
	}
	if count(t, s, "definition_revisions") != len(session.Builtins()) {
		t.Fatal("builtins were duplicated")
	}
	foreign := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", foreign)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "CREATE TABLE old_sessions (id TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), foreign); !errors.Is(err, ErrSchema) {
		t.Fatalf("foreign schema: %v", err)
	}
	after, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("foreign database was modified")
	}
	execTest(t, s, "PRAGMA user_version=99")
	if _, err := Open(t.Context(), path); !errors.Is(err, ErrSchema) {
		t.Fatalf("future schema: %v", err)
	}
}

func TestConcurrentInitialization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	var wg sync.WaitGroup
	results := make(chan *Store, 4)
	failures := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			s, err := Open(t.Context(), path)
			if err != nil {
				failures <- err
			} else {
				results <- s
			}
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var id session.RuntimeID
	for s := range results {
		if id != "" && s.Identity() != id {
			t.Error("concurrent initializers disagree")
		}
		id = s.Identity()
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}
}

func TestTopologyAndRevisionConstraints(t *testing.T) {
	s := fresh(t)
	tree, root := create(t, s, nil)
	other, _ := create(t, s, nil)
	child, err := s.SpawnSession(t.Context(), SpawnSession{ParentID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if child.TreeID != tree.ID || child.ParentID == nil || *child.ParentID != root.ID || child.Definition != root.Definition {
		t.Fatal("child identity")
	}
	mustFail(t, s, "UPDATE sessions SET parent_id=? WHERE id=?", child.ID, root.ID)
	mustFail(t, s, "UPDATE sessions SET tree_id=? WHERE id=?", other.ID, child.ID)
	insert := `INSERT INTO sessions (id,tree_id,parent_id,definition_id,definition_revision,config_revision,working_directory,lifecycle,created_at) SELECT ?,?,?,definition_id,definition_revision,config_revision,working_directory,lifecycle,created_at FROM sessions WHERE id=?`
	mustFail(t, s, insert, "second-root", tree.ID, nil, root.ID)
	mustFail(t, s, insert, "cross-tree", other.ID, root.ID, root.ID)
	mustFail(t, s, insert, "self-cycle", tree.ID, "self-cycle", root.ID)
	mustFail(t, s, insert, "forward-cycle", tree.ID, "not-yet-inserted", root.ID)
	mustFail(t, s, "UPDATE definition_revisions SET document='{}'")
	mustFail(t, s, "UPDATE session_configurations SET configuration='{}'")
	submit(t, s, root.ID, "first")
	turn := claim(t, s, root.ID)
	mustFail(t, s, `INSERT INTO turns (id,session_id,config_revision,history_revision,state,started_at) VALUES ('other',?,1,1,'running',1)`, root.ID)
	mustFail(t, s, "UPDATE turns SET state='succeeded' WHERE id=?", turn.Turn.ID)
	if _, err := s.Finish(t.Context(), turn.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	mustFail(t, s, "UPDATE turns SET state='failed' WHERE id=?", turn.Turn.ID)
	if _, err := s.Root(t.Context(), tree.ID); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationCaptureAndUniformHistory(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	child, err := s.SpawnSession(t.Context(), SpawnSession{ParentID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []session.Session{root, child} {
		submit(t, s, target.ID, string(target.ID))
		active := claim(t, s, target.ID)
		model := session.ModelSelection{Provider: "test", Name: "changed"}
		changed, err := s.UpdateConfiguration(t.Context(), target.ID, 1, session.ConfigPatch{Model: &model})
		if err != nil {
			t.Fatal(err)
		}
		if changed.ConfigRevision != 2 || active.Configuration.Model.Name != "scripted" {
			t.Fatal("configuration capture changed")
		}
		old, err := s.Configuration(t.Context(), target.ID, active.Turn.ConfigRevision)
		if err != nil || old.Model.Name != "scripted" {
			t.Fatalf("old config: %v", err)
		}
		if _, err := s.UpdateConfiguration(t.Context(), target.ID, 1, session.ConfigPatch{}); !errors.Is(err, ErrConflict) {
			t.Fatalf("stale update: %v", err)
		}
		draft := session.MessageDraft{ID: session.MessageID("reply_" + string(target.ID)), Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "complete"}}}
		if _, err := s.Finish(t.Context(), active.Turn.ID, session.Succeeded, nil, []session.MessageDraft{draft}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Finish(t.Context(), active.Turn.ID, session.Succeeded, nil, []session.MessageDraft{draft}); err != nil {
			t.Fatal(err)
		}
		history, err := s.History(t.Context(), target.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) != 2 || history[0].InputID == nil || history[0].Sequence != 1 || history[1].Sequence != 2 {
			t.Fatalf("history: %+v", history)
		}
		submit(t, s, target.ID, "second_"+string(target.ID))
		next := claim(t, s, target.ID)
		if next.Configuration.Model.Name != "changed" || next.Turn.ConfigRevision != 2 {
			t.Fatal("next turn did not capture updated config")
		}
	}
	var copied int
	if err := s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM messages WHERE role='user' AND parts IS NOT NULL").Scan(&copied); err != nil {
		t.Fatal(err)
	}
	if copied != 0 {
		t.Fatal("user payload duplicated")
	}
}

func TestAtomicAdmissionClaimFinishAndRetry(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	identity := session.RequestIdentity{ClientID: "client", RequestID: "request"}
	request := Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "hello"}}}
	execTest(t, s, "CREATE TRIGGER fail_receipt BEFORE INSERT ON receipts BEGIN SELECT RAISE(ABORT,'injected'); END")
	if _, err := s.Admit(t.Context(), identity, request); err == nil {
		t.Fatal("expected injected admission failure")
	}
	if count(t, s, "inputs") != 0 || count(t, s, "receipts") != 0 {
		t.Fatal("partial admission survived")
	}
	execTest(t, s, "DROP TRIGGER fail_receipt")
	accepted, err := s.Admit(t.Context(), identity, request)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.Admit(t.Context(), identity, request)
	if err != nil || !reflect.DeepEqual(accepted, retry) {
		t.Fatalf("retry: %v", err)
	}
	request.Parts[0].Text = "conflict"
	if _, err := s.Admit(t.Context(), identity, request); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting request: %v", err)
	}
	execTest(t, s, "CREATE TRIGGER fail_message BEFORE INSERT ON messages BEGIN SELECT RAISE(ABORT,'injected'); END")
	if _, err := s.Claim(t.Context(), root.ID); err == nil {
		t.Fatal("expected injected claim failure")
	}
	input, err := s.Input(t.Context(), accepted.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count(t, s, "turns") != 0 || count(t, s, "messages") != 0 || input.State != session.Queued {
		t.Fatal("partial claim survived")
	}
	execTest(t, s, "DROP TRIGGER fail_message")
	active := claim(t, s, root.ID)
	execTest(t, s, "CREATE TRIGGER fail_finish BEFORE UPDATE ON turns BEGIN SELECT RAISE(ABORT,'injected'); END")
	draft := session.MessageDraft{ID: "reply", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "done"}}}
	if _, err := s.Finish(t.Context(), active.Turn.ID, session.Succeeded, nil, []session.MessageDraft{draft}); err == nil {
		t.Fatal("expected injected finish failure")
	}
	if count(t, s, "messages") != 1 {
		t.Fatal("partial terminal message survived")
	}
	execTest(t, s, "DROP TRIGGER fail_finish")
	if _, err := s.Finish(t.Context(), active.Turn.ID, session.Succeeded, nil, []session.MessageDraft{draft}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendMessage(t.Context(), active.Turn.ID, draft); err != nil {
		t.Fatal("stable message retry failed", err)
	}
	draft.Parts[0].Text = "different"
	if _, err := s.AppendMessage(t.Context(), active.Turn.ID, draft); !errors.Is(err, ErrConflict) {
		t.Fatalf("message conflict: %v", err)
	}
}

func TestIndependentConnectionsSerializeClaimsAndDuplicateAdmission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	a := openTest(t, path)
	b := openTest(t, path)
	_, root := create(t, a, nil)
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	for i := range 12 {
		wg.Go(func() {
			_, err := []*Store{a, b}[i%2].Admit(t.Context(), session.RequestIdentity{ClientID: "client", RequestID: "same"}, Submission{
				SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "once"}},
			})
			failures <- err
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Error(err)
		}
	}
	if count(t, a, "receipts") != 1 || count(t, a, "inputs") != 1 {
		t.Fatal("duplicate input")
	}
	submit(t, a, root.ID, "second")
	claims := make(chan error, 2)
	for _, s := range []*Store{a, b} {
		wg.Go(func() { _, err := s.Claim(t.Context(), root.ID); claims <- err })
	}
	wg.Wait()
	close(claims)
	success, busy := 0, 0
	for err := range claims {
		if err == nil {
			success++
		} else if errors.Is(err, ErrBusy) {
			busy++
		} else {
			t.Error(err)
		}
	}
	if success != 1 || busy != 1 || count(t, a, "turns") != 1 || count(t, a, "messages") != 1 {
		t.Fatal("claim uniqueness")
	}
}

func TestStopCancelRecoveryAndDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	tree, root := create(t, s, nil)
	child, err := s.SpawnSession(t.Context(), SpawnSession{ParentID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	first := submit(t, s, child.ID, "first")
	active := claim(t, s, child.ID)
	queued := submit(t, s, child.ID, "queued")
	draft := session.MessageDraft{ID: "partial", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "committed before crash"}}}
	if _, err := s.AppendMessage(t.Context(), active.Turn.ID, draft); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetLifecycle(t.Context(), child.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(t.Context(), child.ID); !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped claim: %v", err)
	}
	if err := s.DeleteSubtree(t.Context(), root.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("active deletion: %v", err)
	}
	reopened := openTest(t, path)
	running, err := reopened.Turn(t.Context(), active.Turn.ID)
	if err != nil || running.State != session.Cancelling {
		t.Fatalf("open changed execution: %v", err)
	}
	if _, err := s.SetLifecycle(t.Context(), child.ID, session.Active); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(t.Context(), child.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("resume ignored active turn", err)
	}
	if _, err := s.Finish(t.Context(), active.Turn.ID, session.Succeeded, nil, nil); !errors.Is(err, ErrConflict) {
		t.Fatal("cancelled work reported success", err)
	}
	if n, err := reopened.Recover(t.Context()); err != nil || n != 1 {
		t.Fatalf("recover: %d %v", n, err)
	}
	after, err := s.Admission(t.Context(), first.Receipt.RequestIdentity)
	if err != nil || after.Turn.State != session.Interrupted {
		t.Fatal("interruption missing", err)
	}
	history, err := s.History(t.Context(), child.ID, 0, 100)
	if err != nil || len(history) != 2 {
		t.Fatal("committed history lost", err)
	}
	input, err := s.Input(t.Context(), queued.Input.ID)
	if err != nil || input.State != session.Queued {
		t.Fatal("queued input lost", err)
	}
	cancelled, err := s.CancelInput(t.Context(), input.ID)
	if err != nil || cancelled.State != session.InputCancelled {
		t.Fatal("queued cancellation", err)
	}
	if _, err := s.Claim(t.Context(), child.ID); !errors.Is(err, ErrNoWork) {
		t.Fatal("cancelled input claimed", err)
	}
	if err := s.DeleteSubtree(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	retry, err := s.Admit(t.Context(), first.Receipt.RequestIdentity, Submission{SessionID: child.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "first"}}})
	if err != nil || retry.Receipt.DeletedAt == nil || retry.Input != nil {
		t.Fatal("deleted request resurrected", err)
	}
	if _, err := s.Tree(t.Context(), tree.ID); err != nil {
		t.Fatal("child deletion removed tree", err)
	}
	if err := s.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"session_trees", "sessions", "session_configurations", "turns", "inputs", "messages"} {
		if count(t, s, table) != 0 {
			t.Errorf("%s retained deleted rows", table)
		}
	}
	if count(t, s, "receipts") != 2 || count(t, s, "definition_revisions") == 0 {
		t.Fatal("independent records deleted")
	}
}

func TestAdmissionLimitsAndCancelledContext(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, []session.ResourceLimit{{Kind: session.ResourceDepth, Limit: new(int64(0))}, {Kind: session.ResourceDescendants, Limit: new(int64(0))}, {Kind: session.ResourceQueuedInputs, Limit: new(int64(1))}})
	if _, err := s.SpawnSession(t.Context(), SpawnSession{ParentID: root.ID}); !errors.Is(err, ErrLimit) {
		t.Fatal("child limit", err)
	}
	submit(t, s, root.ID, "one")
	_, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "c", RequestID: "two"}, Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "two"}}})
	if !errors.Is(err, ErrLimit) {
		t.Fatal("queue limit", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Claim(ctx, root.ID); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation not propagated", err)
	}
	if count(t, s, "turns") != 0 {
		t.Fatal("cancelled claim committed")
	}
}
