package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/google/jsonschema-go/jsonschema"
)

// SchemaFor represents encoding/json behavior, including binary data and
// decimal-string counters, in the same Draft-07 schema used by both clients.
func SchemaFor(t reflect.Type) (*jsonschema.Schema, error) {
	if t == reflect.TypeFor[MCPAttachParams]() {
		t = reflect.TypeFor[mcpAttachWire]()
	}
	if t == reflect.TypeFor[ProviderCatalogsResult]() {
		t = reflect.TypeFor[providerCatalogsWire]()
	}
	valueSchema, err := jsonschema.ForType(reflect.TypeFor[session.RuntimeValueWire](), &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{reflect.TypeFor[json.RawMessage](): {}, reflect.TypeFor[[]byte](): {Types: []string{"string", "null"}, ContentEncoding: "base64"}}})
	if err != nil {
		return nil, err
	}
	applyWireTags(valueSchema, reflect.TypeFor[session.RuntimeValueWire]())
	messageSchema, err := jsonschema.For[llm.Message](nil)
	if err != nil {
		return nil, err
	}
	partSchema, err := jsonschema.For[llm.ContentPart](nil)
	if err != nil {
		return nil, err
	}
	// Message.MarshalJSON emits an array when Parts is populated. Reflection
	// only sees the text field because the model-facing Parts field is json:"-".
	messageSchema.Properties["content"] = &jsonschema.Schema{AnyOf: []*jsonschema.Schema{
		{Type: "string"}, {Type: "array", Items: partSchema},
	}}
	schema, err := jsonschema.ForType(t, &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[llm.Message]():             messageSchema,
		reflect.TypeFor[json.RawMessage]():         {},
		reflect.TypeFor[session.RuntimeValue]():    valueSchema,
		reflect.TypeFor[CursorMap]():               {Type: "object", AdditionalProperties: &jsonschema.Schema{Type: "string", Pattern: `^-?(0|[1-9][0-9]*)$`, Format: "int64"}},
		reflect.TypeFor[session.DecimalCounters](): {Type: "array", Items: &jsonschema.Schema{Type: "string", Pattern: `^-?(0|[1-9][0-9]*)$`, Format: "int64"}},
		reflect.TypeFor[[]byte]():                  {Types: []string{"string", "null"}, ContentEncoding: "base64", Pattern: `^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$`},
	}})
	if err != nil {
		return nil, err
	}
	applyWireTags(schema, t)
	if t == reflect.TypeFor[SessionSummariesParams]() {
		// A Unicode pattern keeps standalone validators browser-native; Ajv's
		// min/maxLength keywords otherwise emit a CommonJS UCS-2 helper import.
		schema.Properties["root_ids"] = &jsonschema.Schema{
			Type: "array", MaxItems: new(session.MaxSessionSummaries), UniqueItems: true,
			Items: &jsonschema.Schema{Type: "string", Pattern: `^[\s\S]{1,256}$`},
		}
	}
	if t == reflect.TypeFor[SessionSummariesResult]() {
		items := schema.Properties["items"]
		items.Type, items.Types, items.MaxItems = "array", nil, new(session.MaxSessionSummaries)
	}
	if t == reflect.TypeFor[ProtocolEvent]() {
		schema.Properties["payload"] = &jsonschema.Schema{Type: "object"}
	}
	if t == reflect.TypeFor[ContentEventPayload]() {
		marker := any(true)
		schema.Properties["truncated"].Const = &marker
	}
	schema.Schema = "http://json-schema.org/draft-07/schema#"
	return schema, nil
}

var parameterSchemas sync.Map

