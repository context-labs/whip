package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

func newMailboxFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitAgent(t.Context(), AgentAdmission{
		RootID: rootID, ParentAgentID: root.AgentID, ChildAgentID: "child", Name: "researcher",
		Model: "model", Provider: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	return store, rootID, root.AgentID
}

func TestMailboxMessageIsCanonicalAndBodyStaysBehindRead(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	body := strings.Repeat("private body ", 1_000)
	message, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Subject: "results", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if message.Kind != MessageKindMessage || message.Delivery != MessageDeliveryQueued || message.Status != "pending" {
		t.Fatalf("message = %+v", message)
	}
	// The message row is the wake condition: no notification row exists.
	items, err := store.LoadQueuedInbox(t.Context(), rootID, "child", 0, MaxInboxBatch)
	if err != nil || len(items) != 0 {
		t.Fatalf("inbox = %+v, %v", items, err)
	}
	work, err := store.AgentWorkStatus(t.Context(), rootID, "child", time.Now())
	if err != nil || !work.HasReadyMail || work.HasExplicitInput || !work.NextDeferredAt.IsZero() {
		t.Fatalf("work = %+v, %v", work, err)
	}
	digest, err := store.ReadMailboxDigest(t.Context(), rootID, "child", time.Now())
	if err != nil || digest.PendingTotal != 1 || len(digest.Pending) != 1 || digest.Relationships[rootAgentID] != "parent" {
		t.Fatalf("digest = %+v, %v", digest, err)
	}
	if excerpt := digest.Pending[0].Excerpt; len(excerpt) > MailboxExcerptBytes || excerpt == body || !strings.HasPrefix(body, excerpt) {
		t.Fatalf("excerpt is not a bounded prefix: %d bytes", len(excerpt))
	}
	listed, err := store.ListMailboxMessages(t.Context(), rootID, "child", "pending", "", 50)
	if err != nil || len(listed) != 1 || listed[0].Subject != "results" || len(listed[0].Body.Inline) != 0 || listed[0].Body.Size != int64(len(body)) {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
	read, err := store.ReadMailboxMessage(t.Context(), rootID, "child", message.ID)
	if err != nil || read.Body.ReferenceID == "" {
		t.Fatalf("read = %+v, %v", read, err)
	}
	resolved, _, err := store.ReadContent(t.Context(), read.Body.ReferenceID, rootID, "child", 0, MaxContentRead)
	if err != nil || string(resolved) != body {
		t.Fatalf("read body bytes=%d, %v", len(resolved), err)
	}
	if changed, err := store.CompleteMailboxMessages(t.Context(), rootID, "child", []string{message.ID}); err != nil || changed != 1 {
		t.Fatalf("complete = %d, %v", changed, err)
	}
	if changed, err := store.CompleteMailboxMessages(t.Context(), rootID, "child", []string{message.ID}); err != nil || changed != 0 {
		t.Fatalf("idempotent complete = %d, %v", changed, err)
	}
	if done, err := store.ListMailboxMessages(t.Context(), rootID, "child", "done", "", 50); err != nil || len(done) != 1 {
		t.Fatalf("done = %+v, %v", done, err)
	}
	if work, err := store.AgentWorkStatus(t.Context(), rootID, "child", time.Now()); err != nil || work.HasReadyMail {
		t.Fatalf("work after completion = %+v, %v", work, err)
	}
}

func TestAgentTurnsRetainTheAgentAfterFailure(t *testing.T) {
	store, rootID, _ := newMailboxFixture(t)
	first, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: "child", Kind: "submit", Payload: RuntimePayload{Data: []byte("first")}})
	if err != nil {
		t.Fatal(err)
	}
	start, err := store.StartAgentTurn(t.Context(), rootID, "child", "turn-1")
	if err != nil || start.Trigger != "inbox" || len(start.Items) != 1 {
		t.Fatalf("start = %+v, %v", start, err)
	}
	transcript := []llm.Message{{Role: "system", Content: "system"}, {Role: "user", Content: "work"}, {Role: "assistant", Content: "partial"}}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{TurnID: "turn-1", Status: "failed", Transcript: transcript}); err != nil {
		t.Fatal(err)
	}
	second, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: "child", Kind: "submit", Payload: RuntimePayload{Data: []byte("second")}})
	if err != nil {
		t.Fatal(err)
	}
	// The failed turn returned its human input to the queue; claims are one
	// at a time, so the retried input runs before the new one.
	start, err = store.StartAgentTurn(t.Context(), rootID, "child", "turn-2")
	if err != nil || len(start.Items) != 1 || start.Items[0].Seq != first.InboxSeq {
		t.Fatalf("second claim = %+v, %v", start, err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{TurnID: "turn-2", Status: "succeeded", AcknowledgedInbox: []int64{first.InboxSeq}, Transcript: transcript}); err != nil {
		t.Fatal(err)
	}
	start, err = store.StartAgentTurn(t.Context(), rootID, "child", "turn-3")
	if err != nil || len(start.Items) != 1 || start.Items[0].Seq != second.InboxSeq {
		t.Fatalf("third claim = %+v, %v", start, err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{TurnID: "turn-3", Status: "succeeded", AcknowledgedInbox: []int64{second.InboxSeq}, Transcript: transcript}); err != nil {
		t.Fatal(err)
	}
	// MaxInboxRetries re-queues are allowed; the failure after that interrupts.
	third, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: "child", Kind: "submit", Payload: RuntimePayload{Data: []byte("third")}})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt <= MaxInboxRetries; attempt++ {
		turnID := fmt.Sprintf("turn-retry-%d", attempt)
		if start, err := store.StartAgentTurn(t.Context(), rootID, "child", turnID); err != nil || len(start.Items) != 1 || start.Items[0].Seq != third.InboxSeq {
			t.Fatalf("retry %d claim = %+v, %v", attempt, start, err)
		}
		if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{TurnID: turnID, Status: "failed", Transcript: transcript}); err != nil {
			t.Fatal(err)
		}
	}
	var thirdStatus string
	if err := store.db.QueryRowContext(t.Context(), `SELECT status FROM inbox WHERE root_id=? AND agent_id='child' AND seq=?`, rootID, third.InboxSeq).Scan(&thirdStatus); err != nil || thirdStatus != "interrupted" {
		t.Fatalf("exhausted retry status = %q, %v", thirdStatus, err)
	}
	if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "turn-idle"); err == nil || !strings.Contains(err.Error(), "no queued turn input") {
		t.Fatalf("idle start error = %v", err)
	}
	loaded, err := store.LoadAgentTranscript(t.Context(), rootID, "child")
	if err != nil || len(loaded) != len(transcript) || loaded[2].Content != "partial" {
		t.Fatalf("transcript = %+v, %v", loaded, err)
	}
}

