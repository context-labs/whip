package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func successfulHelperContentAttempt(t *testing.T, s *Store, operation session.Operation, index int) session.ModelAttempt {
	t.Helper()
	attempt := reserveTest(t, s, helperRequest(t, operation, index))
	dispatchTest(t, s, attempt.ID)
	result, err := s.SettleModelAttempt(t.Context(), attempt.ID, session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(11))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestModelHelperContentExemptsOnlyLogicalWriteChargeAndPreservesReadScope(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	operation := helperOperation(t, s, owner, cell, "helper", "models.call", `{"prompt":"question"}`, true)
	attempt := successfulHelperContentAttempt(t, s, operation, 0)
	budgetLimit(t, s, owner.ID, session.BudgetLogicalWrites, 0)
	budgetLimit(t, s, owner.ID, session.BudgetLogicalWriteBytes, 0)
	metadata := contentReference(owner.ID, "caller-controlled", strings.Repeat("provider output 🧪\n", 1024))
	if _, err := s.RegisterContent(t.Context(), metadata); !errors.Is(err, ErrLimit) {
		t.Fatal("ordinary write bypassed exhausted allowance", err)
	}
	reference, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, metadata.Digest, metadata.Size)
	if err != nil || reference.SessionID != owner.ID || reference.ID == metadata.ID || reference.MediaType != "text/plain" || reference.Size != metadata.Size || reference.Digest != metadata.Digest {
		t.Fatalf("provider evidence unavailable or incorrectly scoped: %+v %v", reference, err)
	}
	if count(t, s, "logical_writes") != 0 || count(t, s, "content_references") != 1 || budgetState(t, s, owner.ID, session.BudgetModelCostNanoUSD).Used != 11 {
		t.Fatal("provider evidence recharged logical writes or lost model cost")
	}
	_, stranger := create(t, s, nil)
	for _, id := range []string{reference.ID, reference.Digest} {
		if _, err := s.ContentReference(t.Context(), stranger.ID, id); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign handle/digest granted content authority", id, err)
		}
		if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "stranger", RequestID: id}, Submission{SessionID: stranger.ID, Source: session.UserInput, Parts: []session.Part{{Type: "content", ReferenceID: id}}}); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign helper content entered another owner's transcript", err)
		}
	}
	if own, err := s.ContentReference(t.Context(), owner.ID, reference.ID); err != nil || !reflect.DeepEqual(own, reference) {
		t.Fatal(own, err)
	}
	for _, changed := range []struct {
		digest string
		size   int64
	}{{strings.Repeat("a", 64), metadata.Size}, {metadata.Digest, metadata.Size + 1}} {
		if _, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, changed.digest, changed.size); !errors.Is(err, ErrConflict) {
			t.Fatal("same final attempt changed its output reference", err)
		}
	}
	if allowed, err := s.DispatchModelAttempt(t.Context(), attempt.ID); err != nil || allowed {
		t.Fatal("publication made accounted item dispatchable", allowed, err)
	}
}

func TestModelHelperContentRequiresSuccessfulAccountedHelper(t *testing.T) {
	for _, state := range []session.ModelAttemptState{session.AttemptReserved, session.AttemptDispatched, session.AttemptFailed, session.AttemptCancelled, session.AttemptUncertain} {
		t.Run(string(state), func(t *testing.T) {
			s := fresh(t)
			owner, cell := operationCell(t, s)
			operation := helperOperation(t, s, owner, cell, "helper", "models.call", `{"prompt":"question"}`, true)
			attempt := reserveTest(t, s, helperRequest(t, operation, 0))
			if state != session.AttemptReserved && state != session.AttemptCancelled {
				dispatchTest(t, s, attempt.ID)
			}
			if state != session.AttemptReserved && state != session.AttemptDispatched {
				if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, session.ModelAttemptResult{State: state}, nil); err != nil {
					t.Fatal(err)
				}
			}
			metadata := contentReference(owner.ID, "ignored", "body")
			if _, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, metadata.Digest, metadata.Size); !errors.Is(err, ErrConflict) || count(t, s, "content_references") != 0 || count(t, s, "content_bodies") != 0 {
				t.Fatal("unaccounted/unsuccessful attempt published evidence", err)
			}
		})
	}
	for _, purpose := range []string{"turn", "compaction"} {
		t.Run(purpose, func(t *testing.T) {
			s := fresh(t)
			owner, cell := operationCell(t, s)
			request := attemptRequest(cell.TurnID, "ordinary")
			request.Request.Purpose = purpose
			attempt := reserveTest(t, s, request)
			dispatchTest(t, s, attempt.ID)
			if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, session.ModelAttemptResult{State: session.AttemptSucceeded}, nil); err != nil {
				t.Fatal(err)
			}
			metadata := contentReference(owner.ID, "ignored", "body")
			if _, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, metadata.Digest, metadata.Size); !errors.Is(err, ErrConflict) {
				t.Fatal("ordinary accounting granted helper publication", err)
			}
		})
	}
}

