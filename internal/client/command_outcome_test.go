package client

import (
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestFillCommandPresentation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		result          protocol.CommandResult
		output, failure string
	}{
		{name: "retains output with explicit failure", result: protocol.CommandResult{Operation: "submit", Result: []byte(`{"text":"answer"}`), Failure: &protocol.RPCError{Message: "failure"}}, output: "answer", failure: "failure"},
		{name: "empty explicit failure overrides decode error", result: protocol.CommandResult{Operation: "submit", Result: []byte(`{`), Failure: &protocol.RPCError{}}},
		{name: "nil explicit failure retains decode error", result: protocol.CommandResult{Operation: "submit", Result: []byte(`{`)}, failure: "invalid command result"},
		{name: "empty body clears stale presentation", result: protocol.CommandResult{Operation: "submit", Output: "stale", Error: "stale"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fillCommandPresentation(&test.result)
			if test.result.Output != test.output || test.result.Error != test.failure {
				t.Fatalf("presentation = (%q, %q), want (%q, %q)", test.result.Output, test.result.Error, test.output, test.failure)
			}
		})
	}
}
