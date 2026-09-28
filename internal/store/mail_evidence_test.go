package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

func evidenceReference(t *testing.T, s *Store, owner session.SessionID, id, body string) session.ContentReference {
	t.Helper()
	reference, err := s.RegisterContent(t.Context(), contentReference(owner, id, body))
	if err != nil {
		t.Fatal(err)
	}
	return reference
}

func assertMailEvidence(t *testing.T, s *Store, metadata session.MailMetadata, source session.ContentReference) string {
	t.Helper()
	if metadata.EvidenceRef == nil {
		t.Fatal("missing recipient evidence reference")
	}
	reference, err := s.ContentReference(t.Context(), metadata.RecipientID, *metadata.EvidenceRef)
	if err != nil || reference.Digest != source.Digest || reference.Size != source.Size || reference.MediaType != source.MediaType {
		t.Fatalf("recipient evidence=%+v err=%v", reference, err)
	}
	if (metadata.RecipientID == source.SessionID) != (reference.ID == source.ID) {
		t.Fatal("sharing did not preserve separate ownership")
	}
	return reference.ID
}

func TestMailEvidenceSharesAcrossRelativesWithoutChargingAliases(t *testing.T) {
	s := fresh(t)
	_, parent := create(t, s, nil)
	a := controlChild(t, s, parent.ID, "a").Session
	b := controlChild(t, s, parent.ID, "b").Session
	for i, pair := range [][2]session.SessionID{{parent.ID, a.ID}, {a.ID, parent.ID}, {a.ID, b.ID}, {b.ID, b.ID}} {
		ref := evidenceReference(t, s, pair[0], fmt.Sprintf("source-%d", i), "private evidence bytes")
		before := count(t, s, "logical_writes")
		spec := session.MailSpec{ID: session.MailID(fmt.Sprintf("evidence-%d", i)), SenderID: pair[0], RecipientID: pair[1], Delivery: session.MailNextTurn, EvidenceRef: &ref.ID}
		first := sendMailTest(t, s, spec)
		assertMailEvidence(t, s, first, ref)
		if first.BodyBytes != 0 || count(t, s, "logical_writes") != before+1 {
			t.Fatal("evidence-only mail billed body bytes or another content write")
		}
		var size int64
		if err := s.db.QueryRowContext(t.Context(), "SELECT bytes FROM logical_writes WHERE source_kind='mail' AND source_id=?", spec.ID).Scan(&size); err != nil || size != 0 {
			t.Fatalf("evidence sharing charged bytes: %d %v", size, err)
		}
		refs := count(t, s, "content_references")
		if retry := sendMailTest(t, s, spec); !reflect.DeepEqual(retry, first) || count(t, s, "content_references") != refs {
			t.Fatal("send retry created an alias or changed its result")
		}
		changed := spec
		changed.EvidenceRef = &ref.Digest
		if _, err := s.SendMail(t.Context(), changed); !errors.Is(err, ErrConflict) || count(t, s, "content_references") != refs {
			t.Fatal("changed evidence escaped stable send identity", err)
		}
		if pair[0] != pair[1] {
			if _, err := s.ContentReference(t.Context(), pair[1], ref.ID); !errors.Is(err, ErrNotFound) {
				t.Fatal("sender identity leaked access to recipient", err)
			}
		}
	}
}

