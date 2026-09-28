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
	if grant, err := s.InstructionReadGrant(t.Context(), turn.ID, ""); err != nil || grant != nil {
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
	if grant, err := s.InstructionReadGrant(t.Context(), turn.ID, ""); err != nil || grant != nil {
		t.Fatalf("scope mismatch authorized=%+v %v", grant, err)
	}
	standing := controlGrant(t, s, root, "workspace", "files.read", root.WorkingDirectory)
	child := controlChild(t, s, root.ID, "child")
	childTurn := claim(t, s, child.Session.ID).Turn
	for _, current := range []session.Turn{turn, childTurn} {
		grant, err := s.InstructionReadGrant(t.Context(), current.ID, "")
		if err != nil || grant == nil || grant.SessionID != current.SessionID || grant.OperationID != nil || grant.Resource != root.WorkingDirectory {
			t.Fatalf("standing/delegated grant=%+v %v", grant, err)
		}
	}
	if _, err := s.RevokeGrant(t.Context(), standing.ID); err != nil {
		t.Fatal(err)
	}
	for _, current := range []session.Turn{turn, childTurn} {
		if grant, err := s.InstructionReadGrant(t.Context(), current.ID, ""); err != nil || grant != nil {
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
	if grant, err := s.InstructionReadGrant(t.Context(), cell.TurnID, ""); err != nil || grant != nil {
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
	grant, err := s.InstructionReadGrant(t.Context(), claimed.Turn.ID, "")
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
			grant, err := s.InstructionReadGrant(t.Context(), turn.ID, "")
			if grant != nil || err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("live guard=%+v %v, want %v", grant, err, want)
			}
			grant, err = s.StandingInstructionReadGrant(t.Context(), turn.ID)
			if grant != nil || err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("standing live guard=%+v %v, want %v", grant, err, want)
			}
			grant, err = s.ProjectInstructionReadGrant(t.Context(), turn.ID, "team")
			if grant != nil || err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("project live guard=%+v %v, want %v", grant, err, want)
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
		if grant, err := s.InstructionReadGrant(t.Context(), turn.ID, ""); grant != nil || err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("SQL failure treated as denied authority=%+v %v", grant, err)
		}
		if grant, err := s.StandingInstructionReadGrant(t.Context(), turn.ID); grant != nil || err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("standing SQL failure became denial=%+v %v", grant, err)
		}
		if grant, err := s.ProjectInstructionReadGrant(t.Context(), turn.ID, "team"); grant != nil || err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("project SQL failure became denial=%+v %v", grant, err)
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
	if grant, err := s.InstructionReadGrant(ctx, turn.ID, ""); grant != nil || !errors.Is(err, context.Canceled) {
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
	if got, grant, err := s.SessionInstructions(t.Context(), root.ID); err != nil || len(grant) != 0 || !reflect.DeepEqual(got.Config.Instructions, policy) {
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
		if err != nil || len(grant) != 1 || grant[0].SessionID != owner.ID || grant[0].Resource != owner.WorkingDirectory || !reflect.DeepEqual(got.Config.Instructions, owner.Config.Instructions) {
			t.Fatalf("stopped inspection=%+v grant=%+v err=%v", got, grant, err)
		}
		got.Config.Instructions.ProjectFiles[0] = "caller mutation"
		fresh, _, err := s.SessionInstructions(t.Context(), owner.ID)
		if err != nil || !reflect.DeepEqual(fresh.Config.Instructions, owner.Config.Instructions) {
			t.Fatal("inspection policy aliases caller", err)
		}
	}
	execTest(t, s, "PRAGMA query_only=OFF")
	if _, err := s.RevokeGrant(t.Context(), standing.ID); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []session.SessionID{root.ID, child.Session.ID} {
		got, grant, err := s.SessionInstructions(t.Context(), owner)
		if err != nil || len(grant) != 0 || !reflect.DeepEqual(got.Config.Instructions, policy) {
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
	owner, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &session.Instructions{ProjectRoot: new("alpha"), SkillRoots: []string{"alpha", "beta"}, StandingInstructions: true}})
	if err != nil {
		t.Fatal(err)
	}
	alpha := controlGrant(t, s, owner, "alpha", "skills.read", "alpha")
	beta := controlGrant(t, s, owner, "beta", "skills.read", "beta")
	standing := controlGrant(t, s, owner, "standing", "files.read", owner.WorkingDirectory)
	file := controlGrant(t, s, owner, "standing_file", "instructions.read", "standing")
	projectAlpha := controlGrant(t, s, owner, "project_alpha", "instructions.read", "project:alpha")
	projectBeta := controlGrant(t, s, owner, "project_beta", "instructions.read", "project:beta")
	changed := owner.Config.Clone()
	changed.Instructions.ProjectRoot = new("beta")
	changed.Instructions.Text = "updated policy"
	changed.Instructions.StandingInstructions = false
	changed.Instructions.SkillRoots = []string{"beta", "alpha"}
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
	if _, err := tx.ExecContext(t.Context(), "UPDATE grants SET revoked_at=? WHERE id=?", now(), alpha.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	policy, grant, err := s.SessionInstructions(ctx, owner.ID)
	if err != nil || len(grant) != 5 || grant[0].ID != standing.ID || grant[1].ID != projectAlpha.ID || grant[2].ID != alpha.ID || grant[3].ID != beta.ID || grant[4].ID != file.ID || policy.ConfigRevision != owner.ConfigRevision || policy.WorkingDirectory != owner.WorkingDirectory || !reflect.DeepEqual(policy.Config.Instructions, owner.Config.Instructions) {
		t.Fatalf("inspection blocked on writer or mixed uncommitted policy/authority: %+v %+v %v", policy, grant, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	policy, grant, err = s.SessionInstructions(t.Context(), owner.ID)
	if err != nil || len(grant) != 3 || grant[0].ID != standing.ID || grant[1].ID != projectBeta.ID || grant[2].ID != beta.ID || policy.ConfigRevision != owner.ConfigRevision+1 || policy.WorkingDirectory != owner.WorkingDirectory || !reflect.DeepEqual(policy.Config.Instructions, changed.Instructions) {
		t.Fatalf("inspection missed committed snapshot: %+v %+v %v", policy, grant, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	policy, grant, err = reopened.SessionInstructions(t.Context(), owner.ID)
	if err != nil || len(grant) != 3 || grant[0].ID != standing.ID || grant[1].ID != projectBeta.ID || grant[2].ID != beta.ID || policy.ConfigRevision != owner.ConfigRevision+1 || policy.WorkingDirectory != owner.WorkingDirectory || !reflect.DeepEqual(policy.Config.Instructions, changed.Instructions) {
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
	if _, grant, err := s.SessionInstructions(t.Context(), owner.ID); err != nil || len(grant) != 0 {
		t.Fatalf("inspection used one-use approval=%+v %v", grant, err)
	}
	if dispatched, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !dispatched {
		t.Fatalf("inspection consumed approval: %v %v", dispatched, err)
	}
	if _, grant, err := s.SessionInstructions(t.Context(), "missing"); len(grant) != 0 || !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing session inspection=%+v %v", grant, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, grant, err := s.SessionInstructions(ctx, owner.ID); len(grant) != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspection=%+v %v", grant, err)
	}
	if input, err := s.TurnInput(ctx, cell.TurnID); input != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled input inspection=%+v %v", input, err)
	}
	execTest(t, s, "ALTER TABLE grants RENAME TO inaccessible_grants")
	if _, grant, err := s.SessionInstructions(t.Context(), owner.ID); len(grant) != 0 || err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("SQL error became denied authority=%+v %v", grant, err)
	}
}

func TestNamedInstructionAuthorityScopeAndRevocation(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	policy := session.Instructions{SkillRoots: []string{"beta", "alpha", "absent"}}
	owner, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy})
	if err != nil {
		t.Fatal(err)
	}
	submit(t, s, owner.ID, "prompt")
	turn := claim(t, s, owner.ID).Turn
	_, foreign := create(t, s, nil)
	for _, grant := range []session.Grant{
		{ID: "foreign", SessionID: foreign.ID, Capability: "skills.read", Resource: "alpha"},
		{ID: "wrong_capability", SessionID: owner.ID, Capability: "files.read", Resource: "alpha"},
		{ID: "wrong_resource", SessionID: owner.ID, Capability: "skills.read", Resource: owner.WorkingDirectory},
	} {
		if _, err := s.CreateGrant(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
	}
	if grant, err := s.InstructionReadGrant(t.Context(), turn.ID, "alpha"); err != nil || grant != nil {
		t.Fatalf("foreign/wrong authority accepted: %+v %v", grant, err)
	}
	workspace := controlGrant(t, s, owner, "workspace", "files.read", owner.WorkingDirectory)
	alpha := controlGrant(t, s, owner, "alpha", "skills.read", "alpha")
	beta := controlGrant(t, s, owner, "beta", "skills.read", "beta")
	controlGrant(t, s, owner, "unselected", "skills.read", "unselected")
	child := *controlChild(t, s, owner.ID, "child").Session
	childTurn := claim(t, s, child.ID).Turn
	for _, target := range []session.Session{owner, child} {
		got, grants, err := s.SessionInstructions(t.Context(), target.ID)
		if err != nil || !reflect.DeepEqual(got.Config.Instructions, policy) || len(grants) != 3 {
			t.Fatalf("selected grants=%+v policy=%+v err=%v", grants, got, err)
		}
		want := []session.Grant{workspace, beta, alpha}
		for i, g := range grants {
			if g.SessionID != target.ID || g.Capability != want[i].Capability || g.Resource != want[i].Resource {
				t.Fatalf("grant order/scope mismatch: %+v", grants)
			}
		}
	}
	for _, current := range []session.Turn{turn, childTurn} {
		grant, err := s.InstructionReadGrant(t.Context(), current.ID, "alpha")
		if err != nil || grant == nil || grant.SessionID != current.SessionID || grant.Resource != "alpha" || grant.Capability != "skills.read" {
			t.Fatalf("named turn grant=%+v %v", grant, err)
		}
	}
	if _, err := s.RevokeGrant(t.Context(), alpha.ID); err != nil {
		t.Fatal(err)
	}
	for _, current := range []session.Turn{turn, childTurn} {
		if grant, err := s.InstructionReadGrant(t.Context(), current.ID, "alpha"); err != nil || grant != nil {
			t.Fatalf("revoked root chain admitted: %+v %v", grant, err)
		}
		_, grants, err := s.SessionInstructions(t.Context(), current.SessionID)
		if err != nil || len(grants) != 2 || grants[1].Resource != "beta" {
			t.Fatalf("revoked root inspection=%+v %v", grants, err)
		}
	}
	for _, rootID := range []string{"../alpha", "bad root"} {
		if grant, err := s.InstructionReadGrant(t.Context(), turn.ID, rootID); grant != nil || !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid root=%+v %v", grant, err)
		}
	}
	if _, err := s.SetLifecycle(t.Context(), child.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	if grant, err := s.InstructionReadGrant(t.Context(), childTurn.ID, "beta"); grant != nil || !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped execution admitted=%+v %v", grant, err)
	}
	execTest(t, s, "PRAGMA query_only=ON")
	_, grants, err := s.SessionInstructions(t.Context(), child.ID)
	if err != nil || len(grants) != 2 || grants[1].Resource != "beta" {
		t.Fatalf("stopped read-only inspection denied=%+v %v", grants, err)
	}
	execTest(t, s, "PRAGMA query_only=OFF")
}

