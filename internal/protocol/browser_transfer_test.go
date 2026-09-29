package protocol

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestBrowserTransferContractBoundsAndInternalHostProvenance(t *testing.T) {
	params := SpawnSessionParams{Identity: RequestIdentity{ClientID: "client", RequestID: "transfer"}, ParentID: "parent", Overrides: ConfigPatch{}, Parts: []Part{{Type: "text", Text: "child"}}}
	for _, attachments := range [][]ID{nil, {}, {"a"}, {"a", "b", "c", "d"}} {
		params.BrowserAttachments = attachments
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate("SpawnSessionParams", raw); err != nil {
			t.Fatal(attachments, err)
		}
	}
	for _, attachments := range [][]ID{{"a", "b", "c", "d", "e"}, {"a", "a"}, {ID(strings.Repeat("x", 129))}, {""}} {
		params.BrowserAttachments = attachments
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate("SpawnSessionParams", raw); err == nil {
			t.Fatal("unbounded or duplicate attachment accepted", attachments)
		}
	}
	operation := DirectHostInput{Module: "agents", Name: "spawn", ArgumentsBase64: base64.StdEncoding.EncodeToString([]byte(`{}`))}
	raw, _ := json.Marshal(CallHostToolParams{Identity: params.Identity, SessionID: "parent", Operation: operation})
	if err := Validate("CallHostToolParams", raw); err == nil {
		t.Fatal("private accepted transfer can be forged through tool.call")
	}
}
