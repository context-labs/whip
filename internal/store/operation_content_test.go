package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func registerImage(t *testing.T, s *Store, owner session.SessionID, id string, size int64) session.ContentReference {
	t.Helper()
	ref := contentReference(owner, id, id)
	ref.MediaType = "image/jpeg"
	ref.Size = size
	value, err := s.RegisterContent(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func dispatchImageOperation(t *testing.T, s *Store, cell session.Cell, id string) session.Operation {
	t.Helper()
	operation := admitOperation(t, s, operationSpec(cell, id))
	if _, err := s.ResolvePermission(t.Context(), operation.ID, true); err != nil {
		t.Fatal(err)
	}
	if allowed, err := s.DispatchOperation(t.Context(), operation.ID); err != nil || !allowed {
		t.Fatal(allowed, err)
	}
	return operation
}

func TestTypedOperationImagesReachExactCellAtomicallyAndSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s := openTest(t, path)
	owner, cell := operationCell(t, s)
	ref := registerImage(t, s, owner.ID, "shot", 4)
	operation := dispatchImageOperation(t, s, cell, "image")
	outcome := session.OperationResult{State: session.OperationUncertain, Failure: new("later action lost"), Value: json.RawMessage(`{"text":"unchanged prefix"}`), ContentReferences: []string{ref.ID}}
	for range 2 {
		if _, err := s.SettleOperation(t.Context(), operation.ID, outcome); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	preserved, err := s.Operation(t.Context(), operation.ID)
	if err != nil || !reflect.DeepEqual(preserved.Result, &outcome) {
		t.Fatal("lost committed image evidence", err)
	}
	if dispatch, err := s.DispatchOperation(t.Context(), operation.ID); err != nil || dispatch {
		t.Fatal("settled image effect replayed", dispatch, err)
	}
	output := session.ToolResult{CallID: cell.CallID, Output: "canonical output", IsError: true}
	execTest(t, s, `CREATE TRIGGER reject_image_cell BEFORE UPDATE ON cells BEGIN SELECT RAISE(ABORT,'test'); END`)
	if _, err := s.SettleCell(t.Context(), cell.ID, session.CellFailed, output, nil); err == nil {
		t.Fatal("injected cell transaction committed")
	}
	if got, _ := s.Cell(t.Context(), cell.ID); got.ResultMessageID != nil {
		t.Fatal("partial image cell committed")
	}
	execTest(t, s, "DROP TRIGGER reject_image_cell")
	for range 2 {
		if _, err := s.SettleCell(t.Context(), cell.ID, session.CellFailed, output, nil); err != nil {
			t.Fatal(err)
		}
	}
	history, err := s.History(t.Context(), owner.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	last := history[len(history)-1]
	if last.Role != session.Tool || len(last.Parts) != 2 || last.Parts[0].Result.Output != "canonical output" || last.Parts[1].ReferenceID != ref.ID {
		t.Fatal("image was not exact canonical tool content", last)
	}
	if referenced, err := s.ContentReferenced(t.Context(), ref.Digest); err != nil || !referenced {
		t.Fatal("image body collectible", err)
	}
}

func TestOperationImageOwnershipMediaAndAtomicCellBounds(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	_, other := create(t, s, nil)
	foreign := registerImage(t, s, other.ID, "foreign", 1)
	text := contentReference(owner.ID, "text", "body")
	if _, err := s.RegisterContent(t.Context(), text); err != nil {
		t.Fatal(err)
	}
	own := registerImage(t, s, owner.ID, "own", 1)
	operation := dispatchImageOperation(t, s, cell, "first")
	for _, refs := range [][]string{{foreign.ID}, {text.ID}, {"missing"}, {own.ID, own.ID}} {
		if _, err := s.SettleOperation(t.Context(), operation.ID, session.OperationResult{State: session.OperationSucceeded, ContentReferences: refs}); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("invalid image settled", refs, err)
		}
	}
	// JSON lookalikes remain ordinary output and cannot become model attachments.
	if _, err := s.SettleOperation(t.Context(), operation.ID, session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(`{"content_references":["foreign"]}`)}); err != nil {
		t.Fatal(err)
	}
	refs := []string{}
	for i := range session.MaxOperationAttachments {
		id := fmt.Sprintf("shot_%d", i)
		registerImage(t, s, owner.ID, id, 1)
		refs = append(refs, id)
	}
	operation = dispatchImageOperation(t, s, cell, "bounded")
	if _, err := s.SettleOperation(t.Context(), operation.ID, session.OperationResult{State: session.OperationSucceeded, ContentReferences: refs}); err != nil {
		t.Fatal(err)
	}
	if count, _, err := s.CellAttachmentAllowance(t.Context(), cell.ID); err != nil || count != 0 {
		t.Fatal("incorrect image allowance", count, err)
	}
	extra := dispatchImageOperation(t, s, cell, "extra")
	if _, err := s.SettleOperation(t.Context(), extra.ID, session.OperationResult{State: session.OperationSucceeded, ContentReferences: []string{own.ID}}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("aggregate cell limit escaped", err)
	}
	if _, err := s.SettleOperation(t.Context(), extra.ID, session.OperationResult{State: session.OperationFailed, Failure: new("attachment budget unavailable")}); err != nil {
		t.Fatal(err)
	}
	output := session.ToolResult{CallID: cell.CallID, Output: "output"}
	if _, err := s.SettleCell(t.Context(), cell.ID, session.CellSucceeded, output, nil); err != nil {
		t.Fatal(err)
	}
	history, _ := s.History(t.Context(), owner.ID, 0, 100)
	if got := len(history[len(history)-1].Parts); got != 9 {
		t.Fatal("image count", got)
	}
}

func TestOperationImageByteLimitAndRetiredValidation(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	operation := dispatchImageOperation(t, s, cell, "bytes")
	refs := []string{}
	for i := range 5 {
		id := fmt.Sprintf("large_%d", i)
		ref := registerImage(t, s, owner.ID, id, session.MaxContentBytes)
		refs = append(refs, ref.ID)
	}
	if _, err := s.SettleOperation(t.Context(), operation.ID, session.OperationResult{State: session.OperationSucceeded, ContentReferences: refs}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("aggregate image bytes unbounded", err)
	}
	for _, state := range []session.OperationState{session.OperationDenied, session.OperationCancelled} {
		if (session.OperationResult{State: state, ContentReferences: []string{"image"}}).Validate() == nil {
			t.Fatal("undispatched image accepted")
		}
	}
	// A saved previous format is rejected rather than silently normalized.
	prior := filepath.Join(t.TempDir(), "prior.db")
	old := openTest(t, prior)
	execTest(t, old, "PRAGMA user_version=42")
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), prior); err == nil {
		t.Fatal("incompatible image contract reopened", err)
	}
}

