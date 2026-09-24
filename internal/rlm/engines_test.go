package rlm

import (
	"reflect"
	"testing"
)

func TestEngineDescriptorsRemainIndependent(t *testing.T) {
	t.Parallel()
	want := Engines()
	changed := Engines()
	for i := range changed {
		changed[i].ID = "changed"
		changed[i].Features[0] = "changed"
		changed[i].Limits["output_bytes"] = 0
	}
	if got := Engines(); !reflect.DeepEqual(got, want) {
		t.Fatal("mutating returned descriptors changed the bundled engine metadata")
	}
	for _, engine := range want {
		descriptor, err := ResolveEngine(engine.ID)
		if err != nil {
			t.Fatal(err)
		}
		descriptor.Features[0] = "changed"
		descriptor.Limits["output_bytes"] = 0
	}
	if got := Engines(); !reflect.DeepEqual(got, want) {
		t.Fatal("mutating a resolved descriptor changed the bundled engine metadata")
	}
}
