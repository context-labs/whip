package session

import "testing"

func TestTurnStateTransitions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to TurnState
		allowed  bool
	}{
		{"finish", Running, Succeeded, true},
		{"cancel intent", Running, Cancelling, true},
		{"cancel settled", Cancelling, Cancelled, true},
		{"crash while cancelling", Cancelling, Interrupted, true},
		{"success after cancellation", Cancelling, Succeeded, false},
		{"replay completed work", Succeeded, Running, false},
		{"rewrite failure", Failed, Succeeded, false},
		{"unknown", TurnState("unknown"), Running, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.from.CanTransitionTo(tc.to) != tc.allowed {
				t.Fatal("incorrect transition")
			}
		})
	}
}

func TestPartPayloadDiscriminator(t *testing.T) {
	for _, tc := range []struct {
		name  string
		parts []Part
		valid bool
	}{
		{"text", []Part{{Type: "text", Text: "hello"}}, true},
		{"content", []Part{{Type: "content", ReferenceID: "ref_123"}}, true},
		{"ambiguous", []Part{{Type: "text", Text: "hello", ReferenceID: "ref_123"}}, false},
		{"empty", nil, false},
		{"unknown", []Part{{Type: "image", ReferenceID: "ref_123"}}, false},
		{"unscoped", []Part{{Type: "content", ReferenceID: "../secret"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (ValidateParts(tc.parts) == nil) != tc.valid {
				t.Fatal("incorrect part validation")
			}
		})
	}
}
