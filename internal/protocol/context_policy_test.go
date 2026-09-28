package protocol

import (
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestCompactionPolicyPatchRoundTripKeepsExplicitResetAndOwnsModel(t *testing.T) {
	model := &ModelSelection{Provider: "summary", Name: "fast", Effort: "low"}
	patch := ConfigPatch{Compaction: &CompactionPolicy{Model: model, ThresholdPercent: 25}}
	domain, err := patch.Domain()
	if err != nil || domain.Compaction == nil || domain.Compaction.Model == nil || domain.Compaction.Model.Name != "fast" {
		t.Fatalf("domain=%+v err=%v", domain, err)
	}
	model.Name = "changed"
	if domain.Compaction.Model.Name != "fast" {
		t.Fatal("wire model aliases the domain policy")
	}
	patch.Compaction = &CompactionPolicy{}
	raw, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"compaction":{"model":null,"threshold_percent":0}}` {
		t.Fatalf("explicit reset was erased: %s", raw)
	}
	domain, err = patch.Domain()
	if err != nil || domain.Compaction == nil || domain.Compaction.Model != nil || domain.Compaction.ThresholdPercent != 0 {
		t.Fatalf("reset=%+v err=%v", domain, err)
	}
	base := session.Configuration{Model: session.ModelSelection{Provider: "conversation", Name: "main"}, Compaction: session.CompactionPolicy{Model: &session.ModelSelection{Provider: "summary", Name: "fast"}, ThresholdPercent: 25}}
	resolved, err := session.Resolve(base, session.Builtins()[0], domain)
	if err != nil || resolved.Compaction.Model != nil || resolved.Compaction.ThresholdPercent != 50 {
		t.Fatalf("resolved reset=%+v err=%v", resolved.Compaction, err)
	}
}