func TestNamedInstructionOneUseNeverAuthorizesCaptureOrInspection(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	policy := session.Instructions{SkillRoots: []string{"team"}}
	if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy}); err != nil {
		t.Fatal(err)
	}
	op := admitOperation(t, s, session.OperationSpec{ID: "skill_read", CellID: cell.ID, RequestID: "read", Capability: "skills.read", Resource: "team", Arguments: json.RawMessage(`{}`)})
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	if grant, err := s.InstructionReadGrant(t.Context(), cell.TurnID, "team"); err != nil || grant != nil {
		t.Fatalf("one-use captured=%+v %v", grant, err)
	}
	if _, grants, err := s.SessionInstructions(t.Context(), owner.ID); err != nil || len(grants) != 0 {
		t.Fatalf("one-use inspected=%+v %v", grants, err)
	}
	if dispatched, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !dispatched {
		t.Fatalf("inspection consumed one-use approval: %v %v", dispatched, err)
	}
}

func TestInstructionRootsCapturedCopyClearAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	policy := session.Instructions{ProjectRoot: new("project"), SkillRoots: []string{"team"}, DiscoverSkills: true, StandingInstructions: true}
	owner, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy})
	if err != nil {
		t.Fatal(err)
	}
	policy.SkillRoots[0] = "caller_mutation"
	*policy.ProjectRoot = "caller_mutation"
	controlGrant(t, s, owner, "project", "instructions.read", "project:project")
	controlGrant(t, s, owner, "team", "skills.read", "team")
	controlGrant(t, s, owner, "standing", "instructions.read", "standing")
	child := *controlChild(t, s, owner.ID, "child").Session
	clearedChild, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "cleared_child"}, ChildRequest{
		ParentID: owner.ID, Overrides: session.ConfigPatch{Instructions: &session.Instructions{}},
		Parts: []session.Part{{Type: "text", Text: "child with cleared project"}},
	})
	if err != nil || clearedChild.Session == nil || clearedChild.Session.Config.Instructions.ProjectRoot != nil {
		t.Fatalf("child override retained project selection: %+v %v", clearedChild, err)
	}

	submit(t, s, owner.ID, "parent")
	active := []Claim{claim(t, s, owner.ID), claim(t, s, child.ID)}
	cleared := session.Instructions{}
	owner, err = s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &cleared})
	if err != nil {
		t.Fatal(err)
	}
	current, grants, err := s.SessionInstructions(t.Context(), owner.ID)
	if err != nil || current.Config.Instructions.ProjectRoot != nil || len(current.Config.Instructions.SkillRoots) != 0 || current.Config.Instructions.StandingInstructions || len(grants) != 0 {
		t.Fatalf("inspection retained old selection=%+v %+v %v", current, grants, err)
	}
	childPolicy, childGrants, err := s.SessionInstructions(t.Context(), child.ID)
	if err != nil || !reflect.DeepEqual(childPolicy.Config.Instructions.SkillRoots, []string{"team"}) || len(childGrants) != 3 || childPolicy.Config.Instructions.ProjectRoot == nil || *childPolicy.Config.Instructions.ProjectRoot != "project" || !childPolicy.Config.Instructions.StandingInstructions {
		t.Fatalf("parent clear changed child: %+v %+v %v", childPolicy, childGrants, err)
	}
	for _, claimed := range active {
		captured, err := s.Configuration(t.Context(), claimed.Turn.SessionID, claimed.Turn.ConfigRevision)
		if err != nil || captured.Instructions.ProjectRoot == nil || *captured.Instructions.ProjectRoot != "project" || !captured.Instructions.StandingInstructions || !reflect.DeepEqual(captured.Instructions.SkillRoots, []string{"team"}) || !reflect.DeepEqual(claimed.Configuration.Instructions.SkillRoots, []string{"team"}) {
			t.Fatalf("turn selection changed=%+v %v", captured.Instructions, err)
		}
		// Authority lookup takes the caller's captured selection, never the owner's
		// mutable current selection, so clearing policy cannot rewrite this turn.
		if grant, err := s.InstructionReadGrant(t.Context(), claimed.Turn.ID, "team"); err != nil || grant == nil {
			t.Fatalf("captured root lost authority=%+v %v", grant, err)
		}
		if grant, err := s.StandingInstructionReadGrant(t.Context(), claimed.Turn.ID); err != nil || grant == nil {
			t.Fatalf("captured standing authority denied=%+v %v", grant, err)
		}
		if grant, err := s.ProjectInstructionReadGrant(t.Context(), claimed.Turn.ID, "project"); err != nil || grant == nil {
			t.Fatalf("captured project authority denied=%+v %v", grant, err)
		}
		manifest := instructionManifestTest()
		manifest.Sources = []session.InstructionSource{{Kind: "invoked_skill", Scope: "host", RootID: new("team"), Path: "review/SKILL.md", Bytes: session.MaxInvokedSkillBytes, SHA256: strings.Repeat("ab", 32)}}
		if err := s.SaveInstructionManifest(t.Context(), claimed.Turn.ID, manifest); err != nil {
			t.Fatal(err)
		}
		*manifest.Sources[0].RootID = "caller_mutation"
		if _, err := s.Finish(t.Context(), claimed.Turn.ID, session.Succeeded, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	for _, target := range []session.Session{owner, child} {
		submit(t, s, target.ID, "next_"+string(target.ID))
		next := claim(t, s, target.ID)
		want := target.Config.Instructions.SkillRoots
		if !reflect.DeepEqual(next.Configuration.Instructions.ProjectRoot, target.Config.Instructions.ProjectRoot) || !reflect.DeepEqual(next.Configuration.Instructions.SkillRoots, want) || next.Configuration.Instructions.StandingInstructions != target.Config.Instructions.StandingInstructions {
			t.Fatalf("next-turn roots=%q want=%q", next.Configuration.Instructions.SkillRoots, want)
		}
	}
	for _, claimed := range active {
		captured, err := s.Configuration(t.Context(), claimed.Turn.SessionID, claimed.Turn.ConfigRevision)
		if err != nil || captured.Instructions.ProjectRoot == nil || *captured.Instructions.ProjectRoot != "project" || !captured.Instructions.StandingInstructions || !reflect.DeepEqual(captured.Instructions.SkillRoots, []string{"team"}) {
			t.Fatalf("reopen changed old selection=%+v %v", captured.Instructions, err)
		}
		audit, err := s.InstructionManifest(t.Context(), claimed.Turn.ID)
		if err != nil || audit == nil || len(audit.Sources) != 1 || audit.Sources[0].RootID == nil || *audit.Sources[0].RootID != "team" {
			t.Fatalf("host source audit lost=%+v %v", audit, err)
		}
	}
}

func TestStandingInstructionGrantScopeChildRevocationAndInspection(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	policy := session.Instructions{StandingInstructions: true}
	owner, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy})
	if err != nil {
		t.Fatal(err)
	}
	submit(t, s, owner.ID, "prompt")
	turn := claim(t, s, owner.ID).Turn
	_, foreign := create(t, s, nil)
	for _, grant := range []session.Grant{
		{ID: "foreign", SessionID: foreign.ID, Capability: "instructions.read", Resource: "standing"},
		{ID: "skill_named_standing", SessionID: owner.ID, Capability: "skills.read", Resource: "standing"},
		{ID: "wrong_resource", SessionID: owner.ID, Capability: "instructions.read", Resource: owner.WorkingDirectory},
	} {
		if _, err := s.CreateGrant(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
	}
	if grant, err := s.StandingInstructionReadGrant(t.Context(), turn.ID); err != nil || grant != nil {
		t.Fatalf("scope mismatch admitted=%+v %v", grant, err)
	}
	if _, grants, err := s.SessionInstructions(t.Context(), owner.ID); err != nil || len(grants) != 0 {
		t.Fatalf("scope mismatch inspection=%+v %v", grants, err)
	}
	standing := controlGrant(t, s, owner, "standing", "instructions.read", "standing")
	child := *controlChild(t, s, owner.ID, "child").Session
	childTurn := claim(t, s, child.ID).Turn
	for _, target := range []session.Turn{turn, childTurn} {
		grant, err := s.StandingInstructionReadGrant(t.Context(), target.ID)
		if err != nil || grant == nil || grant.SessionID != target.SessionID || grant.Capability != "instructions.read" || grant.Resource != "standing" {
			t.Fatalf("standing authority=%+v %v", grant, err)
		}
		policy, grants, err := s.SessionInstructions(t.Context(), target.SessionID)
		if err != nil || !policy.Config.Instructions.StandingInstructions || len(grants) != 1 || grants[0].ID != grant.ID {
			t.Fatalf("standing snapshot=%+v %+v %v", policy, grants, err)
		}
	}
	if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	execTest(t, s, "PRAGMA query_only=ON")
	if _, grants, err := s.SessionInstructions(t.Context(), child.ID); err != nil || len(grants) != 1 {
		t.Fatalf("stopped readonly inspection=%+v %v", grants, err)
	}
	execTest(t, s, "PRAGMA query_only=OFF")
	if _, err := s.RevokeGrant(t.Context(), standing.ID); err != nil {
		t.Fatal(err)
	}
	if grant, err := s.StandingInstructionReadGrant(t.Context(), childTurn.ID); err != nil || grant != nil {
		t.Fatalf("revoked issuer still admits active child=%+v %v", grant, err)
	}
	for _, id := range []session.SessionID{owner.ID, child.ID} {
		if _, grants, err := s.SessionInstructions(t.Context(), id); err != nil || len(grants) != 0 {
			t.Fatalf("revoked chain inspection=%+v %v", grants, err)
		}
	}
}

