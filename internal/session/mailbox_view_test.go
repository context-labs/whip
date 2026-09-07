package session

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMailboxInspectionBeforeRootHasOpened(t *testing.T) {
	store, root := collectionStore(t)
	page, err := store.InspectMailboxPage(t.Context(), root, root, "all", nil, 4, 4096)
	if err != nil || len(page.Items) != 0 || page.HasMore {
		t.Fatalf("new root mailbox %+v %v", page, err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM agents`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("inspection created authority: count=%d %v", count, err)
	}
}

func TestMailboxInspectionPaginationDoesNotChangeDelivery(t *testing.T) {
	store, rootID, rootAgent := newMailboxFixture(t)
	var messages []MailboxMessage
	for range 5 {
		message, err := store.SendMailboxMessage(t.Context(), rootID, rootAgent, "child", MailboxSend{Body: "mail body", Subject: "research"})
		if err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
	seen := map[string]bool{}
	var cursor, firstCursor *MailboxCursor
	for {
		page, err := store.InspectMailboxPage(t.Context(), rootID, "child", "all", cursor, 2, 4096)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(page)
		if len(raw) > 4096 || len(page.Items) > 2 {
			t.Fatal("unbounded mailbox page")
		}
		for _, item := range page.Items {
			if seen[item.ID] || len(item.Body.Inline) != 0 || item.Status != "pending" {
				t.Fatalf("invalid metadata %+v", item)
			}
			seen[item.ID] = true
			read, err := store.InspectMailboxMessage(t.Context(), rootID, "child", item.ID)
			if err != nil || string(read.Body.Inline) != "mail body" || read.Body.MediaType != "text/plain" {
				t.Fatalf("inline inspection %+v: %v", read, err)
			}
		}
		if firstCursor == nil {
			firstCursor = page.NextCursor
		}
		if !page.HasMore {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("found %d messages", len(seen))
	}
	for _, message := range messages {
		read, err := store.ReadMailboxMessage(t.Context(), rootID, "child", message.ID)
		if err != nil || read.Status != "pending" || read.Revision != message.Revision || !read.DeliveredAt.IsZero() || !read.DoneAt.IsZero() {
			t.Fatalf("inspection mutated mail: %+v %v", read, err)
		}
	}
	if _, err := store.InspectMailboxPage(t.Context(), rootID, rootAgent, "all", firstCursor, 2, 4096); !errors.Is(err, ErrCollectionChanged) {
		t.Fatalf("cross-agent cursor accepted: %v", err)
	}
	if _, err := store.InspectMailboxMessage(t.Context(), rootID, rootAgent, messages[0].ID); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("cross-agent body accepted: %v", err)
	}
	if _, err := store.CompleteMailboxMessages(t.Context(), rootID, "child", []MailboxReceipt{mailboxTestReceipt(messages[0])}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InspectMailboxPage(t.Context(), rootID, "child", "all", firstCursor, 2, 4096); !errors.Is(err, ErrCollectionChanged) {
		t.Fatalf("stale page accepted: %v", err)
	}
}

func TestMailboxInspectionReferenceAndDecimalRevision(t *testing.T) {
	store, rootID, rootAgent := newMailboxFixture(t)
	body := strings.Repeat("large body ", 1000)
	message, err := store.SendMailboxMessage(t.Context(), rootID, rootAgent, "child", MailboxSend{Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE agent_messages SET revision=9007199254740993 WHERE id=?`, message.ID); err != nil {
		t.Fatal(err)
	}
	read, err := store.InspectMailboxMessage(t.Context(), rootID, "child", message.ID)
	if err != nil || read.Body.ReferenceID == "" || len(read.Body.Digest) != 64 || len(read.Body.Inline) != 0 {
		t.Fatalf("reference %+v: %v", read, err)
	}
	raw, _ := json.Marshal(read)
	if !strings.Contains(string(raw), `"revision":"9007199254740993"`) || len(raw) > 4096 {
		t.Fatalf("invalid bounded decimal encoding: %s", raw)
	}
	data, _, err := store.ReadContent(t.Context(), read.Body.ReferenceID, rootID, "child", 0, MaxContentRead)
	if err != nil || string(data) != body {
		t.Fatalf("scoped body %v", err)
	}
	other, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.InspectMailboxMessage(t.Context(), other, "child", message.ID); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("cross-root body accepted: %v", err)
	}
}
