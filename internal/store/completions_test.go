package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

func completionReference(t *testing.T, value session.Completion) session.ContentReference {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return session.ContentReference{ID: "completion_" + string(value.TurnID), SessionID: value.ParentID, Digest: hex.EncodeToString(digest[:]), Size: int64(len(raw)), MediaType: "application/json"}
}

func pendingCompletionTest(t *testing.T, s *Store, parent, child session.SessionID, turn session.TurnID) session.Completion {
	t.Helper()
	result, err := s.PendingCompletion(t.Context(), parent, child, turn)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func publishCompletionTest(t *testing.T, s *Store, value session.Completion) session.MailMetadata {
	t.Helper()
	result, err := s.PublishCompletion(t.Context(), value.ParentID, value.ChildID, value.TurnID, completionReference(t, value))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func finishCompletionTest(t *testing.T, s *Store, parent, child session.SessionID, text string) session.Completion {
	t.Helper()
	turn := claim(t, s, child).Turn
	draft := session.MessageDraft{ID: session.MessageID("message_" + string(turn.ID)), Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: text}}}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{draft}); err != nil {
		t.Fatal(err)
	}
	return pendingCompletionTest(t, s, parent, child, turn.ID)
}

func TestCompletionReservationRollsBackAndCountsDeletedPendingSources(t *testing.T) {
	s := fresh(t)
	_, parent := create(t, s, []session.ResourceLimit{{Kind: session.ResourceDescendants, Limit: new(int64(256))}})
	execTest(t, s, `CREATE TRIGGER fail_completion_input BEFORE INSERT ON inputs BEGIN SELECT RAISE(ABORT,'initial input failure'); END`)
	request := ChildRequest{ParentID: parent.ID, Parts: []session.Part{{Type: "text", Text: "work"}}}
	if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "rollback"}, request); err == nil {
		t.Fatal("spawn ignored injected failure")
	}
	if count(t, s, "sessions") != 1 || count(t, s, "completion_slots") != 0 || count(t, s, "receipts") != 0 {
		t.Fatal("failed input left a child or reservation")
	}
	execTest(t, s, "DROP TRIGGER fail_completion_input")
	empty := controlChild(t, s, parent.ID, "empty")
	if err := s.DeleteSubtree(t.Context(), empty.Session.ID); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "completion_slots") != 0 {
		t.Fatal("empty deleted slot retained")
	}
	var retained session.Completion
	for i := range session.MaxCompletionSlots {
		child := controlChild(t, s, parent.ID, fmt.Sprintf("child_%d", i))
		retained = finishCompletionTest(t, s, parent.ID, child.Session.ID, "done")
		if err := s.DeleteSubtree(t.Context(), child.Session.ID); err != nil {
			t.Fatal(err)
		}
	}
	if count(t, s, "sessions") != 1 || count(t, s, "completion_slots") != session.MaxCompletionSlots {
		t.Fatal("deleted pending sources did not retain bounded slots")
	}
	inputs, receipts := count(t, s, "inputs"), count(t, s, "receipts")
	if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "full"}, request); !errors.Is(err, ErrLimit) {
		t.Fatalf("full slots admission: %v", err)
	}
	if count(t, s, "inputs") != inputs || count(t, s, "receipts") != receipts || count(t, s, "sessions") != 1 {
		t.Fatal("denied reservation partially admitted child")
	}
	publishCompletionTest(t, s, retained)
	if count(t, s, "completion_slots") != session.MaxCompletionSlots-1 {
		t.Fatal("publication did not release deleted source slot")
	}
	child := controlChild(t, s, parent.ID, "full")
	retry, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "full"}, ChildRequest{ParentID: parent.ID, Parts: []session.Part{{Type: "text", Text: "full"}}})
	if err != nil || retry.Session.ID != child.Session.ID || count(t, s, "completion_slots") != session.MaxCompletionSlots {
		t.Fatalf("spawn retry reserved again: %+v %v", retry, err)
	}
	if err := s.DeleteSubtree(t.Context(), parent.ID); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "completion_slots") != 0 || count(t, s, "content_references") != 0 {
		t.Fatal("parent deletion retained report ownership")
	}
}

