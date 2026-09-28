package session

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCompactionPolicyResolutionAndWholeFieldReset(t *testing.T) {
	helper := ModelSelection{Provider: "helper", Name: "summary", Effort: "low"}
	for _, test := range []struct {
		name                 string
		base                 CompactionPolicy
		definition, override *CompactionPolicy
		want                 CompactionPolicy
	}{
		{name: "default", want: CompactionPolicy{ThresholdPercent: 50}},
		{name: "parent inheritance", base: CompactionPolicy{Model: &helper, ThresholdPercent: 70}, want: CompactionPolicy{Model: &helper, ThresholdPercent: 70}},
		{name: "definition replaces whole policy", base: CompactionPolicy{Model: &helper, ThresholdPercent: 70}, definition: &CompactionPolicy{ThresholdPercent: 30}, want: CompactionPolicy{ThresholdPercent: 30}},
		{name: "override replaces definition", definition: &CompactionPolicy{ThresholdPercent: 30}, override: &CompactionPolicy{Model: &helper}, want: CompactionPolicy{Model: &helper, ThresholdPercent: 50}},
		{name: "explicit reset", base: CompactionPolicy{Model: &helper, ThresholdPercent: 70}, override: &CompactionPolicy{}, want: CompactionPolicy{ThresholdPercent: 50}},
		{name: "lower bound", override: &CompactionPolicy{ThresholdPercent: 1}, want: CompactionPolicy{ThresholdPercent: 1}},
		{name: "upper bound", override: &CompactionPolicy{ThresholdPercent: 100}, want: CompactionPolicy{ThresholdPercent: 100}},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := Configuration{Model: ModelSelection{Provider: "conversation", Name: "chat"}, Compaction: test.base}
			document := DefinitionDocument{ID: "assistant", Name: "Assistant", Defaults: ConfigPatch{Compaction: test.definition}}
			resolved, err := Resolve(base, document, ConfigPatch{Compaction: test.override})
			if err != nil || !reflect.DeepEqual(resolved.Compaction, test.want) || resolved.Model != base.Model {
				t.Fatalf("resolved policy=%+v err=%v want=%+v", resolved.Compaction, err, test.want)
			}
			if !reflect.DeepEqual(base.Compaction, test.base) {
				t.Fatal("resolution mutated host or parent defaults")
			}
		})
	}
}

func TestCompactionPolicyCloneAndDefinitionIsolation(t *testing.T) {
	model := ModelSelection{Provider: "helper", Name: "original", Effort: "low"}
	base := Configuration{Model: ModelSelection{Provider: "conversation", Name: "chat"}, Compaction: CompactionPolicy{Model: &model, ThresholdPercent: 60}}
	resolved, err := Resolve(base, DefinitionDocument{}, ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	clone := resolved.Clone()
	child, err := Resolve(resolved, DefinitionDocument{}, ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	model.Name = "mutated source"
	clone.Compaction.Model.Name = "mutated clone"
	resolved.Compaction.Model.Name = "mutated parent"
	if child.Compaction.Model.Name != "original" || child.Compaction.ThresholdPercent != 60 {
		t.Fatal("child policy aliases its parent or original source")
	}
	document := DefinitionDocument{ID: "policy", Name: "Policy", Defaults: ConfigPatch{Compaction: &CompactionPolicy{Model: &model, ThresholdPercent: 80}}}
	canonical, _, ref, err := CanonicalDefinition(document)
	if err != nil {
		t.Fatal(err)
	}
	fromDefinition, err := Resolve(base, canonical, ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	document.Defaults.Compaction.Model.Name = "later edit"
	canonical.Defaults.Compaction.Model.Name = "changed registered copy"
	if fromDefinition.Compaction.Model.Name != "mutated source" {
		t.Fatal("resolved definition policy aliases a document")
	}
	_, _, changed, err := CanonicalDefinition(canonical)
	if err != nil || changed == ref {
		t.Fatalf("changed helper route reused definition revision: %+v err=%v", changed, err)
	}
	reset := DefinitionDocument{ID: "reset", Name: "Reset", Defaults: ConfigPatch{Compaction: &CompactionPolicy{}}}
	registered, raw, _, err := CanonicalDefinition(reset)
	if err != nil || registered.Defaults.Compaction == nil || !strings.Contains(string(raw), `"compaction":{"model":null,"threshold_percent":0}`) {
		t.Fatalf("reset became inheritance: %s err=%v", raw, err)
	}
	cleared, err := Resolve(base, registered, ConfigPatch{})
	if err != nil || cleared.Compaction.Model != nil || cleared.Compaction.ThresholdPercent != 50 {
		t.Fatalf("registered reset=%+v err=%v", cleared.Compaction, err)
	}
	encoded, err := json.Marshal(cleared)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Configuration
	if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(decoded.Compaction, cleared.Compaction) {
		t.Fatalf("captured default changed across JSON: %+v err=%v", decoded.Compaction, err)
	}
}

func TestCompactionPolicyRejectsInvalidThresholdAndHelperRoute(t *testing.T) {
	for _, policy := range []CompactionPolicy{
		{ThresholdPercent: -1},
		{ThresholdPercent: 101},
		{Model: &ModelSelection{}},
		{Model: &ModelSelection{Provider: "bad provider", Name: "summary"}},
		{Model: &ModelSelection{Provider: "helper"}},
		{Model: &ModelSelection{Provider: "helper", Name: "summary", Effort: strings.Repeat("x", 65)}},
	} {
		if err := (ConfigPatch{Compaction: &policy}).Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid patch accepted: %+v err=%v", policy, err)
		}
		configuration := Configuration{Model: ModelSelection{Provider: "conversation", Name: "chat"}, Compaction: policy}
		if err := configuration.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid configuration accepted: %+v err=%v", policy, err)
		}
	}
	partial := Configuration{Model: ModelSelection{Provider: "conversation", Name: "chat"}}
	if err := partial.Validate(); err != nil || partial.Compaction.ThresholdPercent != 0 {
		t.Fatalf("host validation applied a capture default: %+v err=%v", partial.Compaction, err)
	}
}