func TestModelHelperContentStableBatchIdentityRestartAndTerminalRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	owner, cell := operationCell(t, s)
	operation := helperOperation(t, s, owner, cell, "batch", "models.batch", `{"prompts":["one","two","three"]}`, true)
	metadata := contentReference(owner.ID, "ignored", strings.Repeat("same provider output", 1024))
	attempts := []session.ModelAttempt{}
	references := []session.ContentReference{}
	for i := range 3 {
		attempt := successfulHelperContentAttempt(t, s, operation, i)
		attempts = append(attempts, attempt)
		if i == 2 {
			continue // An unpublished item cannot create a new reference after recovery.
		}
		reference, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, metadata.Digest, metadata.Size)
		if err != nil {
			t.Fatal(err)
		}
		references = append(references, reference)
	}
	if references[0].ID == references[1].ID || count(t, s, "content_bodies") != 1 || count(t, s, "content_references") != 2 {
		t.Fatal("batch item identity collided or body metadata duplicated")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	if _, err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i, reference := range references {
		retry, err := s.RegisterModelHelperContent(t.Context(), attempts[i].ID, metadata.Digest, metadata.Size)
		if err != nil || !reflect.DeepEqual(reference, retry) {
			t.Fatal("exact registration replay after recovery changed identity", retry, err)
		}
	}
	if _, err := s.RegisterModelHelperContent(t.Context(), attempts[2].ID, metadata.Digest, metadata.Size); !errors.Is(err, ErrConflict) {
		t.Fatal("late registration invented recovered operation output", err)
	}
	if budgetState(t, s, owner.ID, session.BudgetModelCostNanoUSD).Used != 33 {
		t.Fatal("recovery changed successful item charges")
	}
}

func TestModelHelperContentStorageFailurePreservesSettledAccounting(t *testing.T) {
	for _, failure := range []string{"body SQL", "reference SQL", "references", "bytes", "body size"} {
		t.Run(failure, func(t *testing.T) {
			s := fresh(t)
			owner, cell := operationCell(t, s)
			operation := helperOperation(t, s, owner, cell, "helper", "models.call", `{"prompt":"question"}`, true)
			attempt := successfulHelperContentAttempt(t, s, operation, 0)
			metadata := contentReference(owner.ID, "ignored", "provider output")
			seed := contentReference(owner.ID, "seed", "existing body")
			switch failure {
			case "body SQL":
				execTest(t, s, `CREATE TRIGGER fail_content BEFORE INSERT ON content_bodies BEGIN SELECT RAISE(ABORT,'injected'); END`)
			case "reference SQL":
				execTest(t, s, `CREATE TRIGGER fail_content BEFORE INSERT ON content_references BEGIN SELECT RAISE(ABORT,'injected'); END`)
			case "references", "bytes":
				limit, size := session.MaxContentReferences, int64(0)
				if failure == "bytes" {
					limit, size = session.MaxSessionContentBytes/session.MaxContentBytes, session.MaxContentBytes
				}
				execTest(t, s, "INSERT INTO content_bodies VALUES (?,?)", seed.Digest, size)
				execTest(t, s, `WITH RECURSIVE ids(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM ids WHERE i<?)
 INSERT INTO content_references SELECT 'capacity_'||i,?,?,'text/plain',? FROM ids`, limit, owner.ID, seed.Digest, now())
			case "body size":
				execTest(t, s, "INSERT INTO content_bodies VALUES (?,?)", metadata.Digest, metadata.Size+1)
			}
			bodies, references := count(t, s, "content_bodies"), count(t, s, "content_references")
			_, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, metadata.Digest, metadata.Size)
			if err == nil || (failure == "references" || failure == "bytes") && !errors.Is(err, ErrLimit) || failure == "body size" && !errors.Is(err, ErrConflict) {
				t.Fatal("storage failure not reported truthfully", err)
			}
			stored, readErr := s.ModelAttempt(t.Context(), attempt.ID)
			if readErr != nil || !reflect.DeepEqual(stored, attempt) || budgetState(t, s, owner.ID, session.BudgetModelCostNanoUSD).Used != 11 || count(t, s, "content_bodies") != bodies || count(t, s, "content_references") != references {
				t.Fatal("publication failure rolled back accounting or partially registered", stored, readErr)
			}
			if strings.HasSuffix(failure, "SQL") {
				execTest(t, s, "DROP TRIGGER fail_content")
				if _, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, metadata.Digest, metadata.Size); err != nil {
					t.Fatal("SQL-only retry failed", err)
				}
			}
			if allowed, err := s.DispatchModelAttempt(t.Context(), attempt.ID); err != nil || allowed {
				t.Fatal("storage failure authorized provider replay", allowed, err)
			}
		})
	}
}

