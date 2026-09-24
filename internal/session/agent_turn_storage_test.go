package session

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

// Fail real SQLite writes so the public operation must unwind its transaction.
func rejectSessionWrite(t *testing.T, store *Store, mutation, table, condition string) {
	t.Helper()
	if condition != "" {
		condition = " WHEN " + condition
	}
	exec(t, store, "CREATE TRIGGER reject_session_write BEFORE "+mutation+" ON "+table+condition+" BEGIN SELECT RAISE(ABORT,'injected session write failure'); END")
}

func requireSessionWriteFailure(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "injected session write failure") {
		t.Fatalf("expected injected storage failure, got %v", err)
	}
}

func requireRootUnchanged(t *testing.T, store *Store, root string, before map[string]string) {
	t.Helper()
	if after := inputRootSnapshot(t, store, root); !reflect.DeepEqual(after, before) {
		for table, original := range before {
			if after[table] != original {
				t.Errorf("failed transaction changed %s: before=%s after=%s", table, original, after[table])
			}
		}
	}
}

func queuedAgentForStorageTest(t *testing.T) (*Store, string, int64) {
	t.Helper()
	store, root, agent := newSwarmFixture(t)
	admitTestChild(t, store, root, agent, "child")
	queued, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: "child", Kind: "submit", Payload: RuntimePayload{Data: []byte("preserve my work")}})
	if err != nil {
		t.Fatal(err)
	}
	return store, root, queued.InboxSeq
}

func TestAgentTurnStartStorageFailuresPreserveQueuedInput(t *testing.T) {
	for _, failure := range []struct{ name, mutation, table, condition string }{
		{"claim input", "UPDATE", "inbox", "NEW.status='running'"},
		{"create turn", "INSERT", "turns", ""},
		{"mark agent running", "UPDATE", "agents", "NEW.status='running'"},
		{"reserve child capacity", "UPDATE", "budgets", ""},
		{"record start event", "INSERT", "events", "NEW.kind='agent.turn.started'"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			store, root, seq := queuedAgentForStorageTest(t)
			before := inputRootSnapshot(t, store, root)
			rejectSessionWrite(t, store, failure.mutation, failure.table, failure.condition)
			_, err := store.StartAgentTurn(t.Context(), root, "child", "turn")
			requireSessionWriteFailure(t, err)
			requireRootUnchanged(t, store, root, before)
			exec(t, store, "DROP TRIGGER reject_session_write")
			start, err := store.StartAgentTurn(t.Context(), root, "child", "turn")
			if err != nil || len(start.Items) != 1 || start.Items[0].Seq != seq || start.Items[0].Status != "running" {
				t.Fatalf("retry did not claim original input: %+v %v", start, err)
			}
		})
	}
}

func TestAgentTurnCommitStorageFailuresPreserveRunningTurn(t *testing.T) {
	for _, failure := range []struct {
		name, mutation, table, condition, status string
		retry, acknowledge                       bool
	}{
		{"turn", "UPDATE", "turns", "", "succeeded", false, true},
		{"agent", "UPDATE", "agents", "", "succeeded", false, true},
		{"acknowledge input", "UPDATE", "inbox", "NEW.status='consumed'", "succeeded", false, true},
		{"acknowledgement event", "INSERT", "events", "NEW.kind='inbox.consumed'", "succeeded", false, true},
		{"retry failed input", "UPDATE", "inbox", "NEW.status='queued'", "failed", true, false},
		{"interrupt invalid input", "UPDATE", "inbox", "NEW.status='interrupted'", "failed", false, false},
		{"transcript", "INSERT", "transcript_messages", "", "succeeded", false, true},
		{"budget release", "UPDATE", "budgets", "", "succeeded", false, true},
		{"terminal event", "INSERT", "events", "NEW.kind='agent.turn.succeeded'", "succeeded", false, true},
	} {
		t.Run(failure.name, func(t *testing.T) {
			store, root, seq := queuedAgentForStorageTest(t)
			if _, err := store.StartAgentTurn(t.Context(), root, "child", "turn"); err != nil {
				t.Fatal(err)
			}
			before := inputRootSnapshot(t, store, root)
			commit := AgentTurnCommit{TurnID: "turn", Status: failure.status, RetryInput: failure.retry, Messages: []llm.Message{{Role: "assistant", Content: "result"}}}
			if failure.acknowledge {
				commit.AcknowledgedInbox = []int64{seq}
			}
			rejectSessionWrite(t, store, failure.mutation, failure.table, failure.condition)
			requireSessionWriteFailure(t, store.FinishAgentTurn(t.Context(), root, "child", commit))
			requireRootUnchanged(t, store, root, before)
			messages, err := store.LoadAgentTranscript(t.Context(), root, "child")
			if err != nil || len(messages) != 0 {
				t.Fatalf("failed commit leaked transcript: %+v %v", messages, err)
			}
			exec(t, store, "DROP TRIGGER reject_session_write")
			if err := store.FinishAgentTurn(t.Context(), root, "child", commit); err != nil {
				t.Fatal(err)
			}
			agent, err := store.LoadAgent(t.Context(), root, "child")
			if err != nil || agent.Status != "idle" {
				t.Fatalf("retry left agent %+v %v", agent, err)
			}
			messages, err = store.LoadAgentTranscript(t.Context(), root, "child")
			if err != nil || len(messages) != 1 || messages[0].Content != "result" {
				t.Fatalf("retry transcript %+v %v", messages, err)
			}
		})
	}
}

