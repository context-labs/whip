package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func subscribeStateTest(t *testing.T, s *Store, actor session.SessionID, id, key string, after int64) session.StateSubscription {
	t.Helper()
	value, err := s.SubscribeState(t.Context(), actor, id, session.StateSubscribe{Key: key, After: after, Delivery: session.MailQueued})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func stateMailTest(t *testing.T, s *Store, owner session.SessionID) session.Mail {
	t.Helper()
	items, err := s.ListMail(t.Context(), owner, "", "", 100)
	if err != nil || len(items) != 1 {
		t.Fatalf("notification list=%+v %v", items, err)
	}
	value, err := s.ReadMail(t.Context(), owner, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestStateSubscriptionsCoalesceAtomicallyAndPreservePresentedRevisions(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	child := spawnChildTest(t, s, "child", childRequest(root.ID)).Session
	subscribeStateTest(t, s, root.ID, "own", "topic", 0)
	subscription := subscribeStateTest(t, s, child.ID, "watcher", "topic", 0)
	first := putStateTest(t, s, stateWrite(root.ID, session.TreeState, "first", "topic", `1`, 0))
	mail := stateMailTest(t, s, child.ID)
	if mail.Source != (session.MailSource{Kind: "state", ID: subscription.ID}) {
		t.Fatalf("forged sender instead of subscription provenance: %+v", mail.Source)
	}
	var evidence session.StateChange
	if err := json.Unmarshal([]byte(mail.Body), &evidence); err != nil || evidence.VersionID != first.ID {
		t.Fatalf("state evidence=%+v %v", evidence, err)
	}
	if own, err := s.ListMail(t.Context(), root.ID, "", "", 100); err != nil || len(own) != 0 {
		t.Fatal("self-write generated notification", own, err)
	}
	own, err := s.StateSubscriptions(t.Context(), root.ID, "", 100)
	if err != nil || own[0].Cursor != 1 {
		t.Fatal("self-write cursor not advanced", own, err)
	}
	claim, err := s.Claim(t.Context(), child.ID)
	if err != nil {
		t.Fatal(err)
	}
	execTest(t, s, `CREATE TRIGGER fail_notification BEFORE INSERT ON mail_revisions WHEN NEW.revision=2 BEGIN SELECT RAISE(ABORT,'fault'); END`)
	writer := spawnChildTest(t, s, "writer", childRequest(root.ID)).Session
	next := stateWrite(writer.ID, session.TreeState, "second", "topic", `2`, 1)
	if _, err := s.WriteState(t.Context(), next); err == nil {
		t.Fatal("notification fault ignored")
	}
	head, err := s.State(t.Context(), root.ID, session.TreeState, "topic")
	if err != nil || head.ID != first.ID {
		t.Fatal("state escaped notification rollback", head, err)
	}
	subscriptions, err := s.StateSubscriptions(t.Context(), child.ID, "", 100)
	if err != nil || subscriptions[0].Cursor != 1 {
		t.Fatal("cursor escaped notification rollback", subscriptions, err)
	}
	execTest(t, s, "DROP TRIGGER fail_notification")
	second := putStateTest(t, s, next)
	updated := stateMailTest(t, s, child.ID)
	if err := json.Unmarshal([]byte(updated.Body), &evidence); err != nil || evidence.AuthorID != writer.ID {
		t.Fatal("coalescing lost new author evidence", evidence, err)
	}
	if updated.ID != mail.ID || updated.Revision != 2 || updated.State != session.MailPending {
		t.Fatal("pending notification did not coalesce", updated)
	}
	history, err := s.History(t.Context(), child.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range history {
		if message.Mail != nil {
			found = true
			if message.Mail.Revision != 1 {
				t.Fatal("old presentation rewritten", message)
			}
		}
	}
	if !found {
		t.Fatal("claim omitted notification")
	}
	finishMailTest(t, s, claim.Turn.ID, session.Succeeded)
	if updated = stateMailTest(t, s, child.ID); updated.State != session.MailPending {
		t.Fatal("old presentation acknowledged replacement", updated)
	}
	if got := putStateTest(t, s, next); got.ID != second.ID || stateMailTest(t, s, child.ID).Revision != 2 {
		t.Fatal("write retry re-notified")
	}
	stale := next
	stale.ID = "stale"
	if _, err := s.WriteState(t.Context(), stale); !errors.Is(err, ErrConflict) {
		t.Fatal("stale write accepted", err)
	}
	if stateMailTest(t, s, child.ID).Revision != 2 {
		t.Fatal("stale CAS changed notification")
	}
}

func TestStateSubscriptionSnapshotRestartCancellationAndIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s := openTest(t, path)
	_, root := create(t, s, session.DefaultTreePolicy())
	child := spawnChildTest(t, s, "child", childRequest(root.ID)).Session
	_, stranger := create(t, s, session.DefaultTreePolicy())
	first := putStateTest(t, s, stateWrite(root.ID, session.TreeState, "first", "topic", `1`, 0))
	// after=0 closes the gap between an earlier empty snapshot and subscribing.
	subscribed := subscribeStateTest(t, s, child.ID, "watcher", "topic", 0)
	if subscribed.Cursor != first.Revision {
		t.Fatal("snapshot gap not closed", subscribed)
	}
	request := session.StateSubscribe{Key: "topic", Delivery: session.MailQueued}
	if retry := subscribeStateTest(t, s, child.ID, "watcher", "topic", 0); retry.ID != subscribed.ID || stateMailTest(t, s, child.ID).Revision != 1 {
		t.Fatal("retry duplicated notification")
	}
	if _, err := s.SubscribeState(t.Context(), stranger.ID, "watcher", request); !errors.Is(err, ErrConflict) {
		t.Fatal("foreign subscription identity reused", err)
	}
	if _, err := s.UnsubscribeState(t.Context(), stranger.ID, subscribed.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign unsubscribe", err)
	}
	request.After = 2
	if _, err := s.SubscribeState(t.Context(), root.ID, "future", request); !errors.Is(err, ErrConflict) {
		t.Fatal("future cursor accepted", err)
	}
	subscribeStateTest(t, s, stranger.ID, "unrelated", "topic", 0)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	putStateTest(t, s, stateWrite(root.ID, session.TreeState, "second", "topic", `2`, 1))
	if stateMailTest(t, s, child.ID).Revision != 2 {
		t.Fatal("restart lost active subscription")
	}
	if unrelated, err := s.ListMail(t.Context(), stranger.ID, "", "", 100); err != nil || len(unrelated) != 0 {
		t.Fatal("notification crossed trees", unrelated, err)
	}
	cancelled, err := s.UnsubscribeState(t.Context(), child.ID, subscribed.ID)
	if err != nil || cancelled.CancelledAt == nil {
		t.Fatal("cancel", cancelled, err)
	}
	if _, err := s.UnsubscribeState(t.Context(), child.ID, subscribed.ID); err != nil {
		t.Fatal("cancel retry", err)
	}
	if retry := subscribeStateTest(t, s, child.ID, "watcher", "topic", 0); retry.CancelledAt == nil {
		t.Fatal("old create retry resurrected subscription")
	}
	putStateTest(t, s, stateWrite(root.ID, session.TreeState, "third", "topic", `3`, 2))
	if stateMailTest(t, s, child.ID).Revision != 2 {
		t.Fatal("cancelled subscription notified")
	}
	if err := s.DeleteSubtree(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "state_subscriptions") != 1 {
		t.Fatal("deleted subscriber retained subscription")
	}
}

func TestStateSubscriptionAdmissionRollsBackWithNotificationAndOperation(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	child := spawnChildTest(t, s, "author", childRequest(owner.ID)).Session
	putStateTest(t, s, stateWrite(child.ID, session.TreeState, "version", "topic", `1`, 0))
	request := session.StateSubscribe{Key: "topic", Delivery: session.MailQueued}
	op := stateOperation(t, s, owner, cell, "subscribe", "subscribe", request)
	execTest(t, s, `CREATE TRIGGER fail_subscribe_outcome BEFORE UPDATE ON operations WHEN NEW.id='subscribe' AND NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'fault'); END`)
	if _, err := s.ApplyStateOperation(t.Context(), op.ID); err == nil {
		t.Fatal("injected subscription settlement failure ignored")
	}
	if count(t, s, "state_subscriptions") != 0 || count(t, s, "mail") != 0 {
		t.Fatal("subscription or notification escaped rollback")
	}
	execTest(t, s, "DROP TRIGGER fail_subscribe_outcome")
	first, err := s.ApplyStateOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.ApplyStateOperation(t.Context(), op.ID)
	if err != nil || string(first) != string(again) || count(t, s, "state_subscriptions") != 1 || count(t, s, "mail") != 1 {
		t.Fatal("subscribe retry duplicated effects", err)
	}
}

func TestStateSubscriptionCoalescingPreservesRecipientDeferral(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	author := spawnChildTest(t, s, "author", childRequest(owner.ID)).Session
	subscribeStateTest(t, s, owner.ID, "watcher", "topic", 0)
	putStateTest(t, s, stateWrite(author.ID, session.TreeState, "first", "topic", `1`, 0))
	mail := stateMailTest(t, s, owner.ID)
	applyMailTest(t, s, mailOperation(t, s, owner, cell, "read", "read", session.MailRead{ID: mail.ID}))
	future := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	applyMailTest(t, s, mailOperation(t, s, owner, cell, "defer", "defer", session.MailDefer{Receipt: mail.MailReceipt, AvailableAt: future}))
	putStateTest(t, s, stateWrite(author.ID, session.TreeState, "second", "topic", `2`, 1))
	updated := stateMailTest(t, s, owner.ID)
	if updated.Revision != 3 || !updated.AvailableAt.Equal(future) {
		t.Fatalf("write bypassed recipient deferral: %+v", updated)
	}
}

func TestStateNotificationPressureRollsBackEveryCursorAndValue(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	child := spawnChildTest(t, s, "child", childRequest(root.ID)).Session
	subscribeStateTest(t, s, root.ID, "a_own", "topic", 0)
	subscribeStateTest(t, s, child.ID, "z_watcher", "topic", 0)
	for i := int64(1); i <= session.MaxMailRevisions; i++ {
		putStateTest(t, s, stateWrite(root.ID, session.TreeState, fmt.Sprintf("value_%d", i), "topic", strconv.FormatInt(i, 10), i-1))
	}
	if _, err := s.WriteState(t.Context(), stateWrite(root.ID, session.TreeState, "overflow", "topic", `0`, session.MaxMailRevisions)); !errors.Is(err, ErrLimit) {
		t.Fatal("notification limit ignored", err)
	}
	for _, actor := range []session.SessionID{root.ID, child.ID} {
		values, err := s.StateSubscriptions(t.Context(), actor, "", 100)
		if err != nil || values[0].Cursor != session.MaxMailRevisions {
			t.Fatal("limit failure advanced a cursor", values, err)
		}
	}
	head, err := s.State(t.Context(), root.ID, session.TreeState, "topic")
	if err != nil || head.Revision != session.MaxMailRevisions {
		t.Fatal("limit failure committed state", head, err)
	}
	if mail := stateMailTest(t, s, child.ID); mail.Revision != session.MaxMailRevisions {
		t.Fatal("limit failure changed mail", mail)
	}
}
