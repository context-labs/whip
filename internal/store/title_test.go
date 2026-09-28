package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

func titleOwner(t *testing.T, s *Store, enabled bool) session.Session {
	t.Helper()
	_, root := create(t, s, []session.ResourceLimit{{Kind: session.ResourceQueuedInputs, Limit: new(int64(1))}})
	root, err := s.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{AutomaticTitle: new(enabled)})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func titleAdmission(t *testing.T, s *Store, root session.Session, key, text string) Admission {
	t.Helper()
	value, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "title-test", RequestID: key}, Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: text}}})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func titleAttempt(t *testing.T, s *Store, claim Claim, key string) session.ModelAttempt {
	t.Helper()
	input, err := s.AutomaticTitleInput(t.Context(), claim.Turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	spec := attemptRequest(claim.Turn.ID, key)
	spec.Request.Purpose, spec.Request.Model, spec.Request.TimeoutMillis = session.AutomaticTitlePurpose, input.Model, 20000
	value := reserveTest(t, s, spec)
	dispatchTest(t, s, value.ID)
	return value
}

func claimTitle(t *testing.T, s *Store, root session.Session) Claim {
	t.Helper()
	value := claim(t, s, root.ID)
	if value.Turn.Kind != session.AutomaticTitleInputKind || value.Input == nil || len(value.Input.Parts) != 0 || value.Turn.Goal != nil {
		t.Fatal("title did not use independent maintenance input", value)
	}
	return value
}

func TestAutomaticTitleAdmissionRollbackExactRetryAndUnicodeBoundary(t *testing.T) {
	for _, test := range []struct {
		name, text, reason string
		enabled            bool
	}{
		{"19", strings.Repeat("猫", 19), "short", true},
		{"20", strings.Repeat("猫", 20), "eligible", true},
		{"bounded", "\n  Start\t" + strings.Repeat("猫", 310), "eligible", true},
		{"disabled", strings.Repeat("x", 20), "disabled", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := fresh(t)
			root := titleOwner(t, s, test.enabled)
			before, _ := s.Tree(t.Context(), root.TreeID)
			execTest(t, s, "CREATE TRIGGER title_failure BEFORE INSERT ON automatic_title_decisions BEGIN SELECT RAISE(ABORT,'injected'); END")
			identity := session.RequestIdentity{ClientID: "title-test", RequestID: "first"}
			request := Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: test.text}}}
			if _, err := s.Admit(t.Context(), identity, request); err == nil || count(t, s, "inputs") != 0 || count(t, s, "receipts") != 0 {
				t.Fatal("admission SQL failure leaked accepted work")
			}
			unchanged, _ := s.Tree(t.Context(), root.TreeID)
			if !reflect.DeepEqual(before, unchanged) {
				t.Fatal("admission rollback leaked fallback metadata")
			}
			execTest(t, s, "DROP TRIGGER title_failure")
			first := titleAdmission(t, s, root, "first", test.text)
			decision, err := s.AutomaticTitleDecision(t.Context(), root.TreeID)
			if err != nil || decision.Reason != test.reason || decision.ConfigRevision != root.ConfigRevision || decision.InputID == nil || *decision.InputID != first.Input.ID || !decision.Model.Equal(root.Config.Model) {
				t.Fatal("wrong immutable initialization", decision, err)
			}
			tree, _ := s.Tree(t.Context(), root.TreeID)
			if tree.Metadata.Title == nil || *tree.Metadata.Title != session.TitleFallback(decision.Source) || tree.Revision != before.Revision+1 || utf8.RuneCountInString(decision.Source) > 300 {
				t.Fatal("fallback/source bounds or metadata revision incorrect", tree)
			}
			retry, err := s.Admit(t.Context(), identity, request)
			if err != nil || !reflect.DeepEqual(first, retry) || count(t, s, "automatic_title_decisions") != 1 || count(t, s, "inputs") != 1 {
				t.Fatal("exact retry reinitialized naming or consumed extra queue slot", err)
			}
			finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
			if test.reason == "eligible" {
				claimTitle(t, s, root)
			} else if _, err := s.Claim(t.Context(), root.ID); !errors.Is(err, ErrNoWork) {
				t.Fatal("ineligible decision invoked a helper", err)
			}
		})
	}
}

