package rpc_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func matchParams(t *testing.T, method string, params any) protocol.MatchReceiptParams {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.MatchReceiptParams{Method: method, ParamsBase64: base64.StdEncoding.EncodeToString(raw)}
}

func TestReceiptMatchRPCValidatesExactOriginalPayloadWithoutSubmitting(t *testing.T) {
	_, c := fixture(t)
	tree := create(t, c)
	params := protocol.SubmitParams{SessionID: tree.Root.ID, Identity: protocol.RequestIdentity{ClientID: "external", RequestID: "original"}, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "original"}}}
	check := matchParams(t, "sessions.submit", params)
	requireHistoryError(t, c, "receipts.match", check, "NOT_FOUND")
	if value := call[protocol.SessionActivity](t, c, "sessions.activity", protocol.SessionParams{SessionID: tree.Root.ID}); value.QueuedInputCount != 0 {
		t.Fatal(value)
	}
	accepted := call[protocol.Admission](t, c, "sessions.submit", params)
	result := call[protocol.Admission](t, c, "receipts.match", check)
	if result.Input.ID != accepted.Input.ID || result.Receipt.Digest != accepted.Receipt.Digest {
		t.Fatal(result)
	}
	params.Parts[0].Text = "changed"
	requireHistoryError(t, c, "receipts.match", matchParams(t, "sessions.submit", params), "CONFLICT")
	bad := matchParams(t, "sessions.submit", map[string]any{"session_id": tree.Root.ID})
	requireHistoryError(t, c, "receipts.match", bad, "INVALID")
	// A restricted outer method cannot recursively inspect, dispatch arbitrary
	// account/network calls, or smuggle fields past the nested request contract.
	for _, method := range []string{"receipts.match", "accounts.openai.begin", "terminal.write"} {
		var output protocol.Admission
		if err := c.Call(t.Context(), "receipts.match", matchParams(t, method, map[string]any{}), &output); err == nil {
			t.Fatal("unlisted method accepted", method)
		}
	}
}

func TestReceiptMatchRPCHostAliasesShareOnlyTheExactDurableIntent(t *testing.T) {
	_, c := fixture(t)
	tree := create(t, c)
	params := protocol.RunShellParams{SessionID: tree.Root.ID, Identity: protocol.RequestIdentity{ClientID: "human", RequestID: "shell"}, Command: "echo example"}
	accepted := call[protocol.Admission](t, c, "shell.run", params)
	result := call[protocol.Admission](t, c, "receipts.match", matchParams(t, "shell.run", params))
	if result.Input.ID != accepted.Input.ID {
		t.Fatal(result)
	}
	params.Command = "echo changed"
	requireHistoryError(t, c, "receipts.match", matchParams(t, "shell.run", params), "CONFLICT")
}
