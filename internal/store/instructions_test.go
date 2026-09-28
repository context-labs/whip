package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func instructionManifestTest() session.InstructionManifest {
	return session.InstructionManifest{
		Bytes: 100, SHA256: strings.Repeat("ab", 32),
		Sources: []session.InstructionSource{{Kind: "project_file", Scope: "workspace", Path: "AGENTS.md", Bytes: 20, SHA256: strings.Repeat("cd", 32)}},
	}
}

func TestInstructionReadGrantRootChildAndIssuerRevocation(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	submit(t, s, root.ID, "root")
	turn := claim(t, s, root.ID).Turn
	if grant, err := s.InstructionReadGrant(t.Context(), turn.ID); err != nil || grant != nil {
		t.Fatalf("ungranted read=%+v %v", grant, err)
	}
	_, foreign := create(t, s, nil)
	for _, grant := range []session.Grant{
		{ID: "foreign", SessionID: foreign.ID, Capability: "files.read", Resource: root.WorkingDirectory},
		{ID: "wrong_capability", SessionID: root.ID, Capability: "files.write", Resource: root.WorkingDirectory},
		{ID: "wrong_scope", SessionID: root.ID, Capability: "files.read", Resource: root.WorkingDirectory + "/nested"},
	} {
		if _, err := s.CreateGrant(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
	}
	if grant, err := s.InstructionReadGrant(t.Context(), turn.ID); err != nil || grant != nil {
		t.Fatalf("scope mismatch authorized=%+v %v", grant, err)
	}
	standing := controlGrant(t, s, root, "workspace", "files.read", root.WorkingDirectory)
	child := controlChild(t, s, root.ID, "child")
	childTurn := claim(t, s, child.Session.ID).Turn
	for _, current := range []session.Turn{turn, childTurn} {
		grant, err := s.InstructionReadGrant(t.Context(), current.ID)
		if err != nil || grant == nil || grant.SessionID != current.SessionID || grant.OperationID != nil || grant.Resource != root.WorkingDirectory {
			t.Fatalf("standing/delegated grant=%+v %v", grant, err)
		}
	}
	if _, err := s.RevokeGrant(t.Context(), standing.ID); err != nil {
		t.Fatal(err)
	}
	for _, current := range []session.Turn{turn, childTurn} {
		if grant, err := s.InstructionReadGrant(t.Context(), current.ID); err != nil || grant != nil {
			t.Fatalf("revoked issuer authorized=%+v %v", grant, err)
		}
	}
	if count(t, s, "cells") != 0 || count(t, s, "operations") != 0 || count(t, s, "permissions") != 0 {
		t.Fatal("instruction authority created execution or permission records")
	}
}

func TestInstructionReadGrantNeverUsesOneUseApproval(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := admitOperation(t, s, session.OperationSpec{
		ID: "read", CellID: cell.ID, RequestID: "read", Capability: "files.read", Resource: owner.WorkingDirectory, Arguments: json.RawMessage(`{"path":"AGENTS.md"}`),
	})
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	if grant, err := s.InstructionReadGrant(t.Context(), cell.TurnID); err != nil || grant != nil {
		t.Fatalf("one-use approval authorized instructions=%+v %v", grant, err)
	}
	if dispatched, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !dispatched {
		t.Fatalf("instruction read consumed approval: %v %v", dispatched, err)
	}
}

func TestInstructionManifestMailOnlyTurnAndCancelledReplay(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	standing := controlGrant(t, s, owner, "workspace", "files.read", owner.WorkingDirectory)
	sendMailTest(t, s, mailSpec(owner.ID, "mail", session.MailQueued))
	claimed := claim(t, s, owner.ID)
	if claimed.Input != nil || claimed.Turn.Kind != session.PromptInput {
		t.Fatal("fixture did not claim mail-only prompt")
	}
	grant, err := s.InstructionReadGrant(t.Context(), claimed.Turn.ID)
	if err != nil || grant == nil || grant.ID != standing.ID {
		t.Fatalf("mail-only authority=%+v %v", grant, err)
	}
	manifest := instructionManifestTest()
	manifest.Sources = nil
	if err := s.SaveInstructionManifest(t.Context(), claimed.Turn.ID, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelTurn(t.Context(), claimed.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveInstructionManifest(t.Context(), claimed.Turn.ID, manifest); err != nil {
		t.Fatal("cancelled exact capture retry", err)
	}
	manifest.Bytes++
	if err := s.SaveInstructionManifest(t.Context(), claimed.Turn.ID, manifest); !errors.Is(err, ErrConflict) {
		t.Fatalf("cancelled changed capture retry=%v", err)
	}
}

func TestInstructionLiveTurnAndDatabaseFailures(t *testing.T) {
	for _, mode := range []string{"cancelled", "stopped", "terminal", "compact", "yielded", "missing", "database"} {
		t.Run(mode, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			var turn session.Turn
			if mode == "compact" {
				turn = compactionTurnTest(t, s, owner.ID, "compact")
			} else {
				submit(t, s, owner.ID, "prompt")
				turn = claim(t, s, owner.ID).Turn
			}
			want := ErrStopped
			switch mode {
			case "cancelled":
				if _, err := s.CancelTurn(t.Context(), turn.ID); err != nil {
					t.Fatal(err)
				}
			case "stopped":
				if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
					t.Fatal(err)
				}
			case "terminal":
				if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
					t.Fatal(err)
				}
			case "compact":
				want = ErrConflict
			case "yielded":
				want = ErrBusy
				if err := s.YieldTurn(t.Context(), turn.ID); err != nil {
					t.Fatal(err)
				}
			case "missing":
				want, turn.ID = ErrNotFound, "missing"
			case "database":
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				want = nil
			}
			grant, err := s.InstructionReadGrant(t.Context(), turn.ID)
			if grant != nil || err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("live guard=%+v %v, want %v", grant, err, want)
			}
			err = s.SaveInstructionManifest(t.Context(), turn.ID, instructionManifestTest())
			if err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("manifest guard=%v, want %v", err, want)
			}
		})
	}
	t.Run("grant query failure", func(t *testing.T) {
		s := fresh(t)
		_, owner := create(t, s, nil)
		submit(t, s, owner.ID, "prompt")
		turn := claim(t, s, owner.ID).Turn
		execTest(t, s, "ALTER TABLE grants RENAME TO inaccessible_grants")
		if grant, err := s.InstructionReadGrant(t.Context(), turn.ID); grant != nil || err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("SQL failure treated as denied authority=%+v %v", grant, err)
		}
	})
}

func TestInstructionManifestImmutableRestartAndDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, root := create(t, s, nil)
	child := controlChild(t, s, root.ID, "child")
	submit(t, s, root.ID, "root")
	turns := []session.Turn{claim(t, s, root.ID).Turn, claim(t, s, child.Session.ID).Turn}
	manifest := instructionManifestTest()
	for _, turn := range turns {
		if value, err := s.InstructionManifest(t.Context(), turn.ID); err != nil || value != nil {
			t.Fatalf("uncaptured turn=%+v %v", value, err)
		}
		execTest(t, s, `CREATE TRIGGER instruction_failure BEFORE INSERT ON turn_instruction_manifests BEGIN SELECT RAISE(ABORT,'injected failure'); END`)
		if err := s.SaveInstructionManifest(t.Context(), turn.ID, manifest); err == nil {
			t.Fatal("SQL failure ignored")
		}
		execTest(t, s, "DROP TRIGGER instruction_failure")
		if value, err := s.InstructionManifest(t.Context(), turn.ID); err != nil || value != nil {
			t.Fatalf("partial capture=%+v %v", value, err)
		}
		if err := s.SaveInstructionManifest(t.Context(), turn.ID, manifest); err != nil {
			t.Fatal(err)
		}
		changed := manifest.Clone()
		changed.Sources[0].Path = "changed.md"
		if err := s.SaveInstructionManifest(t.Context(), turn.ID, changed); !errors.Is(err, ErrConflict) {
			t.Fatalf("replaced manifest: %v", err)
		}
		mustFail(t, s, "UPDATE turn_instruction_manifests SET manifest=manifest WHERE turn_id=?", turn.ID)
		if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveInstructionManifest(t.Context(), turn.ID, manifest); err != nil {
			t.Fatal("terminal exact retry", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	for _, turn := range turns {
		value, err := reopened.InstructionManifest(t.Context(), turn.ID)
		if err != nil || value == nil || !reflect.DeepEqual(*value, manifest) {
			t.Fatalf("restart capture=%+v %v", value, err)
		}
		value.Sources[0].Path = "local mutation"
		if err := reopened.SaveInstructionManifest(t.Context(), turn.ID, manifest); err != nil {
			t.Fatal("read aliased stored state", err)
		}
	}
	if err := reopened.DeleteSubtree(t.Context(), child.Session.ID); err != nil {
		t.Fatal(err)
	}
	if value, err := reopened.InstructionManifest(t.Context(), turns[1].ID); value != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted child manifest=%+v %v", value, err)
	}
	if count(t, reopened, "turn_instruction_manifests") != 1 {
		t.Fatal("child cascade deleted parent audit")
	}
	if err := reopened.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if count(t, reopened, "turn_instruction_manifests") != 0 {
		t.Fatal("root cascade retained audit")
	}
}

func TestInstructionManifestCompetingCapturesAndCancelledContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	_, owner := create(t, s, nil)
	submit(t, s, owner.ID, "prompt")
	turn := claim(t, s, owner.ID).Turn
	manifest := instructionManifestTest()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if grant, err := s.InstructionReadGrant(ctx, turn.ID); grant != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read=%+v %v", grant, err)
	}
	if err := s.SaveInstructionManifest(ctx, turn.ID, manifest); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled save=%v", err)
	}
	if value, err := s.InstructionManifest(ctx, turn.ID); value != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspect=%+v %v", value, err)
	}
	oversized := manifest.Clone()
	oversized.Bytes = session.MaxInstructionBytes + 1
	if err := s.SaveInstructionManifest(t.Context(), turn.ID, oversized); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("oversized capture=%v", err)
	}
	if count(t, s, "turn_instruction_manifests") != 0 {
		t.Fatal("invalid capture was persisted")
	}
	var workers sync.WaitGroup
	outcomes := make(chan error, 2)
	for i, database := range []*Store{s, other} {
		workers.Go(func() {
			value := manifest.Clone()
			value.Bytes += int64(i)
			outcomes <- database.SaveInstructionManifest(t.Context(), turn.ID, value)
		})
	}
	workers.Wait()
	close(outcomes)
	wins := 0
	for err := range outcomes {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if wins != 1 || count(t, s, "turn_instruction_manifests") != 1 {
		t.Fatalf("competing captures won %d times", wins)
	}
	if count(t, s, "model_attempts") != 0 {
		t.Fatal("audit caused provider dispatch")
	}
}

