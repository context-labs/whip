package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func contextAttempt(t *testing.T, s *Store, turn session.TurnID, id string) session.ModelAttemptSpec {
	t.Helper()
	scope, err := s.ModelContextScope(t.Context(), turn)
	if err != nil {
		t.Fatal(err)
	}
	spec := attemptRequest(turn, id)
	spec.Request.Context = &session.ModelContextEvidence{ModelContextScope: scope, EstimatedTokens: 27, ContextWindowTokens: new(int64(10000))}
	spec.Request.InputTokenBound = new(int64(999999))
	return spec
}

func contextUsageTest(t *testing.T, s *Store, owner session.SessionID) session.ContextUsage {
	t.Helper()
	v, err := s.ContextUsage(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestContextUsageUsesLatestDispatchedPrefillWithoutHelperOrChildCharges(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	if v := contextUsageTest(t, s, owner.ID); v.Prefill != nil || v.UnavailableReason != "no_evidence" {
		t.Fatal(v)
	}
	first := contextAttempt(t, s, cell.TurnID, "z_first")
	reserveTest(t, s, first)
	if v := contextUsageTest(t, s, owner.ID); v.Prefill != nil {
		t.Fatal("reservation became prefill", v)
	}
	dispatchTest(t, s, first.ID)
	v := contextUsageTest(t, s, owner.ID)
	if v.Prefill == nil || v.Prefill.InputSource != "estimated" || v.Prefill.InputTokens != 27 || *v.Prefill.ContextWindowTokens != 10000 || v.Prefill.Stale {
		t.Fatal(v)
	}
	large := int64(9007199254740993)
	if _, err := s.SettleModelAttempt(t.Context(), first.ID, session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: &large}}, nil); err != nil {
		t.Fatal(err)
	}
	v = contextUsageTest(t, s, owner.ID)
	if v.Prefill.InputSource != "reported" || v.Prefill.InputTokens != large {
		t.Fatal(v)
	}
	helper := helperOperation(t, s, owner, cell, "helper", "models.call", `{"prompt":"question"}`, true)
	settleUsage(t, s, helperRequest(t, helper, 0), session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: new(int64(1))}})
	child := spawnChildTest(t, s, "child", childRequest(owner.ID))
	childTurn := claim(t, s, child.Session.ID).Turn.ID
	settleUsage(t, s, contextAttempt(t, s, childTurn, "child_attempt"), session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: new(int64(2))}})
	if got := contextUsageTest(t, s, owner.ID); !reflect.DeepEqual(got, v) {
		t.Fatal("unrelated usage replaced prefill", got, v)
	}
	second := contextAttempt(t, s, cell.TurnID, "a_next")
	second.Request.Context.ContextWindowTokens = nil
	second.Request.Purpose = "final"
	settleUsage(t, s, second, session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: new(int64(0))}})
	if got := contextUsageTest(t, s, owner.ID); got.Prefill == nil || got.Prefill.AttemptID != second.ID || got.Prefill.InputTokens != 0 || got.Prefill.InputSource != "reported" || got.Prefill.ContextWindowTokens != nil {
		t.Fatal("latest/zero/unknown changed", got)
	}
	writes := count(t, s, "logical_writes")
	for range 3 {
		contextUsageTest(t, s, owner.ID)
	}
	if count(t, s, "logical_writes") != writes {
		t.Fatal("read wrote state")
	}
	rows, err := s.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+contextUsageQuery, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "attempts_context_owner") || strings.Contains(plan.String(), "TEMP B-TREE") {
		t.Fatal("latest lookup scans/sorts attempts", plan.String())
	}
}

func TestContextUsageStaleTailAndRevisionInvalidation(t *testing.T) {
	for _, change := range []string{"configuration_changed", "history_changed", "selection_changed"} {
		t.Run(change, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			history := compactionHistoryTest(t, s, owner.ID, "history")
			compact := compactionTurnTest(t, s, owner.ID, "compact")
			a := compactionAttemptTest(t, s, compact.ID, "summary_attempt")
			settleCompactionTest(t, s, a.ID, session.CompactionDraft{ID: "summary", ThroughSequence: 2, PinnedMessageIDs: []session.MessageID{history[0].ID}, Text: "retain"})
			if _, err := s.Finish(t.Context(), compact.ID, session.Succeeded, nil, nil); err != nil {
				t.Fatal(err)
			}
			turn := budgetTurn(t, s, owner.ID)
			spec := contextAttempt(t, s, turn, "prefill")
			settleUsage(t, s, spec, session.ModelAttemptResult{State: session.AttemptSucceeded})
			if _, err := s.AppendMessage(t.Context(), turn, session.MessageDraft{ID: "response", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "after prefill"}}}); err != nil {
				t.Fatal(err)
			}
			got := contextUsageTest(t, s, owner.ID)
			if got.Prefill == nil || !got.Prefill.Stale || got.Prefill.ThroughSequence >= got.ThroughSequence {
				t.Fatal("tail growth hidden", got)
			}
			if _, err := s.Finish(t.Context(), turn, session.Succeeded, nil, nil); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "configuration_changed":
				if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "fixture", Name: "changed"}}); err != nil {
					t.Fatal(err)
				}
			case "history_changed":
				rewindStopped(t, s, owner.ID)
				if _, err := s.Rewind(t.Context(), rewindRequest(t, s, owner.ID, "rewind", 0)); err != nil {
					t.Fatal(err)
				}
			case "selection_changed":
				if _, err := s.SelectCompaction(t.Context(), owner.ID, 1, nil); err != nil {
					t.Fatal(err)
				}
			}
			if invalid := contextUsageTest(t, s, owner.ID); invalid.Prefill != nil || invalid.UnavailableReason != change {
				t.Fatal(invalid)
			}
			// Exact admission recovery still compares the original immutable evidence
			// before checking current session/config/context state.
			if retry, err := s.ReserveModelAttempt(t.Context(), spec); err != nil || retry.ID != spec.ID {
				t.Fatal(retry, err)
			}
		})
	}
}

func TestContextEvidenceCannotInventOwnerOrSelection(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	turn := budgetTurn(t, s, owner.ID)
	spec := contextAttempt(t, s, turn, "invalid")
	for _, mutate := range []func(*session.ModelContextEvidence){func(v *session.ModelContextEvidence) { v.SessionID = "foreign" }, func(v *session.ModelContextEvidence) { v.ConfigRevision++ }, func(v *session.ModelContextEvidence) { v.HistoryRevision++ }, func(v *session.ModelContextEvidence) { v.ContextRevision++ }, func(v *session.ModelContextEvidence) { v.ThroughSequence++ }} {
		changed := spec
		e := spec.Request.Context.Clone()
		mutate(&e)
		changed.Request.Context = &e
		if _, err := s.ReserveModelAttempt(t.Context(), changed); err == nil {
			t.Fatal("false capture admitted")
		}
	}
	if _, err := s.ContextUsage(t.Context(), "absent"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
