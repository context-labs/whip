package session

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMailEvidenceValidation(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		ref  *string
		ok   bool
	}{
		{"body", "message", nil, true},
		{"evidence only", "", new("owned-reference"), true},
		{"both", "message", new("owned-reference"), true},
		{"neither", "", nil, false},
		{"empty reference", "message", new(""), false},
		{"invalid reference", "message", new("foreign/reference"), false},
		{"oversized body", strings.Repeat("x", MaxMailBodyBytes+1), new("reference"), false},
		{"invalid UTF-8", "\xff", new("reference"), false},
		{"blank body", " ", new("reference"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := MailSend{RecipientID: "recipient", Delivery: MailQueued, Body: test.body, EvidenceRef: test.ref}
			err := value.Validate()
			if test.ok && err != nil || !test.ok && !errors.Is(err, ErrInvalid) {
				t.Fatalf("validation=%v valid=%v", err, test.ok)
			}
			raw, err := json.Marshal(value)
			if err != nil || !strings.Contains(string(raw), `"evidence_ref":`) {
				t.Fatalf("missing nullable evidence field: %s %v", raw, err)
			}
		})
	}
}
