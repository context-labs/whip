package session

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOutputSchemaExactNumericConstraints(t *testing.T) {
	for _, tc := range []struct{ schema, valid, invalid string }{
		{`{"const":9007199254740993}`, `9007199254740993`, `9007199254740992`},
		{`{"enum":[9007199254740993]}`, `9007199254740993`, `9007199254740994`},
		{`{"minimum":0.1}`, `0.1`, `0.09999999999999999999999999999999999999`},
		{`{"multipleOf":0.1}`, `0.3`, `0.30000000000000000000000000000000000001`},
		{`{"type":"integer"}`, `9007199254740993`, `"9007199254740993"`},
		{`{"type":"string"}`, `"42"`, `42`},
	} {
		t.Run(tc.schema, func(t *testing.T) {
			if err := (ConfigPatch{Output: &OutputPolicy{Schema: json.RawMessage(tc.schema)}}).Validate(); err != nil {
				t.Fatal("configuration rejected exact schema", err)
			}
			got, err := ValidateOutput(json.RawMessage(tc.schema), []Part{{Type: "text", Text: tc.valid}})
			if err != nil || string(got) != tc.valid {
				t.Fatalf("valid exact number=%s %v", got, err)
			}
			got, err = ValidateOutput(json.RawMessage(tc.schema), []Part{{Type: "text", Text: tc.invalid}})
			if got != nil || !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid adjacent value accepted=%s %v", got, err)
			}
		})
	}
}

func TestSchemaReferencesMustBeIncluded(t *testing.T) {
	for _, schema := range []string{
		`{"$defs":{"value":{"type":"integer"}},"$ref":"#/$defs/value"}`,
		`{"$id":"https://example.test/root","$defs":{"value":{"$id":"value","type":"integer"}},"$ref":"value"}`,
		`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"integer"}`,
	} {
		if err := validateSchema(json.RawMessage(schema), false); err != nil {
			t.Fatal("included reference rejected", err)
		}
		if got, err := ValidateOutput(json.RawMessage(schema), []Part{{Type: "text", Text: "42"}}); err != nil || string(got) != "42" {
			t.Fatalf("included reference validation=%s %v", got, err)
		}
	}
	for _, ref := range []string{"https://example.test/schema", "http://localhost:1/schema", "file:///tmp/schema.json", "custom:outside", "relative.json"} {
		schema, err := json.Marshal(map[string]string{"$ref": ref})
		if err != nil {
			t.Fatal(err)
		}
		if err := validateSchema(schema, false); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "external schema loading is disabled") {
			t.Fatalf("external reference was not explicitly rejected: %s %v", ref, err)
		}
	}
}

func TestExactJSONNumbersHaveBoundedArithmetic(t *testing.T) {
	// Reject hostile exponents before big.Rat can expand a short literal into
	// an enormous integer. Schema admission and output use the same guard.
	for _, literal := range []string{"1e999999999", "1e-999999999", "1e4097", "1E-4097", strings.Repeat("9", maxJSONNumberBytes+1)} {
		schema := json.RawMessage(`{"const":` + literal + `}`)
		if err := (ConfigPatch{Output: &OutputPolicy{Schema: schema}}).Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatal("unbounded schema number admitted", err)
		}
		if got, err := ValidateOutput(json.RawMessage(`{}`), []Part{{Type: "text", Text: `{"nested":[` + literal + `]}`}}); got != nil || !errors.Is(err, ErrInvalid) {
			t.Fatalf("unbounded output number admitted: %s %v", got, err)
		}
	}
	for _, literal := range []string{"1e4096", "1e-4096", "1e+00004096", strings.Repeat("9", maxJSONNumberBytes)} {
		schema := json.RawMessage(`{"const":` + literal + `}`)
		got, err := ValidateOutput(schema, []Part{{Type: "text", Text: literal}})
		if err != nil || string(got) != literal {
			t.Fatalf("bounded number rejected: %v", err)
		}
	}
}

func TestSchemaCountBoundsDoNotWrapOrConstrainInstanceKeys(t *testing.T) {
	for _, draft := range []string{
		"http://json-schema.org/draft-04/schema#",
		"http://json-schema.org/draft-06/schema#",
		"http://json-schema.org/draft-07/schema#",
		"https://json-schema.org/draft/2019-09/schema",
		"https://json-schema.org/draft/2020-12/schema",
	} {
		schema := json.RawMessage(`{"$schema":"` + draft + `","minLength":18446744073709551616}`)
		if err := validateSchema(schema, false); !errors.Is(err, ErrInvalid) {
			t.Fatalf("overflowing count accepted under %s: %v", draft, err)
		}
		schema = json.RawMessage(`{"$schema":"` + draft + `","minLength":1}`)
		if got, err := ValidateOutput(schema, []Part{{Type: "text", Text: `"a"`}}); err != nil || string(got) != `"a"` {
			t.Fatalf("bounded count rejected under %s: %s %v", draft, got, err)
		}
	}
	for _, keyword := range []string{"minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties", "minContains", "maxContains"} {
		schema := json.RawMessage(`{"` + keyword + `":18446744073709551616}`)
		if err := validateSchema(schema, false); !errors.Is(err, ErrInvalid) {
			t.Fatalf("overflowing %s accepted: %v", keyword, err)
		}
	}
	for _, schema := range []string{
		`{"properties":{"x":{"minLength":18446744073709551616}}}`,
		`{"allOf":[{"minLength":18446744073709551616}]}`,
		`{"$ref":"#/custom","custom":{"minLength":18446744073709551616}}`,
		`{"$defs":{"node":{"$ref":"#/$defs/node","minLength":18446744073709551616}},"$ref":"#/$defs/node"}`,
	} {
		if err := validateSchema(json.RawMessage(schema), false); !errors.Is(err, ErrInvalid) {
			t.Fatalf("overflowing subschema count accepted: %s %v", schema, err)
		}
	}
	// Keywords inside ordinary instance data are not schema count bounds.
	const value = `{"minLength":18446744073709551616}`
	for _, schema := range []string{`{"const":` + value + `}`, `{"enum":[` + value + `]}`, `{"examples":[` + value + `]}`} {
		got, err := ValidateOutput(json.RawMessage(schema), []Part{{Type: "text", Text: value}})
		if err != nil || string(got) != value {
			t.Fatalf("instance key mistaken for schema keyword: %s %v", got, err)
		}
	}
	for _, schema := range []string{
		`{"maxLength":9223372036854775807}`,
		`{"$defs":{"node":{"type":"object","properties":{"child":{"$ref":"#/$defs/node"}}}},"$ref":"#/$defs/node"}`,
	} {
		if err := validateSchema(json.RawMessage(schema), false); err != nil {
			t.Fatalf("bounded or cyclic schema rejected: %v", err)
		}
	}
}
