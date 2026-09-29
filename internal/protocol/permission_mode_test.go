package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestPermissionModeContractExactRevisionsAndCasing(t *testing.T) {
	created := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	value := PermissionModeEditFromDomain(session.PermissionModeEdit{
		ID: "Edit.Mixed-Case", SessionID: "root", ExpectedRevision: 9007199254740993, Mode: session.PermissionAutomatic,
		Policy: session.PermissionPolicy{TreeID: "tree", Mode: session.PermissionAutomatic, Revision: 9007199254740994, UpdatedAt: created}, PreviousMode: session.PermissionPrompt, CreatedAt: created,
	})
	raw, err := json.Marshal(value)
	if err != nil || Validate("PermissionModeEdit", raw) != nil {
		t.Fatal("invalid projected receipt", string(raw), err)
	}
	if !strings.Contains(string(raw), `"expected_revision":"9007199254740993"`) || !strings.Contains(string(raw), `"id":"Edit.Mixed-Case"`) {
		t.Fatal("receipt lost identity or precision", string(raw))
	}
	operation := OperationFromDomain(session.Operation{ID: "operation", CellID: "cell", RequestID: "request", Capability: "files.read", Resource: "/workspace", Arguments: json.RawMessage(`{}`), SessionID: "root", TurnID: "turn", State: session.OperationReady, PermissionRevision: new(session.Revision(9007199254740993)), CreatedAt: created})
	raw, err = json.Marshal(operation)
	if err != nil || Validate("HostOperation", raw) != nil || !strings.Contains(string(raw), `"permission_revision":"9007199254740993"`) {
		t.Fatal("captured policy lost precision", string(raw), err)
	}
	for _, invalid := range []string{
		`{"edit_id":"edit","session_id":"root","expected_revision":"1","mode":"Automatic"}`,
		`{"edit_id":"edit","session_id":"root","expected_revision":9007199254740993,"mode":"automatic"}`,
		`{"edit_id":"edit","session_id":"root","expected_revision":"0","mode":"automatic"}`,
		`{"edit_id":"edit","session_id":"root","expected_revision":"9223372036854775808","mode":"automatic"}`,
		`{"edit_id":"edit","session_id":"root","expected_revision":"01","mode":"automatic"}`,
	} {
		if Validate("SetPermissionModeParams", []byte(invalid)) == nil {
			t.Fatal("invalid edit validated", invalid)
		}
	}
}
