package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func compactInputOwnerTest(t *testing.T, s *Store, child bool, limits []session.ResourceLimit) session.Session {
	t.Helper()
	_, owner := create(t, s, limits)
	if child {
		owner = *controlChild(t, s, owner.ID, "initial_child").Session
		finishMailTest(t, s, claim(t, s, owner.ID).Turn.ID, session.Succeeded)
	}
	return owner
}

func admitCompactInputTest(t *testing.T, s *Store, owner session.SessionID, key string) Admission {
	t.Helper()
	value, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "compact", RequestID: key}, Submission{SessionID: owner, Source: session.UserInput, Kind: session.CompactInput})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCompactInputUsesReceiptsCapacityConfigAndRecoveryAtEveryDepth(t *testing.T) {
	for _, child := range []bool{false, true} {
		t.Run(fmt.Sprintf("child_%v", child), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s := openTest(t, path)
			owner := compactInputOwnerTest(t, s, child, []session.ResourceLimit{{Kind: session.ResourceQueuedInputs, Limit: new(int64(1))}})
			history, err := s.History(t.Context(), owner.ID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			for i, request := range []Submission{
				{SessionID: owner.ID, Source: session.UserInput, Kind: "unknown"},
				{SessionID: owner.ID, Source: session.UserInput, Kind: session.CompactInput, Parts: []session.Part{{Type: "text", Text: "not a maintenance payload"}}},
			} {
				beforeInputs, beforeReceipts := count(t, s, "inputs"), count(t, s, "receipts")
				if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "invalid", RequestID: strconv.Itoa(i)}, request); !errors.Is(err, session.ErrInvalid) {
					t.Fatalf("invalid compact input=%v", err)
				}
				if count(t, s, "inputs") != beforeInputs || count(t, s, "receipts") != beforeReceipts {
					t.Fatal("invalid admission changed durable queue")
				}
			}
			first := admitCompactInputTest(t, s, owner.ID, "first")
			retry, err := s.Admit(t.Context(), first.Receipt.RequestIdentity, Submission{SessionID: owner.ID, Source: session.UserInput, Kind: session.CompactInput, Parts: []session.Part{}})
			if err != nil || !reflect.DeepEqual(first, retry) || first.Input.Kind != session.CompactInput || first.Input.Parts == nil {
				t.Fatalf("compact admission retry=%+v %v", retry, err)
			}
			if _, err := s.Admit(t.Context(), first.Receipt.RequestIdentity, Submission{SessionID: owner.ID, Source: session.UserInput, Kind: session.PromptInput, Parts: []session.Part{{Type: "text", Text: "different kind"}}}); !errors.Is(err, ErrConflict) {
				t.Fatalf("changed-kind retry=%v", err)
			}
			beforeInputs, beforeReceipts := count(t, s, "inputs"), count(t, s, "receipts")
			if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "compact", RequestID: "second"}, Submission{SessionID: owner.ID, Source: session.UserInput, Kind: session.CompactInput}); !errors.Is(err, ErrLimit) {
				t.Fatalf("compact bypassed queue capacity=%v", err)
			}
			if count(t, s, "inputs") != beforeInputs || count(t, s, "receipts") != beforeReceipts {
				t.Fatal("capacity denial partially admitted input")
			}
			cancelled, err := s.CancelInput(t.Context(), first.Input.ID)
			if err != nil || cancelled.State != session.InputCancelled || cancelled.TurnID != nil || cancelled.Kind != session.CompactInput {
				t.Fatalf("queued cancellation=%+v %v", cancelled, err)
			}
			second := admitCompactInputTest(t, s, owner.ID, "second")
			owner, err = s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &session.Instructions{Text: "captured at claim"}, Output: &session.OutputPolicy{Schema: json.RawMessage(`{"type":"integer"}`)}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openTest(t, path)
			queued, err := s.Admission(t.Context(), second.Receipt.RequestIdentity)
			if err != nil || queued.Input.State != session.Queued || queued.Input.Kind != session.CompactInput || queued.Turn != nil {
				t.Fatalf("restart lost compact queue=%+v %v", queued, err)
			}
			claimed := claim(t, s, owner.ID)
			if claimed.Input.ID != second.Input.ID || claimed.Input.Kind != session.CompactInput || claimed.Turn.Kind != session.CompactInput || claimed.Turn.ConfigRevision != owner.ConfigRevision || claimed.Configuration.Instructions.Text != "captured at claim" {
				t.Fatalf("wrong maintenance claim=%+v", claimed)
			}
			if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &session.Instructions{Text: "later"}}); err != nil {
				t.Fatal(err)
			}
			persisted, err := s.Turn(t.Context(), claimed.Turn.ID)
			if err != nil || persisted.Kind != session.CompactInput || persisted.ConfigRevision != owner.ConfigRevision {
				t.Fatalf("claim configuration or kind changed=%+v %v", persisted, err)
			}
			if _, err := s.CancelInput(t.Context(), second.Input.ID); err != nil {
				t.Fatal(err)
			}
			persisted, err = s.Turn(t.Context(), claimed.Turn.ID)
			if err != nil || persisted.State != session.Cancelling {
				t.Fatalf("claimed compact cancellation=%+v %v", persisted, err)
			}
			finishMailTest(t, s, persisted.ID, session.Cancelled)
			third := admitCompactInputTest(t, s, owner.ID, "third")
			interrupted := claim(t, s, owner.ID).Turn
			fourth := admitCompactInputTest(t, s, owner.ID, "fourth")
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openTest(t, path)
			if recovered, err := s.Recover(t.Context()); err != nil || recovered != 1 {
				t.Fatalf("compact recovery=%d %v", recovered, err)
			}
			admission, err := s.Admission(t.Context(), third.Receipt.RequestIdentity)
			if err != nil || admission.Turn.ID != interrupted.ID || admission.Turn.Kind != session.CompactInput || admission.Turn.State != session.Interrupted {
				t.Fatalf("recovered compact receipt=%+v %v", admission, err)
			}
			next := claim(t, s, owner.ID)
			if next.Input.ID != fourth.Input.ID || next.Turn.Kind != session.CompactInput {
				t.Fatal("recovery replayed claimed maintenance or lost queued work")
			}
			finishMailTest(t, s, next.Turn.ID, session.Succeeded)
			if output, err := s.TurnOutput(t.Context(), next.Turn.ID); err != nil || output != nil {
				t.Fatalf("maintenance applied output schema=%+v %v", output, err)
			}
			after, err := s.History(t.Context(), owner.ID, 0, 100)
			if err != nil || !reflect.DeepEqual(after, history) || count(t, s, "cells") != 0 || count(t, s, "turn_permits") != 0 {
				t.Fatal("maintenance changed transcript/cells or leaked permits", err)
			}
		})
	}
}

