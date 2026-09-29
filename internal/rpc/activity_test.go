package rpc_test

import (
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestActivityRPCDiscoversAnotherClientsQueuedWorkWithoutExecution(t *testing.T) {
	_, c := fixture(t)
	tree := create(t, c)
	owner := tree.Root.ID
	parts := []protocol.Part{{Type: "text", Text: strings.Repeat("界", 2000)}}
	admitted := call[protocol.Admission](t, c, "sessions.submit", protocol.SubmitParams{SessionID: owner, Identity: protocol.RequestIdentity{ClientID: "other-client", RequestID: "pending"}, Source: "user", Parts: parts})
	value := call[protocol.SessionActivity](t, c, "sessions.activity", protocol.SessionParams{SessionID: owner})
	if value.ActiveTurn != nil || value.ActiveInputID != nil || value.QueuedInputCount != 1 || value.ExecutionPermit {
		t.Fatal(value)
	}
	page := call[protocol.InputPageResult](t, c, "inputs.page", protocol.InputPageParams{SessionID: owner, State: "queued", Limit: 1})
	if len(page.Items) != 1 || page.Items[0].ID != admitted.Input.ID || !page.Items[0].PreviewTruncated || page.Items[0].TextPreview == parts[0].Text {
		t.Fatal(page)
	}
	input := call[protocol.Input](t, c, "inputs.get", protocol.SessionInputParams{SessionID: owner, InputID: admitted.Input.ID})
	if input.Parts[0].Text != parts[0].Text || input.TurnID != nil {
		t.Fatal(input)
	}
	requireHistoryError(t, c, "inputs.get", protocol.SessionInputParams{SessionID: "foreign", InputID: input.ID}, "NOT_FOUND")
	var invalid protocol.InputPageResult
	if err := c.Call(t.Context(), "inputs.page", protocol.InputPageParams{SessionID: owner, State: "active", Limit: 1}, &invalid); err == nil {
		t.Fatal("invalid queue state passed client contract validation")
	}
	call[protocol.Input](t, c, "inputs.cancel", protocol.InputParams{InputID: input.ID})
	value = call[protocol.SessionActivity](t, c, "sessions.activity", protocol.SessionParams{SessionID: owner})
	if value.QueuedInputCount != 0 || value.ActiveTurn != nil {
		t.Fatal(value)
	}
}
