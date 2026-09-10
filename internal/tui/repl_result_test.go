package tui

import (
	"testing"
)

func TestReplResultVersions(t *testing.T) {
	tests := []struct {
		name, raw, engine, value, output, failure string
		steps                                     uint64
		jobs                                      bool
		unavailable                               bool
	}{
		{name: "legacy", raw: `{"value":900719925474099312345,"steps":7}`, engine: "starlark", value: "900719925474099312345", steps: 7},
		{name: "legacy none", raw: `{"value":null,"steps":0}`, engine: "starlark"},
		{name: "javascript null", raw: `{"format_version":2,"execution_engine":"quickjs","language":"javascript","has_value":true,"value":null,"metrics":{"quickjs_jobs":3}}`, engine: "quickjs", value: "null", jobs: true},
		{name: "javascript undefined", raw: `{"format_version":2,"execution_engine":"quickjs","language":"javascript","has_value":false}`, engine: "quickjs"},
		{name: "javascript preview", raw: `{"format_version":2,"execution_engine":"quickjs","language":"javascript","has_value":true,"value":{"type":"bigint","value":"900719925474099312345"},"value_preview":"900719925474099312345n"}`, engine: "quickjs", value: "900719925474099312345n"},
		{name: "failure with output", raw: "Error: failed\n" + `{"format_version":2,"execution_engine":"quickjs","language":"javascript","has_value":false,"output":"before failure"}`, engine: "quickjs", output: "before failure", failure: "failed"},
		{name: "unknown version", raw: `{"format_version":3,"execution_engine":"quickjs","language":"javascript","has_value":true,"value":42,"steps":0}`, unavailable: true},
		{name: "mismatched language", raw: `{"format_version":2,"execution_engine":"quickjs","language":"starlark","has_value":true,"value":42}`, unavailable: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cell replCell
			cell.decodeResult(tt.raw)
			if cell.engine != tt.engine || cell.value != tt.value || cell.output != tt.output || cell.errText != tt.failure {
				t.Fatalf("decoded cell = %+v", cell)
			}
			if cell.steps != tt.steps || (cell.jobs != nil) != tt.jobs {
				t.Fatalf("metrics: steps=%d jobs=%v", cell.steps, cell.jobs)
			}
			if cell.resultUnavailable != tt.unavailable {
				t.Fatalf("result unavailable=%v, want %v", cell.resultUnavailable, tt.unavailable)
			}
		})
	}
}