func TestModelHelperContentMetadataBounds(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	operation := helperOperation(t, s, owner, cell, "helper", "models.call", `{"prompt":"question"}`, true)
	attempt := successfulHelperContentAttempt(t, s, operation, 0)
	for _, test := range []struct {
		digest string
		size   int64
	}{{"", 0}, {strings.Repeat("A", 64), 1}, {strings.Repeat("a", 64), -1}, {strings.Repeat("a", 64), session.MaxContentBytes + 1}} {
		if _, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, test.digest, test.size); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("invalid body metadata accepted", test, err)
		}
	}
	if count(t, s, "content_bodies") != 0 || count(t, s, "content_references") != 0 {
		t.Fatal("invalid metadata retained content")
	}
}

func TestModelHelperContentChildDeletionCannotResurrectReferenceOrEraseCharge(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	if _, err := s.CreateGrant(t.Context(), session.Grant{ID: "models", SessionID: root.ID, Capability: "models.call", Resource: string(root.TreeID)}); err != nil {
		t.Fatal(err)
	}
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	cell := childOperationCell(t, s, child.Session.ID)
	operation := admitOperation(t, s, session.OperationSpec{ID: "helper", CellID: cell.ID, RequestID: "helper", Capability: "models.call", Resource: string(root.TreeID), Arguments: json.RawMessage(`{"prompt":"question"}`)})
	if allowed, err := s.DispatchOperation(t.Context(), operation.ID); err != nil || !allowed {
		t.Fatal(allowed, err)
	}
	attempt := successfulHelperContentAttempt(t, s, operation, 0)
	metadata := contentReference(child.Session.ID, "ignored", "child provider output")
	reference, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, metadata.Digest, metadata.Size)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleOperation(t.Context(), operation.ID, session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(fmt.Sprintf(`{"reference_id":%q}`, reference.ID))}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleCell(t.Context(), cell.ID, session.CellSucceeded, session.ToolResult{CallID: "call", Output: "done"}, nil); err != nil {
		t.Fatal(err)
	}
	finishMailTest(t, s, cell.TurnID, session.Succeeded)
	if retry, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, metadata.Digest, metadata.Size); err != nil || !reflect.DeepEqual(reference, retry) {
		t.Fatal("successful settlement lost exact registration retry", retry, err)
	}
	if err := s.DeleteSubtree(t.Context(), child.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, metadata.Digest, metadata.Size); !errors.Is(err, ErrNotFound) || count(t, s, "content_references") != 0 {
		t.Fatal("deleted owner reference resurrected", err)
	}
	if budgetState(t, s, root.ID, session.BudgetModelCostNanoUSD).Used != 11 {
		t.Fatal("deleting provider evidence erased retained ancestor charge")
	}
}
