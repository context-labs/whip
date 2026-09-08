package session

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
)

func TestRecoveryPreservesRunningInputUntilCorruptRowsAreRepaired(t *testing.T) {
	for _, damage := range []string{"nonnumeric budget", "negative budget", "missing turn identity"} {
		t.Run(damage, func(t *testing.T) {
			store, root, _ := queuedAgentForStorageTest(t)
			if _, err := store.StartAgentTurn(t.Context(), root, "child", "turn"); err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "nonnumeric budget":
				exec(t, store, `UPDATE budgets SET used_value='damaged' WHERE root_id=? AND kind='tokens'`, root)
			case "negative budget":
				exec(t, store, `UPDATE budgets SET used_value=-1 WHERE root_id=? AND kind='tokens'`, root)
			default:
				exec(t, store, `UPDATE turns SET id=NULL WHERE id='turn'`)
			}
			before := inputRootSnapshot(t, store, root)
			if err := store.Recover(t.Context()); err == nil {
				t.Fatal("recovery accepted damaged state")
			}
			requireRootUnchanged(t, store, root, before)
			exec(t, store, `UPDATE budgets SET used_value=0 WHERE root_id=? AND kind='tokens'`, root)
			exec(t, store, `UPDATE turns SET id='turn' WHERE root_id=? AND id IS NULL`, root)
			if err := store.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if turn, err := store.ActiveTurn(t.Context(), root, "child"); err != nil || turn != "" {
				t.Fatalf("repaired recovery retained active turn: %q %v", turn, err)
			}
			if budget := budgetState(t, store, root, root, BudgetConcurrentChildTurns); budget.Reserved != 0 {
				t.Fatalf("repaired recovery leaked child capacity: %+v", budget)
			}
		})
	}
}

func TestModelRecoveryRequiresAnIntactReservationLedger(t *testing.T) {
	for _, damage := range []string{"nonnumeric allowance", "nonnumeric budget", "negative budget", "lost reservation", "uncertainty counter overflow", "lost uncertainty marker"} {
		t.Run(damage, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			call, err := store.AdmitModelCall(t.Context(), root, agent, modelAttempt("call", 10, 20))
			if err != nil {
				t.Fatal(err)
			}
			interrupted := damage == "lost uncertainty marker"
			if interrupted {
				if err := store.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			var reserved, incomplete int64
			if err := store.db.QueryRowContext(t.Context(), `SELECT reserved_value,model_incomplete FROM budgets WHERE root_id=? AND agent_id='' AND kind='tokens'`, root).Scan(&reserved, &incomplete); err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "nonnumeric allowance":
				exec(t, store, `UPDATE model_calls SET max_tokens='damaged' WHERE id=?`, call.ID)
			case "nonnumeric budget":
				exec(t, store, `UPDATE budgets SET used_value='damaged' WHERE root_id=? AND kind='tokens'`, root)
			case "negative budget":
				exec(t, store, `UPDATE budgets SET used_value=-1 WHERE root_id=? AND kind='tokens'`, root)
			case "lost reservation":
				exec(t, store, `UPDATE budgets SET reserved_value=0 WHERE root_id=? AND kind='tokens'`, root)
			case "uncertainty counter overflow":
				exec(t, store, `UPDATE budgets SET model_incomplete=? WHERE root_id=? AND kind='tokens'`, int64(math.MaxInt64), root)
			default:
				exec(t, store, `UPDATE budgets SET model_incomplete=0 WHERE root_id=? AND kind='tokens'`, root)
			}
			before := inputRootSnapshot(t, store, root)
			result := llm.ModelAttemptResult{Dispatched: true, Usage: llm.Usage{Reported: true, PromptTokens: 8, CompletionTokens: 4}}
			if interrupted {
				_, err = store.SettleModelCall(t.Context(), root, call.ID, result)
			} else {
				_, err = store.TerminalizeSubtree(t.Context(), root, agent, agent, "stopped")
			}
			if err == nil {
				t.Fatal("damaged ledger was silently settled")
			}
			requireRootUnchanged(t, store, root, before)
			var status string
			if err := store.db.QueryRowContext(t.Context(), `SELECT status FROM model_calls WHERE id=?`, call.ID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			wantStatus := "running"
			if interrupted {
				wantStatus = "interrupted"
			}
			if status != wantStatus {
				t.Fatalf("failed settlement changed model status: %q", status)
			}
			exec(t, store, `UPDATE model_calls SET max_tokens=? WHERE id=?`, call.MaxTokens, call.ID)
			exec(t, store, `UPDATE budgets SET used_value=0,reserved_value=?,model_incomplete=? WHERE root_id=? AND kind='tokens'`, reserved, incomplete, root)
			if _, err := store.SettleModelCall(t.Context(), root, call.ID, result); err != nil {
				t.Fatal(err)
			}
			if _, err := store.SettleModelCall(t.Context(), root, call.ID, result); err != nil {
				t.Fatal(err)
			}
			if budget := budgetState(t, store, root, agent, BudgetTokens); budget.Used != 12 || budget.Reserved != 0 || budget.Uncertain != 0 {
				t.Fatalf("repaired settlement did not charge exactly once: %+v", budget)
			}
		})
	}
}

