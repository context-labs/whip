package protocol

import (
	"encoding/json"
	"testing"
)

func TestCreationIdentityAndCatalogExactCounterContracts(t *testing.T) {
	for _, test := range []struct {
		kind, raw string
		valid     bool
	}{
		{"TreeCreationParams", `{"creation_id":"MiXeD:Creation"}`, true},
		{"TreeCreationParams", `{"creation_id":""}`, false},
		{"TreeCreationParams", `{"creation_id":"bad id"}`, false},
		{"TreeCatalog", `{"revision":"9007199254740993"}`, true},
		{"TreeCatalog", `{"revision":"9223372036854775807"}`, true},
		{"TreeCatalog", `{"revision":"9223372036854775808"}`, false},
		{"TreeCatalog", `{"revision":9007199254740993}`, false},
		{"TreeCatalog", `{"revision":"0"}`, false},
		{"TreeCatalog", `{"revision":"01"}`, false},
		{"ListTreesParams", `{"limit":1,"expected_revision":"9007199254740993","archived":false}`, true},
		{"ListTreesParams", `{"limit":1,"expected_revision":"0"}`, false},
		{"ListTreesResult", `{"revision":"2","items":[],"next_cursor":null}`, true},
		{"ListTreesResult", `{"items":[],"next_cursor":null}`, false},
	} {
		if valid := Validate(test.kind, json.RawMessage(test.raw)) == nil; valid != test.valid {
			t.Fatal(test, valid)
		}
	}
	fixtures, err := Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		if fixture.Type != "CreateTreeParams" || !fixture.Valid {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal(fixture.Value, &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, "creation_id")
		raw, err := json.Marshal(fields)
		if err != nil || Validate("CreateTreeParams", raw) == nil {
			t.Fatal("creation identity not required", err)
		}
	}
}

func TestNavigationSchemaBounds(t *testing.T) {
	for _, test := range []struct {
		name, raw string
		valid     bool
	}{
		{"TreeSummariesParams", `{"root_ids":["root"]}`, true},
		{"TreeSummariesParams", `{"root_ids":null}`, false},
		{"TreeSummariesParams", `{"root_ids":[]}`, false},
		{"TreeSummariesParams", `{"root_ids":["root","root"]}`, false},
		{"TreeSummariesResult", `{"items":[],"missing_root_ids":[]}`, true},
		{"TreeSummariesResult", `{"items":null,"missing_root_ids":[]}`, false},
	} {
		err := Validate(test.name, []byte(test.raw))
		if (err == nil) != test.valid {
			t.Fatal(test, err)
		}
	}
}
