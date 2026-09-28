package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestModelHelperAttemptProvenance(t *testing.T) {
	source := session.ModelAttempt{
		ID: "attempt", TurnID: "turn", LogicalID: "logical", Number: 1,
		OperationID: new(session.OperationID("operation")), BatchIndex: new(0),
		State: session.AttemptSucceeded, CostSource: "unknown", CreatedAt: time.Now(),
		Request: session.ModelRequestSnapshot{
			Purpose: session.ModelHelperPurpose, Model: session.ModelSelection{Provider: "provider", Name: "model"},
			Route: "https://provider.example", Adapter: "openai-chat", RequestDigest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			MaxOutputTokens: 100, TimeoutMillis: 1000,
		},
	}
	projected := ModelAttemptFromDomain(source)
	*source.OperationID, *source.BatchIndex = "changed", 31
	if projected.OperationID == nil || *projected.OperationID != "operation" || projected.BatchIndex == nil || *projected.BatchIndex != 0 || projected.MessageID != nil {
		t.Fatal("helper ownership changed or zero index disappeared", projected)
	}
	for _, index := range []int{0, 31, -1, 32} {
		projected.BatchIndex = &index
		raw, err := json.Marshal(ModelAttemptsResult{Items: []ModelAttempt{projected}})
		if err != nil {
			t.Fatal(err)
		}
		err = Validate("ModelAttemptsResult", raw)
		if (err == nil) != (index >= 0 && index < 32) {
			t.Fatalf("batch index %d validation: %v", index, err)
		}
	}
}