func TestTurnInputReturnsCanonicalClaimWithoutMutations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, root := create(t, s, nil)
	child := controlChild(t, s, root.ID, "child")
	parts := []session.Part{{Type: "text", Text: "$review, preserve literal input"}, {Type: "text", Text: "第二段 $review"}}
	admitted, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "root"}, Submission{SessionID: root.ID, Source: session.UserInput, Parts: parts})
	if err != nil {
		t.Fatal(err)
	}
	rootClaim, childClaim := claim(t, s, root.ID), claim(t, s, child.Session.ID)
	submit(t, s, root.ID, "next")
	claims := []Claim{rootClaim, childClaim}
	for _, claimed := range claims {
		before, err := s.History(t.Context(), claimed.Turn.SessionID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		input, err := s.TurnInput(t.Context(), claimed.Turn.ID)
		if err != nil || input == nil || !reflect.DeepEqual(input, claimed.Input) {
			t.Fatalf("canonical input=%+v want=%+v err=%v", input, claimed.Input, err)
		}
		input.Parts[0].Text = "caller mutation"
		again, err := s.TurnInput(t.Context(), claimed.Turn.ID)
		if err != nil || !reflect.DeepEqual(again, claimed.Input) {
			t.Fatalf("input alias=%+v %v", again, err)
		}
		after, err := s.History(t.Context(), claimed.Turn.SessionID, 0, 100)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("canonical inspection mutated history", err)
		}
		if _, err := s.Finish(t.Context(), claimed.Turn.ID, session.Succeeded, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := s.Admission(t.Context(), admitted.Receipt.RequestIdentity)
	if err != nil || !reflect.DeepEqual(receipt.Receipt, admitted.Receipt) {
		t.Fatal("canonical inspection changed receipt", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	next := claim(t, reopened, root.ID)
	for _, claimed := range claims {
		input, err := reopened.TurnInput(t.Context(), claimed.Turn.ID)
		if err != nil || !reflect.DeepEqual(input, claimed.Input) {
			t.Fatalf("old exact input changed after restart/new turn: %+v %v", input, err)
		}
	}
	if next.Input == nil || next.Input.ID == rootClaim.Input.ID {
		t.Fatal("fixture did not advance to next input")
	}
	if input, err := reopened.TurnInput(t.Context(), "missing"); input != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing turn input=%+v %v", input, err)
	}
}

func TestTurnInputMailOnlyAndCompact(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	spec := mailSpec(owner.ID, "mail_only", session.MailQueued)
	spec.Body = "$mail-is-not-an-input"
	sendMailTest(t, s, spec)
	mail := claim(t, s, owner.ID)
	if input, err := s.TurnInput(t.Context(), mail.Turn.ID); input != nil || err != nil {
		t.Fatalf("mail-only turn invented input=%+v %v", input, err)
	}
	finishMailTest(t, s, mail.Turn.ID, session.Succeeded)
	compact := compactionTurnTest(t, s, owner.ID, "compact")
	input, err := s.TurnInput(t.Context(), compact.ID)
	if err != nil || input == nil || input.Kind != session.CompactInput || len(input.Parts) != 0 {
		t.Fatalf("compact canonical input=%+v %v", input, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if input, err := s.TurnInput(t.Context(), compact.ID); input != nil || err == nil {
		t.Fatalf("database failure became absent input=%+v %v", input, err)
	}
}

func TestSessionInstructionsIdleStoppedAndRevokedChild(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	policy := session.Instructions{Text: "current policy", ProjectFiles: []string{"rules.md"}, DiscoverSkills: false}
	updated, err := s.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Instructions: &policy})
	if err != nil {
		t.Fatal(err)
	}
	if got, grant, err := s.SessionInstructions(t.Context(), root.ID); err != nil || grant != nil || !reflect.DeepEqual(got, policy) {
		t.Fatalf("idle ungranted policy=%+v grant=%+v err=%v", got, grant, err)
	}
	standing := controlGrant(t, s, root, "standing", "files.read", root.WorkingDirectory)
	child := controlChild(t, s, root.ID, "child")
	if _, err := s.SetLifecycle(t.Context(), root.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetLifecycle(t.Context(), child.Session.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	// SQLite's query-only guard makes any accidental mutation fail. No new turn
	// or permit is needed for human inspection, even with a stopped issuer.
	execTest(t, s, "PRAGMA query_only=ON")
	for _, owner := range []session.Session{updated, *child.Session} {
		got, grant, err := s.SessionInstructions(t.Context(), owner.ID)
		if err != nil || grant == nil || grant.SessionID != owner.ID || grant.Resource != owner.WorkingDirectory || !reflect.DeepEqual(got, owner.Config.Instructions) {
			t.Fatalf("stopped inspection=%+v grant=%+v err=%v", got, grant, err)
		}
		got.ProjectFiles[0] = "caller mutation"
		fresh, _, err := s.SessionInstructions(t.Context(), owner.ID)
		if err != nil || !reflect.DeepEqual(fresh, owner.Config.Instructions) {
			t.Fatal("inspection policy aliases caller", err)
		}
	}
	execTest(t, s, "PRAGMA query_only=OFF")
	if _, err := s.RevokeGrant(t.Context(), standing.ID); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []session.SessionID{root.ID, child.Session.ID} {
		got, grant, err := s.SessionInstructions(t.Context(), owner)
		if err != nil || grant != nil || !reflect.DeepEqual(got, policy) {
			t.Fatalf("revoked issuer inspection=%+v grant=%+v err=%v", got, grant, err)
		}
	}
	for _, table := range []string{"turns", "turn_permits", "cells", "operations", "permissions", "turn_instruction_manifests"} {
		if count(t, s, table) != 0 {
			t.Fatalf("inspection created %s records", table)
		}
	}
	if count(t, s, "inputs") != 1 {
		t.Fatal("inspection changed queued child input")
	}
}

func TestSessionInstructionsReadOnlySnapshotWithWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, writer := openTest(t, path), openTest(t, path)
	_, owner := create(t, s, nil)
	standing := controlGrant(t, s, owner, "standing", "files.read", owner.WorkingDirectory)
	changed := owner.Config.Clone()
	changed.Instructions.Text = "updated policy"
	raw, err := encode(changed)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := writer.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO session_configurations(session_id,revision,configuration,created_at) VALUES (?,?,?,?)`, owner.ID, owner.ConfigRevision+1, raw, now()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "UPDATE sessions SET config_revision=? WHERE id=?", owner.ConfigRevision+1, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "UPDATE grants SET revoked_at=? WHERE id=?", now(), standing.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	policy, grant, err := s.SessionInstructions(ctx, owner.ID)
	if err != nil || grant == nil || grant.ID != standing.ID || !reflect.DeepEqual(policy, owner.Config.Instructions) {
		t.Fatalf("inspection blocked on writer or mixed uncommitted policy/authority: %+v %+v %v", policy, grant, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	policy, grant, err = s.SessionInstructions(t.Context(), owner.ID)
	if err != nil || grant != nil || !reflect.DeepEqual(policy, changed.Instructions) {
		t.Fatalf("inspection missed committed snapshot: %+v %+v %v", policy, grant, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	policy, grant, err = reopened.SessionInstructions(t.Context(), owner.ID)
	if err != nil || grant != nil || !reflect.DeepEqual(policy, changed.Instructions) {
		t.Fatalf("inspection after restart: %+v %+v %v", policy, grant, err)
	}
}

func TestSessionInstructionsRejectsOneUseAndPropagatesErrors(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := admitOperation(t, s, session.OperationSpec{ID: "read", CellID: cell.ID, RequestID: "read", Capability: "files.read", Resource: owner.WorkingDirectory, Arguments: json.RawMessage(`{}`)})
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, grant, err := s.SessionInstructions(t.Context(), owner.ID); err != nil || grant != nil {
		t.Fatalf("inspection used one-use approval=%+v %v", grant, err)
	}
	if dispatched, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !dispatched {
		t.Fatalf("inspection consumed approval: %v %v", dispatched, err)
	}
	if _, grant, err := s.SessionInstructions(t.Context(), "missing"); grant != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing session inspection=%+v %v", grant, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, grant, err := s.SessionInstructions(ctx, owner.ID); grant != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspection=%+v %v", grant, err)
	}
	if input, err := s.TurnInput(ctx, cell.TurnID); input != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled input inspection=%+v %v", input, err)
	}
	execTest(t, s, "ALTER TABLE grants RENAME TO inaccessible_grants")
	if _, grant, err := s.SessionInstructions(t.Context(), owner.ID); grant != nil || err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("SQL error became denied authority=%+v %v", grant, err)
	}
}
