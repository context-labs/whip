package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

type Operation struct {
	Name           string
	Params, Result reflect.Type
}

func Operations() []Operation {
	return []Operation{
		{"trees.create", reflect.TypeFor[CreateTreeParams](), reflect.TypeFor[CreateTreeResult]()},
		{"trees.get", reflect.TypeFor[TreeParams](), reflect.TypeFor[Tree]()},
		{"trees.update", reflect.TypeFor[UpdateTreeParams](), reflect.TypeFor[Tree]()},
		{"sessions.get", reflect.TypeFor[SessionParams](), reflect.TypeFor[Session]()},
		{"sessions.spawn", reflect.TypeFor[SpawnSessionParams](), reflect.TypeFor[Session]()},
		{"sessions.list", reflect.TypeFor[ListSessionsParams](), reflect.TypeFor[ListSessionsResult]()},
		{"sessions.configure", reflect.TypeFor[UpdateConfigurationParams](), reflect.TypeFor[Session]()},
		{"sessions.submit", reflect.TypeFor[SubmitParams](), reflect.TypeFor[Admission]()},
		{"sessions.history", reflect.TypeFor[HistoryParams](), reflect.TypeFor[HistoryResult]()},
		{"sessions.lifecycle", reflect.TypeFor[LifecycleParams](), reflect.TypeFor[Session]()},
		{"sessions.delete", reflect.TypeFor[SessionParams](), reflect.TypeFor[DeleteResult]()},
		{"turns.get", reflect.TypeFor[TurnParams](), reflect.TypeFor[Turn]()},
		{"turns.cancel", reflect.TypeFor[TurnParams](), reflect.TypeFor[Turn]()},
		{"inputs.cancel", reflect.TypeFor[InputParams](), reflect.TypeFor[Input]()},
		{"receipts.get", reflect.TypeFor[RequestIdentity](), reflect.TypeFor[Admission]()},
		{"definitions.register", reflect.TypeFor[DefinitionDocument](), reflect.TypeFor[Definition]()},
		{"definitions.get", reflect.TypeFor[DefinitionRef](), reflect.TypeFor[Definition]()},
	}
}

func Types() map[string]reflect.Type {
	result := map[string]reflect.Type{}
	for _, op := range Operations() {
		result[op.Params.Name()] = op.Params
		result[op.Result.Name()] = op.Result
	}
	return result
}

func SchemaFor(t reflect.Type) (*jsonschema.Schema, error) {
	schema, err := jsonschema.ForType(t, &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[ID]():              {Type: "string", Pattern: `^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`},
		reflect.TypeFor[Counter]():         {Type: "string", Pattern: `^(0|[1-9][0-9]{0,18})$`, Format: "counter"},
		reflect.TypeFor[json.RawMessage](): {},
		reflect.TypeFor[Part](): {OneOf: []*jsonschema.Schema{
			{Type: "object", Required: []string{"type", "text"}, Properties: map[string]*jsonschema.Schema{
				"type": {Type: "string", Enum: []any{"text"}}, "text": {Type: "string", Pattern: `^[\s\S]+$`},
			}, AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}},
			{Type: "object", Required: []string{"type", "reference_id"}, Properties: map[string]*jsonschema.Schema{
				"type": {Type: "string", Enum: []any{"content"}}, "reference_id": {Type: "string", Pattern: `^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`},
			}, AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}},
		}},
	}})
	if err != nil {
		return nil, err
	}
	applyTags(schema, t)
	schema.Schema = "http://json-schema.org/draft-07/schema#"
	schema.ID = "https://whip.dev/protocol/v4/" + t.Name()
	schema.Title = t.Name()
	return schema, nil
}

func applyTags(schema *jsonschema.Schema, t reflect.Type) {
	if schema == nil {
		return
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		for field := range t.Fields() {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			child := schema.Properties[name]
			if child == nil {
				continue
			}
			if value := field.Tag.Get("enum"); value != "" {
				for item := range strings.SplitSeq(value, ",") {
					child.Enum = append(child.Enum, item)
				}
			}
			if value := field.Tag.Get("pattern"); value != "" {
				child.Pattern = value
			}
			if value := field.Tag.Get("min"); value != "" {
				number, _ := strconv.ParseFloat(value, 64)
				child.Minimum = &number
			}
			if value := field.Tag.Get("max"); value != "" {
				number, _ := strconv.ParseFloat(value, 64)
				child.Maximum = &number
			}
			applyTags(child, field.Type)
			if field.Type == reflect.TypeFor[[]Part]() {
				child.Type = "array"
				child.Types = nil
				child.MinItems = new(1)
				child.MaxItems = new(128)
			}
		}
	case reflect.Slice:
		if t != reflect.TypeFor[json.RawMessage]() && schema.Type == "array" {
			schema.Type = ""
			schema.Types = []string{"array", "null"}
		}
		applyTags(schema.Items, t.Elem())
	case reflect.Map:
		if schema.Type == "object" {
			schema.Type = ""
			schema.Types = []string{"object", "null"}
		}
		applyTags(schema.AdditionalProperties, t.Elem())
	}
}

// Validate checks schema shape and Go decoding together. Shape validation owns
// required/unknown fields; the typed decoder enforces exact decimal bounds.
func Validate(name string, raw []byte) error {
	t, ok := Types()[name]
	if !ok {
		return fmt.Errorf("unknown contract type %q", name)
	}
	if len(raw) > 8<<20 {
		return errors.New("contract document exceeds 8 MiB")
	}
	schema, err := SchemaFor(t)
	if err != nil {
		return err
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if err := resolved.Validate(value); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(reflect.New(t).Interface()); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
