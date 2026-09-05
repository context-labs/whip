package session

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

func TestMailboxDeferredRevisionSurvivesCommitAndReopen(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	message, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Body: "remind me"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "mail-turn"); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour).Truncate(time.Second)
	revision, err := store.DeferMailboxMessage(t.Context(), rootID, "child", mailboxTestReceipt(message), until)
	if err != nil || revision != message.Revision+1 {
		t.Fatalf("defer revision = %d, %v", revision, err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{
		TurnID: "mail-turn", Status: "succeeded", DeliveredMessages: []MailboxReceipt{mailboxTestReceipt(message)},
	}); err != nil {
		t.Fatal(err)
	}
	store = reopenMailboxStore(t, store)
	deferred, err := store.ReadMailboxMessage(t.Context(), rootID, "child", message.ID)
	if err != nil || deferred.Revision != revision || deferred.Status != "pending" || !deferred.AvailableAt.Equal(until) {
		t.Fatalf("deferred message after reopen = %+v, %v", deferred, err)
	}
	if deferred.DeliveredTurnID != "" || !deferred.DeliveredAt.IsZero() {
		t.Fatalf("old turn delivered new revision: %+v", deferred)
	}
	work, err := store.AgentWorkStatus(t.Context(), rootID, "child", until.Add(-time.Second))
	if err != nil || work.HasReadyMail || !work.NextDeferredAt.Equal(until) {
		t.Fatalf("early wake = %+v, %v", work, err)
	}
	work, err = store.AgentWorkStatus(t.Context(), rootID, "child", until)
	if err != nil || !work.HasReadyMail {
		t.Fatalf("mature wake = %+v, %v", work, err)
	}
}

func TestMailboxReplacedRevisionMustBeObservedBeforeDelivery(t *testing.T) {
	for _, showLatest := range []bool{false, true} {
		t.Run(fmt.Sprintf("show_latest_%t", showLatest), func(t *testing.T) {
			store, rootID, rootAgentID := newMailboxFixture(t)
			first, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{
				Body: "first", Delivery: MessageDeliverySteer, UpsertKey: "updates",
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "mail-turn"); err != nil {
				t.Fatal(err)
			}
			latest, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{
				Body: "replacement", Delivery: MessageDeliverySteer, UpsertKey: "updates",
			})
			if err != nil || latest.ID != first.ID || latest.Revision != first.Revision+1 {
				t.Fatalf("replacement = %+v, %v", latest, err)
			}
			receipts := []MailboxReceipt{mailboxTestReceipt(first), mailboxTestReceipt(first)}
			wantStatus := "pending"
			if showLatest {
				receipts = append(receipts, mailboxTestReceipt(latest))
				wantStatus = "delivered"
			}
			transcript := []llm.Message{{Role: "assistant", Content: "turn still commits"}}
			if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{
				TurnID: "mail-turn", Status: "succeeded", DeliveredMessages: receipts, Transcript: transcript,
			}); err != nil {
				t.Fatal(err)
			}
			stored, err := store.ReadMailboxMessage(t.Context(), rootID, "child", latest.ID)
			if err != nil || stored.Status != wantStatus || stored.Revision != latest.Revision || string(stored.Body.Inline) != "replacement" {
				t.Fatalf("latest = %+v, %v", stored, err)
			}
			loaded, err := store.LoadAgentTranscript(t.Context(), rootID, "child")
			if err != nil || len(loaded) != 1 || loaded[0].Content != transcript[0].Content {
				t.Fatalf("transcript with stale receipts = %+v, %v", loaded, err)
			}
		})
	}
}

func TestMailboxUncommittedRevisionRedeliversAfterRecovery(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	message, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Body: "finish this"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "crashed-turn"); err != nil {
		t.Fatal(err)
	}
	digest, err := store.ReadMailboxDigest(t.Context(), rootID, "child", time.Now())
	if err != nil || len(digest.Pending) != 1 || digest.Pending[0].Revision != message.Revision {
		t.Fatalf("initial digest = %+v, %v", digest, err)
	}
	store = reopenMailboxStore(t, store)
	if err := store.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	digest, err = store.ReadMailboxDigest(t.Context(), rootID, "child", time.Now())
	if err != nil || len(digest.Pending) != 1 || digest.Pending[0].Revision != message.Revision || digest.DeliveredOpen != 0 {
		t.Fatalf("recovered digest = %+v, %v", digest, err)
	}
	if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "recovery-turn"); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{
		TurnID: "recovery-turn", Status: "succeeded", DeliveredMessages: []MailboxReceipt{mailboxTestReceipt(digest.Pending[0])},
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := store.ReadMailboxMessage(t.Context(), rootID, "child", message.ID)
	if err != nil || stored.Status != "delivered" || stored.DeliveredTurnID != "recovery-turn" {
		t.Fatalf("recovered delivery = %+v, %v", stored, err)
	}
}

