package session

import "testing"

func TestModelHelperLogicalIdentity(t *testing.T) {
	seen := map[string]bool{}
	for _, operation := range []OperationID{"first", "second"} {
		for index := range MaxModelBatchItems {
			id, err := ModelHelperLogicalID(operation, index)
			if err != nil || ValidateID(id+"_try_100") != nil || seen[id] {
				t.Fatalf("invalid or reused identity %q: %v", id, err)
			}
			seen[id] = true
			retry, err := ModelHelperLogicalID(operation, index)
			if err != nil || retry != id {
				t.Fatalf("unstable identity: %q %v", retry, err)
			}
		}
	}
	for _, index := range []int{-1, MaxModelBatchItems} {
		if _, err := ModelHelperLogicalID("operation", index); err == nil {
			t.Fatal("accepted invalid index", index)
		}
	}
	if _, err := ModelHelperLogicalID("", 0); err == nil {
		t.Fatal("accepted absent operation")
	}
}
