package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestContinuationSettlementIsPrivateAtomicAndImmutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s := openTest(t, path)
	_, root := create(t, s, nil)
	submit(t, s, root.ID, "input")
	turn := claim(t, s, root.ID).Turn
	attempt := reserveTest(t, s, attemptRequest(turn.ID, "attempt"))
	dispatchTest(t, s, attempt.ID)
	result := session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(123))}
	draft := session.MessageDraft{ID: "answer", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "visible"}}, Continuation: &session.ModelContinuation{Scope: strings.Repeat("a", 64), Data: `[{"type":"reasoning","encrypted_content":"private-marker"}]`}}
	execTest(t, s, `CREATE TRIGGER fail_continuation BEFORE UPDATE ON model_attempts BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, result, &draft); err == nil {
		t.Fatal("injected transaction failure succeeded")
	}
	if values, err := s.Continuations(t.Context(), root.ID, []session.MessageID{draft.ID}); !errors.Is(err, ErrNotFound) || values != nil {
		t.Fatal("private state escaped rollback", err)
	}
	execTest(t, s, "DROP TRIGGER fail_continuation")
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, result, &draft); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, result, &draft); err != nil {
		t.Fatal("exact settlement retry", err)
	}
	for _, changed := range []*session.ModelContinuation{nil, {Scope: strings.Repeat("b", 64), Data: draft.Continuation.Data}, {Scope: draft.Continuation.Scope, Data: `[]`}} {
		copyDraft := draft
		copyDraft.Continuation = changed
		if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, result, &copyDraft); !errors.Is(err, ErrConflict) {
			t.Fatalf("changed private settlement accepted: %v", err)
		}
	}
	other := openTest(t, path)
	values, err := other.Continuations(t.Context(), root.ID, []session.MessageID{draft.ID})
	if err != nil || values[draft.ID] != *draft.Continuation {
		t.Fatalf("reopen lost private evidence: %v", err)
	}
	_, outsider := create(t, s, nil)
	if values, err := s.Continuations(t.Context(), outsider.ID, []session.MessageID{draft.ID}); !errors.Is(err, ErrNotFound) || values != nil {
		t.Fatal("cross-session continuation lookup succeeded")
	}
	history, err := s.History(t.Context(), root.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := s.HistoryMetadata(t.Context(), root.ID, 0, 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.ReadHistoryMessage(t.Context(), root.ID, draft.ID, 0, 65536)
	if err != nil {
		t.Fatal(err)
	}
	search, err := s.SearchHistory(t.Context(), root.ID, 0, 2, "private-marker", 100)
	if err != nil || len(search.Matches) != 0 {
		t.Fatal("private continuation appeared in search", err)
	}
	ledger, err := s.ModelAttempt(t.Context(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{history, metadata, read, ledger, draft} {
		raw, err := json.Marshal(value)
		if err != nil || strings.Contains(string(raw), "private-marker") || strings.Contains(string(raw), "encrypted_content") {
			t.Fatal("private continuation leaked into public serialization", err)
		}
	}
	if err := s.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Continuations(t.Context(), root.ID, []session.MessageID{draft.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted message retained private access", err)
	}
}

func TestContinuationReadChecksAggregateBoundsBeforeReturningData(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	submit(t, s, root.ID, "input")
	turn := claim(t, s, root.ID).Turn
	ids := []session.MessageID{}
	for index := range 5 {
		id := session.MessageID(fmt.Sprintf("answer_%d", index))
		continuation := &session.ModelContinuation{Scope: strings.Repeat("a", 64), Data: `[{"opaque":"` + strings.Repeat("x", 900000) + `"}]`}
		if _, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{ID: id, Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "small visible message"}}, Continuation: continuation}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	values, err := s.Continuations(t.Context(), root.ID, ids)
	if !errors.Is(err, session.ErrContinuationLimit) || values != nil {
		t.Fatalf("oversized private read returned partial data: %v", err)
	}
	if values, err := s.Continuations(t.Context(), root.ID, ids[:1]); err != nil || len(values) != 1 {
		t.Fatalf("bounded private lookup: %v", err)
	}
	if history, err := s.History(t.Context(), root.ID, 0, 100); err != nil || len(history) != 6 {
		t.Fatal("private bytes consumed public history page", err)
	}
}