func TestStandingInstructionOneUseAndMailOnlyPrompt(t *testing.T) {
	t.Run("one use excluded", func(t *testing.T) {
		s := fresh(t)
		owner, cell := operationCell(t, s)
		if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &session.Instructions{StandingInstructions: true}}); err != nil {
			t.Fatal(err)
		}
		op := admitOperation(t, s, session.OperationSpec{ID: "read", CellID: cell.ID, RequestID: "read", Capability: "instructions.read", Resource: "standing", Arguments: json.RawMessage(`{}`)})
		if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
			t.Fatal(err)
		}
		if grant, err := s.StandingInstructionReadGrant(t.Context(), cell.TurnID); err != nil || grant != nil {
			t.Fatalf("one-use capture=%+v %v", grant, err)
		}
		if _, grants, err := s.SessionInstructions(t.Context(), owner.ID); err != nil || len(grants) != 0 {
			t.Fatalf("one-use inspection=%+v %v", grants, err)
		}
		if dispatched, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !dispatched {
			t.Fatalf("inspection consumed one-use approval: %v %v", dispatched, err)
		}
	})
	t.Run("mail only", func(t *testing.T) {
		s := fresh(t)
		_, owner := create(t, s, nil)
		standing := controlGrant(t, s, owner, "standing", "instructions.read", "standing")
		sendMailTest(t, s, mailSpec(owner.ID, "mail", session.MailQueued))
		claimed := claim(t, s, owner.ID)
		if claimed.Input != nil {
			t.Fatal("fixture has canonical input")
		}
		grant, err := s.StandingInstructionReadGrant(t.Context(), claimed.Turn.ID)
		if err != nil || grant == nil || grant.ID != standing.ID {
			t.Fatalf("mail-only standing authority=%+v %v", grant, err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if grant, err := s.StandingInstructionReadGrant(ctx, claimed.Turn.ID); grant != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled read=%+v %v", grant, err)
		}
	})
}

