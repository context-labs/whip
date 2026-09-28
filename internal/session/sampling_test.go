package session

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestSamplingBoundsAndZeroPresence(t *testing.T) {
	for _, test := range []struct {
		name                        string
		value                       *float64
		validTemperature, validTopP bool
	}{
		{"omitted", nil, true, true},
		{"zero", new(0.0), true, true},
		{"fraction", new(0.25), true, true},
		{"one", new(1.0), true, true},
		{"two", new(2.0), true, false},
		{"negative", new(-0.1), false, false},
		{"too large", new(2.1), false, false},
		{"nan", new(math.NaN()), false, false},
		{"positive infinity", new(math.Inf(1)), false, false},
		{"negative infinity", new(math.Inf(-1)), false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, field := range []string{"temperature", "top_p"} {
				selection := ModelSelection{Provider: "provider", Name: "model"}
				selection.Temperature = test.value
				want := test.validTemperature
				if field == "top_p" {
					selection.Temperature, selection.TopP = nil, test.value
					want = test.validTopP
				}
				err := selection.Validate()
				if want && err != nil || !want && !errors.Is(err, ErrInvalid) {
					t.Fatalf("%s validation = %v, want valid %v", field, err, want)
				}
				if want {
					raw, err := json.Marshal(selection)
					if err != nil {
						t.Fatal(err)
					}
					var decoded ModelSelection
					if err := json.Unmarshal(raw, &decoded); err != nil || !selection.Equal(decoded) {
						t.Fatalf("sampling presence changed in JSON: %s %v", raw, err)
					}
				}
			}
		})
	}
	unset := ModelSelection{Provider: "provider", Name: "model"}
	zero := unset
	zero.Temperature = new(0.0)
	if unset.Equal(zero) || zero.Equal(unset) || !zero.Equal(zero.Clone()) {
		t.Fatal("sampling equality confused value identity with pointer identity or omission")
	}
}

func TestSamplingCapturedByWholeModelReplacement(t *testing.T) {
	base := Configuration{Model: ModelSelection{Provider: "p", Name: "main", Temperature: new(0.25), TopP: new(0.8)}, Compaction: CompactionPolicy{Model: &ModelSelection{Provider: "p", Name: "summary", Temperature: new(0.0), TopP: new(0.5)}}}
	inherited, err := Resolve(base, DefinitionDocument{}, ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	cloned := inherited.Clone()
	*inherited.Model.Temperature, *inherited.Model.TopP = 1, 1
	*inherited.Compaction.Model.Temperature, *inherited.Compaction.Model.TopP = 2, 1
	if !cloned.Model.Equal(base.Model) || !cloned.Compaction.Model.Equal(*base.Compaction.Model) {
		t.Fatal("sampling selections share mutable configuration")
	}
	definition := DefinitionDocument{ID: "sampling", Name: "Sampling", Defaults: ConfigPatch{Model: &ModelSelection{Provider: "p", Name: "defined", Temperature: new(0.0)}}}
	captured, _, ref, err := CanonicalDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}
	*definition.Defaults.Model.Temperature = 0.5
	_, _, changed, err := CanonicalDefinition(definition)
	if err != nil || changed == ref {
		t.Fatalf("sampling edit reused definition revision: %v", err)
	}
	resolved, err := Resolve(base, captured, ConfigPatch{})
	if err != nil || resolved.Model.Temperature == nil || *resolved.Model.Temperature != 0 || resolved.Model.TopP != nil {
		t.Fatalf("whole definition model did not replace sampling: %+v %v", resolved.Model, err)
	}
	cleared, err := Resolve(base, captured, ConfigPatch{Model: &ModelSelection{Provider: "p", Name: "clear"}})
	if err != nil || cleared.Model.Temperature != nil || cleared.Model.TopP != nil {
		t.Fatalf("explicit model replacement retained sampling: %+v %v", cleared.Model, err)
	}
}
