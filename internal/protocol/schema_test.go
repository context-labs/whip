package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRegistrySchemasResolveAndHaveUniqueNames(t *testing.T) {
	operations := map[string]bool{}
	types := map[string]reflect.Type{}
	for _, operation := range Operations() {
		key := operation.Surface + ":" + operation.Name
		if operations[key] {
			t.Fatalf("duplicate operation %s", key)
		}
		operations[key] = true
		if operation.Execution == "" || operation.Permission == "" {
			t.Fatalf("missing semantics: %s", key)
		}
		for _, typ := range []reflect.Type{operation.Params, operation.Result} {
			if existing := types[typ.Name()]; existing != nil && existing != typ {
				t.Fatalf("ambiguous type name %s", typ.Name())
			}
			types[typ.Name()] = typ
			schema, err := SchemaFor(typ)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := schema.Resolve(nil); err != nil {
				t.Fatalf("%s: %v", key, err)
			}
		}
	}
}

func TestValidateParametersBeforeAdmission(t *testing.T) {
	for _, test := range []struct {
		name, raw string
		valid     bool
	}{
		{name: "events.subscribe", raw: `{"root_id":"root","subscription_id":"view","cursor":"9007199254740993"}`, valid: true},
		{name: "events.subscribe", raw: `{"root_id":"root","subscription_id":"view","cursor":1}`},
		{name: "events.subscribe", raw: `{"root_id":"root","subscription_id":"view","cursor":"9223372036854775808"}`},
		{name: "events.subscribe", raw: `{"root_id":"root","cursor":"0"}`},
		{name: "events.subscribe", raw: `{"root_id":"root","subscription_id":"view","cursor":"0","extra":true}`},
		{name: "events.subscribe", raw: `{} {}`},
		{name: "daemon.ping", raw: `{}`, valid: true},
		{name: "events.replay", raw: `{"root_id":"root","cursor":"0","limit":100}`, valid: true},
		{name: "daemon.ping", valid: true},
		{name: "unknown", raw: `{}`},
	} {
		t.Run(test.name+test.raw, func(t *testing.T) {
			err := ValidateRPC(test.name, json.RawMessage(test.raw))
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}
