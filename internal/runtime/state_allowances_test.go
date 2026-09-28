package runtime

import (
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tool"
)

func TestStateStagingRetainsSubmittedAppendBytesAndRejectsGuestAccounting(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner := createTest(t, r)
	if _, err := r.WriteState(t.Context(), owner.ID, session.TreeState, "initial", "key", 0, []byte(`[11111111111111111]`)); err != nil {
		t.Fatal(err)
	}
	staged, err := r.stageState(t.Context(), owner.ID, "append", session.StatePut{Scope: session.TreeState, Key: "key", ExpectedRevision: new(int64(1)), Value: json.RawMessage(`[2]`)})
	if err != nil || staged.SubmittedBytes != 3 || staged.Size <= staged.SubmittedBytes {
		t.Fatalf("append confused submitted and merged bytes: %+v %v", staged, err)
	}
	call := tool.Invocation{Name: "append", Arguments: map[string]any{"scope": "tree", "key": "key", "expected_revision": "1", "value": []any{2}, "submitted_bytes": "0"}}
	if _, err := r.prepareState(t.Context(), owner, call); err == nil {
		t.Fatal("guest selected its accounting amount")
	}
	delete(call.Arguments, "submitted_bytes")
	prepared, err := r.prepareState(t.Context(), owner, call)
	if err != nil {
		t.Fatal(err)
	}
	var normalized session.StateWrite
	if err := json.Unmarshal(prepared.Arguments, &normalized); err != nil || normalized.SubmittedBytes != 3 {
		t.Fatalf("normalized accounting=%+v %v", normalized, err)
	}
}