func TestAutomaticTitleAttachmentOnlyRestartAndFrozenPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	root := titleOwner(t, s, true)
	evidenceReference(t, s, root.ID, "attachment", "private attachment expansion")
	if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "attachment"}, Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "content", ReferenceID: "attachment"}}}); err != nil {
		t.Fatal(err)
	}
	first := claim(t, s, root.ID)
	if _, err := s.AppendMessage(t.Context(), first.Turn.ID, outputDraft("attachment-answer", "An ordinary attachment response")); err != nil {
		t.Fatal(err)
	}
	finishMailTest(t, s, first.Turn.ID, session.Succeeded)
	if _, err := s.AutomaticTitleDecision(t.Context(), root.TreeID); !errors.Is(err, ErrNotFound) {
		t.Fatal("attachment-only turn initialized naming", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	helper := session.ModelSelection{Provider: "helper", Name: "title", Effort: "high", Temperature: new(0.2), TopP: new(0.8)}
	root, err := s.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Compaction: &session.CompactionPolicy{Model: &helper}})
	if err != nil {
		t.Fatal(err)
	}
	titleAdmission(t, s, root, "text", "The first authored task after an attachment")
	decision, err := s.AutomaticTitleDecision(t.Context(), root.TreeID)
	if err != nil || !decision.Model.Equal(helper) {
		t.Fatal("helper override was not captured", err)
	}
	if _, err := s.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{AutomaticTitle: new(false), Compaction: &session.CompactionPolicy{}, Model: &session.ModelSelection{Provider: "changed", Name: "changed"}}); err != nil {
		t.Fatal(err)
	}
	finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
	// A fresh human admission wins over the pending title at queue capacity one.
	human := titleAdmission(t, s, root, "next", "A newer task must not replace the captured title source")
	claimed := claim(t, s, root.ID)
	if claimed.Input.ID != human.Input.ID {
		t.Fatal("title jumped ahead of already admitted human work")
	}
	finishMailTest(t, s, claimed.Turn.ID, session.Succeeded)
	title := claimTitle(t, s, root)
	captured, err := s.AutomaticTitleInput(t.Context(), title.Turn.ID)
	if err != nil || !reflect.DeepEqual(captured, decision) || title.Turn.ConfigRevision != root.ConfigRevision || !title.Configuration.AutomaticTitle || !title.Configuration.Compaction.Model.Equal(helper) {
		t.Fatal("claim reinterpreted admission naming policy", err)
	}
	if history, err := s.History(t.Context(), root.ID, 0, 100); err != nil || len(history) != 4 {
		t.Fatal("title claim authored conversation history", len(history), err)
	}
}

