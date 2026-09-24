package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
)

func activeRootForStorageTest(t *testing.T) (*Store, string) {
	t.Helper()
	store, root, seq := queuedAgentForStorageTest(t)
	if _, err := store.StartAgentTurn(t.Context(), root, "child", "child-turn"); err != nil {
		t.Fatal(err)
	}
	if seq < 1 {
		t.Fatal("missing child input")
	}
	command, err := store.AdmitCommand(t.Context(), CommandAdmission{ClientID: "client", CommandID: "command", Scope: CommandScopeRoot, RootID: root, AgentID: root, Kind: "submit", RequestDigest: "request", Payload: RuntimePayload{Data: []byte("human work")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StartRootTurn(t.Context(), root, root, command.Command.IngressSeq); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitModelCall(t.Context(), root, "child", modelAttempt("active-model", 2, 10)); err != nil {
		t.Fatal(err)
	}
	for _, op := range []struct {
		id      string
		pending bool
	}{{"running-operation", false}, {"pending-operation", true}} {
		admission := capability.Admission{Request: capability.Request{RootID: root, AgentID: root, CapabilityID: "files:" + root, CapabilityGeneration: 1, OperationID: op.id, Operation: "read"}, RequirePermission: op.pending}
		if _, err := store.Begin(t.Context(), admission); err != nil {
			t.Fatal(err)
		}
	}
	return store, root
}

func TestRootShutdownStorageFailuresRollBackEveryLedger(t *testing.T) {
	for _, mode := range []string{"stop", "fail", "interrupt", "recover"} {
		t.Run(mode, func(t *testing.T) {
			for _, failure := range []struct{ name, mutation, table, condition string }{
				{"model accounting", "UPDATE", "model_calls", ""},
				{"permission state", "UPDATE", "permission_requests", ""},
				{"operation state", "UPDATE", "operations", ""},
				{"lease state", "UPDATE", "leases", ""},
				{"budget state", "UPDATE", "budgets", ""},
				{"root terminal turn event", "INSERT", "events", "NEW.kind='turn.interrupted'"},
				{"child terminal turn event", "INSERT", "events", "NEW.kind='agent.turn.interrupted'"},
				{"turn state", "UPDATE", "turns", ""},
				{"child state", "UPDATE", "agents", "NEW.id='child'"},
				{"command state", "UPDATE", "commands", ""},
				{"input state", "UPDATE", "inbox", ""},
			} {
				t.Run(failure.name, func(t *testing.T) {
					store, root := activeRootForStorageTest(t)
					before := inputRootSnapshot(t, store, root)
					beforeAccounting, err := store.ModelAccounting(t.Context(), root, "", true)
					if err != nil {
						t.Fatal(err)
					}
					shutdown := func() error {
						switch mode {
						case "stop":
							_, err := store.StopRoot(t.Context(), root, "requested stop")
							return err
						case "fail":
							_, err := store.FailRoot(t.Context(), root, "worker failed")
							return err
						case "interrupt":
							_, err := store.InterruptRoot(t.Context(), root, "connection lost")
							return err
						default:
							return store.Recover(t.Context())
						}
					}
					rejectSessionWrite(t, store, failure.mutation, failure.table, failure.condition)
					requireSessionWriteFailure(t, shutdown())
					requireRootUnchanged(t, store, root, before)
					accounting, err := store.ModelAccounting(t.Context(), root, "", true)
					if err != nil || accounting != beforeAccounting {
						t.Fatalf("failed shutdown changed accounting: %+v -> %+v %v", beforeAccounting, accounting, err)
					}
					exec(t, store, "DROP TRIGGER reject_session_write")
					if err := shutdown(); err != nil {
						t.Fatal(err)
					}
					for _, agent := range []string{root, "child"} {
						if turn, err := store.ActiveTurn(t.Context(), root, agent); err != nil || turn != "" {
							t.Fatalf("retry left %s turn %q %v", agent, turn, err)
						}
					}
					accounting, err = store.ModelAccounting(t.Context(), root, "", true)
					if err != nil || accounting.PendingCalls != 0 {
						t.Fatalf("retry left unsettled model call: %+v %v", accounting, err)
					}
				})
			}
		})
	}
}

func TestRootTerminalEventFailureDoesNotStopOnlyPartOfTheTree(t *testing.T) {
	for _, kind := range []string{"root.stopped", "root.failed", "root.interrupted"} {
		t.Run(kind, func(t *testing.T) {
			store, root := activeRootForStorageTest(t)
			before := inputRootSnapshot(t, store, root)
			rejectSessionWrite(t, store, "INSERT", "events", "NEW.kind='"+kind+"'")
			var err error
			switch kind {
			case "root.stopped":
				_, err = store.StopRoot(t.Context(), root, "stop")
			case "root.failed":
				_, err = store.FailRoot(t.Context(), root, "failed")
			default:
				_, err = store.InterruptRoot(t.Context(), root, "interrupted")
			}
			requireSessionWriteFailure(t, err)
			requireRootUnchanged(t, store, root, before)
		})
	}
}

func TestRootLifecycleRequiresIdentityAndAvailableStorage(t *testing.T) {
	store, root, _ := newSwarmFixture(t)
	for name, call := range map[string]func(context.Context, string) error{
		"stop": func(ctx context.Context, id string) error { _, err := store.StopRoot(ctx, id, "stop"); return err },
		"fail": func(ctx context.Context, id string) error { _, err := store.FailRoot(ctx, id, "failure"); return err },
		"interrupt": func(ctx context.Context, id string) error {
			_, err := store.InterruptRoot(ctx, id, "interruption")
			return err
		},
		"mailbox start": func(ctx context.Context, id string) error {
			_, err := store.StartRootMailboxTurn(ctx, id, root)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(t.Context(), ""); err == nil {
				t.Fatal("empty identity accepted")
			}
		})
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func(context.Context) error{
		"stop":          func(ctx context.Context) error { _, err := store.StopRoot(ctx, root, "stop"); return err },
		"fail":          func(ctx context.Context) error { _, err := store.FailRoot(ctx, root, "failure"); return err },
		"interrupt":     func(ctx context.Context) error { _, err := store.InterruptRoot(ctx, root, "interruption"); return err },
		"mailbox start": func(ctx context.Context) error { _, err := store.StartRootMailboxTurn(ctx, root, root); return err },
		"recover":       func(ctx context.Context) error { return store.Recover(ctx) },
		"append event": func(ctx context.Context) error {
			_, err := store.AppendRootEvent(ctx, root, "stream.text", RuntimePayload{})
			return err
		},
	} {
		t.Run(name+" closed", func(t *testing.T) {
			if err := call(t.Context()); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("closed store result=%v", err)
			}
		})
	}
}

func TestRootTurnCommitWriteFailuresPreserveHumanInputAndHistory(t *testing.T) {
	for _, failure := range []struct {
		name, mutation, table, condition string
		clearGoal                        bool
	}{
		{"turn outcome", "UPDATE", "turns", "", false},
		{"input acknowledgement", "UPDATE", "inbox", "NEW.status='consumed'", false},
		{"command outcome", "UPDATE", "commands", "NEW.status='succeeded'", false},
		{"raw history", "INSERT", "messages", "", false},
		{"compaction", "INSERT", "compactions", "", false},
		{"workspace snapshot", "INSERT", "snapshots", "", false},
		{"session metadata", "UPDATE", "sessions", "NEW.model='new-model'", false},
		{"goal continuation", "INSERT", "inbox", "NEW.kind='goal'", false},
		{"goal event", "INSERT", "events", "NEW.kind='goal.continued'", false},
		{"clear goal", "UPDATE", "sessions", "NEW.goal='' AND OLD.goal<>''", true},
		{"turn event", "INSERT", "events", "NEW.kind='turn.succeeded'", false},
	} {
		t.Run(failure.name, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			if err := store.SetGoal(root, "finish research"); err != nil {
				t.Fatal(err)
			}
			command, err := store.AdmitCommand(t.Context(), CommandAdmission{ClientID: "client", CommandID: "request", Scope: CommandScopeRoot, RootID: root, AgentID: agent, Kind: "submit", RequestDigest: "digest", Payload: RuntimePayload{Data: []byte("research")}})
			if err != nil {
				t.Fatal(err)
			}
			seq := command.Command.IngressSeq
			if err := store.StartRootTurn(t.Context(), root, agent, seq); err != nil {
				t.Fatal(err)
			}
			before := inputRootSnapshot(t, store, root)
			commit := RootTurnCommit{RootID: root, AgentID: agent, InboxSeq: seq, Status: "succeeded", Model: "new-model", Provider: "new-provider", Messages: []llm.Message{{Role: "assistant", Content: "research result"}}, Compactions: []RootCompaction{{Summary: "summary", RawCutoff: new(1)}}, WorkspaceRef: "workspace-snapshot", WorkspaceSeq: 1, GoalContinuation: "continue research"}
			if failure.clearGoal {
				commit.ClearGoal = true
				commit.GoalContinuation = ""
			}
			rejectSessionWrite(t, store, failure.mutation, failure.table, failure.condition)
			requireSessionWriteFailure(t, store.CommitRootTurn(t.Context(), commit))
			requireRootUnchanged(t, store, root, before)
			snapshot, err := store.SnapshotRoot(t.Context(), root)
			if err != nil || len(snapshot.Messages) != 0 || snapshot.Meta.Model != "model" || snapshot.Meta.Goal != "finish research" {
				t.Fatalf("failed commit changed human-visible history or settings: %+v %v", snapshot, err)
			}
			var saved int
			if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM snapshots WHERE session_id=?`, root).Scan(&saved); err != nil || saved != 0 {
				t.Fatalf("failed commit leaked workspace snapshot %d %v", saved, err)
			}
			exec(t, store, "DROP TRIGGER reject_session_write")
			if err := store.CommitRootTurn(t.Context(), commit); err != nil {
				t.Fatal(err)
			}
			raw, err := store.ReadTranscript(t.Context(), root, agent, 0, -1, 10)
			if err != nil || len(raw.Messages) != 1 || raw.Messages[0].Message.Content != "research result" {
				t.Fatalf("retry lost raw history: %+v %v", raw, err)
			}
			stored, err := store.LoadCommand(t.Context(), "client", "request")
			if err != nil || stored.Status != "succeeded" {
				t.Fatalf("retry command=%+v %v", stored, err)
			}
		})
	}
}

func TestRootMailboxStartWriteFailuresDoNotLeaveAnActiveTurn(t *testing.T) {
	for _, table := range []string{"turns", "events"} {
		t.Run(table, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			before := inputRootSnapshot(t, store, root)
			rejectSessionWrite(t, store, "INSERT", table, "")
			_, err := store.StartRootMailboxTurn(t.Context(), root, agent)
			requireSessionWriteFailure(t, err)
			requireRootUnchanged(t, store, root, before)
			exec(t, store, "DROP TRIGGER reject_session_write")
			turn, err := store.StartRootMailboxTurn(t.Context(), root, agent)
			if err != nil || turn == "" {
				t.Fatalf("retry turn=%q %v", turn, err)
			}
		})
	}
}

func TestFinishingCommandCannotPublishAnOutcomeBeforeItsEvent(t *testing.T) {
	for _, table := range []string{"commands", "events", "content_references", "content_grants"} {
		t.Run(table, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			if _, err := store.AdmitCommand(t.Context(), CommandAdmission{ClientID: "client", CommandID: "request", Scope: CommandScopeRoot, RootID: root, AgentID: agent, Kind: "submit", RequestDigest: "digest", Payload: RuntimePayload{Data: []byte("work")}}); err != nil {
				t.Fatal(err)
			}
			before := inputRootSnapshot(t, store, root)
			mutation := "INSERT"
			if table == "commands" {
				mutation = "UPDATE"
			}
			rejectSessionWrite(t, store, mutation, table, "")
			outcome := RuntimePayload{Data: []byte(strings.Repeat("result", 2000))}
			_, err := store.FinishCommand(t.Context(), "client", "request", "succeeded", outcome)
			requireSessionWriteFailure(t, err)
			requireRootUnchanged(t, store, root, before)
			exec(t, store, "DROP TRIGGER reject_session_write")
			result, err := store.FinishCommand(t.Context(), "client", "request", "succeeded", outcome)
			if err != nil {
				t.Fatal(err)
			}
			data, err := store.ResolveRuntimeValue(t.Context(), root, result)
			if err != nil || string(data) != string(outcome.Data) {
				t.Fatalf("retry outcome bytes=%d %v", len(data), err)
			}
		})
	}
}

func TestArtifactDirectoryFailureCannotPublishUnrecoverableWork(t *testing.T) {
	for _, action := range []string{"store content", "enqueue input", "append event", "admit child", "send message", "finish command", "commit root outcome", "continue goal", "scheduled input"} {
		t.Run(action, func(t *testing.T) {
			directory := t.TempDir()
			store, err := Open(filepath.Join(directory, "sessions.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.EnsureAuthority(t.Context(), root); err != nil {
				t.Fatal(err)
			}
			admitTestChild(t, store, root, root, "child")
			command, err := store.AdmitCommand(t.Context(), CommandAdmission{ClientID: "client", CommandID: "request", Scope: CommandScopeRoot, RootID: root, AgentID: root, Kind: "submit", RequestDigest: "digest", Payload: RuntimePayload{Data: []byte("work")}})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.StartRootTurn(t.Context(), root, root, command.Command.IngressSeq); err != nil {
				t.Fatal(err)
			}
			big := RuntimePayload{Data: []byte(strings.Repeat("durable work ", 1000))}
			anchor := time.Now().UTC().Truncate(time.Second)
			scheduleID, err := store.AddSchedule(root, "@every 1h", string(big.Data), anchor)
			if err != nil {
				t.Fatal(err)
			}
			before := inputRootSnapshot(t, store, root)
			artifactDir := filepath.Join(directory, "artifacts", "sha256")
			if err := os.Remove(artifactDir); err != nil {
				t.Fatal(err)
			}
			perform := func() error {
				switch action {
				case "store content":
					_, err := store.StoreContent(t.Context(), ContentGrant{RootID: root, Scope: ContentGrantRoot}, big)
					return err
				case "enqueue input":
					_, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: root, Kind: "submit", Payload: big})
					return err
				case "append event":
					_, err := store.AppendRootEvent(t.Context(), root, "stream.text", big)
					return err
				case "admit child":
					_, err := store.AdmitAgent(t.Context(), AgentAdmission{RootID: root, ParentAgentID: root, ChildAgentID: "new-child", Prompt: big})
					return err
				case "send message":
					_, err := store.SendMailboxMessage(t.Context(), root, root, "child", MailboxSend{Body: string(big.Data)})
					return err
				case "finish command":
					_, err := store.FinishCommand(t.Context(), "client", "request", "succeeded", big)
					return err
				case "scheduled input":
					_, err := store.ClaimScheduleFire(t.Context(), ScheduleFireClaim{RootID: root, AgentID: root, ScheduleID: scheduleID, Slot: anchor})
					return err
				default:
					commit := RootTurnCommit{RootID: root, AgentID: root, InboxSeq: command.Command.IngressSeq, Status: "succeeded", Model: "model", Provider: "provider"}
					if action == "commit root outcome" {
						commit.Outcome = big
					} else {
						commit.GoalContinuation = string(big.Data)
					}
					return store.CommitRootTurn(t.Context(), commit)
				}
			}
			if err := perform(); err == nil || !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unavailable artifact directory returned %v", err)
			}
			requireRootUnchanged(t, store, root, before)
			var fire string
			if err := store.db.QueryRowContext(t.Context(), `SELECT last_fire FROM schedules WHERE session_id=? AND id=?`, root, scheduleID).Scan(&fire); err != nil || fire != "" {
				t.Fatalf("failed content write consumed schedule: %q %v", fire, err)
			}
			var grants int
			if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM content_grants WHERE root_id=?`, root).Scan(&grants); err != nil || grants != 0 {
				t.Fatalf("failed write leaked grants=%d %v", grants, err)
			}
			if err := os.Mkdir(artifactDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := perform(); err != nil {
				t.Fatalf("retry after restoring storage: %v", err)
			}
		})
	}
}
