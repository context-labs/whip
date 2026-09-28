package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

func appendHistory(t *testing.T, s *Store, turn session.Turn, drafts ...session.MessageDraft) {
	t.Helper()
	if err := s.write(t.Context(), func(tx *sql.Tx) error {
		for _, draft := range drafts {
			if _, err := appendMessage(t.Context(), tx, turn, draft); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestHistorySnapshotAndTailRemainFixedAcrossAppend(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	empty, err := s.HistorySnapshot(t.Context(), owner.ID)
	if err != nil || empty.MessageCount != 0 || empty.ThroughSequence != 0 {
		t.Fatalf("empty snapshot=%+v err=%v", empty, err)
	}
	for i := range 6 {
		submit(t, s, owner.ID, fmt.Sprintf("input-%d", i))
		turn := claim(t, s, owner.ID).Turn
		if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{outputDraft(session.MessageID(fmt.Sprintf("answer-%d", i)), "answer")}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := s.HistorySnapshot(t.Context(), owner.ID)
	if err != nil || snapshot.ThroughSequence != 12 || snapshot.MessageCount != 12 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	submit(t, s, owner.ID, "later")
	turn := claim(t, s, owner.ID).Turn
	for _, test := range []struct {
		through int64
		keep    int
		want    int64
	}{{0, 4, 0}, {12, 4, 4}, {12, 1, 10}, {13, 1, 12}, {3, 4, 0}, {3, 1, 2}} {
		after, err := s.ContextTail(t.Context(), owner.ID, test.through, test.keep)
		if err != nil || after != test.want {
			t.Fatalf("tail through=%d keep=%d after=%d want=%d err=%v", test.through, test.keep, after, test.want, err)
		}
	}
	var after int64
	var seen []session.HistoryMetadata
	for {
		page, err := s.HistoryMetadata(t.Context(), owner.ID, after, snapshot.ThroughSequence, 3)
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, page.Items...)
		if page.NextAfter == nil {
			break
		}
		if *page.NextAfter <= after {
			t.Fatal("metadata cursor did not advance")
		}
		after = *page.NextAfter
	}
	if len(seen) != 12 || seen[0].InputID == nil || seen[11].Sequence != snapshot.ThroughSequence {
		t.Fatalf("snapshot admitted later history: %+v", seen)
	}
	for _, boundary := range []int64{0, 12} {
		page, err := s.HistoryMetadata(t.Context(), owner.ID, boundary, boundary, 1)
		if err != nil || len(page.Items) != 0 || page.NextAfter != nil {
			t.Fatalf("empty metadata range=%+v err=%v", page, err)
		}
		messages, err := s.HistoryRange(t.Context(), owner.ID, boundary, boundary, 1)
		if err != nil || len(messages) != 0 {
			t.Fatalf("empty raw range=%+v err=%v", messages, err)
		}
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HistorySnapshot(t.Context(), owner.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted snapshot=%v", err)
	}
	if _, err := s.HistoryRange(t.Context(), owner.ID, 0, 0, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted range=%v", err)
	}
}

func TestHistoryReadsExactPartsAndIsolatesOwners(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	_, other := create(t, s, nil)
	submit(t, s, owner.ID, "first")
	turn := claim(t, s, owner.ID).Turn
	parts := []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{"n":9007199254740993,"exponent":1e+009,"text":"` + strings.Repeat("界", 30000) + `needle"}`)}}}
	draft := session.MessageDraft{ID: "exact-call", Role: session.Assistant, Parts: parts}
	appendHistory(t, s, turn, draft, session.MessageDraft{ID: "exact-result", Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "call", Output: "real output needle"}}}})
	want, err := json.Marshal(parts)
	if err != nil {
		t.Fatal(err)
	}
	var actual []byte
	var offset int64
	for {
		value, err := s.ReadHistoryMessage(t.Context(), owner.ID, draft.ID, offset, 997)
		if err != nil {
			t.Fatal(err)
		}
		if value.Message.PartsBytes != int64(len(want)) || value.Message.Sequence != 2 || value.Message.TurnID != turn.ID {
			t.Fatalf("wrong byte/source metadata: %+v", value.Message)
		}
		actual = append(actual, value.Data...)
		if value.NextOffset == nil {
			break
		}
		offset = *value.NextOffset
	}
	if !bytes.Equal(actual, want) || !bytes.Contains(actual, []byte("9007199254740993")) || !bytes.Contains(actual, []byte("1e+009")) {
		t.Fatal("byte paging altered UTF-8 or numeric lexemes")
	}
	if _, err := s.ReadHistoryMessage(t.Context(), other.ID, draft.ID, 0, 100); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign message ID granted access: %v", err)
	}
	for _, query := range []string{"9007199254740993", "real output needle"} {
		found, err := s.SearchHistory(t.Context(), owner.ID, 0, 3, query, 10)
		if err != nil || len(found.Matches) != 1 || !utf8.ValidString(found.Matches[0].Snippet) {
			t.Fatalf("search=%+v err=%v", found, err)
		}
	}
	page, err := s.HistoryMetadata(t.Context(), owner.ID, 0, 3, 10)
	if err != nil || page.Items[1].PartsBytes != int64(len(want)) {
		t.Fatalf("metadata byte count=%+v err=%v", page, err)
	}
	for _, test := range []struct {
		offset int64
		length int
	}{{-1, 10}, {int64(len(want)) + 1, 10}, {0, 0}, {0, 65537}} {
		if _, err := s.ReadHistoryMessage(t.Context(), owner.ID, draft.ID, test.offset, test.length); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid byte range accepted: %+v %v", test, err)
		}
	}
	eof, err := s.ReadHistoryMessage(t.Context(), owner.ID, draft.ID, int64(len(want)), 1)
	if err != nil || len(eof.Data) != 0 || eof.NextOffset != nil {
		t.Fatalf("EOF=%+v err=%v", eof, err)
	}
}

func TestHistorySearchMakesBoundedProgressWithoutMatches(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	submit(t, s, owner.ID, "start")
	turn := claim(t, s, owner.ID).Turn
	drafts := make([]session.MessageDraft, 120)
	for i := range drafts {
		drafts[i] = outputDraft(session.MessageID(fmt.Sprintf("message-%d", i)), "ordinary text")
	}
	drafts[119].Parts[0].Text = "target"
	appendHistory(t, s, turn, drafts...)
	first, err := s.SearchHistory(t.Context(), owner.ID, 0, 121, "target", 10)
	if err != nil || len(first.Matches) != 0 || first.NextAfter == nil || *first.NextAfter != 100 || first.ScannedMessages != 100 {
		t.Fatalf("empty scan failed to advance: %+v %v", first, err)
	}
	last, err := s.SearchHistory(t.Context(), owner.ID, *first.NextAfter, 121, "target", 10)
	if err != nil || len(last.Matches) != 1 || last.Matches[0].Message.Sequence != 121 || last.NextAfter != nil || last.ScannedMessages != 21 {
		t.Fatalf("later match hidden: %+v %v", last, err)
	}
	for _, query := range []string{"", "\x00", strings.Repeat("x", 257), string([]byte{0xff})} {
		if _, err := s.SearchHistory(t.Context(), owner.ID, 0, 121, query, 1); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid search %q: %v", query, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.HistoryMetadata(ctx, owner.ID, 0, 121, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled history read hid context error: %v", err)
	}
}

func TestHistorySearchAndRangeBoundBytesAndKeepReferences(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	ref, err := s.RegisterContent(t.Context(), contentReference(owner.ID, "source-ref", "secret body needle"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "history", RequestID: "content"}, Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "content", ReferenceID: ref.ID}}}); err != nil {
		t.Fatal(err)
	}
	turn := claim(t, s, owner.ID).Turn
	drafts := make([]session.MessageDraft, 8)
	for i := range drafts {
		drafts[i] = outputDraft(session.MessageID(fmt.Sprintf("large-%d", i)), strings.Repeat("a", 600<<10))
	}
	appendHistory(t, s, turn, drafts...)
	page, err := s.SearchHistory(t.Context(), owner.ID, 0, 9, "secret body needle", 100)
	if err != nil || len(page.Matches) != 0 || page.NextAfter == nil || page.ScannedBytes > MaxPageBytes || page.ScannedMessages >= 9 {
		t.Fatalf("search read bodies or exceeded scan bound: %+v %v", page, err)
	}
	values, err := s.HistoryRange(t.Context(), owner.ID, 0, 9, 100)
	if err != nil || len(values) >= 9 || len(values) == 0 || values[0].Parts[0].ReferenceID != ref.ID {
		t.Fatalf("raw range bound/reference lost: count=%d err=%v", len(values), err)
	}
	size := 0
	for _, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		size += len(raw)
	}
	if size > MaxPageBytes {
		t.Fatalf("raw page exceeded byte bound: %d", size)
	}
}

func TestHistoryMailUsesImmutableRevisionWithoutObservation(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	spec := mailSpec(owner.ID, "source-mail", session.MailQueued)
	spec.Subject, spec.Body = "escaped <subject>", "original mail needle 界"
	sendMailTest(t, s, spec)
	turn := claim(t, s, owner.ID).Turn
	baseline, err := s.History(t.Context(), owner.ID, 0, 10)
	if err != nil || len(baseline) != 1 || baseline[0].Mail == nil {
		t.Fatalf("mail source=%+v err=%v", baseline, err)
	}
	want, err := json.Marshal(baseline[0].Parts)
	if err != nil {
		t.Fatal(err)
	}
	observations := count(t, s, "turn_mail_observations")
	spec.Body = "replacement should stay unseen"
	if _, err := s.ReplaceMail(t.Context(), spec, 1); err != nil {
		t.Fatal(err)
	}
	metadata, err := s.HistoryMetadata(t.Context(), owner.ID, 0, 1, 10)
	if err != nil || len(metadata.Items) != 1 || metadata.Items[0].PartsBytes != int64(len(want)) || metadata.Items[0].Mail.Revision != 1 {
		t.Fatalf("mail metadata=%+v err=%v", metadata, err)
	}
	read, err := s.ReadHistoryMessage(t.Context(), owner.ID, baseline[0].ID, 0, 65536)
	if err != nil || !bytes.Equal(read.Data, want) {
		t.Fatalf("mail source changed: %s err=%v", read.Data, err)
	}
	found, err := s.SearchHistory(t.Context(), owner.ID, 0, 1, "original mail needle", 10)
	if err != nil || len(found.Matches) != 1 || found.Matches[0].Message.Mail.Revision != 1 {
		t.Fatalf("mail search=%+v err=%v", found, err)
	}
	if count(t, s, "turn_mail_observations") != observations {
		t.Fatal("history inspection observed a mail revision")
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	mailStateTest(t, s, owner.ID, spec.ID, session.MailPending, 2)
}
