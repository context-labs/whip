package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func TestRootMailboxFailureRequiresExplicitInputAcrossRestart(t *testing.T) {
	for _, failure := range []error{capability.ErrDenied, errors.New("provider unavailable"), context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "sessions.db")
			store := openStore(t, dbPath)
			rootID := createRoot(t, store)
			open := func() (*Daemon, *Session, *fakeRunner) {
				runner := &fakeRunner{turn: func(_ context.Context, input string, _ bool) (string, error) {
					if input == "resume successfully" {
						return "done", nil
					}
					return "", failure
				}}
				owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
					return Components{Runner: runner}, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = owner.Close() })
				root, err := owner.Open(rootID)
				if err != nil {
					t.Fatal(err)
				}
				return owner, root, runner
			}
			owner, root, runner := open()
			childID := rootID + ":sender"
			if err := root.AdmitAgent(t.Context(), session.AgentAdmission{
				ParentAgentID: rootID, ChildAgentID: childID, Name: "sender", Prompt: session.RuntimePayload{Data: []byte("work")},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := root.SendMailboxMessage(t.Context(), childID, rootID, session.MailboxSend{Body: "result"}); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				blocked, err := store.RootMailboxNeedsInput(t.Context(), rootID, rootID)
				if err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("mailbox turn never settled")
				}
				time.Sleep(time.Millisecond)
			}
			// Queries and duplicate wakes must not bypass the durable retry barrier.
			for range 10 {
				root.notify()
				if err := root.routeControl(t.Context(), func(context.Context) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if got := runner.calls.Load(); got != 1 {
				t.Fatalf("failed mail ran %d times", got)
			}
			pending, err := store.ListMailboxMessages(t.Context(), rootID, rootID, "pending", "", 10)
			if err != nil || len(pending) != 1 {
				t.Fatalf("pending mail was lost: %+v, %v", pending, err)
			}
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
			store = openStore(t, dbPath)
			_, root, runner = open()
			for range 10 {
				root.notify()
				if err := root.routeControl(t.Context(), func(context.Context) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if got := runner.calls.Load(); got != 0 {
				t.Fatalf("restart retried failed mail %d times", got)
			}
			receipt, err := root.Submit(t.Context(), "try again")
			if err != nil {
				t.Fatal(err)
			}
			if got := waitReceipt(t, receipt); !errors.Is(got.Err, failure) {
				t.Fatalf("explicit turn did not run: %+v", got)
			}
			if got := runner.calls.Load(); got != 1 {
				t.Fatalf("explicit input ran %d times", got)
			}
			receipt, err = root.Submit(t.Context(), "resume successfully")
			if err != nil {
				t.Fatal(err)
			}
			if got := waitReceipt(t, receipt); got.Err != nil {
				t.Fatalf("explicit recovery failed: %+v", got)
			}
			// The fake runner does not deliver mail. Successful explicit input
			// must restore automatic delivery, which now makes one more attempt.
			deadline = time.Now().Add(3 * time.Second)
			for {
				blocked, err := store.RootMailboxNeedsInput(t.Context(), rootID, rootID)
				if err != nil {
					t.Fatal(err)
				}
				if runner.calls.Load() >= 3 && blocked {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("successful input did not restore mailbox delivery")
				}
				time.Sleep(time.Millisecond)
			}
			if got := runner.calls.Load(); got != 3 {
				t.Fatalf("recovery retried failed mail: %d calls", got)
			}
		})
	}
}

// Keep the node outside the recursive runtime so durable wakes cannot launch
// model work while the test controls its turn boundaries and commit.
func mailboxDeliveryFixture(t *testing.T) (*session.Store, *Session, *AgentSession, session.AgentTurnStart) {
	t.Helper()
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	childID := rootID + ":reader"
	if err := root.AdmitAgent(t.Context(), session.AgentAdmission{
		ParentAgentID: rootID, ChildAgentID: childID, Name: "reader",
		Prompt: session.RuntimePayload{Data: []byte("work")},
	}); err != nil {
		t.Fatal(err)
	}
	start, err := root.StartAgentTurn(t.Context(), childID, childID+":turn")
	if err != nil {
		t.Fatal(err)
	}
	node := &AgentSession{id: childID, parentID: rootID, name: "reader", root: root}
	node.host = &recursiveHost{session: node}
	return store, root, node, start
}