func TestMailboxBurstDerivesOneReadySignal(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	for index := range 10 {
		if _, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Subject: "update", Body: fmt.Sprintf("body-%d", index)}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.LoadQueuedInbox(t.Context(), rootID, "child", 0, MaxInboxBatch)
	if err != nil || len(items) != 0 {
		t.Fatalf("burst wrote inbox rows = %+v, %v", items, err)
	}
	digest, err := store.ReadMailboxDigest(t.Context(), rootID, "child", time.Now())
	if err != nil || digest.PendingTotal != 10 || len(digest.Pending) != 10 || digest.DeliveredOpen != 0 {
		t.Fatalf("digest = %+v, %v", digest, err)
	}
	start, err := store.StartAgentTurn(t.Context(), rootID, "child", "mail-turn")
	if err != nil || start.Trigger != "mailbox" || len(start.Items) != 0 {
		t.Fatalf("start = %+v, %v", start, err)
	}
	var trigger string
	if err := store.db.QueryRowContext(t.Context(), `SELECT trigger FROM turns WHERE id='mail-turn'`).Scan(&trigger); err != nil || trigger != "mailbox" {
		t.Fatalf("turn trigger = %q, %v", trigger, err)
	}
}

func TestMailboxTurnDeliversOnCommitAndNewMailStaysPending(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	first, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Subject: "first", Body: "body-1"})
	if err != nil {
		t.Fatal(err)
	}
	start, err := store.StartAgentTurn(t.Context(), rootID, "child", "turn")
	if err != nil || start.Trigger != "mailbox" {
		t.Fatalf("start = %+v, %v", start, err)
	}
	second, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Subject: "second", Body: "body-2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{TurnID: "turn", Status: "succeeded", DeliveredMessages: []string{first.ID}}); err != nil {
		t.Fatal(err)
	}
	delivered, err := store.ListMailboxMessages(t.Context(), rootID, "child", "delivered", "", 10)
	if err != nil || len(delivered) != 1 || delivered[0].ID != first.ID || delivered[0].DeliveredTurnID != "turn" {
		t.Fatalf("delivered = %+v, %v", delivered, err)
	}
	pending, err := store.ListMailboxMessages(t.Context(), rootID, "child", "pending", "", 10)
	if err != nil || len(pending) != 1 || pending[0].ID != second.ID {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	digest, err := store.ReadMailboxDigest(t.Context(), rootID, "child", time.Now())
	if err != nil || digest.PendingTotal != 1 || digest.DeliveredOpen != 1 {
		t.Fatalf("digest = %+v, %v", digest, err)
	}
	// The delivered message wakes nobody again; the new one does.
	if start, err := store.StartAgentTurn(t.Context(), rootID, "child", "turn-2"); err != nil || start.Trigger != "mailbox" {
		t.Fatalf("second start = %+v, %v", start, err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{TurnID: "turn-2", Status: "succeeded", DeliveredMessages: []string{second.ID}}); err != nil {
		t.Fatal(err)
	}
	if work, err := store.AgentWorkStatus(t.Context(), rootID, "child", time.Now()); err != nil || work.HasReadyMail {
		t.Fatalf("work after delivery = %+v, %v", work, err)
	}
}

func TestFailedMailboxTurnRedeliversPendingMail(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	message, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Subject: "first", Body: "body-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "turn"); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{TurnID: "turn", Status: "failed", DeliveredMessages: []string{message.ID}, Error: "provider down"}); err != nil {
		t.Fatal(err)
	}
	pending, err := store.ListMailboxMessages(t.Context(), rootID, "child", "pending", "", 10)
	if err != nil || len(pending) != 1 || pending[0].ID != message.ID {
		t.Fatalf("failed turn marked mail delivered: %+v, %v", pending, err)
	}
}

