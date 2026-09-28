package protocol

import (
	"encoding/json"
	"testing"
)

func TestResourceCountersAndInheritance(t *testing.T) {
	for _, raw := range []string{
		`{"session_id":"child","expected_revision":"9007199254740993","resource":{"kind":"descendants","limit":"9007199254740994"}}`,
		`{"session_id":"child","expected_revision":"0","resource":{"kind":"queued_inputs","limit":null}}`,
	} {
		if err := Validate("SetResourceParams", []byte(raw)); err != nil {
			t.Fatal(err)
		}
		var params SetResourceParams
		if err := json.Unmarshal([]byte(raw), &params); err != nil {
			t.Fatal(err)
		}
		domain := params.Resource.Domain()
		if params.Resource.Limit != nil {
			*params.Resource.Limit = 1
			if domain.Limit == nil || *domain.Limit != 9007199254740994 {
				t.Fatal("conversion lost exact counter or aliased its source")
			}
		} else if domain.Limit != nil {
			t.Fatal("inheritance became a finite limit")
		}
	}
	for _, raw := range []string{
		`{"session_id":"child","expected_revision":1,"resource":{"kind":"descendants","limit":"1"}}`,
		`{"session_id":"child","expected_revision":"1","resource":{"kind":"descendants","limit":1}}`,
		`{"session_id":"child","expected_revision":"1","resource":{"kind":"descendants","limit":"9223372036854775808"}}`,
		`{"session_id":"child","expected_revision":"1","resource":{"kind":"unknown","limit":"1"}}`,
	} {
		if err := Validate("SetResourceParams", []byte(raw)); err == nil {
			t.Fatalf("accepted malformed resource request: %s", raw)
		}
	}
	limits := ResourceLimitsDomain([]ResourceLimit{{Kind: "depth", Limit: new(Counter(1))}, {Kind: "depth", Limit: new(Counter(2))}})
	if len(limits) != 2 || *limits[0].Limit != 1 || *limits[1].Limit != 2 {
		t.Fatal("conversion hid duplicate limits from domain validation")
	}
}
