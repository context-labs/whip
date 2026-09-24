package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestMailboxStorageFailureCannotMasqueradeAsEmptyMail(t *testing.T) {
	for _, failure := range []string{"closed database", "missing messages", "corrupt revision"} {
		t.Run(failure, func(t *testing.T) {
			store, root, parent := newMailboxFixture(t)
			message, err := store.SendMailboxMessage(t.Context(), root, parent, "child", MailboxSend{Body: "research result"})
			if err != nil {
				t.Fatal(err)
			}
			receipt := mailboxTestReceipt(message)
			actions := map[string]func(context.Context) error{
				"list": func(ctx context.Context) error {
					_, err := store.ListMailboxMessages(ctx, root, "child", "pending", "", 10)
					return err
				},
				"read": func(ctx context.Context) error {
					_, err := store.ReadMailboxMessage(ctx, root, "child", message.ID)
					return err
				},
				"complete": func(ctx context.Context) error {
					_, err := store.CompleteMailboxMessages(ctx, root, "child", []MailboxReceipt{receipt})
					return err
				},
				"defer": func(ctx context.Context) error {
					_, err := store.DeferMailboxMessage(ctx, root, "child", receipt, time.Now().Add(time.Hour))
					return err
				},
				"digest": func(ctx context.Context) error {
					_, err := store.ReadMailboxDigest(ctx, root, "child", time.Now())
					return err
				},
				"inspect page": func(ctx context.Context) error {
					_, err := store.InspectMailboxPage(ctx, root, "child", "pending", nil, 10, 4096)
					return err
				},
				"inspect message": func(ctx context.Context) error {
					_, err := store.InspectMailboxMessage(ctx, root, "child", message.ID)
					return err
				},
			}
			if failure != "corrupt revision" {
				actions["send"] = func(ctx context.Context) error {
					_, err := store.SendMailboxMessage(ctx, root, parent, "child", MailboxSend{Body: "more work"})
					return err
				}
				actions["summary"] = func(ctx context.Context) error { _, err := store.MailboxSummary(ctx, root, "child"); return err }
				actions["readiness"] = func(ctx context.Context) error {
					_, err := store.AgentWorkStatus(ctx, root, "child", time.Now())
					return err
				}
			}
			switch failure {
			case "closed database":
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			case "missing messages":
				exec(t, store, "DROP TABLE agent_messages")
			default:
				exec(t, store, `UPDATE agent_messages SET revision='not an integer'`)
			}
			for name, action := range actions {
				t.Run(name, func(t *testing.T) {
					if err := action(t.Context()); err == nil {
						t.Fatal("storage failure returned success")
					}
				})
			}
		})
	}
}

