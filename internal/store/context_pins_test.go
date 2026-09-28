package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestContextPinsReadExactOpeningInputsInRawOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	_, other := create(t, s, nil)
	foreign := compactionHistoryTest(t, s, other.ID, "foreign")
	reference, err := s.RegisterContent(t.Context(), contentReference(owner.ID, "raw-reference", "not hydrated"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "pins", RequestID: "first"}, Submission{
		SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "content", ReferenceID: reference.ID}},
	}); err != nil {
		t.Fatal(err)
	}
	first := claim(t, s, owner.ID).Turn
	if _, err := s.Finish(t.Context(), first.ID, session.Succeeded, nil, []session.MessageDraft{outputDraft("first-answer", "answer")}); err != nil {
		t.Fatal(err)
	}
	sendMailTest(t, s, mailSpec(owner.ID, "observation", session.MailSteer))
	submit(t, s, owner.ID, "second")
	second := claim(t, s, owner.ID).Turn
	if _, err := s.Finish(t.Context(), second.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	history, err := s.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 4 || history[0].InputID == nil || history[2].InputID == nil || history[3].Mail == nil {
		t.Fatalf("fixture history=%+v err=%v", history, err)
	}
	ids := []session.MessageID{history[2].ID, history[0].ID}
	want := []session.Message{history[0], history[2]}
	for _, reopened := range []bool{false, true} {
		if reopened {
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openTest(t, path)
		}
		got, err := s.ContextPins(t.Context(), owner.ID, ids)
		if err != nil || !reflect.DeepEqual(got, want) || got[0].Parts[0].Type != "content" || got[0].Parts[0].ReferenceID != reference.ID {
			t.Fatalf("reopened=%v exact raw pins=%+v err=%v", reopened, got, err)
		}
	}
	if empty, err := s.ContextPins(t.Context(), owner.ID, nil); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty pins=%+v err=%v", empty, err)
	}
	tooMany := make([]session.MessageID, session.MaxCompactionPins+1)
	for i := range tooMany {
		tooMany[i] = session.MessageID(fmt.Sprintf("pin-%d", i))
	}
	for _, test := range []struct {
		name  string
		owner session.SessionID
		ids   []session.MessageID
		want  error
	}{
		{"missing owner", "absent", nil, ErrNotFound},
		{"missing", owner.ID, []session.MessageID{"absent"}, ErrNotFound},
		{"foreign", owner.ID, []session.MessageID{history[0].ID, foreign[0].ID}, ErrNotFound},
		{"assistant", owner.ID, []session.MessageID{history[1].ID}, ErrNotFound},
		{"mail user", owner.ID, []session.MessageID{history[3].ID}, ErrNotFound},
		{"duplicate", owner.ID, []session.MessageID{history[0].ID, history[0].ID}, session.ErrInvalid},
		{"invalid ID", owner.ID, []session.MessageID{"invalid ID"}, session.ErrInvalid},
		{"too many", owner.ID, tooMany, session.ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := s.ContextPins(t.Context(), test.owner, test.ids)
			if !errors.Is(err, test.want) || len(got) != 0 {
				t.Fatalf("partial or invalid pins=%+v err=%v", got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.ContextPins(ctx, owner.ID, ids); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup=%v", err)
	}
}

func TestContextPinsByteLimitNeverReturnsPartialSetOrLosesBilling(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	var ids []session.MessageID
	for i := range 5 {
		admission, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "pins", RequestID: strconv.Itoa(i)}, Submission{
			SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: strings.Repeat("x", session.MaxDocumentBytes-512)}},
		})
		if err != nil {
			t.Fatal(err)
		}
		turn := claim(t, s, owner.ID).Turn
		if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
			t.Fatal(err)
		}
		message, err := s.History(t.Context(), owner.ID, int64(i), 1)
		if err != nil || len(message) != 1 || message[0].InputID == nil || *message[0].InputID != admission.Input.ID {
			t.Fatalf("input fixture=%+v err=%v", message, err)
		}
		ids = append(ids, message[0].ID)
	}
	if pins, err := s.ContextPins(t.Context(), owner.ID, ids[:3]); err != nil || len(pins) != 3 {
		t.Fatalf("bounded pins count=%d err=%v", len(pins), err)
	}
	if pins, err := s.ContextPins(t.Context(), owner.ID, ids); !errors.Is(err, ErrLimit) || len(pins) != 0 {
		t.Fatalf("oversized set returned %d pins err=%v", len(pins), err)
	}
	turn := compactionTurnTest(t, s, owner.ID, "compact")
	attempt := compactionAttemptTest(t, s, turn.ID, "oversized")
	draft := session.CompactionDraft{ID: "oversized-summary", ThroughSequence: 5, PinnedMessageIDs: ids, Text: "candidate"}
	value := settleCompactionTest(t, s, attempt.ID, draft)
	if value.Rejection == nil || value.Compaction != nil || value.Selected || value.Attempt.State != session.AttemptSucceeded || value.Attempt.CostNanoUSD == nil || *value.Attempt.CostNanoUSD != 1200 {
		t.Fatalf("oversized pin set lost accounting or entered context: %+v", value)
	}
}

