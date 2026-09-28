package session

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestGoalRequestAndEligibilityResolution(t *testing.T) {
	for _, limit := range []*int64{nil, new(int64(0)), new(int64(math.MaxInt64))} {
		spec, err := (GoalRequest{Text: "objective", MaxContinuations: limit}).Resolve()
		want := int64(100)
		if limit != nil {
			want = *limit
		}
		if err != nil || spec.MaxContinuations != want {
			t.Fatalf("resolve %+v %v", spec, err)
		}
		if limit != nil {
			*limit = 7
		}
		if spec.MaxContinuations != want {
			t.Fatal("request pointer aliased")
		}
	}
	for _, request := range []GoalRequest{{}, {Text: " \t\n"}, {Text: "\x00"}, {Text: "\xff"}, {Text: "goal", MaxContinuations: new(int64(-1))}, {Text: strings.Repeat("x", MaxDocumentBytes+1)}} {
		if _, err := request.Resolve(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid accepted %v", err)
		}
	}
	if _, err := (GoalRequest{Text: strings.Repeat("x", MaxDocumentBytes)}).Resolve(); err != nil {
		t.Fatal(err)
	}
	base := Configuration{Model: ModelSelection{Provider: "local", Name: "model"}}
	assistant, err := Resolve(base, Builtins()[0], ConfigPatch{})
	if err != nil || !assistant.GoalsEnabled {
		t.Fatalf("builtin %+v %v", assistant, err)
	}
	constrained := DefinitionDocument{ID: "constrained", Name: "Constrained", Defaults: ConfigPatch{GoalsEnabled: new(false)}}
	resolved, err := Resolve(assistant, constrained, ConfigPatch{})
	if err != nil || resolved.GoalsEnabled {
		t.Fatalf("definition failed to disable %+v %v", resolved, err)
	}
	flag := true
	enabled, err := Resolve(resolved, constrained, ConfigPatch{GoalsEnabled: &flag})
	if err != nil || !enabled.GoalsEnabled {
		t.Fatal(err)
	}
	flag = false
	if !enabled.GoalsEnabled {
		t.Fatal("config aliased patch")
	}
	raw, err := json.Marshal(ConfigPatch{GoalsEnabled: new(false)})
	if err != nil {
		t.Fatal(err)
	}
	var patch ConfigPatch
	if err := json.Unmarshal(raw, &patch); err != nil || patch.GoalsEnabled == nil || *patch.GoalsEnabled {
		t.Fatalf("false lost %s %v", raw, err)
	}
	builtin := Builtins()
	*builtin[0].Defaults.GoalsEnabled = false
	if !*Builtins()[0].Defaults.GoalsEnabled {
		t.Fatal("builtin pointer shared")
	}
}