func TestProjectInstructionGrantExactScopeChildRevocationAndInspection(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	policy := session.Instructions{ProjectRoot: new("standing")}
	owner, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy})
	if err != nil {
		t.Fatal(err)
	}
	submit(t, s, owner.ID, "prompt")
	turn := claim(t, s, owner.ID).Turn
	_, foreign := create(t, s, nil)
	for _, grant := range []session.Grant{
		{ID: "foreign", SessionID: foreign.ID, Capability: "instructions.read", Resource: "project:standing"},
		{ID: "standing_file", SessionID: owner.ID, Capability: "instructions.read", Resource: "standing"},
		{ID: "same_skill_name", SessionID: owner.ID, Capability: "skills.read", Resource: "standing"},
		{ID: "wrong_capability", SessionID: owner.ID, Capability: "files.read", Resource: "project:standing"},
		{ID: "other_project", SessionID: owner.ID, Capability: "instructions.read", Resource: "project:other"},
	} {
		if _, err := s.CreateGrant(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
	}
	if grant, err := s.ProjectInstructionReadGrant(t.Context(), turn.ID, "standing"); err != nil || grant != nil {
		t.Fatalf("colliding or foreign authority accepted: %+v %v", grant, err)
	}
	if _, grants, err := s.SessionInstructions(t.Context(), owner.ID); err != nil || len(grants) != 0 {
		t.Fatalf("colliding inspection authority accepted: %+v %v", grants, err)
	}
	project := controlGrant(t, s, owner, "project", "instructions.read", "project:standing")
	child := *controlChild(t, s, owner.ID, "child").Session
	childTurn := claim(t, s, child.ID).Turn
	for _, target := range []session.Turn{turn, childTurn} {
		grant, err := s.ProjectInstructionReadGrant(t.Context(), target.ID, "standing")
		if err != nil || grant == nil || grant.SessionID != target.SessionID || grant.Capability != "instructions.read" || grant.Resource != "project:standing" {
			t.Fatalf("project authority without workspace grant=%+v %v", grant, err)
		}
	}
	if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	if grant, err := s.ProjectInstructionReadGrant(t.Context(), childTurn.ID, "standing"); err != nil || grant == nil {
		t.Fatalf("stopped issuer revoked child authority=%+v %v", grant, err)
	}
	execTest(t, s, "PRAGMA query_only=ON")
	for _, target := range []session.Session{owner, child} {
		wantLifecycle := session.Active
		if target.ID == owner.ID {
			wantLifecycle = session.Stopped
		}
		got, grants, err := s.SessionInstructions(t.Context(), target.ID)
		if err != nil || got.ID != target.ID || got.WorkingDirectory != target.WorkingDirectory || got.Lifecycle != wantLifecycle || !reflect.DeepEqual(got.Config.Instructions, policy) || len(grants) != 1 || grants[0].Resource != "project:standing" {
			t.Fatalf("stopped project snapshot=%+v %+v %v", got, grants, err)
		}
		*got.Config.Instructions.ProjectRoot = "caller_mutation"
	}
	execTest(t, s, "PRAGMA query_only=OFF")
	if _, err := s.RevokeGrant(t.Context(), project.ID); err != nil {
		t.Fatal(err)
	}
	if grant, err := s.ProjectInstructionReadGrant(t.Context(), childTurn.ID, "standing"); err != nil || grant != nil {
		t.Fatalf("revoked issuer still authorizes active child: %+v %v", grant, err)
	}
	for _, id := range []session.SessionID{owner.ID, child.ID} {
		if _, grants, err := s.SessionInstructions(t.Context(), id); err != nil || len(grants) != 0 {
			t.Fatalf("revoked project chain remains inspectable=%+v %v", grants, err)
		}
	}
	for _, id := range []string{"", "../standing", "bad/root", "bad root"} {
		if grant, err := s.ProjectInstructionReadGrant(t.Context(), turn.ID, id); grant != nil || !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid project ID %q accepted: %+v %v", id, grant, err)
		}
	}
}

func TestProjectInstructionOneUseNeverAuthorizesCaptureOrInspection(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &session.Instructions{ProjectRoot: new("team")}}); err != nil {
		t.Fatal(err)
	}
	op := admitOperation(t, s, session.OperationSpec{ID: "read", CellID: cell.ID, RequestID: "read", Capability: "instructions.read", Resource: "project:team", Arguments: json.RawMessage(`{}`)})
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	if grant, err := s.ProjectInstructionReadGrant(t.Context(), cell.TurnID, "team"); err != nil || grant != nil {
		t.Fatalf("one-use project capture=%+v %v", grant, err)
	}
	if _, grants, err := s.SessionInstructions(t.Context(), owner.ID); err != nil || len(grants) != 0 {
		t.Fatalf("one-use project inspection=%+v %v", grants, err)
	}
	if dispatched, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !dispatched {
		t.Fatalf("project inspection consumed approval: %v %v", dispatched, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if grant, err := s.ProjectInstructionReadGrant(ctx, cell.TurnID, "team"); grant != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled project authority=%+v %v", grant, err)
	}
}