func TestMailEvidenceRetainsRecipientAccessAfterSenderDeletionAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, parent := create(t, s, nil)
	sender := controlChild(t, s, parent.ID, "sender").Session
	ref := evidenceReference(t, s, sender.ID, "source", "survives sender")
	spec := session.MailSpec{ID: "durable", SenderID: sender.ID, RecipientID: parent.ID, Delivery: session.MailNextTurn, EvidenceRef: &ref.ID}
	first := sendMailTest(t, s, spec)
	if err := s.DeleteSubtree(t.Context(), sender.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	if retry := sendMailTest(t, s, spec); !reflect.DeepEqual(retry, first) {
		t.Fatal("retry depended on deleted source owner")
	}
	assertMailEvidence(t, s, first, ref)
	if _, err := s.ContentReference(t.Context(), sender.ID, ref.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("sender reference survived deletion", err)
	}
	if err := s.DeleteSubtree(t.Context(), parent.ID); err != nil {
		t.Fatal("recipient cleanup left a mail-content foreign key", err)
	}
	retry, err := s.SendMail(t.Context(), spec)
	if err != nil || retry.DeletedAt == nil || retry.Mail != nil || count(t, s, "content_references") != 0 {
		t.Fatalf("deleted recipient retry=%+v err=%v", retry, err)
	}
}