func mailboxSend(t *testing.T, root *Session, node *AgentSession, send session.MailboxSend) session.MailboxMessage {
	t.Helper()
	message, err := root.SendMailboxMessage(t.Context(), root.ID(), node.id, send)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func mailboxRunDigest(t *testing.T, node *AgentSession) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { streamText(w, "done") }))
	defer server.Close()
	services := tools.NewServices()
	t.Cleanup(services.Close)
	node.agent = agent.NewRuntime(llm.New(server.URL, "key"), "model", 1024, "system", services)
	node.agent.WorkingDir = node.root.WorkingDirectory()
	if _, err := node.RunTurn(t.Context(), "work", nil, false, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func mailboxCommit(t *testing.T, root *Session, node *AgentSession, start session.AgentTurnStart) {
	t.Helper()
	journal := node.turnJournal()
	ack := append([]int64(nil), journal.DeliveredInbox...)
	for _, item := range start.Items {
		ack = append(ack, item.Seq)
	}
	if err := root.FinishAgentTurn(t.Context(), node.id, session.AgentTurnCommit{
		TurnID: start.TurnID, Status: "succeeded", AcknowledgedInbox: ack, DeliveredMessages: journal.DeliveredMessages,
		Messages: []llm.Message{{Role: "user", Content: "work"}, {Role: "assistant", Content: "done"}},
	}); err != nil {
		t.Fatal(err)
	}
}

func mailboxStored(t *testing.T, store *session.Store, root *Session, node *AgentSession, id string) session.MailboxMessage {
	t.Helper()
	message, err := store.ReadMailboxMessage(t.Context(), root.ID(), node.id, id)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func TestMailboxHostSeparatesObservationsAndDelivery(t *testing.T) {
	store, root, node, start := mailboxDeliveryFixture(t)
	digest := mailboxSend(t, root, node, session.MailboxSend{Body: "digest content"})
	mailboxRunDigest(t, node)
	listed := mailboxSend(t, root, node, session.MailboxSend{Body: "list content"})
	result, err := node.host.messages(t.Context(), "list", map[string]any{"status": "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if rows := result.([]map[string]any); len(rows) != 2 {
		t.Fatalf("list = %+v", rows)
	}
	largeBody := strings.Repeat("read ", (session.InlineValueLimit/5)+1)
	read := mailboxSend(t, root, node, session.MailboxSend{Body: largeBody})
	result, err = node.host.messages(t.Context(), "read", map[string]any{"id": read.ID})
	if err != nil {
		t.Fatal(err)
	}
	if value := result.(map[string]any); value["body"] != largeBody || value["revision"] != read.Revision {
		t.Fatalf("read lost referenced body or revision: %+v", value)
	}
	journal := node.turnJournal()
	want := []session.MailboxReceipt{{ID: digest.ID, Revision: digest.Revision}, {ID: read.ID, Revision: read.Revision}}
	if !slices.Equal(journal.DeliveredMessages, want) {
		t.Fatalf("delivery journal = %+v, want %+v", journal.DeliveredMessages, want)
	}
	if receipt := node.messageReceipts([]string{listed.ID})[0]; receipt.Revision != listed.Revision {
		t.Fatalf("list did not establish an explicit-action observation: %+v", receipt)
	}
	mailboxCommit(t, root, node, start)
	for _, message := range []session.MailboxMessage{digest, listed, read} {
		wantStatus := "delivered"
		if message.ID == listed.ID {
			wantStatus = "pending"
		}
		if stored := mailboxStored(t, store, root, node, message.ID); stored.Status != wantStatus {
			t.Fatalf("message %s status = %s, want %s", message.ID, stored.Status, wantStatus)
		}
	}
	// A later turn can finish an already-delivered ID without carrying an old
	// observation forward; an unobserved pending ID still requires a read.
	node.turn = turnJournal{}
	if _, err := node.host.messages(t.Context(), "complete", map[string]any{"ids": []any{listed.ID}}); !errors.Is(err, session.ErrMessageChanged) {
		t.Fatalf("unobserved pending completion = %v", err)
	}
	if _, err := node.host.messages(t.Context(), "complete", map[string]any{"ids": []any{digest.ID}}); err != nil {
		t.Fatalf("later delivered completion = %v", err)
	}
}

func TestMailboxHostRejectsStaleExplicitActions(t *testing.T) {
	for _, operation := range []string{"complete", "defer"} {
		t.Run(operation, func(t *testing.T) {
			store, root, node, _ := mailboxDeliveryFixture(t)
			stable := mailboxSend(t, root, node, session.MailboxSend{Body: "stable"})
			old := mailboxSend(t, root, node, session.MailboxSend{Body: "old", UpsertKey: "updates"})
			if _, err := node.host.messages(t.Context(), "list", map[string]any{"status": "pending"}); err != nil {
				t.Fatal(err)
			}
			latest := mailboxSend(t, root, node, session.MailboxSend{Body: "latest", UpsertKey: "updates"})
			if latest.ID != old.ID || latest.Revision != old.Revision+1 {
				t.Fatalf("replacement = %+v", latest)
			}
			arguments := map[string]any{"id": old.ID, "seconds": float64(3600)}
			if operation == "complete" {
				arguments = map[string]any{"ids": []any{stable.ID, old.ID}}
			}
			if _, err := node.host.messages(t.Context(), operation, arguments); !errors.Is(err, session.ErrMessageChanged) {
				t.Fatalf("stale %s = %v", operation, err)
			}
			for _, message := range []session.MailboxMessage{stable, latest} {
				stored := mailboxStored(t, store, root, node, message.ID)
				if stored.Status != "pending" || stored.Revision != message.Revision || !stored.AvailableAt.IsZero() {
					t.Fatalf("stale %s partially mutated mail: %+v", operation, stored)
				}
			}
			if _, err := node.host.messages(t.Context(), "read", map[string]any{"id": latest.ID}); err != nil {
				t.Fatal(err)
			}
			if _, err := node.host.messages(t.Context(), operation, arguments); err != nil {
				t.Fatalf("fresh %s = %v", operation, err)
			}
			stored := mailboxStored(t, store, root, node, latest.ID)
			if operation == "complete" && stored.Status != "done" {
				t.Fatalf("fresh completion = %+v", stored)
			}
			if operation == "defer" && (stored.Status != "pending" || stored.Revision != latest.Revision+1 || stored.AvailableAt.IsZero()) {
				t.Fatalf("fresh deferral = %+v", stored)
			}
		})
	}
}

func TestMailboxHostDeferralKeepsFutureWakeAfterCommit(t *testing.T) {
	store, root, node, start := mailboxDeliveryFixture(t)
	message := mailboxSend(t, root, node, session.MailboxSend{Body: "remind me"})
	mailboxRunDigest(t, node)
	until := time.Now().Add(time.Hour).Truncate(time.Second)
	for revision := message.Revision + 1; revision <= message.Revision+2; revision++ {
		result, err := node.host.messages(t.Context(), "defer", map[string]any{"id": message.ID, "until": until.Format(time.RFC3339)})
		if err != nil {
			t.Fatal(err)
		}
		if value := result.(map[string]any); value["revision"] != revision {
			t.Fatalf("deferral omitted its new revision: %+v", value)
		}
		if receipt := node.messageReceipts([]string{message.ID})[0]; receipt.Revision != revision {
			t.Fatalf("deferral did not update explicit-action observation: %+v", receipt)
		}
	}
	journal := node.turnJournal()
	if want := (session.MailboxReceipt{ID: message.ID, Revision: message.Revision}); !slices.Equal(journal.DeliveredMessages, []session.MailboxReceipt{want}) {
		t.Fatalf("deferral created automatic receipts: %+v", journal.DeliveredMessages)
	}
	mailboxCommit(t, root, node, start)
	stored := mailboxStored(t, store, root, node, message.ID)
	if stored.Status != "pending" || stored.Revision != message.Revision+2 || !stored.AvailableAt.Equal(until) || stored.DeliveredTurnID != "" {
		t.Fatalf("commit consumed deferred revision: %+v", stored)
	}
	for _, at := range []time.Time{until.Add(-time.Second), until} {
		work, err := store.AgentWorkStatus(t.Context(), root.ID(), node.id, at)
		if err != nil || work.HasReadyMail != at.Equal(until) {
			t.Fatalf("deferred work at %s = %+v, %v", at, work, err)
		}
	}
}

func TestMailboxSteerReplacementAppearsAtLaterBoundary(t *testing.T) {
	store, root, node, start := mailboxDeliveryFixture(t)
	message := mailboxSend(t, root, node, session.MailboxSend{
		Body: "first steer", Delivery: session.MessageDeliverySteer, UpsertKey: "updates",
	})
	for _, body := range []string{"first steer", "replacement steer"} {
		if body == "replacement steer" {
			replaced := mailboxSend(t, root, node, session.MailboxSend{
				Body: body, Delivery: session.MessageDeliverySteer, UpsertKey: "updates",
			})
			if replaced.ID != message.ID || replaced.Revision != message.Revision+1 {
				t.Fatalf("coalesced message identity = %+v", replaced)
			}
		}
		steers, err := node.pullSteers(t.Context(), start.TurnID)
		if err != nil || len(steers) != 1 || !strings.Contains(steers[0].Content, body) {
			t.Fatalf("boundary %q = %+v, %v", body, steers, err)
		}
		steers, err = node.pullSteers(t.Context(), start.TurnID)
		if err != nil || len(steers) != 0 {
			t.Fatalf("unchanged revision delivered twice: %+v, %v", steers, err)
		}
	}
	want := []session.MailboxReceipt{{ID: message.ID, Revision: message.Revision}, {ID: message.ID, Revision: message.Revision + 1}}
	if got := node.turnJournal().DeliveredMessages; !slices.Equal(got, want) {
		t.Fatalf("boundary receipts = %+v, want %+v", got, want)
	}
	mailboxCommit(t, root, node, start)
	stored := mailboxStored(t, store, root, node, message.ID)
	if stored.Status != "delivered" || stored.Revision != message.Revision+1 || stored.DeliveredTurnID != start.TurnID {
		t.Fatalf("latest observed steer did not settle: %+v", stored)
	}
}