func TestMailboxExplicitActionsRejectStaleReceiptsAtomically(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	first, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Body: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Body: "old", UpsertKey: "updates"})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Body: "latest", UpsertKey: "updates"})
	if err != nil {
		t.Fatal(err)
	}
	for _, receipt := range []MailboxReceipt{mailboxTestReceipt(old), {ID: latest.ID}} {
		changed, err := store.CompleteMailboxMessages(t.Context(), rootID, "child", []MailboxReceipt{mailboxTestReceipt(first), receipt})
		if !errors.Is(err, ErrMessageChanged) || changed != 0 {
			t.Fatalf("complete stale batch = %d, %v", changed, err)
		}
		if _, err := store.DeferMailboxMessage(t.Context(), rootID, "child", receipt, time.Now().Add(time.Hour)); !errors.Is(err, ErrMessageChanged) {
			t.Fatalf("defer stale = %v", err)
		}
	}
	for _, message := range []MailboxMessage{first, latest} {
		stored, err := store.ReadMailboxMessage(t.Context(), rootID, "child", message.ID)
		if err != nil || stored.Status != "pending" || stored.Revision != message.Revision || !stored.AvailableAt.IsZero() {
			t.Fatalf("failed batch mutated message = %+v, %v", stored, err)
		}
	}
	var events int
	if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM events WHERE kind IN ('message.done','message.deferred')`).Scan(&events); err != nil || events != 0 {
		t.Fatalf("failed batch persisted events = %d, %v", events, err)
	}
	receipts := []MailboxReceipt{mailboxTestReceipt(first), mailboxTestReceipt(first), mailboxTestReceipt(latest)}
	if changed, err := store.CompleteMailboxMessages(t.Context(), rootID, "child", receipts); err != nil || changed != 2 {
		t.Fatalf("complete fresh batch = %d, %v", changed, err)
	}
	if changed, err := store.CompleteMailboxMessages(t.Context(), rootID, "child", receipts); err != nil || changed != 0 {
		t.Fatalf("repeat batch = %d, %v", changed, err)
	}
	if _, err := store.CompleteMailboxMessages(t.Context(), rootID, "child", []MailboxReceipt{mailboxTestReceipt(old)}); !errors.Is(err, ErrMessageChanged) {
		t.Fatalf("stale completed revision = %v", err)
	}
}

func TestMailboxDeliveredIDsMayBeHandledInLaterTurns(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	var receipts []MailboxReceipt
	for _, body := range []string{"complete later", "defer later"} {
		message, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Body: body})
		if err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, mailboxTestReceipt(message))
	}
	if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "mail-turn"); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{
		TurnID: "mail-turn", Status: "succeeded", DeliveredMessages: receipts,
	}); err != nil {
		t.Fatal(err)
	}
	for _, wantChanged := range []int64{1, 0} {
		changed, err := store.CompleteMailboxMessages(t.Context(), rootID, "child", []MailboxReceipt{{ID: receipts[0].ID}})
		if err != nil || changed != wantChanged {
			t.Fatalf("complete by id = %d, %v; want %d", changed, err, wantChanged)
		}
	}
	if _, err := store.DeferMailboxMessage(t.Context(), rootID, "child", MailboxReceipt{ID: receipts[0].ID}, time.Now()); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("deferring completed message = %v", err)
	}
	revision, err := store.DeferMailboxMessage(t.Context(), rootID, "child", MailboxReceipt{ID: receipts[1].ID}, time.Now().Add(time.Hour))
	if err != nil || revision != receipts[1].Revision+1 {
		t.Fatalf("defer delivered by id = %d, %v", revision, err)
	}
	if _, err := store.CompleteMailboxMessages(t.Context(), rootID, "child", []MailboxReceipt{{ID: receipts[1].ID}}); !errors.Is(err, ErrMessageChanged) {
		t.Fatalf("old delivered id completed deferred revision = %v", err)
	}
}

func TestMailboxReceiptActionsEnforceRecipient(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	message, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Body: "private"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		root      string
		recipient string
		id        string
	}{
		{name: "wrong recipient", root: rootID, recipient: rootAgentID, id: message.ID},
		{name: "wrong root", root: "other-root", recipient: "child", id: message.ID},
		{name: "missing", root: rootID, recipient: "child", id: "missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			receipt := MailboxReceipt{ID: test.id, Revision: message.Revision}
			if _, err := store.ReadMailboxMessage(t.Context(), test.root, test.recipient, test.id); !errors.Is(err, ErrAgentAccess) {
				t.Fatalf("read = %v", err)
			}
			if _, err := store.CompleteMailboxMessages(t.Context(), test.root, test.recipient, []MailboxReceipt{receipt}); !errors.Is(err, ErrAgentAccess) {
				t.Fatalf("complete = %v", err)
			}
			if _, err := store.DeferMailboxMessage(t.Context(), test.root, test.recipient, receipt, time.Now()); !errors.Is(err, ErrAgentAccess) {
				t.Fatalf("defer = %v", err)
			}
		})
	}
}

func TestMailboxMutationEventFailureRollsBack(t *testing.T) {
	for _, operation := range []string{"complete", "defer", "deliver", "replace"} {
		t.Run(operation, func(t *testing.T) {
			store, rootID, rootAgentID := newMailboxFixture(t)
			message, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Body: "original", UpsertKey: "updates"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "mail-turn"); err != nil {
				t.Fatal(err)
			}
			exec(t, store, `CREATE TRIGGER fail_mail_event BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'event failure'); END`)
			switch operation {
			case "complete":
				_, err = store.CompleteMailboxMessages(t.Context(), rootID, "child", []MailboxReceipt{mailboxTestReceipt(message)})
			case "defer":
				_, err = store.DeferMailboxMessage(t.Context(), rootID, "child", mailboxTestReceipt(message), time.Now().Add(time.Hour))
			case "deliver":
				err = store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{
					TurnID: "mail-turn", Status: "succeeded", DeliveredMessages: []MailboxReceipt{mailboxTestReceipt(message)},
					Transcript: []llm.Message{{Role: "assistant", Content: "must roll back"}},
				})
			case "replace":
				_, err = store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Body: "replacement", UpsertKey: "updates"})
			}
			if err == nil {
				t.Fatal("event failure was ignored")
			}
			stored, err := store.ReadMailboxMessage(t.Context(), rootID, "child", message.ID)
			if err != nil || stored.Revision != message.Revision || stored.Status != "pending" || !stored.AvailableAt.IsZero() || string(stored.Body.Inline) != "original" {
				t.Fatalf("failed mutation retained changes = %+v, %v", stored, err)
			}
			if operation == "deliver" {
				loaded, err := store.LoadAgentTranscript(t.Context(), rootID, "child")
				if err != nil || len(loaded) != 0 {
					t.Fatalf("failed delivery retained transcript = %+v, %v", loaded, err)
				}
			}
		})
	}
}

func TestMailboxReadBodyMatchesRevisionDuringReplacement(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	message, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{Body: "1", UpsertKey: "updates"})
	if err != nil {
		t.Fatal(err)
	}
	// Exercise concurrent replacement without timing assumptions. Every read
	// must describe one revision, whichever side of the update it observes.
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-start
		for revision := int64(2); revision <= 200; revision++ {
			if _, err := store.SendMailboxMessage(t.Context(), rootID, rootAgentID, "child", MailboxSend{
				Body: strconv.FormatInt(revision, 10), UpsertKey: "updates",
			}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	close(start)
	var readErr error
	for range 200 {
		read, err := store.ReadMailboxMessage(t.Context(), rootID, "child", message.ID)
		if err != nil {
			readErr = err
			break
		}
		if string(read.Body.Inline) != strconv.FormatInt(read.Revision, 10) {
			readErr = fmt.Errorf("revision %d has body %q", read.Revision, read.Body.Inline)
			break
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
}

func reopenMailboxStore(t *testing.T, store *Store) *Store {
	t.Helper()
	var sequence int
	var name, path string
	if err := store.db.QueryRowContext(t.Context(), `PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
