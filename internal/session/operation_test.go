package session

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOperationIntentValidation(t *testing.T) {
	valid := OperationSpec{ID: "op", CellID: "cell", RequestID: "request", Capability: "filesystem.read", Resource: "/workspace", Arguments: json.RawMessage(`{"path":"test"}`)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*OperationSpec){
		"id":              func(s *OperationSpec) { s.ID = "" },
		"request_id":      func(s *OperationSpec) { s.RequestID = "bad request" },
		"capability_path": func(s *OperationSpec) { s.Capability = "filesystem/read" },
		"empty_segment":   func(s *OperationSpec) { s.Capability = "filesystem..read" },
		"resource_empty":  func(s *OperationSpec) { s.Resource = "" },
		"resource_large":  func(s *OperationSpec) { s.Resource = strings.Repeat("x", 4097) },
		"resource_utf8":   func(s *OperationSpec) { s.Resource = string([]byte{255}) },
		"null":            func(s *OperationSpec) { s.Arguments = json.RawMessage(`null`) },
		"array":           func(s *OperationSpec) { s.Arguments = json.RawMessage(`[]`) },
		"invalid_json":    func(s *OperationSpec) { s.Arguments = json.RawMessage(`{`) },
		"large": func(s *OperationSpec) {
			s.Arguments = json.RawMessage(`{"x":"` + strings.Repeat("a", MaxDocumentBytes) + `"}`)
		},
		"escaped_large": func(s *OperationSpec) {
			s.Arguments = json.RawMessage(`{"x":"` + strings.Repeat("<", MaxDocumentBytes/2) + `"}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			spec := valid
			change(&spec)
			if err := spec.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid operation accepted: %v", err)
			}
		})
	}
}

func TestOperationOutcomeValidation(t *testing.T) {
	for _, state := range []OperationState{OperationSucceeded, OperationFailed, OperationDenied, OperationCancelled, OperationUncertain} {
		if err := (OperationResult{State: state}).Validate(); err != nil {
			t.Fatalf("%s: %v", state, err)
		}
	}
	for _, result := range []OperationResult{
		{State: OperationReady},
		{State: OperationSucceeded, Failure: new("failed")},
		{State: OperationCancelled, Value: json.RawMessage(`null`)},
		{State: OperationDenied, Value: json.RawMessage(`{}`)},
		{State: OperationSucceeded, Value: json.RawMessage(`{`)},
		{State: OperationFailed, Failure: new(string([]byte{255}))},
		{State: OperationFailed, Failure: new(strings.Repeat("a", 16385))},
		{State: OperationSucceeded, Value: json.RawMessage(`"` + strings.Repeat("a", MaxDocumentBytes) + `"`)},
		{State: OperationSucceeded, Value: json.RawMessage(`"` + strings.Repeat("<", MaxDocumentBytes/2) + `"`)},
	} {
		if err := result.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid outcome accepted: state=%s, error=%v", result.State, err)
		}
	}
}
