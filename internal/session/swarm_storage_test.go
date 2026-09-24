package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

func TestAgentAdmissionStorageFailuresLeaveNoChildOrReservation(t *testing.T) {
	for _, failure := range []struct{ name, mutation, table, condition string }{
		{"child row", "INSERT", "agents", ""},
		{"child budget", "INSERT", "budgets", ""},
		{"reserve live capacity", "UPDATE", "budgets", ""},
		{"prompt content", "INSERT", "content_references", ""},
		{"prompt grant", "INSERT", "content_grants", ""},
		{"prompt input", "INSERT", "inbox", ""},
		{"prompt event", "INSERT", "events", "NEW.kind='agent.prompt.queued'"},
		{"reserved event", "INSERT", "events", "NEW.kind='budget.active_child.reserved'"},
		{"admission event", "INSERT", "events", "NEW.kind='agent.admitted'"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			store, root, parent := newSwarmFixture(t)
			before := inputRootSnapshot(t, store, root)
			admission := AgentAdmission{RootID: root, ParentAgentID: parent, ChildAgentID: "child", Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 100}}, Prompt: RuntimePayload{Data: []byte(strings.Repeat("initial prompt", 1000))}}
			rejectSessionWrite(t, store, failure.mutation, failure.table, failure.condition)
			_, err := store.AdmitAgent(t.Context(), admission)
			requireSessionWriteFailure(t, err)
			requireRootUnchanged(t, store, root, before)
			if _, err := store.LoadAgent(t.Context(), root, "child"); !errors.Is(err, ErrAgentAccess) {
				t.Fatalf("failed admission created child: %v", err)
			}
			var grants int
			if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM content_grants WHERE root_id=? AND agent_id='child'`, root).Scan(&grants); err != nil || grants != 0 {
				t.Fatalf("failed admission leaked grants=%d %v", grants, err)
			}
			exec(t, store, "DROP TRIGGER reject_session_write")
			if _, err := store.AdmitAgent(t.Context(), admission); err != nil {
				t.Fatal(err)
			}
			items, err := store.LoadQueuedInbox(t.Context(), root, "child", 0, 10)
			if err != nil || len(items) != 1 || items[0].Payload.ReferenceID == "" {
				t.Fatalf("retry lost initial prompt: %+v %v", items, err)
			}
		})
	}
}

func TestSubtreeTerminalizationStorageFailuresAreRetryable(t *testing.T) {
	for _, failure := range []struct{ name, mutation, table, condition string }{
		{"agent status", "UPDATE", "agents", ""},
		{"queued input", "UPDATE", "inbox", ""},
		{"turn status", "UPDATE", "turns", ""},
		{"release live capacity", "UPDATE", "budgets", ""},
		{"terminal event", "INSERT", "events", "NEW.kind='agent.subtree.stopped'"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			store, root, _ := queuedAgentForStorageTest(t)
			if _, err := store.StartAgentTurn(t.Context(), root, "child", "turn"); err != nil {
				t.Fatal(err)
			}
			before := inputRootSnapshot(t, store, root)
			rejectSessionWrite(t, store, failure.mutation, failure.table, failure.condition)
			_, err := store.TerminalizeSubtree(t.Context(), root, root, "child", "stopped")
			requireSessionWriteFailure(t, err)
			requireRootUnchanged(t, store, root, before)
			exec(t, store, "DROP TRIGGER reject_session_write")
			if _, err := store.TerminalizeSubtree(t.Context(), root, root, "child", "stopped"); err != nil {
				t.Fatal(err)
			}
			child, err := store.LoadAgent(t.Context(), root, "child")
			if err != nil || child.Status != "stopped" {
				t.Fatalf("retry child=%+v %v", child, err)
			}
			if running, err := store.ActiveTurn(t.Context(), root, "child"); err != nil || running != "" {
				t.Fatalf("retry retained active turn %q %v", running, err)
			}
			if used := budgetState(t, store, root, root, BudgetConcurrentChildTurns); used.Reserved != 0 {
				t.Fatalf("retry leaked capacity %+v", used)
			}
		})
	}
}

func TestSwarmRejectsInvalidLineageAndClosedStore(t *testing.T) {
	store, root, parent := newSwarmFixture(t)
	admitTestChild(t, store, root, parent, "child")
	admitTestChild(t, store, root, parent, "sibling")
	t.Run("duplicate budget", func(t *testing.T) {
		if _, err := store.AdmitAgent(t.Context(), AgentAdmission{RootID: root, ParentAgentID: parent, ChildAgentID: "duplicate", Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: 1}, {Kind: BudgetTokens, Limit: 2}}}); err == nil {
			t.Fatal("duplicate cap accepted")
		}
	})
	t.Run("missing parent", func(t *testing.T) {
		if _, err := store.AdmitAgent(t.Context(), AgentAdmission{RootID: root, ParentAgentID: "absent", ChildAgentID: "orphan"}); !errors.Is(err, ErrAgentAccess) {
			t.Fatalf("orphan admission=%v", err)
		}
	})
	for _, args := range []struct{ name, caller, target, status string }{
		{"missing caller", "absent", "child", "stopped"}, {"missing target", parent, "absent", "stopped"}, {"missing identity", "", "child", "stopped"}, {"invalid status", parent, "child", "failed"},
	} {
		t.Run(args.name, func(t *testing.T) {
			if _, err := store.TerminalizeSubtree(t.Context(), root, args.caller, args.target, args.status); err == nil {
				t.Fatal("invalid terminalization accepted")
			}
		})
	}
	if _, err := store.TerminalizeSubtree(t.Context(), root, parent, "child", "stopped"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitAgent(t.Context(), AgentAdmission{RootID: root, ParentAgentID: "child", ChildAgentID: "grandchild"}); !errors.Is(err, ErrAgentTerminal) {
		t.Fatalf("terminal parent admission=%v", err)
	}
	if _, err := store.TerminalizeSubtree(t.Context(), root, "child", "sibling", "stopped"); !errors.Is(err, ErrAgentTerminal) {
		t.Fatalf("terminal caller=%v", err)
	}
	if _, err := store.TerminalizeSubtree(t.Context(), root, parent, "child", "stopped"); !errors.Is(err, ErrAgentTerminal) {
		t.Fatalf("duplicate stop=%v", err)
	}
	if _, err := store.ListAgentRelatives(t.Context(), root, ""); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("empty caller=%v", err)
	}
	if _, err := store.ListAgentRelatives(t.Context(), root, "absent"); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("unknown caller=%v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func(context.Context) error{
		"admit": func(ctx context.Context) error {
			_, err := store.AdmitAgent(ctx, AgentAdmission{RootID: root, ParentAgentID: parent, ChildAgentID: "new"})
			return err
		},
		"relatives": func(ctx context.Context) error { _, err := store.ListAgentRelatives(ctx, root, parent); return err },
		"terminalize": func(ctx context.Context) error {
			_, err := store.TerminalizeSubtree(ctx, root, parent, "sibling", "stopped")
			return err
		},
		"authorize": func(ctx context.Context) error {
			return store.AuthorizeCapability(ctx, root, parent, capability.Reference{ID: "files:" + root, Generation: 1}, "read", "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(t.Context()); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("closed database result=%v", err)
			}
		})
	}
}
