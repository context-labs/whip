package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStandingPublicationProjectionAndEditBounds(t *testing.T) {
	for _, value := range []HostStandingInstructions{{Published: true}, {Revision: new(strings.Repeat("a", 64)), Text: new("unpublished")}, {Published: true, Revision: new("invalid"), Text: new("")}, {Published: true, Revision: new(strings.Repeat("a", 64)), Text: new("nul\x00")}} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate("HostStandingInstructions", raw); err == nil {
			t.Fatal("invalid publication projection accepted", string(raw))
		}
	}
	for _, text := range []string{"nul\x00", strings.Repeat("x", 65537)} {
		raw, err := json.Marshal(WriteHostStandingInstructionsParams{ExpectedRevision: strings.Repeat("a", 64), Text: text})
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate("WriteHostStandingInstructionsParams", raw); err == nil {
			t.Fatal("invalid text accepted")
		}
	}
}
