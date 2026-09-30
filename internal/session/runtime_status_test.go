package session

import (
	"errors"
	"fmt"
	"testing"
)

func TestTurnAndCommandOutcomeStatusBoundaries(t *testing.T) {
	for _, test := range []struct {
		status   string
		terminal bool
	}{
		{"succeeded", true},
		{"failed", true},
		{"cancelled", true},
		{"interrupted", true},
		{"queued", false},
		{"running", false},
		{"waiting", false},
		{"idle", false},
		{"stopped", false},
		{"deleted", false},
		{"unknown", false},
		{"", false},
		{"Succeeded", false},
		{" succeeded", false},
	} {
		t.Run(fmt.Sprintf("status=%q", test.status), func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			if _, err := store.AdmitCommand(t.Context(), CommandAdmission{
				ClientID: "client", CommandID: "command", Scope: CommandScopeDaemon, RequestDigest: "digest",
			}); err != nil {
				t.Fatal(err)
			}
			_, err := store.FinishCommand(t.Context(), "client", "command", test.status, RuntimePayload{})
			if test.terminal {
				if err != nil {
					t.Fatal(err)
				}
			} else if want := fmt.Sprintf("command outcome status %q is not terminal", test.status); err == nil || err.Error() != want {
				t.Fatalf("command error = %v; want %q", err, want)
			}
			command, err := store.LoadCommand(t.Context(), "client", "command")
			if err != nil {
				t.Fatal(err)
			}
			wantCommand := "queued"
			if test.terminal {
				wantCommand = test.status
			}
			if command.Status != wantCommand {
				t.Fatalf("command status = %q; want %q", command.Status, wantCommand)
			}

			admitTestChild(t, store, root, agent, "child")
			queueOutcomeTurn(t, store, root, "child", "child-turn")
			err = store.FinishAgentTurn(t.Context(), root, "child", AgentTurnCommit{TurnID: "child-turn", Status: test.status})
			if test.terminal {
				if err != nil {
					t.Fatal(err)
				}
			} else if want := fmt.Sprintf("invalid turn status %q", test.status); err == nil || err.Error() != want {
				t.Fatalf("child turn error = %v; want %q", err, want)
			}
			child, err := store.LoadAgent(t.Context(), root, "child")
			if err != nil {
				t.Fatal(err)
			}
			wantAgent, wantTurn := "running", "running"
			if test.terminal {
				wantAgent, wantTurn = "idle", test.status
			}
			if outcome := savedOutcome(t, store, root, "child"); child.Status != wantAgent || outcome == nil || outcome.Status != wantTurn {
				t.Fatalf("child status = %q, outcome = %+v; want %q, %q", child.Status, outcome, wantAgent, wantTurn)
			}

			turnID, err := store.StartRootMailboxTurn(t.Context(), root, agent)
			if err != nil {
				t.Fatal(err)
			}
			err = store.CommitRootTurn(t.Context(), RootTurnCommit{RootID: root, AgentID: agent, TurnID: turnID, Status: test.status})
			// Root commits alone retain the existing empty-status success default.
			wantRootTurn := "running"
			if test.terminal || test.status == "" {
				if err != nil {
					t.Fatal(err)
				}
				wantRootTurn = test.status
				if wantRootTurn == "" {
					wantRootTurn = "succeeded"
				}
			} else if want := fmt.Sprintf("invalid root turn status %q", test.status); err == nil || err.Error() != want {
				t.Fatalf("root turn error = %v; want %q", err, want)
			}
			if outcome := savedOutcome(t, store, root, agent); outcome == nil || outcome.Status != wantRootTurn {
				t.Fatalf("root outcome = %+v; want %q", outcome, wantRootTurn)
			}
		})
	}
}

// Admission uses Go predicates while subscription discovery and root shutdown
// use SQL predicates. They must agree without treating unknown values as terminal.
func TestAgentStatusAdmissionAndSQLBoundariesAgree(t *testing.T) {
	for _, test := range []struct {
		status   string
		terminal bool
	}{
		{"succeeded", true},
		{"failed", true},
		{"cancelled", true},
		{"interrupted", true},
		{"stopped", true},
		{"deleted", true},
		{"queued", false},
		{"running", false},
		{"waiting", false},
		{"idle", false},
		{"unknown", false},
		{"", false},
		{"Failed", false},
		{" failed", false},
	} {
		t.Run(fmt.Sprintf("status=%q", test.status), func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			if _, err := store.CreateBlackboardSubscription(t.Context(), root, agent, "key"); err != nil {
				t.Fatal(err)
			}
			exec(t, store, `UPDATE agents SET status=? WHERE id=?`, test.status, agent)
			subscribers, err := store.SubscribedAgents(t.Context(), root, "key")
			if err != nil {
				t.Fatal(err)
			}
			if test.terminal && len(subscribers) != 0 || !test.terminal && (len(subscribers) != 1 || subscribers[0] != agent) {
				t.Fatalf("subscribers = %v for terminal=%v", subscribers, test.terminal)
			}
			_, err = store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: agent, Kind: "submit", Payload: RuntimePayload{Data: []byte("work")}})
			if test.terminal && !errors.Is(err, ErrRootTerminal) || !test.terminal && err != nil {
				t.Fatalf("enqueue error = %v for terminal=%v", err, test.terminal)
			}
			_, err = store.EnsureAuthority(t.Context(), root)
			if test.terminal && !errors.Is(err, ErrRootTerminal) || !test.terminal && err != nil {
				t.Fatalf("authority error = %v for terminal=%v", err, test.terminal)
			}
			for _, operation := range []string{"stop", "fail"} {
				exec(t, store, `UPDATE agents SET status=? WHERE id=?`, test.status, agent)
				if operation == "stop" {
					_, err = store.StopRoot(t.Context(), root, "test")
				} else {
					_, err = store.FailRoot(t.Context(), root, "test")
				}
				if test.terminal && !errors.Is(err, ErrRootTerminal) || !test.terminal && err != nil {
					t.Fatalf("%s error = %v for terminal=%v", operation, err, test.terminal)
				}
			}
		})
	}
}