func TestCompactInputNeverObservesMailOrClearsPromptRetryBarrier(t *testing.T) {
	for _, child := range []bool{false, true} {
		t.Run(fmt.Sprintf("child_%v", child), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s := openTest(t, path)
			owner := compactInputOwnerTest(t, s, child, nil)
			before := count(t, s, "messages")
			for _, delivery := range []session.MailDelivery{session.MailQueued, session.MailSteer, session.MailNextTurn} {
				sendMailTest(t, s, mailSpec(owner.ID, string(delivery), delivery))
			}
			compact := compactionTurnTest(t, s, owner.ID, "before_prompt")
			steers, err := s.ObserveSteers(t.Context(), compact.ID)
			if err != nil || len(steers) != 0 || count(t, s, "messages") != before || count(t, s, "turn_mail_observations") != 0 {
				t.Fatalf("compact presented mail=%+v %v", steers, err)
			}
			finishMailTest(t, s, compact.ID, session.Succeeded)
			for _, id := range []session.MailID{"queued", "steer", "next_turn"} {
				mailStateTest(t, s, owner.ID, id, session.MailPending, 1)
			}
			ordinary := claim(t, s, owner.ID)
			if ordinary.Input != nil || ordinary.Turn.Kind != session.PromptInput || count(t, s, "turn_mail_observations") != 3 {
				t.Fatalf("mail-only turn kind/presentation=%+v", ordinary)
			}
			finishMailTest(t, s, ordinary.Turn.ID, session.Failed)
			observations := count(t, s, "turn_mail_observations")
			compact = compactionTurnTest(t, s, owner.ID, "after_failure")
			finishMailTest(t, s, compact.ID, session.Succeeded)
			if count(t, s, "turn_mail_observations") != observations {
				t.Fatal("compact observed failed-turn mail")
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openTest(t, path)
			ready, err := s.QueuedSessions(t.Context(), QueueCursor{}, 100)
			if err != nil || len(ready) != 0 {
				t.Fatalf("successful compact cleared mail failure barrier=%+v %v", ready, err)
			}
			if _, err := s.Claim(t.Context(), owner.ID); !errors.Is(err, ErrNoWork) {
				t.Fatalf("maintenance made failed mail runnable=%v", err)
			}
			for _, id := range []session.MailID{"queued", "steer", "next_turn"} {
				mailStateTest(t, s, owner.ID, id, session.MailPending, 1)
			}
			submit(t, s, owner.ID, "explicit_retry")
			finishMailTest(t, s, claim(t, s, owner.ID).Turn.ID, session.Succeeded)
			for _, id := range []session.MailID{"queued", "steer", "next_turn"} {
				mailStateTest(t, s, owner.ID, id, session.MailDelivered, 1)
			}
		})
	}
}