func TestCompletionFinishAtomicAndTerminalReplayDoesNotRecreate(t *testing.T) {
	s := fresh(t)
	_, parent := create(t, s, nil)
	child := controlChild(t, s, parent.ID, "child")
	turn := claim(t, s, child.Session.ID).Turn
	draft := session.MessageDraft{ID: "answer", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "answer"}}}
	execTest(t, s, `CREATE TRIGGER fail_completion_capture BEFORE UPDATE ON completion_slots WHEN NEW.turn_id IS NOT NULL BEGIN SELECT RAISE(ABORT,'capture failure'); END`)
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{draft}); err == nil {
		t.Fatal("finish ignored report failure")
	}
	current, err := s.Turn(t.Context(), turn.ID)
	if err != nil || current.State != session.Running || count(t, s, "messages") != 1 || count(t, s, "turn_permits") != 1 {
		t.Fatalf("partial settlement: %+v %v", current, err)
	}
	execTest(t, s, "DROP TRIGGER fail_completion_capture")
	writes := count(t, s, "logical_writes")
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{draft}); err != nil {
		t.Fatal(err)
	}
	value := pendingCompletionTest(t, s, parent.ID, child.Session.ID, turn.ID)
	if value.InputID == nil || *value.InputID != child.Admission.Input.ID || value.MessageID == nil || *value.MessageID != draft.ID || value.Text != "answer" || value.TextBytes != 6 || value.State != session.Succeeded || value.Mode != session.ReportNotice {
		t.Fatalf("wrong exact evidence: %+v", value)
	}
	publishCompletionTest(t, s, value)
	for range 2 {
		if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{draft}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PendingCompletion(t.Context(), parent.ID, child.Session.ID, turn.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("terminal retry recreated report: %v", err)
	}
	if _, err := s.PublishCompletion(t.Context(), parent.ID, child.Session.ID, turn.ID, completionReference(t, value)); !errors.Is(err, ErrConflict) {
		t.Fatalf("published token replayed: %v", err)
	}
	if count(t, s, "mail") != 1 || count(t, s, "content_references") != 1 || count(t, s, "logical_writes") != writes {
		t.Fatal("completion duplicated records or charged derived writes")
	}
	// Roots use the same terminal path, but have no completion recipient.
	rootTurn := claim(t, s, parent.ID).Turn
	if _, err := s.Finish(t.Context(), rootTurn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	items, err := s.CompletionCandidates(t.Context(), "", 100)
	if err != nil || len(items) != 0 {
		t.Fatalf("root emitted a report: %+v %v", items, err)
	}
}

func TestCompletionModeCapturedByTurnAndSuppressedSuccessPreservesFailure(t *testing.T) {
	for _, mode := range []session.ReportMode{session.ReportNotice, session.ReportInline, session.ReportMessage} {
		for _, state := range []session.TurnState{session.Succeeded, session.Failed, session.Cancelled, session.Interrupted} {
			t.Run(string(mode)+"/"+string(state), func(t *testing.T) {
				s := fresh(t)
				_, parent := create(t, s, nil)
				child, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child"}, ChildRequest{ParentID: parent.ID, Overrides: session.ConfigPatch{ReportMode: &mode}, Parts: []session.Part{{Type: "text", Text: "work"}}})
				if err != nil {
					t.Fatal(err)
				}
				turn := claim(t, s, child.Session.ID).Turn
				if _, err := s.UpdateConfiguration(t.Context(), child.Session.ID, child.Session.ConfigRevision, session.ConfigPatch{ReportMode: new(session.ReportMessage)}); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Finish(t.Context(), turn.ID, state, nil, nil); err != nil {
					t.Fatal(err)
				}
				items, err := s.PendingCompletions(t.Context(), parent.ID, "", 100)
				if err != nil {
					t.Fatal(err)
				}
				if mode == session.ReportMessage && state == session.Succeeded {
					if len(items) != 0 {
						t.Fatal("message success emitted report")
					}
				} else if len(items) != 1 || items[0].Mode != mode || items[0].State != state {
					t.Fatalf("finish used current config: %+v", items)
				}
			})
		}
	}
	s := fresh(t)
	_, parent := create(t, s, nil)
	child, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child"}, ChildRequest{ParentID: parent.ID, Overrides: session.ConfigPatch{ReportMode: new(session.ReportMessage)}, Parts: []session.Part{{Type: "text", Text: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	first := claim(t, s, child.Session.ID).Turn
	failure := "failed"
	if _, err := s.Finish(t.Context(), first.ID, session.Failed, &failure, nil); err != nil {
		t.Fatal(err)
	}
	submit(t, s, child.Session.ID, "next")
	next := claim(t, s, child.Session.ID).Turn
	if _, err := s.Finish(t.Context(), next.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if value := pendingCompletionTest(t, s, parent.ID, child.Session.ID, first.ID); value.Failure == nil || *value.Failure != failure {
		t.Fatal("suppressed success removed previous failure")
	}
}

func TestCompletionPublicationAtomicExactAndCoalesced(t *testing.T) {
	s := fresh(t)
	_, parent := create(t, s, nil)
	child := controlChild(t, s, parent.ID, "child")
	first := finishCompletionTest(t, s, parent.ID, child.Session.ID, "first")
	submit(t, s, child.Session.ID, "second")
	second := finishCompletionTest(t, s, parent.ID, child.Session.ID, "second")
	if _, err := s.PublishCompletion(t.Context(), parent.ID, child.Session.ID, first.TurnID, completionReference(t, first)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale snapshot published: %v", err)
	}
	for _, field := range []string{"owner", "identity", "type", "size", "digest"} {
		ref := completionReference(t, second)
		switch field {
		case "owner":
			ref.SessionID = child.Session.ID
		case "identity":
			ref.ID = "wrong"
		case "type":
			ref.MediaType = "text/plain"
		case "size":
			ref.Size++
		case "digest":
			ref.Digest = strings.Repeat("0", 64)
		}
		if _, err := s.PublishCompletion(t.Context(), parent.ID, child.Session.ID, second.TurnID, ref); !errors.Is(err, ErrConflict) {
			t.Fatalf("%s mismatch admitted: %v", field, err)
		}
	}
	execTest(t, s, `CREATE TRIGGER fail_completion_publish BEFORE UPDATE ON completion_slots WHEN NEW.turn_id IS NULL BEGIN SELECT RAISE(ABORT,'clear failure'); END`)
	if _, err := s.PublishCompletion(t.Context(), parent.ID, child.Session.ID, second.TurnID, completionReference(t, second)); err == nil {
		t.Fatal("publication ignored slot failure")
	}
	if count(t, s, "mail") != 0 || count(t, s, "content_references") != 0 || count(t, s, "content_bodies") != 0 {
		t.Fatal("publication partially committed")
	}
	if value := pendingCompletionTest(t, s, parent.ID, child.Session.ID, second.TurnID); !reflect.DeepEqual(value, second) {
		t.Fatal("rollback changed evidence")
	}
	execTest(t, s, "DROP TRIGGER fail_completion_publish")
	published := publishCompletionTest(t, s, second)
	// A recipient deferral changes a mail revision; publication preserves it.
	deferred := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	if err := s.write(t.Context(), func(tx *sql.Tx) error {
		current, err := readMail(t.Context(), tx, parent.ID, published.ID)
		if err != nil {
			return err
		}
		_, err = replaceMail(t.Context(), tx, session.MailSpec{ID: current.ID, RecipientID: parent.ID, Delivery: session.MailQueued, Subject: current.Subject, Body: current.Body, AvailableAt: &deferred}, current)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	parentTurn := claim(t, s, parent.ID)
	history, err := s.History(t.Context(), parent.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	submit(t, s, child.Session.ID, "third")
	third := finishCompletionTest(t, s, parent.ID, child.Session.ID, "third")
	replacement := publishCompletionTest(t, s, third)
	if replacement.ID != published.ID || replacement.Revision != 3 || !replacement.AvailableAt.Equal(deferred) {
		t.Fatalf("did not preserve pending identity/deferral: %+v", replacement)
	}
	after, err := s.History(t.Context(), parent.ID, 0, 100)
	if err != nil || !reflect.DeepEqual(after, history) {
		t.Fatal("replacement changed observed immutable revision", err)
	}
	if _, err := s.Finish(t.Context(), parentTurn.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	mailStateTest(t, s, parent.ID, published.ID, session.MailPending, 3)
	// Only observing the new revision permits successful acknowledgement.
	current := claim(t, s, parent.ID)
	if current.Input != nil {
		t.Fatal("completion mail invented an input")
	}
	if _, err := s.Finish(t.Context(), current.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	mailStateTest(t, s, parent.ID, published.ID, session.MailDelivered, 3)
	submit(t, s, child.Session.ID, "fourth")
	fourth := finishCompletionTest(t, s, parent.ID, child.Session.ID, "fourth")
	freshMail := publishCompletionTest(t, s, fourth)
	if freshMail.ID == published.ID || freshMail.Revision != 1 {
		t.Fatal("delivered completion identity was replaced")
	}
	for _, value := range []session.Completion{second, third, fourth} {
		if _, err := s.ContentReference(t.Context(), parent.ID, "completion_"+string(value.TurnID)); err != nil {
			t.Fatal("old revision evidence disappeared", err)
		}
	}
}

func TestCompletionSettlementSurvivesPublicationPressure(t *testing.T) {
	for _, pressure := range []string{"mail", "content"} {
		t.Run(pressure, func(t *testing.T) {
			s := fresh(t)
			_, parent := create(t, s, nil)
			child := controlChild(t, s, parent.ID, "child")
			if pressure == "content" {
				for i := range 16 {
					ref := contentReference(parent.ID, fmt.Sprintf("full_%d", i), "same")
					ref.Size = session.MaxContentBytes
					if _, err := s.RegisterContent(t.Context(), ref); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				if err := s.write(t.Context(), func(tx *sql.Tx) error {
					for i := range session.MaxPendingMail {
						spec := session.MailSpec{ID: session.MailID(fmt.Sprintf("full_%d", i)), RecipientID: parent.ID, Delivery: session.MailQueued, Body: "occupied"}
						if _, err := tx.ExecContext(t.Context(), "INSERT INTO mail VALUES (?,'state',?,?,?,1,'pending',?,NULL)", spec.ID, strconv.Itoa(i), parent.ID, "seed", now()); err != nil {
							return err
						}
						if err := insertMailRevision(t.Context(), tx, spec, 1, now()); err != nil {
							return err
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			writes := count(t, s, "logical_writes")
			value := finishCompletionTest(t, s, parent.ID, child.Session.ID, "settled despite pressure")
			before := count(t, s, "content_references")
			if _, err := s.PublishCompletion(t.Context(), parent.ID, child.Session.ID, value.TurnID, completionReference(t, value)); !errors.Is(err, ErrLimit) {
				t.Fatalf("expected deferred publication: %v", err)
			}
			if current := pendingCompletionTest(t, s, parent.ID, child.Session.ID, value.TurnID); !reflect.DeepEqual(current, value) {
				t.Fatal("pressure damaged snapshot")
			}
			if count(t, s, "content_references") != before || count(t, s, "logical_writes") != writes {
				t.Fatal("failed publication partially charged/registered")
			}
			terminal, err := s.Turn(t.Context(), value.TurnID)
			if err != nil || terminal.State != session.Succeeded {
				t.Fatalf("quota blocked turn: %+v %v", terminal, err)
			}
			if pressure == "content" {
				execTest(t, s, "DELETE FROM content_references WHERE id='full_0'")
			} else {
				execTest(t, s, "UPDATE mail SET state='done' WHERE id='full_0'")
			}
			publishCompletionTest(t, s, value)
			if count(t, s, "logical_writes") != writes {
				t.Fatal("derived report charged authored-write allowance")
			}
		})
	}
}

func TestCompletionRecoveryDeletionRetentionAndMailOnlyInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, parent := create(t, s, nil)
	child := controlChild(t, s, parent.ID, "child")
	turn := claim(t, s, child.Session.ID).Turn
	draft := session.MessageDraft{ID: "partial", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "partial"}}}
	if _, err := s.AppendMessage(t.Context(), turn.ID, draft); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateConfiguration(t.Context(), child.Session.ID, child.Session.ConfigRevision, session.ConfigPatch{ReportMode: new(session.ReportInline)}); err != nil {
		t.Fatal(err)
	}
	queued := submit(t, s, child.Session.ID, "queued")
	execTest(t, s, `CREATE TRIGGER fail_recovery_completion BEFORE UPDATE ON completion_slots WHEN NEW.turn_id IS NOT NULL BEGIN SELECT RAISE(ABORT,'recovery capture failure'); END`)
	if _, err := s.Recover(t.Context()); err == nil {
		t.Fatal("recovery ignored snapshot failure")
	}
	current, err := s.Turn(t.Context(), turn.ID)
	if err != nil || current.State != session.Running || count(t, s, "turn_permits") != 1 {
		t.Fatal("partial recovery", err)
	}
	execTest(t, s, "DROP TRIGGER fail_recovery_completion")
	reopened := openTest(t, path)
	if n, err := reopened.Recover(t.Context()); err != nil || n != 1 {
		t.Fatalf("recover: %d %v", n, err)
	}
	value := pendingCompletionTest(t, reopened, parent.ID, child.Session.ID, turn.ID)
	if value.State != session.Interrupted || value.Mode != session.ReportNotice || value.Text != "partial" || value.Failure == nil || *value.Failure != "runtime restarted" {
		t.Fatalf("wrong recovery evidence: %+v", value)
	}
	next := claim(t, reopened, child.Session.ID)
	if next.Input.ID != queued.Input.ID {
		t.Fatal("recovery replayed prior input")
	}
	if _, err := reopened.Finish(t.Context(), next.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	sendMailTest(t, reopened, mailSpec(child.Session.ID, "mail_only", session.MailQueued))
	mailTurn := claim(t, reopened, child.Session.ID)
	if mailTurn.Input != nil {
		t.Fatal("expected mail-only turn")
	}
	if _, err := reopened.Finish(t.Context(), mailTurn.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	value = pendingCompletionTest(t, reopened, parent.ID, child.Session.ID, mailTurn.Turn.ID)
	if value.InputID != nil || value.MessageID != nil || value.Text != "" {
		t.Fatal("mail-only completion copied prior input/result")
	}
	if err := reopened.DeleteSubtree(t.Context(), child.Session.ID); err != nil {
		t.Fatal(err)
	}
	if retained := pendingCompletionTest(t, reopened, parent.ID, child.Session.ID, mailTurn.Turn.ID); !reflect.DeepEqual(retained, value) {
		t.Fatal("source deletion lost pending evidence")
	}
	published := publishCompletionTest(t, reopened, value)
	if _, err := reopened.ContentReference(t.Context(), parent.ID, "completion_"+string(value.TurnID)); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ReadMail(t.Context(), parent.ID, published.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := reopened.Recover(t.Context()); err != nil || n != 0 {
		t.Fatalf("repeat recovery: %d %v", n, err)
	}
	if count(t, reopened, "completion_slots") != 0 {
		t.Fatal("published deleted source slot retained")
	}
}

func TestCompletionNoticeBoundsAndMetadataPagination(t *testing.T) {
	for _, mode := range []session.ReportMode{session.ReportNotice, session.ReportInline} {
		t.Run(string(mode), func(t *testing.T) {
			s := fresh(t)
			_, parent := create(t, s, nil)
			child, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child"}, ChildRequest{ParentID: parent.ID, Overrides: session.ConfigPatch{ReportMode: &mode}, Parts: []session.Part{{Type: "text", Text: "work"}}})
			if err != nil {
				t.Fatal(err)
			}
			turn := claim(t, s, child.Session.ID).Turn
			text := strings.Repeat("<界\x01", 2000)
			failure := strings.Repeat("<", 16384)
			draft := session.MessageDraft{ID: "output", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: text}}}
			if _, err := s.Finish(t.Context(), turn.ID, session.Failed, &failure, []session.MessageDraft{draft}); err != nil {
				t.Fatal(err)
			}
			value := pendingCompletionTest(t, s, parent.ID, child.Session.ID, turn.ID)
			mail := publishCompletionTest(t, s, value)
			read, err := s.ReadMail(t.Context(), parent.ID, mail.ID)
			if err != nil {
				t.Fatal(err)
			}
			var notice session.CompletionNotice
			if err := json.Unmarshal([]byte(read.Body), &notice); err != nil {
				t.Fatal(err)
			}
			limit := 160
			if mode == session.ReportInline {
				limit = 4 << 10
			}
			if len(read.Body) > session.MaxMailBodyBytes || len(notice.Preview) > limit || !utf8.ValidString(notice.Preview) || !notice.TextTruncated || !notice.FailureTruncated || notice.TextBytes != int64(len(text)) {
				t.Fatalf("bad bounded notice: %+v", notice)
			}
			if value.Text != text || *value.Failure != failure {
				t.Fatal("notice truncation altered full evidence")
			}
		})
	}
	s := fresh(t)
	_, parent := create(t, s, nil)
	_, foreign := create(t, s, nil)
	for i := range 50 {
		child := controlChild(t, s, parent.ID, fmt.Sprint("child", i))
		turn := claim(t, s, child.Session.ID).Turn
		failure := strings.Repeat("<", 16384)
		if _, err := s.Finish(t.Context(), turn.ID, session.Failed, &failure, nil); err != nil {
			t.Fatal(err)
		}
	}
	var after session.SessionID
	seen := 0
	pages := 0
	for {
		items, err := s.CompletionCandidates(t.Context(), after, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == 0 {
			break
		}
		raw, err := json.Marshal(items)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > MaxPageBytes+101 {
			t.Fatal("metadata page exceeds byte budget")
		}
		pages++
		for _, item := range items {
			if item.ChildID <= after {
				t.Fatal("unstable pagination")
			}
			after = item.ChildID
			seen++
		}
	}
	if seen != 50 || pages < 2 {
		t.Fatalf("missing byte-limited metadata pages: %d/%d", seen, pages)
	}
	if items, err := s.PendingCompletions(t.Context(), foreign.ID, "", 100); err != nil || len(items) != 0 {
		t.Fatalf("foreign parent read: %+v %v", items, err)
	}
	if _, err := s.CompletionCandidates(t.Context(), "", 0); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("invalid page size accepted", err)
	}
}
