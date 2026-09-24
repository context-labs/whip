package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

func TestModelCallWriteFailuresPreserveReservationsAndRetry(t *testing.T) {
	for _, phase := range []string{"admit", "settle"} {
		t.Run(phase, func(t *testing.T) {
			for _, table := range []string{"budgets", "model_calls", "events"} {
				t.Run(table, func(t *testing.T) {
					store, root, agent := newSwarmFixture(t)
					attempt := modelAttempt("accounted-call", 10, 20)
					var call ModelCallReservation
					if phase == "settle" {
						var err error
						call, err = store.AdmitModelCall(t.Context(), root, agent, attempt)
						if err != nil {
							t.Fatal(err)
						}
					}
					before := inputRootSnapshot(t, store, root)
					beforeAccounting, err := store.ModelAccounting(t.Context(), root, agent, false)
					if err != nil {
						t.Fatal(err)
					}
					operation := "UPDATE"
					if table == "events" || phase == "admit" && table == "model_calls" {
						operation = "INSERT"
					}
					rejectSessionWrite(t, store, operation, table, "")
					result := llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Reported: true, PromptTokens: 8, CompletionTokens: 4}, Elapsed: time.Millisecond}
					if phase == "admit" {
						_, err = store.AdmitModelCall(t.Context(), root, agent, attempt)
					} else {
						_, err = store.SettleModelCall(t.Context(), root, call.ID, result)
					}
					requireSessionWriteFailure(t, err)
					requireRootUnchanged(t, store, root, before)
					accounting, err := store.ModelAccounting(t.Context(), root, agent, false)
					if err != nil || accounting != beforeAccounting {
						t.Fatalf("failure changed accounting %+v -> %+v %v", beforeAccounting, accounting, err)
					}
					exec(t, store, "DROP TRIGGER reject_session_write")
					if phase == "admit" {
						call, err = store.AdmitModelCall(t.Context(), root, agent, attempt)
						if err != nil {
							t.Fatal(err)
						}
					}
					if _, err := store.SettleModelCall(t.Context(), root, call.ID, result); err != nil {
						t.Fatal(err)
					}
					if _, err := store.SettleModelCall(t.Context(), root, call.ID, result); err != nil {
						t.Fatal(err)
					}
					accounting, err = store.ModelAccounting(t.Context(), root, agent, false)
					if err != nil || accounting.PendingCalls != 0 || accounting.ReportedCalls != 1 {
						t.Fatalf("retry did not settle exactly once: %+v %v", accounting, err)
					}
					budget := budgetState(t, store, root, agent, BudgetTokens)
					if budget.Used != 12 || budget.Reserved != 0 {
						t.Fatalf("retry leaked or double-charged token reservation: %+v", budget)
					}
				})
			}
		})
	}
}

func TestCorruptModelLedgerCannotBeSettledOrRecoveredSilently(t *testing.T) {
	for _, field := range []string{"attempt", "reservations", "contributions"} {
		t.Run(field, func(t *testing.T) {
			for _, action := range []string{"settle", "recover", "interrupt"} {
				t.Run(action, func(t *testing.T) {
					store, root, agent := newSwarmFixture(t)
					call, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("call", 10, 20))
					if err != nil {
						t.Fatal(err)
					}
					exec(t, store, "UPDATE model_calls SET "+field+"='corrupt ledger' WHERE id=?", call.ID)
					before := inputRootSnapshot(t, store, root)
					switch action {
					case "settle":
						_, err = store.SettleModelCall(t.Context(), root, call.ID, llm.ModelAttemptResult{Dispatched: true})
					case "recover":
						err = store.Recover(t.Context())
					default:
						_, err = store.InterruptRoot(t.Context(), root, "restart")
					}
					if err == nil {
						t.Fatal("corrupt model ledger silently settled")
					}
					requireRootUnchanged(t, store, root, before)
					var status string
					if err := store.db.QueryRowContext(t.Context(), `SELECT status FROM model_calls WHERE id=?`, call.ID).Scan(&status); err != nil || status != "running" {
						t.Fatalf("corrupt ledger lost unresolved status: %q %v", status, err)
					}
				})
			}
		})
	}
}

func TestModelCallsRejectUnknownAgentsAndClosedStorage(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	if _, err := store.ModelAccounting(t.Context(), root, "absent", false); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("unknown agent=%v", err)
	}
	if _, err := store.AdmitModelCall(t.Context(), root, "absent", modelAttempt("call", 1, 1)); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("unknown caller=%v", err)
	}
	if _, err := store.ModelAccounting(t.Context(), "absent", "", true); err == nil {
		t.Fatal("missing root returned zero cost")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for name, action := range map[string]func(context.Context) error{
		"admit": func(ctx context.Context) error {
			_, err := store.AdmitModelCall(ctx, root, agent, modelAttempt("call", 1, 1))
			return err
		},
		"settle": func(ctx context.Context) error {
			_, err := store.SettleModelCall(ctx, root, "call", llm.ModelAttemptResult{})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(t.Context()); err == nil {
				t.Fatal("closed ledger returned success")
			}
		})
	}
}
