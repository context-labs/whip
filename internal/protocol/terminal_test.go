package protocol

import (
	"fmt"
	"testing"
)

func TestTerminalReadWaitIsOptionalAndBounded(t *testing.T) {
	for _, test := range []struct {
		name  string
		field string
		valid bool
	}{
		{"omitted", "", true},
		{"immediate", `,"wait_ms":0`, true},
		{"maximum", `,"wait_ms":5000`, true},
		{"negative", `,"wait_ms":-1`, false},
		{"oversize", `,"wait_ms":5001`, false},
		{"fraction", `,"wait_ms":0.5`, false},
		{"null", `,"wait_ms":null`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := fmt.Appendf(nil, `{"process_epoch":"boot","id":"terminal","cursor":"0","limit":32768%s}`, test.field)
			if err := Validate("TerminalReadParams", raw); (err == nil) != test.valid {
				t.Fatalf("valid=%v: %v", test.valid, err)
			}
		})
	}
}
