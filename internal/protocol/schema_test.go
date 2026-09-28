package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestContractFixtures(t *testing.T) {
	fixtures, err := Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	for i, fixture := range fixtures {
		err := Validate(fixture.Type, fixture.Value)
		if (err == nil) != fixture.Valid {
			t.Errorf("fixture %d %s valid=%v: %v", i, fixture.Type, fixture.Valid, err)
		}
	}
}

func TestPatchClearRoundTripAndOwnership(t *testing.T) {
	patch := ConfigPatch{Tools: map[string]ToolDeclaration{}, Output: &OutputPolicy{}}
	raw, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ConfigPatch
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	domain, err := decoded.Domain()
	if err != nil {
		t.Fatal(err)
	}
	if domain.Tools == nil || len(domain.Tools) != 0 || domain.Output == nil {
		t.Fatal("explicit clear became inheritance")
	}
	if reflect.DeepEqual(domain.Output.Schema, []byte("{}")) {
		t.Fatal("clear changed schema")
	}
}

func TestEveryOperationHasResolvableSchema(t *testing.T) {
	names := map[string]bool{}
	for _, operation := range Operations() {
		if names[operation.Name] {
			t.Fatal("duplicate operation", operation.Name)
		}
		names[operation.Name] = true
		for _, typ := range []reflect.Type{operation.Params, operation.Result} {
			schema, err := SchemaFor(typ)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := schema.Resolve(nil); err != nil {
				t.Fatalf("%s: %v", typ.Name(), err)
			}
		}
	}
}
