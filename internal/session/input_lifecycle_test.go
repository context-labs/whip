package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
)

func inputTestCommand(t *testing.T, store *Store, rootID, agentID, id, kind string) CommandAdmissionResult {
	t.Helper()
	result, err := store.AdmitCommand(t.Context(), CommandAdmission{
		ClientID: rootID, CommandID: id, Scope: CommandScopeRoot, RootID: rootID,
		AgentID: agentID, Kind: kind, RequestDigest: id, Payload: RuntimePayload{Data: []byte(id)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func inputTestStatus(t *testing.T, store *Store, rootID, agentID string, seq int64, want string) {
	t.Helper()
	var status string
	if err := store.db.QueryRowContext(t.Context(), `SELECT status FROM inbox WHERE root_id=? AND agent_id=? AND seq=?`, rootID, agentID, seq).Scan(&status); err != nil || status != want {
		t.Fatalf("inbox %s/%d status = %q, %v; want %q", agentID, seq, status, err, want)
	}
}

func inputCommandStatus(t *testing.T, store *Store, rootID, id, want string) {
	t.Helper()
	command, err := store.LoadCommand(t.Context(), rootID, id)
	if err != nil || command.Status != want {
		t.Fatalf("command %s status = %+v, %v; want %q", id, command, err, want)
	}
}

func TestRootInputRecoveryPreservesOnlyUnclaimedModelCommands(t *testing.T) {
	for _, action := range []string{"restart", "interrupt", "stop", "fail"} {
		t.Run(action, func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			running := inputTestCommand(t, store, rootID, rootAgentID, "running", "submit")
			if err := store.StartRootTurn(t.Context(), rootID, rootAgentID, running.Command.IngressSeq); err != nil {
				t.Fatal(err)
			}
			turnID, err := store.RunningTurnID(t.Context(), rootID, rootAgentID)
			if err != nil || turnID == "" {
				t.Fatalf("active turn = %q, %v", turnID, err)
			}
			steer := inputTestCommand(t, store, rootID, rootAgentID, "injected-steer", "steer")
			claimed, err := store.ClaimSteers(t.Context(), rootID, rootAgentID, turnID)
			if err != nil || len(claimed) != 1 || claimed[0].Seq != steer.Command.IngressSeq {
				t.Fatalf("claim steer = %+v, %v", claimed, err)
			}
			queued := inputTestCommand(t, store, rootID, rootAgentID, "queued", "submit")
			queuedSteer := inputTestCommand(t, store, rootID, rootAgentID, "queued-steer", "steer")
			for _, status := range []string{"queued", "running", "waiting"} {
				id := "control-" + status
				if _, err := store.AdmitControlCommand(t.Context(), CommandAdmission{
					ClientID: rootID, CommandID: id, Scope: CommandScopeRoot, RootID: rootID,
					AgentID: rootAgentID, Kind: "goal.set", RequestDigest: id,
				}); err != nil {
					t.Fatal(err)
				}
				exec(t, store, `UPDATE commands SET status=? WHERE client_id=? AND command_id=?`, status, rootID, id)
			}
			switch action {
			case "restart":
				store = reopenMailboxStore(t, store)
				err = store.Recover(t.Context())
			case "interrupt":
				_, err = store.InterruptRoot(t.Context(), rootID, "daemon shutdown")
			case "stop":
				_, err = store.StopRoot(t.Context(), rootID, "user stopped session")
			case "fail":
				_, err = store.FailRoot(t.Context(), rootID, "actor failed")
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, command := range []CommandAdmissionResult{running, steer} {
				inputTestStatus(t, store, rootID, rootAgentID, command.Command.IngressSeq, "interrupted")
				inputCommandStatus(t, store, rootID, command.Command.CommandID, "interrupted")
			}
			for _, status := range []string{"queued", "running", "waiting"} {
				inputCommandStatus(t, store, rootID, "control-"+status, "interrupted")
			}
			wantQueued := "queued"
			if action == "stop" || action == "fail" {
				wantQueued = "interrupted"
			}
			for _, command := range []CommandAdmissionResult{queued, queuedSteer} {
				inputTestStatus(t, store, rootID, rootAgentID, command.Command.IngressSeq, wantQueued)
				inputCommandStatus(t, store, rootID, command.Command.CommandID, wantQueued)
			}
			if wantQueued != "queued" {
				return
			}
			for _, command := range []CommandAdmissionResult{queued, queuedSteer} {
				if err := store.StartRootTurn(t.Context(), rootID, rootAgentID, command.Command.IngressSeq); err != nil {
					t.Fatal(err)
				}
				if err := store.CommitRootTurn(t.Context(), RootTurnCommit{
					Model: "model", Provider: "provider",
					RootID: rootID, AgentID: rootAgentID, InboxSeq: command.Command.IngressSeq,
					Messages: []llm.Message{{Role: "assistant", Content: "finished " + command.Command.CommandID}},
					Outcome:  RuntimePayload{Data: []byte("finished")},
				}); err != nil {
					t.Fatalf("preserved command cannot commit: %v", err)
				}
				inputCommandStatus(t, store, rootID, command.Command.CommandID, "succeeded")
			}
			retry := inputTestCommand(t, store, rootID, rootAgentID, "queued", "submit")
			if retry.New || retry.Command.Status != "succeeded" || retry.Command.IngressSeq != queued.Command.IngressSeq {
				t.Fatalf("command retry admitted duplicate input: %+v", retry)
			}
		})
	}
}

func TestRootInterruptionCannotAlterAnotherRunningTree(t *testing.T) {
	store, firstRoot, firstAgent := newSwarmFixture(t)
	secondRoot, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	secondAuthority, err := store.EnsureAuthority(t.Context(), secondRoot)
	if err != nil {
		t.Fatal(err)
	}
	for index, fixture := range []struct{ root, agent string }{{firstRoot, firstAgent}, {secondRoot, secondAuthority.AgentID}} {
		child := fmt.Sprintf("child-%d", index)
		admitTestChild(t, store, fixture.root, fixture.agent, child)
		rootCommand := inputTestCommand(t, store, fixture.root, fixture.agent, "active", "submit")
		if err := store.StartRootTurn(t.Context(), fixture.root, fixture.agent, rootCommand.Command.IngressSeq); err != nil {
			t.Fatal(err)
		}
		inputTestCommand(t, store, fixture.root, fixture.agent, "queued", "submit")
		for _, kind := range []string{"submit", "steer"} {
			if _, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: fixture.root, AgentID: child, Kind: kind, Payload: RuntimePayload{Data: []byte(kind)}}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.StartAgentTurn(t.Context(), fixture.root, child, child+"-turn"); err != nil {
			t.Fatal(err)
		}
		grant := capability.Grant{ID: child + "-read", RootID: fixture.root, AgentID: child, Operations: []string{"read"}}
		if err := store.IssueCapability(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Begin(t.Context(), capability.Admission{Request: capability.Request{
			RootID: fixture.root, AgentID: child, CapabilityID: grant.ID, OperationID: child + "-operation", Operation: "read", TraceID: child,
			Reservations: []capability.Reservation{{Kind: string(BudgetActiveOperations), Amount: 1}},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	before := inputRootSnapshot(t, store, secondRoot)
	if _, err := store.InterruptRoot(t.Context(), firstRoot, "shutdown one session"); err != nil {
		t.Fatal(err)
	}
	if after := inputRootSnapshot(t, store, secondRoot); !reflect.DeepEqual(before, after) {
		t.Fatalf("other root changed:\nbefore=%v\nafter=%v", before, after)
	}
	var childStatus string
	if err := store.db.QueryRowContext(t.Context(), `SELECT status FROM agents WHERE id='child-0'`).Scan(&childStatus); err != nil || childStatus != "idle" {
		t.Fatalf("interrupted child status = %q, %v", childStatus, err)
	}
	if state := budgetState(t, store, firstRoot, firstAgent, BudgetConcurrentChildTurns); state.Reserved != 0 {
		t.Fatalf("interrupted turn reservation = %+v", state)
	}
	if state := budgetState(t, store, firstRoot, firstAgent, BudgetActiveOperations); state.Reserved != 0 {
		t.Fatalf("interrupted operation reservation = %+v", state)
	}
	inputTestStatus(t, store, firstRoot, "child-0", 1, "interrupted")
	inputTestStatus(t, store, firstRoot, "child-0", 2, "queued")
	inputCommandStatus(t, store, firstRoot, "active", "interrupted")
	inputCommandStatus(t, store, firstRoot, "queued", "queued")
	if state := budgetState(t, store, secondRoot, secondAuthority.AgentID, BudgetConcurrentChildTurns); state.Reserved != 1 {
		t.Fatalf("other turn reservation = %+v", state)
	}
}

func inputRootSnapshot(t *testing.T, store *Store, rootID string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, table := range []string{"agents", "commands", "turns", "inbox", "budgets", "operations", "leases", "events"} {
		rows, err := store.db.QueryContext(t.Context(), `SELECT * FROM `+table+` WHERE root_id=? ORDER BY rowid`, rootID)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var all [][]any
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			all = append(all, values)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(all)
		if err != nil {
			t.Fatal(err)
		}
		result[table] = string(encoded)
	}
	return result
}

func TestStartRootTurnRejectsTerminalCommandAtomically(t *testing.T) {
	for _, status := range []string{"interrupted", "failed", "succeeded", "cancelled", "running", "waiting"} {
		t.Run(status, func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			command := inputTestCommand(t, store, rootID, rootAgentID, "inconsistent", "submit")
			exec(t, store, `UPDATE commands SET status=? WHERE client_id=? AND command_id='inconsistent'`, status, rootID)
			before := inputRootSnapshot(t, store, rootID)
			if err := store.StartRootTurn(t.Context(), rootID, rootAgentID, command.Command.IngressSeq); err == nil {
				t.Fatal("nonqueued command started model work")
			}
			if after := inputRootSnapshot(t, store, rootID); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed admission retained partial writes:\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestClaimSteersRequiresExactActiveTurnAndRollsBackBatch(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	initial := inputTestCommand(t, store, rootID, rootAgentID, "initial", "submit")
	if err := store.StartRootTurn(t.Context(), rootID, rootAgentID, initial.Command.IngressSeq); err != nil {
		t.Fatal(err)
	}
	turnID, err := store.RunningTurnID(t.Context(), rootID, rootAgentID)
	if err != nil {
		t.Fatal(err)
	}
	first := inputTestCommand(t, store, rootID, rootAgentID, "first-steer", "steer")
	second := inputTestCommand(t, store, rootID, rootAgentID, "second-steer", "steer.parts")
	for _, test := range []struct{ root, agent, turn string }{
		{rootID, rootAgentID, "missing"}, {rootID, "child", turnID}, {"missing", rootAgentID, turnID},
	} {
		if _, err := store.ClaimSteers(t.Context(), test.root, test.agent, test.turn); err == nil {
			t.Fatalf("mismatched turn admitted steer: %+v", test)
		}
	}
	before := inputRootSnapshot(t, store, rootID)
	exec(t, store, `UPDATE commands SET status='interrupted' WHERE client_id=? AND command_id='second-steer'`, rootID)
	if _, err := store.ClaimSteers(t.Context(), rootID, rootAgentID, turnID); err == nil {
		t.Fatal("steer claim started a terminal correlated command")
	}
	inputCommandStatus(t, store, rootID, "first-steer", "queued")
	inputTestStatus(t, store, rootID, rootAgentID, first.Command.IngressSeq, "queued")
	exec(t, store, `UPDATE commands SET status='queued' WHERE client_id=? AND command_id='second-steer'`, rootID)
	exec(t, store, `CREATE TRIGGER fail_second_steer BEFORE UPDATE ON inbox WHEN NEW.kind='steer.parts' BEGIN SELECT RAISE(ABORT,'claim failure'); END`)
	if _, err := store.ClaimSteers(t.Context(), rootID, rootAgentID, turnID); err == nil {
		t.Fatal("claim ignored transactional failure")
	}
	if after := inputRootSnapshot(t, store, rootID); !reflect.DeepEqual(before, after) {
		t.Fatal("failed steer claim did not roll back both inbox and command states")
	}
	exec(t, store, `DROP TRIGGER fail_second_steer`)
	claimed, err := store.ClaimSteers(t.Context(), rootID, rootAgentID, turnID)
	if err != nil || len(claimed) != 2 || claimed[0].Seq != first.Command.IngressSeq || claimed[1].Seq != second.Command.IngressSeq {
		t.Fatalf("claim = %+v, %v", claimed, err)
	}
	for _, item := range claimed {
		if item.Status != "running" {
			t.Fatalf("claim returned stale status: %+v", item)
		}
	}
	if more, err := store.ClaimSteers(t.Context(), rootID, rootAgentID, turnID); err != nil || len(more) != 0 {
		t.Fatalf("duplicate boundary claim = %+v, %v", more, err)
	}
	if err := store.CommitRootTurn(t.Context(), RootTurnCommit{
		Model: "model", Provider: "provider",
		RootID: rootID, AgentID: rootAgentID, InboxSeq: initial.Command.IngressSeq,
		AcknowledgedInbox: []int64{first.Command.IngressSeq, second.Command.IngressSeq},
	}); err != nil {
		t.Fatal(err)
	}
	late := inputTestCommand(t, store, rootID, rootAgentID, "late", "steer")
	if err := store.StartRootTurn(t.Context(), rootID, rootAgentID, late.Command.IngressSeq); err != nil {
		t.Fatal(err)
	}
	inputTestCommand(t, store, rootID, rootAgentID, "next-steer", "steer")
	if _, err := store.ClaimSteers(t.Context(), rootID, rootAgentID, turnID); err == nil {
		t.Fatal("late boundary claimed a later turn's input")
	}
}

func TestChildSteerSequenceDoesNotTouchRootCommand(t *testing.T) {
	store, rootID, rootAgentID := newMailboxFixture(t)
	rootInput := inputTestCommand(t, store, rootID, rootAgentID, "root-one", "submit")
	if err := store.StartRootTurn(t.Context(), rootID, rootAgentID, rootInput.Command.IngressSeq); err != nil {
		t.Fatal(err)
	}
	rootQueued := inputTestCommand(t, store, rootID, rootAgentID, "root-two", "submit")
	if _, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: "child", Kind: "submit", Payload: RuntimePayload{Data: []byte("child initial")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "child-turn"); err != nil {
		t.Fatal(err)
	}
	steer, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: "child", Kind: "steer", Payload: RuntimePayload{Data: []byte("child steer")}})
	if err != nil || steer.InboxSeq != rootQueued.Command.IngressSeq {
		t.Fatalf("fixture must collide sequences: %+v, %v", steer, err)
	}
	claimed, err := store.ClaimSteers(t.Context(), rootID, "child", "child-turn")
	if err != nil || len(claimed) != 1 {
		t.Fatalf("child claim = %+v, %v", claimed, err)
	}
	inputCommandStatus(t, store, rootID, "root-two", "queued")
	if err := store.RejectTurnInput(t.Context(), rootID, "child", "child-turn", steer.InboxSeq, "invalid child payload"); err != nil {
		t.Fatal(err)
	}
	inputCommandStatus(t, store, rootID, "root-two", "queued")
	inputTestStatus(t, store, rootID, rootAgentID, rootQueued.Command.IngressSeq, "queued")
	inputTestStatus(t, store, rootID, "child", steer.InboxSeq, "interrupted")
}

func TestRejectTurnInputSettlesInvalidSteerWithoutFailingTurn(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	initial := inputTestCommand(t, store, rootID, rootAgentID, "initial", "submit")
	if err := store.StartRootTurn(t.Context(), rootID, rootAgentID, initial.Command.IngressSeq); err != nil {
		t.Fatal(err)
	}
	turnID, err := store.RunningTurnID(t.Context(), rootID, rootAgentID)
	if err != nil {
		t.Fatal(err)
	}
	steer := inputTestCommand(t, store, rootID, rootAgentID, "bad-steer", "steer.parts")
	if _, err := store.ClaimSteers(t.Context(), rootID, rootAgentID, turnID); err != nil {
		t.Fatal(err)
	}
	before := inputRootSnapshot(t, store, rootID)
	if err := store.RejectTurnInput(t.Context(), rootID, rootAgentID, "stale-turn", steer.Command.IngressSeq, "bad payload"); err == nil {
		t.Fatal("stale turn rejected input owned by live turn")
	}
	exec(t, store, `CREATE TRIGGER fail_input_event BEFORE INSERT ON events WHEN NEW.kind='inbox.failed' BEGIN SELECT RAISE(ABORT,'reject failure'); END`)
	if err := store.RejectTurnInput(t.Context(), rootID, rootAgentID, turnID, steer.Command.IngressSeq, "bad payload"); err == nil {
		t.Fatal("rejection ignored event failure")
	}
	if after := inputRootSnapshot(t, store, rootID); !reflect.DeepEqual(before, after) {
		t.Fatal("failed rejection retained partial input or command settlement")
	}
	exec(t, store, `DROP TRIGGER fail_input_event`)
	if err := store.RejectTurnInput(t.Context(), rootID, rootAgentID, turnID, steer.Command.IngressSeq, "bad payload"); err != nil {
		t.Fatal(err)
	}
	inputTestStatus(t, store, rootID, rootAgentID, steer.Command.IngressSeq, "interrupted")
	command, err := store.LoadCommand(t.Context(), rootID, "bad-steer")
	if err != nil || command.Status != "failed" || !strings.Contains(string(command.Outcome.Inline), "bad payload") {
		t.Fatalf("rejected command = %+v, %v", command, err)
	}
	if err := store.RejectTurnInput(t.Context(), rootID, rootAgentID, turnID, steer.Command.IngressSeq, "again"); !errors.Is(err, ErrInboxTerminal) {
		t.Fatalf("duplicate rejection = %v", err)
	}
	if err := store.CommitRootTurn(t.Context(), RootTurnCommit{
		Model: "model", Provider: "provider",
		RootID: rootID, AgentID: rootAgentID, InboxSeq: initial.Command.IngressSeq,
		Messages: []llm.Message{{Role: "assistant", Content: "valid work completed"}},
	}); err != nil {
		t.Fatalf("valid turn could not finish after rejecting steer: %v", err)
	}
	inputCommandStatus(t, store, rootID, "initial", "succeeded")
}

func TestRootCommitSettlesUnacknowledgedSteerClaimsOnly(t *testing.T) {
	for _, status := range []string{"succeeded", "failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			initial := inputTestCommand(t, store, rootID, rootAgentID, "initial", "submit")
			if err := store.StartRootTurn(t.Context(), rootID, rootAgentID, initial.Command.IngressSeq); err != nil {
				t.Fatal(err)
			}
			claimed := inputTestCommand(t, store, rootID, rootAgentID, "claimed-not-delivered", "steer")
			turnID, err := store.RunningTurnID(t.Context(), rootID, rootAgentID)
			if err != nil {
				t.Fatal(err)
			}
			if items, err := store.ClaimSteers(t.Context(), rootID, rootAgentID, turnID); err != nil || len(items) != 1 {
				t.Fatalf("claim = %+v, %v", items, err)
			}
			queued := inputTestCommand(t, store, rootID, rootAgentID, "unclaimed", "steer")
			if err := store.CommitRootTurn(t.Context(), RootTurnCommit{
				RootID: rootID, AgentID: rootAgentID, InboxSeq: initial.Command.IngressSeq,
				Status: status, Model: "model", Provider: "provider",
			}); err != nil {
				t.Fatal(err)
			}
			inputTestStatus(t, store, rootID, rootAgentID, claimed.Command.IngressSeq, "interrupted")
			inputCommandStatus(t, store, rootID, "claimed-not-delivered", "interrupted")
			inputTestStatus(t, store, rootID, rootAgentID, queued.Command.IngressSeq, "queued")
			inputCommandStatus(t, store, rootID, "unclaimed", "queued")
		})
	}
}

func TestTurnCommitRejectsUnclaimedAndStaleInboxAcknowledgements(t *testing.T) {
	for _, recipient := range []string{"root", "child"} {
		for _, itemStatus := range []string{"queued", "consumed", "interrupted", "missing"} {
			t.Run(recipient+"/"+itemStatus, func(t *testing.T) {
				store, rootID, rootAgentID := newMailboxFixture(t)
				agentID := rootAgentID
				if recipient == "child" {
					agentID = "child"
				}
				initial, err := store.EnqueueInbox(t.Context(), InboxEnqueue{
					RootID: rootID, AgentID: agentID, Kind: "submit", Payload: RuntimePayload{Data: []byte("valid work")},
				})
				if err != nil {
					t.Fatal(err)
				}
				if recipient == "root" {
					err = store.StartRootTurn(t.Context(), rootID, agentID, initial.InboxSeq)
				} else {
					_, err = store.StartAgentTurn(t.Context(), rootID, agentID, "child-turn")
				}
				if err != nil {
					t.Fatal(err)
				}
				stale, err := store.EnqueueInbox(t.Context(), InboxEnqueue{
					RootID: rootID, AgentID: agentID, Kind: "steer", Payload: RuntimePayload{Data: []byte("not owned by journal")},
				})
				if err != nil {
					t.Fatal(err)
				}
				acknowledged := stale.InboxSeq
				switch itemStatus {
				case "consumed":
					// Non-turn consumers intentionally retain queued consumption.
					if _, err := store.ConsumeInbox(t.Context(), rootID, agentID, stale.InboxSeq); err != nil {
						t.Fatal(err)
					}
				case "interrupted":
					exec(t, store, `UPDATE inbox SET status='interrupted' WHERE root_id=? AND agent_id=? AND seq=?`, rootID, agentID, stale.InboxSeq)
				case "missing":
					acknowledged++
				}
				before := inputRootSnapshot(t, store, rootID)
				if recipient == "root" {
					err = store.CommitRootTurn(t.Context(), RootTurnCommit{
						RootID: rootID, AgentID: agentID, InboxSeq: initial.InboxSeq,
						AcknowledgedInbox: []int64{acknowledged}, Model: "model", Provider: "provider",
						Messages: []llm.Message{{Role: "assistant", Content: "must not persist"}},
					})
				} else {
					err = store.FinishAgentTurn(t.Context(), rootID, agentID, AgentTurnCommit{
						TurnID: "child-turn", Status: "succeeded", AcknowledgedInbox: []int64{initial.InboxSeq, acknowledged},
						Messages: []llm.Message{{Role: "assistant", Content: "must not persist"}},
					})
				}
				if !errors.Is(err, ErrInboxTerminal) {
					t.Fatalf("invalid acknowledgement = %v", err)
				}
				if after := inputRootSnapshot(t, store, rootID); !reflect.DeepEqual(before, after) {
					t.Fatalf("rejected journal retained partial settlement:\nbefore=%v\nafter=%v", before, after)
				}
				var history int
				if err := store.db.QueryRowContext(t.Context(), `SELECT
					(SELECT count(*) FROM messages WHERE session_id=?) +
					(SELECT count(*) FROM transcript_messages WHERE root_id=?)`, rootID, rootID).Scan(&history); err != nil || history != 0 {
					t.Fatalf("rejected journal persisted history = %d, %v", history, err)
				}
			})
		}
	}
}

func TestRootTurnCommitRequiresCorrelatedCommandStillRunning(t *testing.T) {
	for _, status := range []string{"queued", "waiting"} {
		t.Run(status, func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			command := inputTestCommand(t, store, rootID, rootAgentID, "initial", "submit")
			if err := store.StartRootTurn(t.Context(), rootID, rootAgentID, command.Command.IngressSeq); err != nil {
				t.Fatal(err)
			}
			exec(t, store, `UPDATE commands SET status=? WHERE client_id=? AND command_id='initial'`, status, rootID)
			before := inputRootSnapshot(t, store, rootID)
			if err := store.CommitRootTurn(t.Context(), RootTurnCommit{
				RootID: rootID, AgentID: rootAgentID, InboxSeq: command.Command.IngressSeq,
				Model: "model", Provider: "provider", Outcome: RuntimePayload{Data: []byte("must not persist")},
			}); err == nil {
				t.Fatal("turn completed a command that was not running")
			}
			if after := inputRootSnapshot(t, store, rootID); !reflect.DeepEqual(before, after) {
				t.Fatal("invalid command settlement retained partial writes")
			}
		})
	}
}

func TestChildTurnDuplicateClaimReceiptsRemainIdempotent(t *testing.T) {
	store, rootID, _ := newMailboxFixture(t)
	input, err := store.EnqueueInbox(t.Context(), InboxEnqueue{
		RootID: rootID, AgentID: "child", Kind: "submit", Payload: RuntimePayload{Data: []byte("work")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "child-turn"); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{
		TurnID: "child-turn", Status: "succeeded", AcknowledgedInbox: []int64{input.InboxSeq, input.InboxSeq},
	}); err != nil {
		t.Fatal(err)
	}
	inputTestStatus(t, store, rootID, "child", input.InboxSeq, "consumed")
	var consumedEvents int
	if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM events WHERE root_id=? AND kind='inbox.consumed'`, rootID).Scan(&consumedEvents); err != nil || consumedEvents != 1 {
		t.Fatalf("duplicate receipt consumed %d times, %v", consumedEvents, err)
	}
}

func TestCorruptInputIsClaimedAndSettledWithoutStrandingAgent(t *testing.T) {
	for _, recipient := range []string{"root", "child"} {
		for _, boundary := range []bool{false, true} {
			for _, corruption := range []string{"oversized inline", "missing reference"} {
				name := fmt.Sprintf("%s/boundary_%t/%s", recipient, boundary, corruption)
				t.Run(name, func(t *testing.T) {
					store, rootID, rootAgentID := newMailboxFixture(t)
					agentID := rootAgentID
					if recipient == "child" {
						agentID = "child"
					}
					enqueue := func(id, kind string) int64 {
						if recipient == "root" {
							return inputTestCommand(t, store, rootID, agentID, id, kind).Command.IngressSeq
						}
						sequence, err := store.EnqueueInbox(t.Context(), InboxEnqueue{
							RootID: rootID, AgentID: agentID, Kind: kind, Payload: RuntimePayload{Data: []byte(id)},
						})
						if err != nil {
							t.Fatal(err)
						}
						return sequence.InboxSeq
					}
					start := func(seq int64, id string) InboxItem {
						if recipient == "child" {
							claim, err := store.StartAgentTurn(t.Context(), rootID, agentID, id)
							if err != nil || len(claim.Items) != 1 || claim.Items[0].Seq != seq {
								t.Fatalf("child claim = %+v, %v", claim, err)
							}
							return claim.Items[0]
						}
						items, err := store.LoadQueuedInbox(t.Context(), rootID, agentID, seq-1, 1)
						if err != nil || len(items) != 1 {
							t.Fatalf("root input discovery = %+v, %v", items, err)
						}
						if err := store.StartRootTurn(t.Context(), rootID, agentID, seq); err != nil {
							t.Fatalf("root claim = %v", err)
						}
						return items[0]
					}
					finish := func(seq int64, id, status string, invalid bool) {
						var err error
						if recipient == "root" {
							err = store.CommitRootTurn(t.Context(), RootTurnCommit{
								RootID: rootID, AgentID: agentID, InboxSeq: seq, Status: status,
								Model: "model", Provider: "provider",
							})
						} else {
							var acknowledged []int64
							if !invalid {
								acknowledged = []int64{seq}
							}
							err = store.FinishAgentTurn(t.Context(), rootID, agentID, AgentTurnCommit{
								TurnID: id, Status: status, RetryInput: false, AcknowledgedInbox: acknowledged,
							})
						}
						if err != nil {
							t.Fatalf("settlement = %v", err)
						}
					}
					var initial int64
					kind := "submit"
					if boundary {
						initial = enqueue("initial", "submit")
						start(initial, "active-turn")
						kind = "steer"
					}
					bad := enqueue("bad", kind)
					if corruption == "oversized inline" {
						exec(t, store, `UPDATE inbox SET payload_inline=?,payload_ref=NULL WHERE root_id=? AND agent_id=? AND seq=?`,
							[]byte(strings.Repeat("x", 4*InlineValueLimit)), rootID, agentID, bad)
					} else {
						exec(t, store, `PRAGMA foreign_keys=OFF`)
						exec(t, store, `UPDATE inbox SET payload_inline=NULL,payload_ref='missing-reference' WHERE root_id=? AND agent_id=? AND seq=?`, rootID, agentID, bad)
						exec(t, store, `PRAGMA foreign_keys=ON`)
					}
					var item InboxItem
					if boundary {
						turnID, err := store.RunningTurnID(t.Context(), rootID, agentID)
						if err != nil {
							t.Fatal(err)
						}
						claimed, err := store.ClaimSteers(t.Context(), rootID, agentID, turnID)
						if err != nil || len(claimed) != 1 || claimed[0].Seq != bad {
							t.Fatalf("corrupt steer prevented its durable claim: %+v, %v", claimed, err)
						}
						item = claimed[0]
					} else {
						item = start(bad, "active-turn")
					}
					inputTestStatus(t, store, rootID, agentID, bad, "running")
					if corruption == "oversized inline" && len(item.Payload.Inline) != InlineValueLimit+1 {
						t.Fatalf("corrupt inline discovery was not bounded: %d bytes", len(item.Payload.Inline))
					}
					_, resolveErr := store.ResolveInboxPayload(t.Context(), item)
					if !errors.Is(resolveErr, ErrInvalidInput) {
						t.Fatalf("corrupt input resolution = %v", resolveErr)
					}
					if boundary {
						turnID, err := store.RunningTurnID(t.Context(), rootID, agentID)
						if err != nil {
							t.Fatal(err)
						}
						if err := store.RejectTurnInput(t.Context(), rootID, agentID, turnID, bad, resolveErr.Error()); err != nil {
							t.Fatal(err)
						}
						finish(initial, "active-turn", "succeeded", false)
					} else {
						finish(bad, "active-turn", "failed", true)
					}
					wantInputStatus := "interrupted"
					if recipient == "root" {
						inputCommandStatus(t, store, rootID, "bad", "failed")
						if !boundary {
							wantInputStatus = "consumed"
						}
					}
					inputTestStatus(t, store, rootID, agentID, bad, wantInputStatus)
					var status string
					if err := store.db.QueryRowContext(t.Context(), `SELECT status FROM agents WHERE root_id=? AND id=?`, rootID, agentID).Scan(&status); err != nil || status != "idle" {
						t.Fatalf("recipient left unavailable: %q, %v", status, err)
					}
					if turnID, err := store.RunningTurnID(t.Context(), rootID, agentID); err != nil || turnID != "" {
						t.Fatalf("stranded running turn = %q, %v", turnID, err)
					}
					if state := budgetState(t, store, rootID, rootAgentID, BudgetConcurrentChildTurns); state.Reserved != 0 {
						t.Fatalf("stranded turn budget = %+v", state)
					}
					next := enqueue("next", "submit")
					nextItem := start(next, "next-turn")
					if data, err := store.ResolveInboxPayload(t.Context(), nextItem); err != nil || string(data) != "next" {
						t.Fatalf("following input = %q, %v", data, err)
					}
					finish(next, "next-turn", "succeeded", false)
				})
			}
		}
	}
}