func TestCompactInputPreservesPreviousChildCompletionThroughFinishAndRecovery(t *testing.T) {
	s := fresh(t)
	_, parent := create(t, s, nil)
	child := controlChild(t, s, parent.ID, "child")
	initial := claim(t, s, child.Session.ID).Turn
	if _, err := s.Finish(t.Context(), initial.ID, session.Failed, new("original child failure"), []session.MessageDraft{outputDraft("child_evidence", "retain this exact outcome")}); err != nil {
		t.Fatal(err)
	}
	before := pendingCompletionTest(t, s, parent.ID, child.Session.ID, initial.ID)
	for _, state := range []session.TurnState{session.Succeeded, session.Failed, session.Interrupted} {
		compact := compactionTurnTest(t, s, child.Session.ID, string(state))
		if state == session.Interrupted {
			if _, err := s.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
		} else {
			finishMailTest(t, s, compact.ID, state)
		}
		after := pendingCompletionTest(t, s, parent.ID, child.Session.ID, initial.ID)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("compact %s replaced prior child report: %+v", state, after)
		}
		current, err := s.Turn(t.Context(), compact.ID)
		if err != nil || current.State != state || current.Kind != session.CompactInput {
			t.Fatalf("compact outcome=%+v %v", current, err)
		}
	}
}

func TestCompactInputAndHelperAttemptCannotAuthorTranscriptOrCells(t *testing.T) {
	for _, kind := range []session.InputKind{session.PromptInput, session.CompactInput} {
		t.Run(string(kind), func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			var turn session.Turn
			if kind == session.PromptInput {
				submit(t, s, owner.ID, "prompt")
				turn = claim(t, s, owner.ID).Turn
			} else {
				turn = compactionTurnTest(t, s, owner.ID, "compact")
				if _, err := s.ReserveModelAttempt(t.Context(), attemptRequest(turn.ID, "wrong_purpose")); !errors.Is(err, session.ErrInvalid) {
					t.Fatalf("compact admitted normal model request=%v", err)
				}
				if count(t, s, "model_attempts") != 0 {
					t.Fatal("wrong-purpose admission left attempt")
				}
				if _, err := s.AppendMessage(t.Context(), turn.ID, outputDraft("invented", "summary")); !errors.Is(err, session.ErrInvalid) {
					t.Fatalf("compact authored assistant message=%v", err)
				}
				if _, _, err := s.BeginCell(t.Context(), session.CellSpec{ID: "cell", TurnID: turn.ID, CallMessageID: "invented", CallID: "call"}); !errors.Is(err, ErrConflict) {
					t.Fatalf("compact admitted cell=%v", err)
				}
			}
			before := count(t, s, "messages")
			attempt := compactionAttemptTest(t, s, turn.ID, "helper")
			draft := outputDraft("helper_output", "summary is not an answer")
			if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, compactionOutcomeTest(), &draft); !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("helper contaminated transcript=%v", err)
			}
			pending, err := s.ModelAttempt(t.Context(), attempt.ID)
			if err != nil || pending.State != session.AttemptDispatched || count(t, s, "messages") != before {
				t.Fatalf("rejected transcript write partially settled=%+v %v", pending, err)
			}
			if _, err := s.SettleCompaction(t.Context(), attempt.ID, compactionOutcomeTest(), nil); err != nil {
				t.Fatal(err)
			}
			if kind == session.CompactInput {
				if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{draft}); !errors.Is(err, session.ErrInvalid) {
					t.Fatalf("compact finish authored output=%v", err)
				}
			}
			finishMailTest(t, s, turn.ID, session.Succeeded)
			if count(t, s, "messages") != before || count(t, s, "cells") != 0 {
				t.Fatal("maintenance helper changed raw execution history")
			}
		})
	}
}