func TestMessageDeliveryClassesAndDeferral(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	if _, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Body: "x", Delivery: "immediate"}); err == nil {
		t.Fatal("invalid delivery class was accepted")
	}
	steer, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Subject: "course correction", Body: "stop", Delivery: MessageDeliverySteer})
	if err != nil {
		t.Fatal(err)
	}
	humanSteer, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: "child", Kind: "steer", Payload: RuntimePayload{Data: []byte("also this")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: "child", Kind: "submit", Payload: RuntimePayload{Data: []byte("not a steer")}}); err != nil {
		t.Fatal(err)
	}
	inbox, mail, err := store.PendingSteers(t.Context(), rootID, "child", time.Now())
	if err != nil || len(inbox) != 1 || inbox[0].Seq != humanSteer.InboxSeq || len(mail) != 1 || mail[0].ID != steer.ID {
		t.Fatalf("pending steers = %+v / %+v, %v", inbox, mail, err)
	}
	until := time.Now().Add(time.Hour)
	if err := store.DeferMailboxMessage(t.Context(), rootID, "child", steer.ID, until); err != nil {
		t.Fatal(err)
	}
	if _, mail, err := store.PendingSteers(t.Context(), rootID, "child", time.Now()); err != nil || len(mail) != 0 {
		t.Fatalf("deferred steer still pending = %+v, %v", mail, err)
	}
	work, err := store.AgentWorkStatus(t.Context(), rootID, "child", time.Now())
	if err != nil || work.HasReadyMail || !work.HasExplicitInput || work.NextDeferredAt.IsZero() || work.NextDeferredAt.After(until.Add(time.Second)) {
		t.Fatalf("work = %+v, %v", work, err)
	}
	if work, err := store.AgentWorkStatus(t.Context(), rootID, "child", until.Add(time.Second)); err != nil || !work.HasReadyMail {
		t.Fatalf("work after deferral matures = %+v, %v", work, err)
	}
	if err := store.DeferMailboxMessage(t.Context(), rootID, "child", "missing", until); err == nil {
		t.Fatal("deferring an unknown message succeeded")
	}
	if changed, err := store.CompleteMailboxMessages(t.Context(), rootID, "child", []string{steer.ID}); err != nil || changed != 1 {
		t.Fatalf("complete deferred = %d, %v", changed, err)
	}
}

func TestSubscriptionMessagesUpsertPerSubscription(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	subscription, err := store.CreateBlackboardSubscription(t.Context(), rootID, "child", "plan")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"v1", "v2", "v3"} {
		if _, err := store.SetBlackboard(t.Context(), rootID, rootAgentID, "plan", RuntimePayload{Data: []byte(value)}); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := store.ListMailboxMessages(t.Context(), rootID, "child", "pending", "", 10)
	if err != nil || len(pending) != 1 || pending[0].Kind != MessageKindStateChanged || pending[0].Subject != "plan" {
		t.Fatalf("hot key produced %d messages: %+v, %v", len(pending), pending, err)
	}
	if !strings.Contains(pending[0].Excerpt, subscription.ID) || !strings.Contains(pending[0].Excerpt, `"version":3`) {
		t.Fatalf("upserted excerpt = %q", pending[0].Excerpt)
	}
	// The author's own writes are not news to it.
	if own, err := store.ListMailboxMessages(t.Context(), rootID, rootAgentID, "pending", "", 10); err != nil || len(own) != 0 {
		t.Fatalf("author received its own change: %+v, %v", own, err)
	}
}