func TestCompactionSplitRequiresExactOpeningInputAtEveryDepth(t *testing.T) {
	for _, child := range []bool{false, true} {
		for _, terminal := range []bool{false, true} {
			t.Run(fmt.Sprintf("child_%v/terminal_%v", child, terminal), func(t *testing.T) {
				s := fresh(t)
				_, owner := create(t, s, nil)
				if child {
					owner = *controlChild(t, s, owner.ID, "child").Session
					initial := claim(t, s, owner.ID).Turn
					if _, err := s.Finish(t.Context(), initial.ID, session.Succeeded, nil, nil); err != nil {
						t.Fatal(err)
					}
				}
				older := compactionHistoryTest(t, s, owner.ID, "older")
				sendMailTest(t, s, mailSpec(owner.ID, "steer", session.MailSteer))
				submit(t, s, owner.ID, "current")
				turn := claim(t, s, owner.ID).Turn
				appendHistory(t, s, turn, outputDraft("first-answer", "first"), outputDraft("last-answer", "last"))
				history, err := s.History(t.Context(), owner.ID, older[len(older)-1].Sequence, 100)
				if err != nil || len(history) != 4 || history[0].InputID == nil || history[1].Mail == nil {
					t.Fatalf("split fixture=%+v err=%v", history, err)
				}
				if terminal {
					if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
						t.Fatal(err)
					}
					turn = compactionTurnTest(t, s, owner.ID, "compact")
				}
				pin, err := s.ContextBoundaryPin(t.Context(), owner.ID, history[2].Sequence)
				if err != nil || pin == nil || *pin != history[0].ID {
					t.Fatalf("partial boundary pin=%v err=%v", pin, err)
				}
				lastPin, err := s.ContextBoundaryPin(t.Context(), owner.ID, history[3].Sequence)
				if err != nil || (terminal && lastPin != nil) || (!terminal && (lastPin == nil || *lastPin != history[0].ID)) {
					t.Fatalf("last boundary pin=%v terminal=%v err=%v", lastPin, terminal, err)
				}
				for i, pins := range [][]session.MessageID{nil, {older[0].ID}, {history[1].ID}} {
					attempt := compactionAttemptTest(t, s, turn.ID, fmt.Sprintf("invalid-%d", i))
					draft := session.CompactionDraft{ID: session.CompactionID(fmt.Sprintf("invalid-%d", i)), ThroughSequence: history[2].Sequence, PinnedMessageIDs: pins, Text: "candidate"}
					value := settleCompactionTest(t, s, attempt.ID, draft)
					if value.Compaction != nil || value.Rejection == nil || value.Selected || value.Attempt.State != session.AttemptSucceeded || value.Attempt.CostNanoUSD == nil {
						t.Fatalf("invalid pin lost billing or entered context: %+v", value)
					}
				}
				attempt := compactionAttemptTest(t, s, turn.ID, "exact")
				draft := session.CompactionDraft{ID: "exact-summary", ThroughSequence: history[2].Sequence, PinnedMessageIDs: []session.MessageID{history[0].ID}, Text: "exact input retained"}
				value := settleCompactionTest(t, s, attempt.ID, draft)
				if !value.Selected || value.Rejection != nil || value.Head.Revision != 1 {
					t.Fatalf("exact split pin rejected: %+v", value)
				}
				if terminal {
					// Covering the rest of the old turn makes its opening pin optional.
					last := session.CompactionDraft{ID: "complete-summary", ExpectedRevision: 1, BaseID: &draft.ID, ThroughSequence: history[3].Sequence, Text: "whole turn covered"}
					value = settleCompactionTest(t, s, compactionAttemptTest(t, s, turn.ID, "complete").ID, last)
					if !value.Selected || value.Rejection != nil || len(value.Compaction.PinnedMessageIDs) != 0 {
						t.Fatalf("fully covered turn retained mandatory pin: %+v", value)
					}
				} else {
					if _, err := s.CancelTurn(t.Context(), turn.ID); err != nil {
						t.Fatal(err)
					}
					pin, err := s.ContextBoundaryPin(t.Context(), owner.ID, history[3].Sequence)
					if err != nil || pin == nil || *pin != history[0].ID {
						t.Fatalf("cancelling turn lost pin=%v err=%v", pin, err)
					}
				}
			})
		}
	}
}

func TestCompactionMailOnlyBoundaryNeedsNoSyntheticInputPin(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	sendMailTest(t, s, mailSpec(owner.ID, "queued", session.MailQueued))
	claimed := claim(t, s, owner.ID)
	if claimed.Input != nil {
		t.Fatal("mail-only turn gained an input")
	}
	appendHistory(t, s, claimed.Turn, outputDraft("first", "first"), outputDraft("last", "last"))
	for _, through := range []int64{1, 2, 3} {
		pin, err := s.ContextBoundaryPin(t.Context(), owner.ID, through)
		if err != nil || pin != nil {
			t.Fatalf("mail-only live boundary %d pin=%v err=%v", through, pin, err)
		}
	}
	if _, err := s.Finish(t.Context(), claimed.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	turn := compactionTurnTest(t, s, owner.ID, "compact")
	if pin, err := s.ContextBoundaryPin(t.Context(), owner.ID, 2); err != nil || pin != nil {
		t.Fatalf("mail-only terminal partial boundary pin=%v err=%v", pin, err)
	}
	draft := session.CompactionDraft{ID: "mail-summary", ThroughSequence: 2, Text: "mail-only summary"}
	value := settleCompactionTest(t, s, compactionAttemptTest(t, s, turn.ID, "attempt").ID, draft)
	if !value.Selected || value.Rejection != nil || len(value.Compaction.PinnedMessageIDs) != 0 {
		t.Fatalf("mail-only split rejected: %+v", value)
	}
	for _, test := range []struct {
		owner   session.SessionID
		through int64
		want    error
	}{{owner.ID, 0, session.ErrInvalid}, {owner.ID, -1, session.ErrInvalid}, {owner.ID, 4, ErrNotFound}, {"absent", 1, ErrNotFound}} {
		if pin, err := s.ContextBoundaryPin(t.Context(), test.owner, test.through); !errors.Is(err, test.want) || pin != nil {
			t.Fatalf("invalid boundary=%+v pin=%v err=%v", test, pin, err)
		}
	}
}