// ValidateParams enforces required fields, unknown fields, JSON types, and Go
// scalar bounds before a handler admits work. Signed payloads remain opaque.
func ValidateParams(surface, name string, raw json.RawMessage) error {
	var operation Operation
	found := false
	for _, candidate := range Operations() {
		if candidate.Surface == surface && candidate.Name == name {
			operation = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unsupported %s operation %q", surface, name)
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	cached, ok := parameterSchemas.Load(operation.Params)
	if !ok {
		schema, err := SchemaFor(operation.Params)
		if err != nil {
			return err
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return err
		}
		cached, _ = parameterSchemas.LoadOrStore(operation.Params, resolved)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return errors.New("invalid JSON parameters")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid trailing JSON parameters")
	}
	value, err := schemaNumbers(value)
	if err != nil {
		return err
	}
	if err := cached.(*jsonschema.Resolved).Validate(value); err != nil {
		return fmt.Errorf("invalid %s parameters: %w", name, err)
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(reflect.New(operation.Params).Interface()); err != nil {
		return fmt.Errorf("invalid %s parameters: %w", name, err)
	}
	return nil
}

func ValidateRPC(name string, raw json.RawMessage) error { return ValidateParams("rpc", name, raw) }
func ValidateRuntime(name string, raw json.RawMessage) error {
	if err := ValidateParams("runtime", name, raw); err != nil {
		return err
	}
	if name == "cancel" {
		var params CancelParams
		if err := json.Unmarshal(raw, &params); err != nil || (params.TurnID == "") == (params.TargetCommandID == "") {
			return errors.New("cancel requires exactly one turn_id or target_command_id")
		}
	}
	if name == "history.rewind" || name == "session.fork" {
		var params struct {
			ExpectedRevision *int64 `json:"expected_revision,string"`
		}
		if err := json.Unmarshal(raw, &params); err != nil || params.ExpectedRevision == nil || *params.ExpectedRevision < 0 {
			return fmt.Errorf("%s requires a nonnegative expected_revision", name)
		}
	}
	if name == "history.clear" {
		var params ClearHistoryParams
		if err := json.Unmarshal(raw, &params); err != nil || (params.ExpectedRevision != nil && *params.ExpectedRevision < 0) {
			return errors.New("history.clear expected_revision must be nonnegative")
		}
	}
	return nil
}
func RuntimeLookup(name string) (Operation, bool) { return LookupRuntime(name) }

// jsonschema-go does not account for encoding/json's ,string option. Keep the
// schema tied to the actual tag rather than guessing from integer width.
func applyWireTags(schema *jsonschema.Schema, t reflect.Type) {
	if t == reflect.TypeFor[session.RuntimeValue]() {
		t = reflect.TypeFor[session.RuntimeValueWire]()
	}
	if schema == nil {
		return
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		if t == reflect.TypeFor[llm.Usage]() {
			// Protocol 4.0 peers omit this additive provenance field. Keep the
			// JSON tag non-optional so persisted false still means unreported.
			schema.Required = slices.DeleteFunc(schema.Required, func(name string) bool { return name == "reported" })
		}
		if t == reflect.TypeFor[InputAttachment]() {
			schema.Properties["kind"].Enum = []any{"image", "text"}
		}
		for _, field := range reflect.VisibleFields(t) {
			if !field.IsExported() {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			name := tag[0]
			if name == "-" {
				delete(schema.Properties, "-")
				schema.Required = slices.DeleteFunc(schema.Required, func(name string) bool { return name == "-" })
				continue
			}
			if name == "" {
				name = field.Name
			}
			child := schema.Properties[name]
			if child == nil {
				if field.Anonymous {
					applyWireTags(schema, field.Type)
				}
				continue
			}
			scalar := field.Type
			for scalar.Kind() == reflect.Pointer {
				scalar = scalar.Elem()
			}
			isInteger := scalar.Kind() >= reflect.Int && scalar.Kind() <= reflect.Uint64
			if slices.Contains(tag, "string") && isInteger {
				*child = jsonschema.Schema{Type: "string", Pattern: `^-?(0|[1-9][0-9]*)$`, Format: "int64"}
				if field.Type.Kind() == reflect.Pointer {
					child.Type = ""
					child.Types = []string{"string", "null"}
				}
				continue
			}
			applyWireTags(child, field.Type)
		}
		if t == reflect.TypeFor[session.CollectionEntry]() {
			// Keep typed properties in their own intersection branch. The TS
			// generator otherwise loses them beside required-only oneOf branches.
			properties := *schema
			var variants []*jsonschema.Schema
			for _, name := range []string{"agent", "inbox", "blackboard", "budget", "capability", "schedule", "permission", "body"} {
				variants = append(variants, &jsonschema.Schema{
					Type: "object", Properties: map[string]*jsonschema.Schema{name: schema.Properties[name].CloneSchemas()},
					Required: []string{name}, AdditionalProperties: schema.AdditionalProperties.CloneSchemas(),
				})
			}
			*schema = jsonschema.Schema{AllOf: []*jsonschema.Schema{&properties, {OneOf: variants}}}
		}
	case reflect.Map:
		applyWireTags(schema.AdditionalProperties, t.Elem())
	case reflect.Slice, reflect.Array:
		applyWireTags(schema.Items, t.Elem())
	}
}

// The schema library treats json.Number as a string. Preserve integer values
// exactly while converting only actual fractional/exponent values to floats.
func schemaNumbers(value any) (any, error) {
	switch value := value.(type) {
	case json.Number:
		if number, err := value.Int64(); err == nil {
			return number, nil
		}
		if number, err := strconv.ParseUint(string(value), 10, 64); err == nil {
			return number, nil
		}
		if !strings.ContainsAny(string(value), ".eE") {
			return nil, errors.New("JSON integer exceeds supported range; use a decimal string")
		}
		return value.Float64()
	case map[string]any:
		for key, item := range value {
			normalized, err := schemaNumbers(item)
			if err != nil {
				return nil, err
			}
			value[key] = normalized
		}
	case []any:
		for index, item := range value {
			normalized, err := schemaNumbers(item)
			if err != nil {
				return nil, err
			}
			value[index] = normalized
		}
	}
	return value, nil
}