func TestMailboxSendWriteFailurePreservesRecipientAndContentGrants(t *testing.T) {
	for _, failure := range []struct{ name, mutation, table, condition string }{
		{"message", "INSERT", "agent_messages", ""},
		{"content object", "INSERT", "content_objects", ""},
		{"content reference", "INSERT", "content_references", ""},
		{"content grant", "INSERT", "content_grants", ""},
		{"queued event", "INSERT", "events", "NEW.kind='message.queued'"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			store, root, parent := newMailboxFixture(t)
			before := inputRootSnapshot(t, store, root)
			rejectSessionWrite(t, store, failure.mutation, failure.table, failure.condition)
			send := MailboxSend{Body: strings.Repeat("report ", 2000)}
			_, err := store.SendMailboxMessage(t.Context(), root, parent, "child", send)
			requireSessionWriteFailure(t, err)
			requireRootUnchanged(t, store, root, before)
			summary, err := store.MailboxSummary(t.Context(), root, "child")
			if err != nil || summary.UnreadCount != 0 {
				t.Fatalf("failed send became visible: %+v %v", summary, err)
			}
			var grants int
			if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM content_grants WHERE root_id=?`, root).Scan(&grants); err != nil || grants != 0 {
				t.Fatalf("failed send leaked grants=%d %v", grants, err)
			}
			exec(t, store, "DROP TRIGGER reject_session_write")
			message, err := store.SendMailboxMessage(t.Context(), root, parent, "child", send)
			if err != nil {
				t.Fatal(err)
			}
			got, err := store.ReadMailboxMessage(t.Context(), root, "child", message.ID)
			if err != nil || got.Body.ReferenceID == "" {
				t.Fatalf("retry body reference: %q %v", got.Body.ReferenceID, err)
			}
			data, _, err := store.ReadContent(t.Context(), got.Body.ReferenceID, root, "child", 0, len(send.Body))
			if err != nil || string(data) != send.Body {
				t.Fatalf("retry body mismatch: bytes=%d %v", len(data), err)
			}
		})
	}
}

func TestMailboxReplacementFailurePreservesRevisionAndBody(t *testing.T) {
	for _, failure := range []struct{ name, mutation, table, condition string }{
		{"message row", "UPDATE", "agent_messages", ""},
		{"replacement event", "INSERT", "events", "NEW.kind='message.updated'"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			store, root, parent := newMailboxFixture(t)
			original, err := store.SendMailboxMessage(t.Context(), root, parent, "child", MailboxSend{Body: "original", UpsertKey: "watch:plan"})
			if err != nil {
				t.Fatal(err)
			}
			rejectSessionWrite(t, store, failure.mutation, failure.table, failure.condition)
			_, err = store.SendMailboxMessage(t.Context(), root, parent, "child", MailboxSend{Body: "replacement", UpsertKey: "watch:plan"})
			requireSessionWriteFailure(t, err)
			saved, err := store.ReadMailboxMessage(t.Context(), root, "child", original.ID)
			if err != nil || saved.Revision != original.Revision || string(saved.Body.Inline) != "original" {
				t.Fatalf("failed replacement changed message: %+v %v", saved, err)
			}
			exec(t, store, "DROP TRIGGER reject_session_write")
			replacement, err := store.SendMailboxMessage(t.Context(), root, parent, "child", MailboxSend{Body: "replacement", UpsertKey: "watch:plan"})
			if err != nil || replacement.ID != original.ID || replacement.Revision != original.Revision+1 {
				t.Fatalf("retry replaced wrong revision: %+v %v", replacement, err)
			}
		})
	}
}

func TestMailboxSummaryAndByteBoundedPagesPreserveAllMessages(t *testing.T) {
	store, root, parent := newMailboxFixture(t)
	for range 12 {
		if _, err := store.SendMailboxMessage(t.Context(), root, parent, "child", MailboxSend{Body: strings.Repeat("body ", 100), Subject: strings.Repeat("s", 200)}); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := store.MailboxSummary(t.Context(), root, "child")
	if err != nil || summary.UnreadCount != 12 || summary.OldestAt == "" || summary.NewestAt == "" || !reflect.DeepEqual(summary.Senders, []string{parent}) {
		t.Fatalf("summary %+v %v", summary, err)
	}
	var cursor *MailboxCursor
	seen := map[string]bool{}
	pages := 0
	for {
		page, err := store.InspectMailboxPage(t.Context(), root, "child", "all", cursor, 128, 4096)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(page)
		if err != nil || len(raw) > 4096 || len(page.Items) == 0 {
			t.Fatalf("invalid page %+v bytes=%d %v", page, len(raw), err)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate %s", item.ID)
			}
			seen[item.ID] = true
		}
		pages++
		if !page.HasMore {
			break
		}
		if page.NextCursor == nil || pages > 12 {
			t.Fatalf("no continuation progress %+v", page)
		}
		cursor = page.NextCursor
	}
	if len(seen) != 12 || pages < 2 {
		t.Fatalf("read %d messages in %d pages", len(seen), pages)
	}
	after, err := store.MailboxSummary(t.Context(), root, "child")
	if err != nil || !reflect.DeepEqual(summary, after) {
		t.Fatalf("inspection changed unread summary %+v -> %+v %v", summary, after, err)
	}
}

func TestMailboxInspectionRejectsOversizedOrUnscopedData(t *testing.T) {
	store, root, parent := newMailboxFixture(t)
	message, err := store.SendMailboxMessage(t.Context(), root, parent, "child", MailboxSend{Body: "data"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.InspectMailboxMessage(t.Context(), root, parent, message.ID); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("wrong recipient: %v", err)
	}
	if _, err := store.InspectMailboxMessage(t.Context(), root, "child", ""); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("missing message id: %v", err)
	}
	exec(t, store, `UPDATE agent_messages SET body_inline=zeroblob(?)`, InlineValueLimit+1)
	if _, err := store.InspectMailboxMessage(t.Context(), root, "child", message.ID); err == nil {
		t.Fatal("oversized inline body accepted")
	}
	exec(t, store, `UPDATE agent_messages SET subject=?`, strings.Repeat("x", 5000))
	if _, err := store.InspectMailboxPage(t.Context(), root, "child", "all", nil, 10, 4096); err == nil {
		t.Fatal("oversized metadata produced a non-progressing page")
	}
}

func TestMailboxInvalidRequestsDoNotChangeUnreadMail(t *testing.T) {
	store, root, parent := newMailboxFixture(t)
	message, err := store.SendMailboxMessage(t.Context(), root, parent, "child", MailboxSend{Body: "keep pending"})
	if err != nil {
		t.Fatal(err)
	}
	before := inputRootSnapshot(t, store, root)
	for name, action := range map[string]func(context.Context) error{
		"send to self": func(ctx context.Context) error {
			_, err := store.SendMailboxMessage(ctx, root, parent, parent, MailboxSend{Body: "message"})
			return err
		},
		"blank body": func(ctx context.Context) error {
			_, err := store.SendMailboxMessage(ctx, root, parent, "child", MailboxSend{Body: "  "})
			return err
		},
		"oversized subject": func(ctx context.Context) error {
			_, err := store.SendMailboxMessage(ctx, root, parent, "child", MailboxSend{Body: "text", Subject: strings.Repeat("s", maxMailboxSubject+1)})
			return err
		},
		"unowned evidence": func(ctx context.Context) error {
			_, err := store.SendMailboxMessage(ctx, root, parent, "child", MailboxSend{EvidenceReferenceID: "missing"})
			return err
		},
		"invalid list limit": func(ctx context.Context) error {
			_, err := store.ListMailboxMessages(ctx, root, "child", "all", "", 101)
			return err
		},
		"invalid list status": func(ctx context.Context) error {
			_, err := store.ListMailboxMessages(ctx, root, "child", "invented", "", 10)
			return err
		},
		"missing read coordinates": func(ctx context.Context) error {
			_, err := store.ReadMailboxMessage(ctx, root, "", message.ID)
			return err
		},
		"empty completion": func(ctx context.Context) error {
			_, err := store.CompleteMailboxMessages(ctx, root, "child", nil)
			return err
		},
		"negative revision": func(ctx context.Context) error {
			_, err := store.CompleteMailboxMessages(ctx, root, "child", []MailboxReceipt{{ID: message.ID, Revision: -1}})
			return err
		},
		"missing deferral coordinates": func(ctx context.Context) error {
			_, err := store.DeferMailboxMessage(ctx, root, "child", MailboxReceipt{}, time.Now())
			return err
		},
		"zero deferral time": func(ctx context.Context) error {
			_, err := store.DeferMailboxMessage(ctx, root, "child", mailboxTestReceipt(message), time.Time{})
			return err
		},
		"missing readiness coordinates": func(ctx context.Context) error {
			_, err := store.AgentWorkStatus(ctx, root, "", time.Now())
			return err
		},
		"missing digest coordinates": func(ctx context.Context) error {
			_, err := store.ReadMailboxDigest(ctx, root, "", time.Now())
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(t.Context()); err == nil {
				t.Fatal("invalid request accepted")
			}
			requireRootUnchanged(t, store, root, before)
		})
	}
	saved, err := store.ReadMailboxMessage(t.Context(), root, "child", message.ID)
	if err != nil || saved.Status != "pending" || saved.Revision != message.Revision {
		t.Fatalf("invalid requests changed message %+v %v", saved, err)
	}
}

func TestSiblingDigestPreservesUnicodeAndEvidenceAccess(t *testing.T) {
	store, root, parent := newMailboxFixture(t)
	admitTestChild(t, store, root, parent, "sibling")
	body := strings.Repeat("界", MailboxExcerptBytes/3+2)
	evidence, err := store.StoreContent(t.Context(), ContentGrant{RootID: root, AgentID: "sibling", Scope: ContentGrantAgent}, RuntimePayload{Data: []byte("evidence")})
	if err != nil {
		t.Fatal(err)
	}
	message, err := store.SendMailboxMessage(t.Context(), root, "sibling", "child", MailboxSend{Body: body, EvidenceReferenceID: evidence.ReferenceID})
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(message.Excerpt) || len(message.Excerpt) > MailboxExcerptBytes {
		t.Fatalf("invalid excerpt bytes=%d", len(message.Excerpt))
	}
	digest, err := store.ReadMailboxDigest(t.Context(), root, "child", time.Now())
	if err != nil || len(digest.Pending) != 1 || digest.Relationships["sibling"] != "sibling" {
		t.Fatalf("sibling digest %+v %v", digest, err)
	}
	data, _, err := store.ReadContent(t.Context(), evidence.ReferenceID, root, "child", 0, 100)
	if err != nil || string(data) != "evidence" {
		t.Fatalf("evidence not promoted to recipient: %q %v", data, err)
	}
	if _, err := store.SendMailboxMessage(t.Context(), root, "sibling", "child", MailboxSend{EvidenceReferenceID: evidence.ReferenceID}); err != nil {
		t.Fatalf("already shared evidence failed: %v", err)
	}
}
