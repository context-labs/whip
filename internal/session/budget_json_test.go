package session

import (
	"encoding/json"
	"testing"
)

func TestBudgetLimitsUseExactDecimalStrings(t *testing.T) {
	for _, value := range []string{`null`, `"0"`, `"9007199254740993"`} {
		raw := `{"kind":"logical_writes","limit":` + value + `}`
		var limit BudgetLimit
		if err := json.Unmarshal([]byte(raw), &limit); err != nil {
			t.Fatal(err)
		}
		if err := limit.Validate(); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(limit)
		if err != nil || string(encoded) != raw {
			t.Fatalf("budget did not round trip exactly: %s %v", encoded, err)
		}
	}
	for _, value := range []string{`0`, `9007199254740993`, `"9223372036854775808"`} {
		var limit BudgetLimit
		if err := json.Unmarshal([]byte(`{"kind":"logical_write_bytes","limit":`+value+`}`), &limit); err == nil {
			t.Fatalf("inexact or overflowing number accepted: %s", value)
		}
	}
}
