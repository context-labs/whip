package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Bound exact rational arithmetic before the validator materializes numbers.
// These are resource limits, not rounding rules: larger values are rejected.
const (
	maxJSONNumberBytes = 4096
	maxJSONExponent    = 4096
)

func validateSchema(raw json.RawMessage, optional bool) error {
	_, err := compileSchema(raw, optional)
	return err
}

//nolint:nilnil // An absent optional schema intentionally has no compiled contract.
func compileSchema(raw json.RawMessage, optional bool) (*jsonschema.Schema, error) {
	trimmed := bytes.TrimSpace(raw)
	if optional && (len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))) {
		return nil, nil
	}
	document, err := decodeExactJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: schema: %w", ErrInvalid, err)
	}
	if _, ok := document.(map[string]any); !ok {
		return nil, fmt.Errorf("%w: schema must be a JSON object", ErrInvalid)
	}
	compiler := jsonschema.NewCompiler()
	// An explicit $schema may select supported drafts 4, 6, 7, or 2019-09.
	// Schemas without a declaration use the stable 2020-12 default.
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(rejectSchemaLoader{})
	counts, err := schemaCountVocabulary()
	if err != nil {
		return nil, err
	}
	compiler.RegisterVocabulary(counts)
	compiler.AssertVocabs()
	const location = "https://whip.invalid/schema"
	if err := compiler.AddResource(location, document); err != nil {
		return nil, fmt.Errorf("%w: schema: %w", ErrInvalid, err)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("%w: schema: %w", ErrInvalid, err)
	}
	return compiled, nil
}

func schemaCountVocabulary() (*jsonschema.Vocabulary, error) {
	// v6.0.3 compiles these keywords through big.Int.Int64. Reject overflow
	// during admission using the library's schema-position traversal, including
	// local reference targets, without constraining instance data in const/enum.
	properties := map[string]any{}
	for _, keyword := range []string{"minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties", "minContains", "maxContains"} {
		properties[keyword] = map[string]any{"maximum": json.Number("9223372036854775807")}
	}
	const location = "https://whip.invalid/schema-counts"
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(rejectSchemaLoader{})
	if err := compiler.AddResource(location, map[string]any{"properties": properties}); err != nil {
		return nil, err
	}
	meta, err := compiler.Compile(location)
	if err != nil {
		return nil, err
	}
	return &jsonschema.Vocabulary{
		URL: location, Schema: meta,
		Compile: func(*jsonschema.CompilerContext, map[string]any) (jsonschema.SchemaExt, error) {
			return nil, nil //nolint:nilnil // Admission-only bound adds no runtime validation extension.
		},
	}, nil
}

type rejectSchemaLoader struct{}

func (rejectSchemaLoader) Load(string) (any, error) {
	return nil, fmt.Errorf("%w: external schema loading is disabled; include referenced schemas in the document", ErrInvalid)
}

func decodeExactJSON(raw []byte) (any, error) {
	if len(raw) > MaxDocumentBytes || !utf8.Valid(raw) {
		return nil, fmt.Errorf("%w: JSON must be bounded UTF-8", ErrInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: expected one JSON value: %w", ErrInvalid, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("%w: JSON contains trailing data", ErrInvalid)
	}
	if err := boundJSONNumbers(value); err != nil {
		return nil, err
	}
	return value, nil
}

func boundJSONNumbers(value any) error {
	switch value := value.(type) {
	case json.Number:
		literal := string(value)
		if len(literal) > maxJSONNumberBytes {
			return fmt.Errorf("%w: JSON number exceeds the %d-byte literal limit", ErrInvalid, maxJSONNumberBytes)
		}
		if pos := strings.IndexAny(literal, "eE"); pos >= 0 {
			exponent := strings.TrimLeft(literal[pos+1:], "+-")
			magnitude := 0
			for _, digit := range exponent {
				magnitude = magnitude*10 + int(digit-'0')
				if magnitude > maxJSONExponent {
					return fmt.Errorf("%w: JSON number exceeds the absolute exponent limit of %d", ErrInvalid, maxJSONExponent)
				}
			}
		}
	case []any:
		for _, item := range value {
			if err := boundJSONNumbers(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range value {
			if err := boundJSONNumbers(item); err != nil {
				return err
			}
		}
	}
	return nil
}
