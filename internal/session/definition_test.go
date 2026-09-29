package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestDefinitionRevisionAndResolutionIsolation(t *testing.T) {
	base := Configuration{
		Model:        ModelSelection{Provider: "local", Name: "model"},
		Instructions: Instructions{ProjectFiles: []string{"AGENTS.md"}},
		Tools:        map[string]ToolDeclaration{"read": {InputSchema: json.RawMessage(`{"type":"object"}`)}},
		Hooks:        map[string]HookDeclaration{"before_tool": {Operations: []string{"files.read"}, TimeoutMillis: 500}},
		OutputSchema: json.RawMessage(`{"type":"string"}`),
	}
	document := Builtins()[0]
	registered, _, firstRef, err := CanonicalDefinition(document)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Resolve(base, registered, ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(base, registered, ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	first.Instructions.ProjectFiles[0] = "changed"
	first.Tools["read"].InputSchema[0] = '!'
	first.Hooks["before_tool"].Operations[0] = "write"
	first.OutputSchema[0] = '!'
	if second.Instructions.ProjectFiles[0] != "AGENTS.md" ||
		second.Hooks["before_tool"].Operations[0] != "files.read" ||
		!json.Valid(second.Tools["read"].InputSchema) || !json.Valid(second.OutputSchema) {
		t.Fatal("resolved sessions share mutable configuration")
	}
	if !json.Valid(base.Tools["read"].InputSchema) || base.Hooks["before_tool"].Operations[0] != "files.read" {
		t.Fatal("resolution mutated caller defaults")
	}
	registered.Defaults.Instructions.Text = "new revision"
	_, _, nextRef, err := CanonicalDefinition(registered)
	if err != nil {
		t.Fatal(err)
	}
	if firstRef == nextRef {
		t.Fatal("changed definition reused a revision")
	}
	if document.Defaults.Instructions.Text == registered.Defaults.Instructions.Text {
		t.Fatal("registered document aliases caller")
	}
	document.Defaults.Instructions.Text = "mutated builtin"
	if Builtins()[0].Defaults.Instructions.Text == document.Defaults.Instructions.Text {
		t.Fatal("builtin documents share mutable defaults")
	}
}

func TestPatchClearingSurvivesRegistration(t *testing.T) {
	doc := DefinitionDocument{ID: "clear", Name: "Clear", Defaults: ConfigPatch{
		Tools: map[string]ToolDeclaration{}, Output: &OutputPolicy{},
	}}
	canonical, raw, _, err := CanonicalDefinition(doc)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Defaults.Output == nil || canonical.Defaults.Tools == nil {
		t.Fatalf("explicit clearing became inheritance: %s", raw)
	}
	got, err := Resolve(Configuration{
		Model:        ModelSelection{Provider: "local", Name: "model"},
		Tools:        map[string]ToolDeclaration{"read": {InputSchema: json.RawMessage(`{}`)}},
		OutputSchema: json.RawMessage(`{"type":"string"}`),
	}, canonical, ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 0 || (len(got.OutputSchema) != 0 && !bytes.Equal(got.OutputSchema, []byte("null"))) {
		t.Fatalf("inherited contract not cleared: %+v", got)
	}
}

func TestDefinitionCanonicalSchemaOrdering(t *testing.T) {
	doc := DefinitionDocument{ID: "custom", Name: "Custom", Defaults: ConfigPatch{
		Tools: map[string]ToolDeclaration{"lookup": {InputSchema: json.RawMessage(`{"type":"object", "properties": {}}`)}},
	}}
	_, a, refA, err := CanonicalDefinition(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc.Defaults.Tools["lookup"] = ToolDeclaration{InputSchema: json.RawMessage(`{ "properties": {}, "type": "object" }`)}
	_, b, refB, err := CanonicalDefinition(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) || refA != refB {
		t.Fatal("formatting changed revision identity")
	}
}

func TestConfigurationValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		patch ConfigPatch
	}{
		{"missing provider", ConfigPatch{Model: &ModelSelection{Name: "model"}}},
		{"empty report mode", ConfigPatch{ReportMode: new(ReportMode(""))}},
		{"unknown report mode", ConfigPatch{ReportMode: new(ReportMode("automatic"))}},
		{"malformed schema", ConfigPatch{Tools: map[string]ToolDeclaration{"read": {InputSchema: json.RawMessage(`[]`)}}}},
		{"invalid schema type", ConfigPatch{Tools: map[string]ToolDeclaration{"read": {InputSchema: json.RawMessage(`{"type":17}`)}}}},
		{"remote schema", ConfigPatch{Tools: map[string]ToolDeclaration{"read": {InputSchema: json.RawMessage(`{"$ref":"https://example.test/schema"}`)}}}},
		{"unpinned child", ConfigPatch{Children: map[string]DefinitionRef{"child": {ID: "assistant"}}}},
		{"unknown hook", ConfigPatch{Hooks: map[string]HookDeclaration{"event": {TimeoutMillis: 10}}}},
		{"unbounded hook", ConfigPatch{Hooks: map[string]HookDeclaration{"before_tool": {TimeoutMillis: 60001}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.patch.Validate(), ErrInvalid) {
				t.Fatal("invalid declaration accepted")
			}
		})
	}
	valid := Configuration{Model: ModelSelection{Provider: "local", Name: "model"}}
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Configuration
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(valid.Model, decoded.Model) {
		t.Fatal("model selection changed")
	}
}

func TestReportModeResolution(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		base                 ReportMode
		definition, override *ReportMode
		want                 ReportMode
	}{
		{name: "default notice", want: ReportNotice},
		{name: "parent inheritance", base: ReportInline, want: ReportInline},
		{name: "definition overrides parent", base: ReportInline, definition: new(ReportMessage), want: ReportMessage},
		{name: "session overrides definition", base: ReportMessage, definition: new(ReportInline), override: new(ReportNotice), want: ReportNotice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := Configuration{Model: ModelSelection{Provider: "local", Name: "model"}, ReportMode: tc.base}
			document := DefinitionDocument{ID: "reporter", Name: "Reporter", Defaults: ConfigPatch{ReportMode: tc.definition}}
			resolved, err := Resolve(base, document, ConfigPatch{ReportMode: tc.override})
			if err != nil || resolved.ReportMode != tc.want {
				t.Fatalf("report policy = %q, %v; want %q", resolved.ReportMode, err, tc.want)
			}
			if tc.override != nil {
				*tc.override = ReportMessage
			}
			if resolved.ReportMode != tc.want || base.ReportMode != tc.base {
				t.Fatal("resolved policy aliases or changes its source")
			}
		})
	}
}
