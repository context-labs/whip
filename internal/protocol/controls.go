package protocol

import (
	"reflect"

	"github.com/context-labs/whip/internal/session"
	"github.com/google/jsonschema-go/jsonschema"
)

type RunConfiguration struct {
	System   string `json:"system"`
	MaxTurns int    `json:"max_turns" min:"0" max:"1000000"`
	Headless bool   `json:"headless"`
	CacheKey string `json:"cache_key"`
}

func (p RunConfiguration) Domain() session.RunConfiguration {
	return session.RunConfiguration{System: p.System, MaxTurns: p.MaxTurns, Headless: p.Headless, CacheKey: p.CacheKey}
}

type RunConfigureParams struct {
	ID               ID               `json:"id"`
	SessionID        ID               `json:"session_id"`
	ExpectedRevision Counter          `json:"expected_revision"`
	Configuration    RunConfiguration `json:"configuration"`
}

type WorkspaceSetParams struct {
	ID               ID      `json:"id"`
	SessionID        ID      `json:"session_id"`
	ExpectedRevision Counter `json:"expected_revision"`
	Path             string  `json:"path"`
}

type WorkspaceInspection struct {
	SessionID             ID      `json:"session_id"`
	WorkingDirectory      string  `json:"working_directory"`
	ConfigurationRevision Counter `json:"configuration_revision"`
}

type ControlEdit struct {
	ID        ID       `json:"id"`
	SessionID ID       `json:"session_id"`
	Revision  Counter  `json:"revision"`
	Deleted   bool     `json:"deleted"`
	Session   *Session `json:"session"`
}

func ControlEditFromDomain(value session.ControlEdit) (ControlEdit, error) {
	result := ControlEdit{ID: ID(value.ID), SessionID: ID(value.SessionID), Revision: Counter(value.Revision), Deleted: value.Deleted}
	if value.Session != nil {
		current, err := SessionFromDomain(*value.Session)
		if err != nil {
			return result, err
		}
		result.Session = &current
	}
	return result, nil
}

func controlsSchema(schema *jsonschema.Schema, t reflect.Type) {
	switch t {
	case reflect.TypeFor[ReloadEdit]():
		schema.AllOf = append(schema.AllOf,
			&jsonschema.Schema{If: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"state": {Const: new(any("applied"))}}}, Then: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"revision": {Type: "string"}}}, Else: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"revision": {Type: "null"}}}},
			&jsonschema.Schema{If: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"state": {Const: new(any("pending"))}}}, Then: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"settled_at": {Type: "null"}}}, Else: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"settled_at": {Type: "string"}}}})

	case reflect.TypeFor[RunConfiguration]():
		schema.Properties["system"].MaxLength = new(session.MaxInstructionBytes / 2)
		schema.Properties["cache_key"].MaxLength = new(4096)
	case reflect.TypeFor[WorkspaceSetParams]():
		schema.Properties["path"].MaxLength = new(4096)
	}
}