func TestMailEvidenceReplacementDeferralAndHistoryKeepExactReferences(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	sender := controlChild(t, s, owner.ID, "sender").Session
	old := evidenceReference(t, s, sender.ID, "old", "old hidden body")
	latest := evidenceReference(t, s, sender.ID, "new", "new hidden body")
	spec := session.MailSpec{
		ID: "revisions", SenderID: sender.ID, RecipientID: owner.ID, Delivery: session.MailQueued,
		Body: strings.Repeat("界", 2000), EvidenceRef: &old.ID,
	}
	first := sendMailTest(t, s, spec)
	turn := claim(t, s, owner.ID).Turn
	history, err := s.History(t.Context(), owner.ID, 0, 10)
	if err != nil || len(history) != 1 || len(history[0].Parts) != 1 {
		t.Fatalf("mail history=%+v err=%v", history, err)
	}
	text := history[0].Parts[0].Text
	if !utf8.ValidString(text) || !strings.Contains(text, *first.EvidenceRef) || strings.Contains(text, "hidden body") || len(text) > 2500 || history[0].Parts[0].Type != "text" {
		t.Fatal("digest lost bounded evidence access or hydrated content")
	}
	baseline, err := json.Marshal(history[0].Parts)
	if err != nil {
		t.Fatal(err)
	}
	message, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{
		ID: "call", Role: session.Assistant,
		Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: []byte(`{"code":"1"}`)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cell, dispatch, err := s.BeginCell(t.Context(), session.CellSpec{ID: "cell", TurnID: turn.ID, CallMessageID: message.ID, CallID: "call"})
	if err != nil || !dispatch {
		t.Fatalf("cell=%+v dispatch=%v err=%v", cell, dispatch, err)
	}
	read := mailOperation(t, s, owner, cell, "read-old", "read", session.MailRead{ID: spec.ID})
	oldResult := applyMailTest(t, s, read)
	spec.EvidenceRef, spec.Body = &latest.ID, "new body"
	replaced, err := s.ReplaceMail(t.Context(), spec, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertMailEvidence(t, s, replaced.MailMetadata, latest)
	if again := applyMailTest(t, s, read); !bytes.Equal(oldResult, again) || !bytes.Contains(again, []byte(*first.EvidenceRef)) {
		t.Fatal("mail.read retry changed its immutable evidence revision")
	}
	applyMailTest(t, s, mailOperation(t, s, owner, cell, "read-new", "read", session.MailRead{ID: spec.ID}))
	refs := count(t, s, "content_references")
	op := mailOperation(t, s, owner, cell, "defer", "defer", session.MailDefer{Receipt: replaced.MailReceipt, AvailableAt: time.Now().Add(time.Hour)})
	deferred := applyMailTest(t, s, op)
	if !bytes.Equal(deferred, applyMailTest(t, s, op)) || count(t, s, "content_references") != refs {
		t.Fatal("deferral reshared content")
	}
	current := mailStateTest(t, s, owner.ID, spec.ID, session.MailPending, 3)
	if *current.EvidenceRef != *replaced.EvidenceRef {
		t.Fatal("deferral lost recipient alias")
	}
	observed := count(t, s, "turn_mail_observations")
	raw, err := s.ReadHistoryMessage(t.Context(), owner.ID, history[0].ID, 0, 65536)
	if err != nil || !bytes.Equal(raw.Data, baseline) {
		t.Fatalf("old history evidence changed: %s %v", raw.Data, err)
	}
	metadata, err := s.HistoryMetadata(t.Context(), owner.ID, 0, 1, 10)
	if err != nil || len(metadata.Items) != 1 || metadata.Items[0].PartsBytes != int64(len(baseline)) {
		t.Fatalf("history metadata=%+v err=%v", metadata, err)
	}
	found, err := s.SearchHistory(t.Context(), owner.ID, 0, 1, *first.EvidenceRef, 10)
	if err != nil || len(found.Matches) != 1 || count(t, s, "turn_mail_observations") != observed {
		t.Fatalf("history evidence search=%+v err=%v", found, err)
	}
	assertMailEvidence(t, s, first, old)
	settleMailCell(t, s, cell)
	finishMailTest(t, s, turn.ID, session.Succeeded)
	mailStateTest(t, s, owner.ID, spec.ID, session.MailPending, 3)
}

func TestMailEvidenceOwnershipAndQuotaDenialsAreAtomic(t *testing.T) {
	for _, kind := range []string{"foreign", "digest", "recipient alias", "quota"} {
		t.Run(kind, func(t *testing.T) {
			s := fresh(t)
			_, sender := create(t, s, nil)
			recipient := controlChild(t, s, sender.ID, "recipient").Session
			ref := evidenceReference(t, s, sender.ID, "source", "bytes")
			foreign := evidenceReference(t, s, recipient.ID, "foreign", "secret")
			id, want := ref.ID, ErrNotFound
			switch kind {
			case "foreign":
				id = foreign.ID
			case "digest":
				id = ref.Digest
			case "recipient alias":
				first := sendMailTest(t, s, session.MailSpec{ID: "first", SenderID: sender.ID, RecipientID: recipient.ID, Delivery: session.MailNextTurn, EvidenceRef: &ref.ID})
				id = *first.EvidenceRef
			case "quota":
				for i := range 16 {
					full := contentReference(recipient.ID, fmt.Sprintf("full-%d", i), "same large body")
					full.Size = session.MaxContentBytes
					if i == 15 {
						full.Size -= foreign.Size
						full.Digest = strings.Repeat("a", 64)
					}
					if _, err := s.RegisterContent(t.Context(), full); err != nil {
						t.Fatal(err)
					}
				}
				want = ErrLimit
			}
			beforeRefs, beforeMail, beforeWrites := count(t, s, "content_references"), count(t, s, "mail"), count(t, s, "logical_writes")
			spec := session.MailSpec{ID: "denied", SenderID: sender.ID, RecipientID: recipient.ID, Delivery: session.MailNextTurn, EvidenceRef: &id}
			if _, err := s.SendMail(t.Context(), spec); !errors.Is(err, want) {
				t.Fatalf("send denial=%v want=%v", err, want)
			}
			if count(t, s, "content_references") != beforeRefs || count(t, s, "mail") != beforeMail || count(t, s, "logical_writes") != beforeWrites {
				t.Fatal("denied send leaked an alias, mail identity, or write charge")
			}
		})
	}
}

func TestMailEvidenceFaultsRollBackSharingAndOperationSettlement(t *testing.T) {
	for _, stage := range []string{"content", "revision", "settlement", "write charge"} {
		t.Run(stage, func(t *testing.T) {
			s := fresh(t)
			owner, cell := operationCell(t, s)
			recipient := controlChild(t, s, owner.ID, "recipient").Session
			ref := evidenceReference(t, s, owner.ID, "source", "bytes")
			op := mailOperation(t, s, owner, cell, "atomic", "send", session.MailSend{RecipientID: recipient.ID, Delivery: session.MailNextTurn, EvidenceRef: &ref.ID})
			triggers := map[string]string{
				"content":      "BEFORE INSERT ON content_references",
				"revision":     "BEFORE INSERT ON mail_revisions",
				"settlement":   "BEFORE UPDATE ON operations WHEN NEW.state='succeeded'",
				"write charge": "BEFORE INSERT ON logical_writes WHEN NEW.source_kind='mail'",
			}
			execTest(t, s, "CREATE TRIGGER fail_evidence "+triggers[stage]+" BEGIN SELECT RAISE(ABORT,'fault'); END")
			beforeWrites := count(t, s, "logical_writes")
			if _, err := s.ApplyMailOperation(t.Context(), op.ID); err == nil {
				t.Fatal("injected failure ignored")
			}
			pending, err := s.Operation(t.Context(), op.ID)
			if err != nil || pending.State != session.OperationReady || count(t, s, "mail") != 0 || count(t, s, "mail_revisions") != 0 || count(t, s, "content_references") != 1 || count(t, s, "logical_writes") != beforeWrites {
				t.Fatalf("partial evidence send escaped: %+v %v", pending, err)
			}
			execTest(t, s, "DROP TRIGGER fail_evidence")
			first := applyMailTest(t, s, op)
			if retry := applyMailTest(t, s, op); !bytes.Equal(first, retry) || count(t, s, "content_references") != 2 {
				t.Fatal("settled operation retry reshared evidence")
			}
		})
	}
}

func TestMailEvidenceRevocationBeforeDispatchCreatesNoAlias(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	recipient := controlChild(t, s, owner.ID, "recipient").Session
	ref := evidenceReference(t, s, owner.ID, "source", "bytes")
	op := mailOperation(t, s, owner, cell, "revoked", "send", session.MailSend{RecipientID: recipient.ID, Delivery: session.MailQueued, EvidenceRef: &ref.ID})
	if _, err := s.RevokeGrant(t.Context(), "grant_revoked"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyMailOperation(t.Context(), op.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("revoked mail authority dispatched", err)
	}
	if count(t, s, "content_references") != 1 || count(t, s, "mail") != 0 {
		t.Fatal("revoked mail operation granted content access")
	}
}

func TestMailEvidenceReplacementRollsBackAliasWithRevision(t *testing.T) {
	s := fresh(t)
	_, sender := create(t, s, nil)
	recipient := controlChild(t, s, sender.ID, "recipient").Session
	old := evidenceReference(t, s, sender.ID, "old", "old body")
	latest := evidenceReference(t, s, sender.ID, "new", "new body")
	spec := session.MailSpec{ID: "replace", SenderID: sender.ID, RecipientID: recipient.ID, Delivery: session.MailNextTurn, EvidenceRef: &old.ID}
	first := sendMailTest(t, s, spec)
	spec.EvidenceRef = &latest.ID
	for _, stage := range []string{"BEFORE INSERT ON mail_revisions", "BEFORE UPDATE ON mail", "BEFORE INSERT ON logical_writes"} {
		execTest(t, s, "CREATE TRIGGER fail_replace "+stage+" BEGIN SELECT RAISE(ABORT,'fault'); END")
		if _, err := s.ReplaceMail(t.Context(), spec, 1); err == nil {
			t.Fatal("replacement fault ignored")
		}
		current := mailStateTest(t, s, recipient.ID, spec.ID, session.MailPending, 1)
		if count(t, s, "content_references") != 3 || count(t, s, "mail_revisions") != 1 || *current.EvidenceRef != *first.EvidenceRef {
			t.Fatal("replacement escaped partial alias/revision")
		}
		execTest(t, s, "DROP TRIGGER fail_replace")
	}
	replaced, err := s.ReplaceMail(t.Context(), spec, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertMailEvidence(t, s, replaced.MailMetadata, latest)
	assertMailEvidence(t, s, first, old)
}
