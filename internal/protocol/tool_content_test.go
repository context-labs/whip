package protocol

import (
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestToolContentContractRequiresFirstResultAndBoundedUniqueImages(t *testing.T) {
	fixtures, err := Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	for _, fixture := range fixtures {
		if fixture.Type == "Message" {
			var value map[string]any
			if json.Unmarshal(fixture.Value, &value) == nil && value["id"] == "message_image" {
				message = value
				break
			}
		}
	}
	if message == nil {
		t.Fatal("missing image fixture")
	}
	good := message["parts"].([]any)
	for _, parts := range [][]any{{good[1], good[0]}, {good[0], good[0]}, {good[0], good[1], good[1]}, append(append([]any{}, good...), make([]any, 8)...)} {
		message["parts"] = parts
		raw, _ := json.Marshal(message)
		if Validate("Message", raw) == nil {
			t.Fatal("invalid tool attachment shape accepted")
		}
	}
	message["parts"] = good
	raw, _ := json.Marshal(message)
	if err := Validate("Message", raw); err != nil {
		t.Fatal(err)
	}
	source := session.Operation{Result: &session.OperationResult{State: session.OperationSucceeded, ContentReferences: []string{"image"}}}
	projected := OperationFromDomain(source)
	source.Result.ContentReferences[0] = "changed"
	if projected.Result.ContentReferences[0] != "image" {
		t.Fatal("attachment projection aliases domain")
	}
}