func TestAutomaticTitleManualMetadataOwnershipAndAtomicSettlement(t *testing.T) {
	for _, edit := range []string{"none", "same", "clear", "rename-back", "pin", "archive"} {
		t.Run(edit, func(t *testing.T) {
			s := fresh(t)
			root := titleOwner(t, s, true)
			titleAdmission(t, s, root, "first", "Please build a reliable recursive task backend")
			finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
			claimed := claimTitle(t, s, root)
			attempt := titleAttempt(t, s, claimed, "title-attempt")
			tree, _ := s.Tree(t.Context(), root.TreeID)
			if edit != "none" {
				metadata := tree.Metadata
				switch edit {
				case "clear":
					metadata.Title = nil
				case "pin":
					metadata.Pinned = true
				case "archive":
					metadata.Archived = true
				case "rename-back":
					changed, err := s.UpdateTree(t.Context(), tree.ID, tree.Revision, session.TreeMetadata{Title: new("Temporary")})
					if err != nil {
						t.Fatal(err)
					}
					tree.Revision = changed.Revision
				}
				var err error
				tree, err = s.UpdateTree(t.Context(), tree.ID, tree.Revision, metadata)
				if err != nil {
					t.Fatal(err)
				}
			}
			outcome := compactionOutcomeTest()
			draft := &session.AutomaticTitleDraft{Text: "Reliable recursive backend"}
			for _, fault := range []string{"BEFORE UPDATE ON model_attempts", "BEFORE INSERT ON automatic_title_results", "BEFORE UPDATE ON session_trees"} {
				if edit != "none" && strings.Contains(fault, "session_trees") {
					continue
				}
				execTest(t, s, "CREATE TRIGGER title_failure "+fault+" BEGIN SELECT RAISE(ABORT,'injected'); END")
				if _, err := s.SettleAutomaticTitle(t.Context(), attempt.ID, outcome, draft); err == nil {
					t.Fatal("SQL fault did not roll back title settlement")
				}
				pending, _ := s.ModelAttempt(t.Context(), attempt.ID)
				unchanged, _ := s.Tree(t.Context(), root.TreeID)
				if pending.State != session.AttemptDispatched || count(t, s, "automatic_title_results") != 0 || !reflect.DeepEqual(tree, unchanged) {
					t.Fatal("billing, candidate or title escaped rollback")
				}
				execTest(t, s, "DROP TRIGGER title_failure")
			}
			settled, err := s.SettleAutomaticTitle(t.Context(), attempt.ID, outcome, draft)
			if err != nil || settled.Candidate == nil || settled.Candidate.Applied != (edit == "none") || settled.Attempt.CostNanoUSD == nil || *settled.Attempt.CostNanoUSD != 1200 {
				t.Fatal("candidate/CAS/billing did not settle atomically", settled, err)
			}
			current, _ := s.Tree(t.Context(), root.TreeID)
			manual, err := s.UpdateTree(t.Context(), tree.ID, current.Revision, session.TreeMetadata{Title: new("Later human title")})
			if err != nil {
				t.Fatal(err)
			}
			retry, err := s.SettleAutomaticTitle(t.Context(), attempt.ID, outcome, draft)
			after, _ := s.Tree(t.Context(), root.TreeID)
			if err != nil || !reflect.DeepEqual(settled, retry) || !reflect.DeepEqual(manual, after) {
				t.Fatal("lost acknowledgement retry reapplied selection or changed evidence", err)
			}
			mustFail(t, s, "UPDATE automatic_title_decisions SET eligible=0 WHERE tree_id=?", tree.ID)
			mustFail(t, s, "UPDATE automatic_title_results SET text='Changed' WHERE attempt_id=?", attempt.ID)
			if _, err := s.SettleAutomaticTitle(t.Context(), attempt.ID, outcome, &session.AutomaticTitleDraft{Text: "Changed"}); !errors.Is(err, ErrConflict) {
				t.Fatal("changed candidate retry accepted", err)
			}
			finishMailTest(t, s, claimed.Turn.ID, session.Succeeded)
			if output, err := s.TurnOutput(t.Context(), claimed.Turn.ID); err != nil || output != nil {
				t.Fatal("title created ordinary output", err)
			}
			if _, err := s.Claim(t.Context(), root.ID); !errors.Is(err, ErrNoWork) {
				t.Fatal("completed title rearmed", err)
			}
		})
	}
}

func TestAutomaticTitleRecoveryDistinguishesPendingAndAttemptedWork(t *testing.T) {
	for _, stage := range []string{"pending", "claimed", "reserved", "dispatched"} {
		t.Run(stage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s := openTest(t, path)
			root := titleOwner(t, s, true)
			titleAdmission(t, s, root, "first", "This title source crosses the helper threshold")
			finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
			if stage != "pending" {
				claimed := claimTitle(t, s, root)
				if stage != "claimed" {
					spec := attemptRequest(claimed.Turn.ID, "title-attempt")
					spec.Request.Purpose, spec.Request.Model, spec.Request.TimeoutMillis = session.AutomaticTitlePurpose, root.Config.Model, 20000
					reserveTest(t, s, spec)
					if stage == "dispatched" {
						dispatchTest(t, s, spec.ID)
					}
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openTest(t, path)
			if _, err := s.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if stage == "pending" {
				claimTitle(t, s, root)
			} else if _, err := s.Claim(t.Context(), root.ID); !errors.Is(err, ErrNoWork) {
				t.Fatal("interrupted naming replayed", err)
			}
			if stage == "reserved" || stage == "dispatched" {
				attempt, err := s.ModelAttempt(t.Context(), "title-attempt")
				expected := session.AttemptCancelled
				if stage == "dispatched" {
					expected = session.AttemptUncertain
				}
				if err != nil || attempt.State != expected {
					t.Fatal("recovery lost dispatch accounting", attempt, err)
				}
			}
		})
	}
}

func TestAutomaticTitleManualInitializationAndNonAuthoredWorkStayIneligible(t *testing.T) {
	for _, source := range []string{"manual-clear", "manual-title", "agent", "mail", "schedule", "goal", "child", "fork"} {
		t.Run(source, func(t *testing.T) {
			s := fresh(t)
			root := titleOwner(t, s, true)
			switch source {
			case "manual-clear", "manual-title":
				metadata := session.TreeMetadata{}
				if source == "manual-title" {
					metadata.Title = new("Explicit title")
				}
				if _, err := s.UpdateTree(t.Context(), root.TreeID, 1, metadata); err != nil {
					t.Fatal(err)
				}
			case "agent":
				if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "agent"}, Submission{SessionID: root.ID, Source: session.AgentInput, Parts: []session.Part{{Type: "text", Text: "Agent work cannot initialize naming"}}}); err != nil {
					t.Fatal(err)
				}
				finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
			case "mail":
				sendMailTest(t, s, mailSpec(root.ID, "mail", session.MailQueued))
				finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
			case "schedule":
				scheduled := makeSchedule(t, s, root.ID, "initial", "@at 2020-01-01T00:00:00Z")
				if _, err := s.FireSchedule(t.Context(), scheduled.ID, *scheduled.NextDue); err != nil {
					t.Fatal(err)
				}
				finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
			case "goal":
				createGoalTest(t, s, root.ID, "initial", nil, true)
				finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Failed)
			case "fork":
				root = *forkTest(t, s, forkRequestTest(t, s, root.ID, "empty-fork", 0)).Root
			case "child":
				root = *controlChild(t, s, root.ID, "child").Session
				finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
			}
			before, _ := s.Tree(t.Context(), root.TreeID)
			titleAdmission(t, s, root, "authored", "Later authored text cannot rearm initialized naming")
			finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
			after, _ := s.Tree(t.Context(), root.TreeID)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("ineligible source changed selected title")
			}
			if _, err := s.Claim(t.Context(), root.ID); !errors.Is(err, ErrNoWork) {
				t.Fatal("ineligible initialization invoked helper", err)
			}
		})
	}
}

