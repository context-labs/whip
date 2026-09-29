package store

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestTranscriptPagesUseActualTailAndRetiredSequenceGaps(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	request := session.HistoryPageRequest{SessionID: owner.ID, Direction: "backward", Limit: 2}
	if page, err := s.TranscriptPage(t.Context(), request); err != nil || len(page.Messages) != 0 || page.NextCursor != nil || page.Snapshot.Revision != 1 {
		t.Fatal("empty tail", page, err)
	}
	appendTurn := func(key string) {
		submit(t, s, owner.ID, key)
		turn := claim(t, s, owner.ID).Turn
		if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{outputDraft(session.MessageID("answer_"+key), key)}); err != nil {
			t.Fatal(err)
		}
	}
	for index := range 3 {
		appendTurn(fmt.Sprintf("input_%d", index))
	}
	if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rewind(t.Context(), session.RewindRequest{ID: "edit", SessionID: owner.ID, ExpectedRevision: 1, ObservedThrough: 6, KeepThrough: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Active); err != nil {
		t.Fatal(err)
	}
	appendTurn("later")
	request.ExpectedRevision = new(session.Revision(2))
	for _, direction := range []string{"forward", "backward"} {
		request.Direction, request.Cursor = direction, nil
		var seen []int64
		for {
			page, err := s.TranscriptPage(t.Context(), request)
			if err != nil || page.Snapshot.ThroughSequence != 8 || page.Snapshot.MessageCount != 4 {
				t.Fatal(page, err)
			}
			if !slices.IsSortedFunc(page.Messages, func(a, b session.Message) int { return cmp.Compare(a.Sequence, b.Sequence) }) {
				t.Fatal("page is not ascending", page)
			}
			for _, message := range page.Messages {
				if message.SessionID != owner.ID || message.RetiredRevision != nil {
					t.Fatal("invalid provenance", message)
				}
				seen = append(seen, message.Sequence)
			}
			if page.NextCursor == nil {
				break
			}
			if request.Cursor != nil && *page.NextCursor == *request.Cursor {
				t.Fatal("cursor did not advance")
			}
			request.Cursor = page.NextCursor
		}
		want := []int64{1, 2, 7, 8}
		if direction == "backward" {
			want = []int64{7, 8, 1, 2}
		}
		if !slices.Equal(seen, want) {
			t.Fatal("retirement gap changed paging", seen, want)
		}
	}
	request.Direction, request.Cursor = "backward", new(int64(0))
	if page, err := s.TranscriptPage(t.Context(), request); err != nil || len(page.Messages) != 0 || page.NextCursor != nil {
		t.Fatal("zero is an exclusive boundary, not latest", page, err)
	}
	request.ExpectedRevision = new(session.Revision(1))
	if _, err := s.TranscriptPage(t.Context(), request); !errors.Is(err, ErrConflict) {
		t.Fatal("stale empty page did not reject", err)
	}
	request.SessionID = "missing"
	if _, err := s.TranscriptPage(t.Context(), request); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown session", err)
	}
}

func TestTranscriptPagesBoundBytesAndPreserveExactCounters(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	const base = int64(9007199254740992)
	// Seed an imported prefix before ordinary allocation: immutable history must
	// not be rewritten merely to exercise counters above JavaScript precision.
	execTest(t, s, `INSERT INTO history_groups(id,session_id,source_session_id,source_group_id,created_at) VALUES('imported',?,'source','original',?)`, owner.ID, now())
	parts, err := json.Marshal(outputDraft("seed", strings.Repeat("x", 900000)).Parts)
	if err != nil {
		t.Fatal(err)
	}
	execTest(t, s, `INSERT INTO messages(id,session_id,group_id,sequence,role,parts,created_at,source_session_id,source_message_id,source_sequence) VALUES('seed',?,'imported',?,'assistant',?,?,'source','original',1)`, owner.ID, base+1, string(parts), now())
	submit(t, s, owner.ID, "input")
	turn := claim(t, s, owner.ID).Turn
	for index := range 5 {
		appendHistory(t, s, turn, outputDraft(session.MessageID(fmt.Sprintf("large_%d", index)), strings.Repeat("x", 900000)))
	}
	for _, direction := range []string{"forward", "backward"} {
		request := session.HistoryPageRequest{SessionID: owner.ID, Direction: direction, Limit: 100}
		count := 0
		for {
			page, err := s.TranscriptPage(t.Context(), request)
			if err != nil || len(page.Messages) == 0 || page.Snapshot.ThroughSequence != base+7 {
				t.Fatal("large page", page.Snapshot, err)
			}
			size := 0
			for _, message := range page.Messages {
				raw, err := json.Marshal(message)
				if err != nil || message.Sequence <= base {
					t.Fatal("counter or message changed", message.Sequence, err)
				}
				size += len(raw)
			}
			if size > MaxPageBytes {
				t.Fatal("history byte budget exceeded", size)
			}
			count += len(page.Messages)
			if page.NextCursor == nil {
				break
			}
			request.Cursor = page.NextCursor
		}
		if count != 7 {
			t.Fatal("byte truncation hid a message", count)
		}
	}
}
