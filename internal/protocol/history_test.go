package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestImportedHistoryProjectsProvenanceWithoutLocalExecution(t *testing.T) {
	created := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	message := MessageFromDomain(session.Message{
		ID: "imported", SessionID: "destination", GroupID: "imported_group", OpeningInput: true,
		Source:   &session.MessageSource{SessionID: "source", MessageID: "original", Sequence: 9007199254740993},
		Sequence: 1, Role: session.User, Parts: []session.Part{{Type: "text", Text: "Original input"}}, CreatedAt: created,
	})
	if message.TurnID != nil || message.InputID != nil || message.Mail != nil || !message.OpeningInput {
		t.Fatalf("import fabricated execution provenance: %+v", message)
	}
	metadata := HistoryMetadataFromDomain(session.HistoryMetadata{
		ID: "imported", SessionID: "destination", GroupID: "imported_group", OpeningInput: true,
		Source:   &session.MessageSource{SessionID: "source", MessageID: "original", Sequence: 9007199254740993},
		Sequence: 1, Role: session.User, PartsBytes: 43,
		RetiredBy: new(session.HistoryEditID("edit")), RetiredRevision: new(session.Revision(9007199254740994)),
	})
	read := ReadHistoryResult{Message: metadata, DataBase64: "e30="}
	for name, value := range map[string]any{"Message": message, "ReadHistoryResult": read} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate(name, raw); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if name == "ReadHistoryResult" {
			fields = fields["message"].(map[string]any)
			if fields["retired_revision"] != "9007199254740994" || fields["retired_by"] != "edit" {
				t.Fatalf("retirement evidence lost: %s", raw)
			}
		}
		if fields["turn_id"] != nil || fields["input_id"] != nil || fields["source"].(map[string]any)["sequence"] != "9007199254740993" {
			t.Fatalf("provenance did not survive wire encoding: %s", raw)
		}
	}
}

func TestHistorySchemasRejectLossyRevisionAndMissingProvenance(t *testing.T) {
	for _, raw := range []string{
		`{"edit_id":"edit","session_id":"owner","expected_revision":9007199254740993,"observed_through":"2","keep_through":"0"}`,
		`{"edit_id":"edit","session_id":"owner","expected_revision":"1","observed_through":"9223372036854775808","keep_through":"0"}`,
		`{"edit_id":"edit","session_id":"owner","expected_revision":"1","observed_through":"2","keep_through":"-1"}`,
	} {
		if err := Validate("RewindParams", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid rewind: %s", raw)
		}
	}
	message := Message{ID: "message", SessionID: "owner", GroupID: "group", TurnID: nil, Sequence: 1, Role: "user", Parts: []Part{{Type: "text", Text: "input"}}, CreatedAt: "2026-09-28T12:00:00Z"}
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"group_id", "opening_input", "source", "turn_id"} {
		original := fields[name]
		delete(fields, name)
		raw, err = json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate("Message", raw); err == nil {
			t.Fatalf("accepted message without %s", name)
		}
		fields[name] = original
	}
}
