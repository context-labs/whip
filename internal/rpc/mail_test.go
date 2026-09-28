package rpc_test

import (
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestMailInspectionIsReadOnlyAndScopedToRecipient(t *testing.T) {
	_, client := fixture(t)
	tree := create(t, client)
	child := call[protocol.SpawnSessionResult](t, client, "sessions.spawn", protocol.SpawnSessionParams{
		Identity: protocol.RequestIdentity{ClientID: "mail-test", RequestID: "child"}, ParentID: tree.Root.ID,
		Parts: []protocol.Part{{Type: "text", Text: "Initial child work"}},
	})
	params := protocol.SendMailParams{MailID: "mail_rpc", SenderID: child.Session.ID, RecipientID: tree.Root.ID, Delivery: "next_turn", Subject: "", Body: "Only the agent's successful turn acknowledges presentation."}
	first := call[protocol.MailAdmission](t, client, "mail.send", params)
	retry := call[protocol.MailAdmission](t, client, "mail.send", params)
	if first.MailID != retry.MailID || first.Mail == nil || first.Mail.Revision != 1 {
		t.Fatalf("unstable send: %+v / %+v", first, retry)
	}
	list := call[protocol.ListMailResult](t, client, "mail.list", protocol.ListMailParams{SessionID: tree.Root.ID, Limit: 100})
	body := call[protocol.ReadMailResult](t, client, "mail.read", protocol.ReadMailParams{SessionID: tree.Root.ID, MailID: first.MailID})
	if len(list.Items) != 1 || body.Body != params.Body || body.Mail.State != "pending" {
		t.Fatalf("inspection changed delivery: %+v / %+v", list, body)
	}
	history := call[protocol.HistoryResult](t, client, "sessions.history", protocol.HistoryParams{SessionID: tree.Root.ID, Limit: 100})
	if len(history.Items) != 0 {
		t.Fatal("mail inspection started work")
	}
	var result protocol.ReadMailResult
	if err := client.Call(t.Context(), "mail.read", protocol.ReadMailParams{SessionID: child.Session.ID, MailID: first.MailID}, &result); err == nil {
		t.Fatal("sender inspected recipient's inbox")
	}
	var admitted protocol.MailAdmission
	changed := params
	changed.Body = "different"
	if err := client.Call(t.Context(), "mail.send", changed, &admitted); err == nil {
		t.Fatal("mail identity reused for changed payload")
	}
	invalid := params
	invalid.MailID = "mail_bad_time"
	invalid.AvailableAt = new("tomorrow")
	if err := client.Call(t.Context(), "mail.send", invalid, &admitted); err == nil {
		t.Fatal("invalid timestamp admitted")
	}
	unrelated := create(t, client)
	invalid = params
	invalid.MailID = "mail_cross_tree"
	invalid.RecipientID = unrelated.Root.ID
	if err := client.Call(t.Context(), "mail.send", invalid, &admitted); err == nil {
		t.Fatal("cross-tree mail admitted")
	}
	call[protocol.DeleteResult](t, client, "sessions.delete", protocol.SessionParams{SessionID: tree.Root.ID})
	deleted := call[protocol.MailAdmission](t, client, "mail.send", params)
	if deleted.Mail != nil || deleted.DeletedAt == nil {
		t.Fatalf("deleted mail recreated: %+v", deleted)
	}
}