func TestShutdownRequiresReadableOperationEvidence(t *testing.T) {
	for _, pending := range []bool{false, true} {
		for _, damage := range []string{"nonnumeric content size", "truncated content", "invalid reservation"} {
			t.Run(damage+"/pending="+map[bool]string{false: "false", true: "true"}[pending], func(t *testing.T) {
				store, root, agent := newSwarmFixture(t)
				admission := capability.Admission{Request: capability.Request{RootID: root, AgentID: agent, CapabilityID: "files:" + root, CapabilityGeneration: 1, OperationID: "operation", Operation: "read"}, RequirePermission: pending}
				if _, err := store.Begin(t.Context(), admission); err != nil {
					t.Fatal(err)
				}
				var original []byte
				if err := store.db.QueryRowContext(t.Context(), `SELECT payload_inline FROM operations WHERE id='operation'`).Scan(&original); err != nil {
					t.Fatal(err)
				}
				body := original
				if damage == "invalid reservation" {
					admission.Request.Reservations = []capability.Reservation{{Kind: "tokens", Amount: -1}}
					var err error
					body, err = json.Marshal(admission)
					if err != nil {
						t.Fatal(err)
					}
				}
				value, err := store.StoreContent(t.Context(), ContentGrant{RootID: root, AgentID: agent, Scope: ContentGrantAgent}, RuntimePayload{Data: body})
				if err != nil {
					t.Fatal(err)
				}
				exec(t, store, `UPDATE operations SET payload_inline=NULL,payload_ref=? WHERE id='operation'`, value.ReferenceID)
				switch damage {
				case "nonnumeric content size":
					exec(t, store, `UPDATE content_references SET size='damaged' WHERE id=?`, value.ReferenceID)
				case "truncated content":
					exec(t, store, `UPDATE content_references SET size=size+1 WHERE id=?`, value.ReferenceID)
				}
				before := inputRootSnapshot(t, store, root)
				_, err = store.TerminalizeSubtree(t.Context(), root, agent, agent, "stopped")
				if err == nil {
					t.Fatal("shutdown discarded damaged operation evidence")
				}
				if damage == "invalid reservation" && !errors.Is(err, capability.ErrDenied) {
					t.Fatalf("invalid stored reservation: %v", err)
				}
				requireRootUnchanged(t, store, root, before)
				exec(t, store, `UPDATE operations SET payload_inline=?,payload_ref=NULL WHERE id='operation'`, original)
				if _, err := store.TerminalizeSubtree(t.Context(), root, agent, agent, "stopped"); err != nil {
					t.Fatal(err)
				}
				var active int
				if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM operations WHERE root_id=? AND status IN ('running','waiting')`, root).Scan(&active); err != nil || active != 0 {
					t.Fatalf("repaired shutdown retained active operation: %d %v", active, err)
				}
			})
		}
	}
}