func TestMailboxCapsBacklogRateAndBodySize(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	if _, err := store.SendMailboxMessage(t.Context(), rootID, "child", rootAgentID, MailboxSend{Body: strings.Repeat("x", MaxMailboxBody+1)}); err == nil {
		t.Fatal("oversized body was accepted")
	}
	for index := range MaxPendingPerPair {
		if _, err := store.SendMailboxMessage(t.Context(), rootID, "child", rootAgentID, MailboxSend{Body: fmt.Sprintf("report %d", index)}); err != nil {
			t.Fatalf("send %d: %v", index, err)
		}
	}
	if _, err := store.SendMailboxMessage(t.Context(), rootID, "child", rootAgentID, MailboxSend{Body: "one too many"}); !errors.Is(err, ErrMailboxBacklog) {
		t.Fatalf("backlog cap error=%v", err)
	}
	// Upserted runtime notices replace a pending row and bypass the cap.
	if _, err := store.SendMailboxMessage(t.Context(), rootID, "child", rootAgentID, MailboxSend{Kind: MessageKindAgentCompleted, Body: "done", UpsertKey: "agent.turn:child"}); err != nil {
		t.Fatalf("upsert under backlog cap: %v", err)
	}
	pending, err := store.ListMailboxMessages(t.Context(), rootID, rootAgentID, "pending", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(pending))
	for _, message := range pending {
		ids = append(ids, message.ID)
	}
	if _, err := store.CompleteMailboxMessages(t.Context(), rootID, rootAgentID, ids); err != nil {
		t.Fatal(err)
	}
	// 21 sends so far (20 + the exempt upsert); the window allows 30 in total.
	for index := 21; index < MaxMessagesPerWindow; index++ {
		if _, err := store.SendMailboxMessage(t.Context(), rootID, "child", rootAgentID, MailboxSend{Body: fmt.Sprintf("burst %d", index)}); err != nil {
			t.Fatalf("send %d: %v", index, err)
		}
	}
	if _, err := store.SendMailboxMessage(t.Context(), rootID, "child", rootAgentID, MailboxSend{Body: "over the rate"}); !errors.Is(err, ErrMailboxRateLimited) {
		t.Fatalf("rate limit error=%v", err)
	}
}

// next_turn mail never makes a node runnable on its own; it rides along with
// whatever turn comes next, including a mailbox turn caused by other mail.
func TestNextTurnMailWakesNobodyButRidesAlong(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	fyi, err := store.SendMailboxMessage(t.Context(), rootID, "child", rootAgentID, MailboxSend{Subject: "fyi", Body: "no rush", Delivery: MessageDeliveryNextTurn})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.AgentWorkStatus(t.Context(), rootID, rootAgentID, time.Now())
	if err != nil || work.HasReadyMail || work.HasExplicitInput || !work.NextDeferredAt.IsZero() {
		t.Fatalf("next_turn mail made the node runnable: %+v err=%v", work, err)
	}
	if _, err := store.SendMailboxMessage(t.Context(), rootID, "child", rootAgentID, MailboxSend{Subject: "report", Body: "done"}); err != nil {
		t.Fatal(err)
	}
	work, err = store.AgentWorkStatus(t.Context(), rootID, rootAgentID, time.Now())
	if err != nil || !work.HasReadyMail {
		t.Fatalf("queued mail did not make the node runnable: %+v err=%v", work, err)
	}
	digest, err := store.ReadMailboxDigest(t.Context(), rootID, rootAgentID, time.Now())
	if err != nil || digest.PendingTotal != 2 || len(digest.Pending) != 2 || digest.Pending[0].ID != fyi.ID {
		t.Fatalf("digest should carry the next_turn message along, oldest first: %+v err=%v", digest, err)
	}
}

func TestNextTurnMailDoesNotStartChildTurn(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	if _, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Subject: "fyi", Body: "later", Delivery: MessageDeliveryNextTurn}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "child:fyi-only"); err == nil {
		t.Fatal("next_turn mail alone started a child turn")
	}
	if _, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Subject: "work", Body: "now"}); err != nil {
		t.Fatal(err)
	}
	start, err := store.StartAgentTurn(t.Context(), rootID, "child", "child:mail")
	if err != nil || start.Trigger != "mailbox" {
		t.Fatalf("queued mail did not start a mailbox turn: %+v err=%v", start, err)
	}
	digest, err := store.ReadMailboxDigest(t.Context(), rootID, "child", time.Now())
	if err != nil || digest.PendingTotal != 2 {
		t.Fatalf("digest should carry the next_turn message along with the queued one: %+v err=%v", digest, err)
	}
}
