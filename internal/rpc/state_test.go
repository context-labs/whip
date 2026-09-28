package rpc_test

import (
	"encoding/base64"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestStateRPCExactJSONImmutableReadsAndRevisionConflicts(t *testing.T) {
	_, client := fixture(t)
	owner, other := create(t, client), create(t, client)
	params := protocol.WriteStateParams{SessionID: owner.Root.ID, Scope: "session", VersionID: "state_first", Key: "key", DataBase64: base64.StdEncoding.EncodeToString([]byte(`[9007199254740993]`))}
	first := call[protocol.StateVersion](t, client, "state.write", params)
	params.VersionID = "state_second"
	params.ExpectedRevision = 1
	params.DataBase64 = base64.StdEncoding.EncodeToString([]byte(`[2]`))
	second := call[protocol.StateVersion](t, client, "state.append", params)
	if second.Revision != 2 {
		t.Fatal("append revision", second)
	}
	read := call[protocol.ReadStateResult](t, client, "state.read", protocol.ReadStateParams{SessionID: owner.Root.ID, VersionID: first.ID, Length: 65536})
	data, err := base64.StdEncoding.DecodeString(read.DataBase64)
	if err != nil || string(data) != `[9007199254740993]` {
		t.Fatalf("old value changed: %s %v", data, err)
	}
	if retry := call[protocol.StateVersion](t, client, "state.append", params); retry.ID != second.ID {
		t.Fatal("append retry changed version")
	}
	params.VersionID = "state_stale"
	var result protocol.StateVersion
	if err := client.Call(t.Context(), "state.write", params, &result); err == nil {
		t.Fatal("stale revision accepted")
	}
	var inaccessible protocol.ReadStateResult
	if err := client.Call(t.Context(), "state.read", protocol.ReadStateParams{SessionID: other.Root.ID, VersionID: first.ID, Length: 10}, &inaccessible); err == nil {
		t.Fatal("unrelated tree read state")
	}
	head := call[protocol.StateVersion](t, client, "state.get", protocol.GetStateParams{SessionID: owner.Root.ID, Scope: "session", Key: "key"})
	if head.ID != second.ID {
		t.Fatal("head changed after rejected write")
	}
	history := call[protocol.StateVersionsResult](t, client, "state.history", protocol.StateHistoryParams{SessionID: owner.Root.ID, Scope: "session", Key: "key", After: 1, Limit: 1})
	if len(history.Items) != 1 || history.Items[0].ID != second.ID {
		t.Fatal("history cursor", history)
	}
	messages := call[protocol.HistoryResult](t, client, "sessions.history", protocol.HistoryParams{SessionID: owner.Root.ID, Limit: 100})
	if len(messages.Items) != 0 {
		t.Fatal("state access admitted work")
	}
	malformed := map[string]any{"session_id": owner.Root.ID, "scope": "session", "version_id": "bad", "key": "key", "data_base64": "bnVsbA=="}
	if err := client.Call(t.Context(), "state.write", malformed, &result); err == nil {
		t.Fatal("omitted expected revision accepted")
	}
	malformed["expected_revision"] = 0
	if err := client.Call(t.Context(), "state.write", malformed, &result); err == nil {
		t.Fatal("numeric revision accepted instead of exact decimal string")
	}
}