func TestAgentTurnInvalidCommitIsAtomicAndCanRetry(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*AgentTurnCommit)
	}{
		{"invalid status", func(c *AgentTurnCommit) { c.Status = "unknown" }},
		{"missing turn", func(c *AgentTurnCommit) { c.TurnID = "" }},
		{"invalid receipt", func(c *AgentTurnCommit) { c.AcknowledgedInbox = []int64{0} }},
		{"unencodable usage", func(c *AgentTurnCommit) {
			c.Messages = []llm.Message{{Role: "assistant", Usage: &llm.Usage{Cost: new(math.NaN())}}}
		}},
		{"invalid compaction", func(c *AgentTurnCommit) { c.Compactions = []RootCompaction{{Cutoff: -1}} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, root, seq := queuedAgentForStorageTest(t)
			if _, err := store.StartAgentTurn(t.Context(), root, "child", "turn"); err != nil {
				t.Fatal(err)
			}
			before := inputRootSnapshot(t, store, root)
			commit := AgentTurnCommit{TurnID: "turn", Status: "succeeded", AcknowledgedInbox: []int64{seq}}
			test.mutate(&commit)
			if err := store.FinishAgentTurn(t.Context(), root, "child", commit); err == nil {
				t.Fatal("invalid commit accepted")
			}
			requireRootUnchanged(t, store, root, before)
			if err := store.FinishAgentTurn(t.Context(), root, "child", AgentTurnCommit{TurnID: "turn", Status: "succeeded", AcknowledgedInbox: []int64{seq, seq}, Messages: []llm.Message{{Role: ""}, {Role: "system"}, {Role: "assistant", Content: "saved"}}}); err != nil {
				t.Fatal(err)
			}
			messages, err := store.LoadAgentTranscript(t.Context(), root, "child")
			if err != nil || len(messages) != 1 || messages[0].Content != "saved" {
				t.Fatalf("retry transcript=%+v %v", messages, err)
			}
		})
	}
}

func TestAgentTurnRejectsMissingTerminalAndClosedState(t *testing.T) {
	store, root, _ := queuedAgentForStorageTest(t)
	for _, test := range []struct{ name, root, agent, turn string }{
		{"missing root", "", "child", "turn"}, {"missing agent", root, "", "turn"}, {"missing turn", root, "child", ""}, {"unknown agent", root, "missing", "turn"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.StartAgentTurn(t.Context(), test.root, test.agent, test.turn); err == nil {
				t.Fatal("invalid turn accepted")
			}
		})
	}
	if _, err := store.StartAgentTurn(t.Context(), root, "child", "turn"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAgentTurn(t.Context(), root, "child", "second"); !errors.Is(err, ErrAgentTerminal) {
		t.Fatalf("running agent accepted second turn: %v", err)
	}
	if err := store.FinishAgentTurn(t.Context(), root, "missing", AgentTurnCommit{TurnID: "absent", Status: "failed"}); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("missing agent error=%v", err)
	}
	if _, err := store.LoadAgent(t.Context(), "", "child"); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("empty root error=%v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	calls := map[string]func(context.Context) error{
		"start": func(ctx context.Context) error {
			_, err := store.StartAgentTurn(ctx, root, "child", "turn")
			return err
		},
		"finish": func(ctx context.Context) error {
			return store.FinishAgentTurn(ctx, root, "child", AgentTurnCommit{TurnID: "turn", Status: "succeeded"})
		},
		"load":       func(ctx context.Context) error { _, err := store.LoadAgent(ctx, root, "child"); return err },
		"retained":   func(ctx context.Context) error { _, err := store.LoadRetainedAgents(ctx, root); return err },
		"transcript": func(ctx context.Context) error { _, err := store.LoadAgentTranscript(ctx, root, "child"); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(t.Context()); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("closed store error=%v", err)
			}
		})
	}
}
