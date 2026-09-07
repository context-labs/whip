package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

func TestRegistrySchemasResolveAndHaveUniqueNames(t *testing.T) {
	operations := map[string]bool{}
	types := map[string]reflect.Type{}
	for _, operation := range Operations() {
		key := operation.Surface + ":" + operation.Name
		if operations[key] {
			t.Fatalf("duplicate operation %s", key)
		}
		operations[key] = true
		if operation.Execution == "" || operation.Permission == "" {
			t.Fatalf("missing semantics: %s", key)
		}
		for _, typ := range []reflect.Type{operation.Params, operation.Result} {
			if existing := types[typ.Name()]; existing != nil && existing != typ {
				t.Fatalf("ambiguous type name %s", typ.Name())
			}
			types[typ.Name()] = typ
			schema, err := SchemaFor(typ)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := schema.Resolve(nil); err != nil {
				t.Fatalf("%s: %v", key, err)
			}
		}
	}
}

func TestValidateParametersBeforeAdmission(t *testing.T) {
	for _, test := range []struct {
		name, raw string
		valid     bool
	}{
		{name: "events.subscribe", raw: `{"root_id":"root","subscription_id":"view","cursor":"9007199254740993"}`, valid: true},
		{name: "events.subscribe", raw: `{"root_id":"root","subscription_id":"view","cursor":1}`},
		{name: "events.subscribe", raw: `{"root_id":"root","subscription_id":"view","cursor":"9223372036854775808"}`},
		{name: "events.subscribe", raw: `{"root_id":"root","cursor":"0"}`},
		{name: "events.subscribe", raw: `{"root_id":"root","subscription_id":"view","cursor":"0","extra":true}`},
		{name: "events.subscribe", raw: `{} {}`},
		{name: "daemon.ping", raw: `{}`, valid: true},
		{name: "events.replay", raw: `{"root_id":"root","cursor":"0","limit":100}`, valid: true},
		{name: "daemon.ping", valid: true},
		{name: "permission.decide", raw: `{"decision":{"command_id":"decision","root_id":"root","permission_id":"permission","allow":true}}`, valid: true},
		{name: "permission.decide", raw: `{"decision":{"command_id":"decision","root_id":"root","permission_id":"permission","allow":"true"}}`},
		{name: "permission.decide", raw: `{"decision":{"command_id":"decision","root_id":"root","permission_id":"permission","allow":true},"signature":"old-signature"}`},
		{name: "identity.enroll", raw: `{}`},
		{name: "identity.status", raw: `{}`},
		{name: "permission.mode", raw: `{}`},
		{name: "unknown", raw: `{}`},
	} {
		t.Run(test.name+test.raw, func(t *testing.T) {
			err := ValidateRPC(test.name, json.RawMessage(test.raw))
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}

func TestAttachmentKindUsesTheGeneratedContract(t *testing.T) {
	for _, kind := range []string{"image", "text", "file"} {
		body, err := json.Marshal(SubmitPayload{Text: "", Attachments: []InputAttachment{{Kind: kind, Content: ContentHandle{ReferenceID: "reference", Digest: "digest", Size: 1, MediaType: "text/plain", Source: "upload"}}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateRuntime("submit", body); (err == nil) != (kind != "file") {
			t.Fatalf("attachment kind %s: %v", kind, err)
		}
	}
}

func TestClearHistoryOptionalRevision(t *testing.T) {
	for _, test := range []struct {
		raw   string
		valid bool
	}{
		{`{}`, true},
		{`{"expected_revision":"0"}`, true},
		{`{"expected_revision":"9007199254740993"}`, true},
		{`{"expected_revision":"-1"}`, false},
		{`{"expected_revision":1}`, false},
	} {
		if err := ValidateRuntime("history.clear", json.RawMessage(test.raw)); (err == nil) != test.valid {
			t.Fatalf("%s: valid=%v, err=%v", test.raw, test.valid, err)
		}
	}
}

func TestMultimodalMessageSchemaMatchesMarshalJSON(t *testing.T) {
	schema, err := SchemaFor(reflect.TypeFor[llm.Message]())
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []llm.Message{
		{Role: "user", Content: "plain text"},
		{Role: "user", Content: "with attachment", Parts: []llm.ContentPart{
			{Type: "text", Text: "attachment body"}, llm.ImagePart("png", []byte("bounded schema fixture")),
		}},
	} {
		data, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(value); err != nil {
			t.Fatalf("serialized message does not match schema: %s: %v", data, err)
		}
	}
}
