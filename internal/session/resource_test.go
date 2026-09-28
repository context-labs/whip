package session

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestResourceResolutionOwnsLimitsAndRejectsAmbiguousRoots(t *testing.T) {
	defaults := []ResourceLimit{{Kind: ResourceQueuedInputs, Limit: new(int64(20))}}
	overrides := []ResourceLimit{{Kind: ResourceDepth, Limit: new(int64(0))}}
	resolved, err := ResolveResourceLimits(defaults, overrides)
	if err != nil {
		t.Fatal(err)
	}
	*defaults[0].Limit, *overrides[0].Limit = 99, 99
	for _, limit := range resolved {
		if limit.Limit == nil {
			t.Fatal("root was resolved without a finite cap")
		}
		if limit.Kind == ResourceDepth && *limit.Limit != 0 || limit.Kind == ResourceQueuedInputs && *limit.Limit != 20 {
			t.Fatal("resolved root aliases source configuration", limit)
		}
	}
	for _, invalid := range [][]ResourceLimit{
		{{Kind: ResourceDepth, Limit: new(int64(129))}},
		{{Kind: ResourceDepth}},
		{{Kind: ResourceQueuedInputs, Limit: new(int64(-1))}},
		{{Kind: "unknown", Limit: new(int64(0))}},
		{{Kind: ResourceDepth, Limit: new(int64(1))}, {Kind: ResourceDepth, Limit: new(int64(2))}},
	} {
		if _, err := ResolveResourceLimits(nil, invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid root accepted: %+v, %v", invalid, err)
		}
	}
	if err := ValidateResourceLimits([]ResourceLimit{{Kind: ResourceDepth}}); err != nil {
		t.Fatal("child inheritance rejected", err)
	}
}

func TestResourceLimitUsesExactDecimalStrings(t *testing.T) {
	var limit ResourceLimit
	if err := json.Unmarshal([]byte(`{"kind":"queued_inputs","limit":"9223372036854775807"}`), &limit); err != nil {
		t.Fatal(err)
	}
	if limit.Limit == nil || *limit.Limit != math.MaxInt64 {
		t.Fatal("limit lost precision", limit)
	}
	raw, err := json.Marshal(limit)
	if err != nil || string(raw) != `{"kind":"queued_inputs","limit":"9223372036854775807"}` {
		t.Fatalf("limit round trip: %s, %v", raw, err)
	}
	if err := json.Unmarshal([]byte(`{"kind":"queued_inputs","limit":9223372036854775807}`), &limit); err == nil {
		t.Fatal("host/guest JSON admitted an ambiguous numeric counter")
	}
}
