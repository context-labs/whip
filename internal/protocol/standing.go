package protocol

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/context-labs/whip/internal/session"
)

// HostStandingInstructions exposes raw text only for the active publication.
// Its opaque revision binds bytes and file identity; no filesystem path escapes.
type HostStandingInstructions struct {
	Published bool    `json:"published"`
	Revision  *string `json:"revision" pattern:"^[a-f0-9]{64}$"`
	Text      *string `json:"text" pattern:"^[^\\x00]*$"`
}

type WriteHostStandingInstructionsParams struct {
	ExpectedRevision string `json:"expected_revision" pattern:"^[a-f0-9]{64}$"`
	Text             string `json:"text" pattern:"^[^\\x00]*$"`
}

func standingSchema(schema *jsonschema.Schema, t reflect.Type) {
	switch t {
	case reflect.TypeFor[WriteHostStandingInstructionsParams]():
		schema.Properties["text"].MaxLength = new(session.MaxInstructionSourceBytes)
	case reflect.TypeFor[HostStandingInstructions]():
		schema.Properties["text"].MaxLength = new(session.MaxInstructionSourceBytes)
		schema.OneOf = []*jsonschema.Schema{
			{Properties: map[string]*jsonschema.Schema{"published": {Const: new(any(false))}, "revision": {Type: "null"}, "text": {Type: "null"}}},
			{Properties: map[string]*jsonschema.Schema{"published": {Const: new(any(true))}, "revision": {Type: "string"}, "text": {Type: "string"}}},
		}
	}
}