func TestImageRecoveryProjectsOnlySettledEvidenceAndNeverGrantsReplay(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	_, other := create(t, s, nil)
	image := registerImage(t, s, owner.ID, "observed", 5)
	first := dispatchImageOperation(t, s, cell, "finished")
	if _, err := s.SettleOperation(t.Context(), first.ID, session.OperationResult{State: session.OperationSucceeded, ContentReferences: []string{image.ID}}); err != nil {
		t.Fatal(err)
	}
	lost := dispatchImageOperation(t, s, cell, "lost")
	if _, err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	parts, err := s.CellResultParts(t.Context(), owner.ID, cell.ID)
	if err != nil || len(parts) != 2 || parts[1].ReferenceID != image.ID || !parts[0].Result.IsError {
		t.Fatal("recovery dropped settled screenshot", parts, err)
	}
	if _, err := s.CellResultParts(t.Context(), other.ID, cell.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign session read cell image", err)
	}
	operation, err := s.Operation(t.Context(), lost.ID)
	if err != nil || operation.State != session.OperationUncertain || len(operation.Result.ContentReferences) != 0 {
		t.Fatal("recovery invented lost outcome images", operation, err)
	}
	for _, id := range []session.OperationID{first.ID, lost.ID} {
		if allowed, err := s.DispatchOperation(t.Context(), id); err != nil || allowed {
			t.Fatal("recovery replayed effect", allowed, err)
		}
	}
}
