package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

// Test-only SQL exercises the retirement schema before an edit command exists.
// The eventual command also owns admission, boundary validation and REPL reset.
func retireHistoryTest(t *testing.T, s *Store, owner session.SessionID, keep int64) session.HistoryEdit {
	t.Helper()
	snapshot, err := s.HistorySnapshot(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	id := session.HistoryEditID(fmt.Sprintf("edit_%s_%d", owner, snapshot.Revision+1))
	err = s.write(t.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(), "INSERT INTO history_edits VALUES (?,?,?,?,?,?,?,?)", id, owner, strings.Repeat("a", 64), snapshot.Revision, snapshot.Revision+1, snapshot.ThroughSequence, keep, now()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(t.Context(), "UPDATE sessions SET history_revision=history_revision+1 WHERE id=?", owner); err != nil {
			return err
		}
		_, err := tx.ExecContext(t.Context(), "UPDATE messages SET retired_by=?,retired_revision=? WHERE session_id=? AND sequence>? AND retired_revision IS NULL", id, snapshot.Revision+1, owner, keep)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := s.HistoryEdit(t.Context(), owner, id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestHistoryFoundationNativeGroupsAndRevisionCapture(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	child := controlChild(t, s, root.ID, "child").Session
	for _, owner := range []session.Session{root, *child} {
		if owner.ID == root.ID {
			submit(t, s, root.ID, "root")
		}
		claimed := claim(t, s, owner.ID)
		if owner.HistoryRevision != 1 || claimed.Turn.HistoryRevision != 1 {
			t.Fatal("initial/captured history revision", owner, claimed.Turn)
		}
		group, err := s.HistoryGroup(t.Context(), owner.ID, session.HistoryGroupID(claimed.Turn.ID))
		if err != nil || group.TurnID == nil || *group.TurnID != claimed.Turn.ID || group.Source != nil {
			t.Fatal("native group invented provenance", group, err)
		}
		history, err := s.History(t.Context(), owner.ID, 0, 100)
		if err != nil || len(history) != 1 || !history[0].OpeningInput || history[0].InputID == nil || *history[0].InputID != claimed.Input.ID || history[0].GroupID != group.ID {
			t.Fatal("opening input was not explicit", history, err)
		}
		finishMailTest(t, s, claimed.Turn.ID, session.Succeeded)
		compact := compactionTurnTest(t, s, owner.ID, string(owner.ID)+"_compact")
		if compact.HistoryRevision != 1 {
			t.Fatal(compact)
		}
		if _, err := s.HistoryGroup(t.Context(), owner.ID, session.HistoryGroupID(compact.ID)); !errors.Is(err, ErrNotFound) {
			t.Fatal("maintenance invented a history group", err)
		}
		finishMailTest(t, s, compact.ID, session.Succeeded)
	}
	sendMailTest(t, s, session.MailSpec{ID: "mail_only", SenderID: root.ID, RecipientID: child.ID, Delivery: session.MailQueued, Body: "mail"})
	mail := claim(t, s, child.ID)
	history, err := s.History(t.Context(), child.ID, 1, 100)
	if err != nil || mail.Input != nil || len(history) != 1 || history[0].OpeningInput || history[0].Mail == nil || history[0].GroupID != session.HistoryGroupID(mail.Turn.ID) {
		t.Fatal("mail-only group fabricated an opening input", history, err)
	}
}

func TestHistoryFoundationRetirementKeepsEvidenceAndNeverReusesSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	owner, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Output: &session.OutputPolicy{Schema: json.RawMessage(`{"type":"integer"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	input := submit(t, s, owner.ID, "retire_this")
	turn := claim(t, s, owner.ID).Turn
	attempt := reserveTest(t, s, attemptRequest(turn.ID, "answer"))
	dispatchTest(t, s, attempt.ID)
	draft := outputDraft("answer", "42")
	draft.Continuation = &session.ModelContinuation{Scope: strings.Repeat("a", 64), Data: `[{"opaque":"secret"}]`}
	outcome := session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(17))}
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, &draft); err != nil {
		t.Fatal(err)
	}
	finishMailTest(t, s, turn.ID, session.Succeeded)
	before, err := s.ReadHistoryMessage(t.Context(), owner.ID, draft.ID, 0, 65536)
	if err != nil {
		t.Fatal(err)
	}
	execTest(t, s, `CREATE TRIGGER injected_retirement BEFORE UPDATE ON messages BEGIN SELECT RAISE(ABORT,'injected'); END`)
	err = s.write(t.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(), "INSERT INTO history_edits VALUES ('rollback',?,?,1,2,2,0,?)", owner.ID, strings.Repeat("b", 64), now()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(t.Context(), "UPDATE sessions SET history_revision=2 WHERE id=?", owner.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(t.Context(), "UPDATE messages SET retired_by='rollback',retired_revision=2 WHERE session_id=?", owner.ID)
		return err
	})
	if err == nil || count(t, s, "history_edits") != 0 {
		t.Fatal("partial edit survived a SQL failure", err)
	}
	execTest(t, s, "DROP TRIGGER injected_retirement")
	edit := retireHistoryTest(t, s, owner.ID, 0)
	if edit.ExpectedRevision != 1 || edit.Revision != 2 || edit.ObservedThrough != 2 {
		t.Fatal(edit)
	}
	if snapshot, err := s.HistorySnapshot(t.Context(), owner.ID); err != nil || snapshot.Revision != 2 || snapshot.MessageCount != 0 || snapshot.ThroughSequence != 0 {
		t.Fatal(snapshot, err)
	}
	if history, err := s.History(t.Context(), owner.ID, 0, 100); err != nil || len(history) != 0 {
		t.Fatal("active history retained retired messages", history, err)
	}
	if history, err := s.HistoryRange(t.Context(), owner.ID, 0, 2, 100); err != nil || len(history) != 0 {
		t.Fatal("active range retained retired messages", history, err)
	}
	if metadata, err := s.HistoryMetadata(t.Context(), owner.ID, 0, 2, 100); err != nil || len(metadata.Items) != 0 {
		t.Fatal(metadata, err)
	}
	if found, err := s.SearchHistory(t.Context(), owner.ID, 0, 2, "42", 100); err != nil || len(found.Matches) != 0 {
		t.Fatal(found, err)
	}
	if pins, err := s.ContextPins(t.Context(), owner.ID, []session.MessageID{"message_" + session.MessageID(input.Input.ID)}); !errors.Is(err, ErrNotFound) || pins != nil {
		t.Fatal("retired opening reentered context", pins, err)
	}
	if values, err := s.Continuations(t.Context(), owner.ID, []session.MessageID{draft.ID}); !errors.Is(err, ErrNotFound) || values != nil {
		t.Fatal("retired private state reentered context", values, err)
	}
	after, err := s.ReadHistoryMessage(t.Context(), owner.ID, draft.ID, 0, 65536)
	if err != nil || !reflect.DeepEqual(after.Data, before.Data) || after.Message.RetiredBy == nil || *after.Message.RetiredBy != edit.ID {
		t.Fatal("exact retired evidence changed", after, err)
	}
	if output, err := s.TurnOutput(t.Context(), turn.ID); err != nil || output == nil || string(output.Value) != "42" {
		t.Fatal("retirement hid exact turn output", output, err)
	}
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, &draft); err != nil {
		t.Fatal("retirement broke a settled accounting retry", err)
	}
	for _, statement := range []string{
		"UPDATE messages SET retired_by=NULL,retired_revision=NULL WHERE id='answer'",
		"UPDATE messages SET parts='[]' WHERE id='answer'",
		"UPDATE history_edits SET keep_through=1",
		"UPDATE sessions SET history_revision=3",
		"UPDATE turns SET history_revision=2",
	} {
		mustFail(t, s, statement)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	submit(t, s, owner.ID, "next")
	next := claim(t, s, owner.ID).Turn
	if next.HistoryRevision != 2 {
		t.Fatal("claim did not capture new history", next)
	}
	finishMailTest(t, s, next.ID, session.Succeeded)
	history, err := s.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 1 || history[0].Sequence != 3 {
		t.Fatal("retirement reused a historical sequence", history, err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil || count(t, s, "history_edits") != 0 || count(t, s, "history_groups") != 0 {
		t.Fatal("owner deletion leaked history identities", err)
	}
}

// Test-only imported rows prove that the schema/read boundaries need no source
// lifetime, fake input or local model attempt. No fork command is implemented.
func importHistoryTest(t *testing.T, s *Store, owner session.SessionID, history []session.Message) []session.Message {
	t.Helper()
	groups := map[session.HistoryGroupID]bool{}
	for _, message := range history {
		group := "copy_" + string(message.GroupID)
		if !groups[message.GroupID] {
			execTest(t, s, "INSERT INTO history_groups (id,session_id,source_session_id,source_group_id,created_at) VALUES (?,?,?,?,?)", group, owner, message.SessionID, message.GroupID, now())
			groups[message.GroupID] = true
		}
		parts, err := encode(message.Parts)
		if err != nil {
			t.Fatal(err)
		}
		execTest(t, s, `INSERT INTO messages (id,session_id,group_id,sequence,role,parts,created_at,opening_input,source_session_id,source_message_id,source_sequence,model_continuation)
 SELECT ?,?,?,COALESCE((SELECT MAX(sequence) FROM messages WHERE session_id=?),0)+1,?,?,?,?,?,?,?,model_continuation FROM messages WHERE id=?`,
			"copy_"+string(message.ID), owner, group, owner, message.Role, parts, now(), message.OpeningInput, message.SessionID, message.ID, message.Sequence, message.ID)
	}
	value, err := s.History(t.Context(), owner, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestHistoryFoundationImportedProvenancePinsSummaryAndPrivateStateSurviveSourceDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	source, cell := operationCell(t, s)
	settleMailCell(t, s, cell)
	draft := outputDraft("source_final", "visible answer")
	draft.Continuation = &session.ModelContinuation{Scope: strings.Repeat("a", 64), Data: `[{"opaque":"private-marker"}]`}
	if _, err := s.AppendMessage(t.Context(), cell.TurnID, draft); err != nil {
		t.Fatal(err)
	}
	finishMailTest(t, s, cell.TurnID, session.Succeeded)
	history, err := s.History(t.Context(), source.ID, 0, 100)
	if err != nil || len(history) != 4 {
		t.Fatal(history, err)
	}
	maintenance := compactionTurnTest(t, s, source.ID, "summarize")
	attempt := compactionAttemptTest(t, s, maintenance.ID, "summary")
	summary := settleCompactionTest(t, s, attempt.ID, session.CompactionDraft{ID: "native_summary", ThroughSequence: 4, Text: "exact source summary"}).Compaction
	finishMailTest(t, s, maintenance.ID, session.Succeeded)
	_, destination := create(t, s, nil)
	turns, inputs, attempts, writes := count(t, s, "turns"), count(t, s, "inputs"), count(t, s, "model_attempts"), count(t, s, "logical_writes")
	copied := importHistoryTest(t, s, destination.ID, history)
	execTest(t, s, `INSERT INTO compactions (id,session_id,history_revision,source_session_id,source_compaction_id,expected_revision,through_sequence,pinned_message_ids,text,created_at)
 VALUES ('imported_summary',?,1,?,?,0,4,'[]',?,?)`, destination.ID, source.ID, summary.ID, summary.Text, now())
	execTest(t, s, "INSERT INTO context_heads VALUES (?,1,'imported_summary')", destination.ID)
	if count(t, s, "turns") != turns || count(t, s, "inputs") != inputs || count(t, s, "model_attempts") != attempts || count(t, s, "logical_writes") != writes {
		t.Fatal("import foundations required synthetic execution or charges")
	}
	for i, message := range copied {
		if message.TurnID != "" || message.InputID != nil || message.Mail != nil || message.Source == nil || message.Source.SessionID != source.ID || message.Source.MessageID != history[i].ID || message.Source.Sequence != history[i].Sequence || !reflect.DeepEqual(message.Parts, history[i].Parts) {
			t.Fatal("import lost identity or fabricated local execution", message)
		}
	}
	group, err := s.HistoryGroup(t.Context(), destination.ID, copied[0].GroupID)
	if err != nil || group.TurnID != nil || group.Source == nil || group.Source.GroupID != history[0].GroupID {
		t.Fatal(group, err)
	}
	if pin, err := s.ContextBoundaryPin(t.Context(), destination.ID, 2); err != nil || pin == nil || *pin != copied[0].ID {
		t.Fatal("partial imported group lost opening pin", pin, err)
	}
	if pin, err := s.ContextBoundaryPin(t.Context(), destination.ID, 4); err != nil || pin != nil {
		t.Fatal("complete imported group required a pin", pin, err)
	}
	if pins, err := s.ContextPins(t.Context(), destination.ID, []session.MessageID{copied[0].ID}); err != nil || len(pins) != 1 || !pins[0].OpeningInput || pins[0].InputID != nil {
		t.Fatal(pins, err)
	}
	if err := s.write(t.Context(), func(tx *sql.Tx) error { return completeCompactionBoundary(t.Context(), tx, group.ID, 2) }); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("import split assistant call from result", err)
	}
	if err := s.write(t.Context(), func(tx *sql.Tx) error { return completeCompactionBoundary(t.Context(), tx, group.ID, 3) }); err != nil {
		t.Fatal("settled imported call was rejected", err)
	}
	mustFail(t, s, "UPDATE history_groups SET source_group_id='forged' WHERE id=?", group.ID)
	mustFail(t, s, "UPDATE messages SET source_sequence=999 WHERE id=?", copied[0].ID)
	if err := s.DeleteSubtree(t.Context(), source.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	if actual, err := s.History(t.Context(), destination.ID, 0, 100); err != nil || !reflect.DeepEqual(actual, copied) {
		t.Fatal("source deletion/restart changed imported evidence", actual, err)
	}
	imported, err := s.Compaction(t.Context(), destination.ID, "imported_summary")
	if err != nil || imported.TurnID != "" || imported.AttemptID != "" || imported.Source == nil || imported.Source.CompactionID != summary.ID || imported.Text != summary.Text || imported.HistoryRevision != 1 {
		t.Fatal("copied summary borrowed deleted accounting", imported, err)
	}
	if values, err := s.Continuations(t.Context(), destination.ID, []session.MessageID{copied[3].ID}); err != nil || values[copied[3].ID] != *draft.Continuation {
		t.Fatal("copied scoped private state was lost", values, err)
	}
	compactionHistoryTest(t, s, destination.ID, "local")
	if after, err := s.ContextTail(t.Context(), destination.ID, 8, 1); err != nil || after != 4 {
		t.Fatal("context tail confused local and imported groups", after, err)
	}
	if after, err := s.ContextTail(t.Context(), destination.ID, 8, 2); err != nil || after != 0 {
		t.Fatal("context tail omitted imported group", after, err)
	}
	if err := s.DeleteSubtree(t.Context(), destination.ID); err != nil || count(t, s, "history_groups") != 0 || count(t, s, "compactions") != 0 {
		t.Fatal("destination cleanup retained imported identities", err)
	}
}

func TestHistoryFoundationSummaryCompatibilityTracksRevisionNotSequenceGaps(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	compactionHistoryTest(t, s, owner.ID, "old")
	turn := compactionTurnTest(t, s, owner.ID, "compact_old")
	attempt := compactionAttemptTest(t, s, turn.ID, "old_summary")
	old := settleCompactionTest(t, s, attempt.ID, session.CompactionDraft{ID: "old_summary", ThroughSequence: 4, Text: "old history"})
	finishMailTest(t, s, turn.ID, session.Succeeded)
	retireHistoryTest(t, s, owner.ID, 0)
	if _, err := s.SelectCompaction(t.Context(), owner.ID, old.Head.Revision, &old.Compaction.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("retired coverage remained selectable", err)
	}
	if exact, err := s.Compaction(t.Context(), owner.ID, old.Compaction.ID); err != nil || !reflect.DeepEqual(exact, *old.Compaction) {
		t.Fatal("retirement removed immutable summary evidence", exact, err)
	}
	head, err := s.SelectCompaction(t.Context(), owner.ID, old.Head.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	compactionHistoryTest(t, s, owner.ID, "new")
	turn = compactionTurnTest(t, s, owner.ID, "compact_new")
	attempt = compactionAttemptTest(t, s, turn.ID, "new_summary")
	value := settleCompactionTest(t, s, attempt.ID, session.CompactionDraft{ID: "new_summary", ExpectedRevision: head.Revision, ThroughSequence: 8, Text: "new active history"})
	if !value.Selected || value.Rejection != nil || value.Compaction.HistoryRevision != 2 {
		t.Fatal("old retired gaps prevented new compaction", value)
	}
	rows, err := s.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN SELECT EXISTS(SELECT 1 FROM messages WHERE session_id=? AND sequence<=? AND retired_revision>?)", owner.ID, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	indexed := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		indexed = indexed || strings.Contains(detail, "message_retired_coverage")
	}
	if err := rows.Err(); err != nil || !indexed {
		t.Fatal("summary compatibility scans current history instead of retired rows", err)
	}
}

func TestHistoryFoundationFormulationRejectsStaleSourcesWithoutLosingBilling(t *testing.T) {
	for _, dispatched := range []bool{false, true} {
		t.Run(strconv.FormatBool(dispatched), func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			compactionHistoryTest(t, s, owner.ID, "source")
			admitFormulationTest(t, s, owner.ID, "formulate", session.GoalFormulationRequest{GoalID: "goal"})
			claimed := claim(t, s, owner.ID)
			var attempt session.ModelAttempt
			if dispatched {
				attempt = formulationAttemptTest(t, s, claimed, "formulate")
			}
			retireHistoryTest(t, s, owner.ID, 0)
			if _, err := s.GoalFormulationInput(t.Context(), claimed.Turn.ID); !errors.Is(err, ErrConflict) {
				t.Fatal("stale formulation silently changed source", err)
			}
			if !dispatched {
				request := attemptRequest(claimed.Turn.ID, "formulate")
				request.Request.Purpose, request.Request.Model = session.GoalFormulationPurpose, claimed.Configuration.Model
				if _, err := s.ReserveModelAttempt(t.Context(), request); !errors.Is(err, ErrConflict) || count(t, s, "model_attempts") != 0 {
					t.Fatal("stale source reached provider admission", err)
				}
				return
			}
			result := settleFormulationTest(t, s, attempt.ID)
			if result.Accepted || result.Rejection == nil || result.Candidate == nil || result.Candidate.HistoryRevision != 1 || result.Attempt.State != session.AttemptSucceeded {
				t.Fatal("stale activation lost billed candidate", result)
			}
			if exact, err := s.GoalFormulation(t.Context(), owner.ID, attempt.ID); err != nil || !reflect.DeepEqual(exact, *result.Candidate) {
				t.Fatal("historical candidate inspection depended on current history", exact, err)
			}
		})
	}
}
