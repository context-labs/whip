package protocol

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestSamplingProtocolCaptureAndProjection(t *testing.T) {
	wire := ModelSelection{Provider: "p", Name: "model", Temperature: new(0.0), TopP: new(0.75)}
	patch := ConfigPatch{Model: &wire, Compaction: &CompactionPolicy{Model: &wire, ThresholdPercent: 50}}
	domain, err := patch.Domain()
	if err != nil {
		t.Fatal(err)
	}
	*wire.Temperature, *wire.TopP = 1, 1
	if domain.Model.Temperature == nil || *domain.Model.Temperature != 0 || *domain.Compaction.Model.TopP != 0.75 {
		t.Fatal("domain capture aliases mutable protocol values")
	}
	attempt := session.ModelAttempt{Request: session.ModelRequestSnapshot{Model: *domain.Model}}
	projected := ModelAttemptFromDomain(attempt)
	*attempt.Request.Model.Temperature, *attempt.Request.Model.TopP = 2, 0
	if projected.Request.Model.Temperature == nil || *projected.Request.Model.Temperature != 0 || *projected.Request.Model.TopP != 0.75 {
		t.Fatal("public attempt projection aliases durable selection")
	}
	for _, raw := range []string{
		`{"model":{"provider":"p","name":"m","effort":""}}`,
		`{"model":{"provider":"p","name":"m","effort":"","temperature":null,"top_p":null}}`,
	} {
		var clearing ConfigPatch
		if err := json.Unmarshal([]byte(raw), &clearing); err != nil {
			t.Fatal(err)
		}
		captured, err := clearing.Domain()
		if err != nil || captured.Model == nil || captured.Model.Temperature != nil || captured.Model.TopP != nil {
			t.Fatalf("whole model did not clear sampling: %+v %v", captured.Model, err)
		}
	}
	wire.Temperature = new(2.1)
	if _, err := patch.Domain(); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("domain conversion accepted invalid sampling", err)
	}
}