func TestAutomaticTitleLedgerRejectsWrongPurposeModelAndAdditionalAttempts(t *testing.T) {
	s := fresh(t)
	root := titleOwner(t, s, true)
	titleAdmission(t, s, root, "first", "A title helper needs exactly one bounded request")
	finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
	claimed := claimTitle(t, s, root)
	spec := attemptRequest(claimed.Turn.ID, "title-attempt")
	spec.Request.Purpose, spec.Request.Model, spec.Request.TimeoutMillis = session.AutomaticTitlePurpose, root.Config.Model, 20000
	for _, change := range []func(*session.ModelAttemptSpec){
		func(p *session.ModelAttemptSpec) { p.Request.Purpose = "turn" },
		func(p *session.ModelAttemptSpec) { p.Request.Model.Name = "changed" },
		func(p *session.ModelAttemptSpec) { p.Request.TimeoutMillis++ },
		func(p *session.ModelAttemptSpec) { p.Number = 2 },
	} {
		wrong := spec
		change(&wrong)
		if _, err := s.ReserveModelAttempt(t.Context(), wrong); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("invalid title dispatch accepted", err)
		}
	}
	reserveTest(t, s, spec)
	second := spec
	second.ID, second.LogicalID = "second-title", "second-title"
	if _, err := s.ReserveModelAttempt(t.Context(), second); !errors.Is(err, ErrConflict) {
		t.Fatal("second title attempt accepted", err)
	}
	dispatchTest(t, s, spec.ID)
	if _, err := s.SettleModelAttempt(t.Context(), spec.ID, compactionOutcomeTest(), new(outputDraft("title-message", "Not a conversation message"))); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("title authored a transcript message", err)
	}
	settled, err := s.SettleAutomaticTitle(t.Context(), spec.ID, compactionOutcomeTest(), &session.AutomaticTitleDraft{Text: "first\nsecond"})
	if err != nil || settled.Candidate != nil || settled.Rejection == nil || settled.Attempt.CostNanoUSD == nil {
		t.Fatal("invalid candidate bypassed validation or lost billing", err)
	}
}

func TestAutomaticTitleQueueUsesSharedAdmissionClock(t *testing.T) {
	s := fresh(t)
	root := titleOwner(t, s, true)
	titleAdmission(t, s, root, "first", "An authored source long enough to request a title")
	finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
	decision, err := s.AutomaticTitleDecision(t.Context(), root.TreeID)
	if err != nil {
		t.Fatal(err)
	}
	later := titleOwner(t, s, false)
	titleAdmission(t, s, later, "later", "A later ordinary prompt")
	page, err := s.QueuedSessions(t.Context(), QueueCursor{}, 1)
	if err != nil || len(page) != 1 || page[0].SessionID != root.ID || page[0].ReadyAt != decision.CreatedAt.UnixMicro() {
		t.Fatal("title intent must use the same admission clock as ordinary queued inputs", page, decision, err)
	}
	next, err := s.QueuedSessions(t.Context(), page[0], 1)
	if err != nil || len(next) != 1 || next[0].SessionID != later.ID {
		t.Fatal(next, err)
	}
}
