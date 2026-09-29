package protocol

import (
	"reflect"

	"github.com/context-labs/whip/internal/session"
	"github.com/google/jsonschema-go/jsonschema"
)

// HostSkillRoots is a human configuration projection, never a grant or skill body.
type HostSkillRoots struct {
	Revision string          `json:"revision" pattern:"^[a-f0-9]{64}$"`
	Roots    []HostSkillRoot `json:"roots"`
	Defaults []ID            `json:"defaults"`
}
type HostSkillRoot struct {
	ID   ID     `json:"id"`
	Path string `json:"path" pattern:"^[^\\x00]+$"`
}
type PublishSkillRootParams struct {
	ExpectedRevision string `json:"expected_revision" pattern:"^[a-f0-9]{64}$"`
	ID               ID     `json:"id"`
	Path             string `json:"path" pattern:"^[^\\x00]+$"`
}
type SetDefaultSkillRootsParams struct {
	ExpectedRevision string `json:"expected_revision" pattern:"^[a-f0-9]{64}$"`
	Roots            []ID   `json:"roots"`
}

func skillRootSchema(schema *jsonschema.Schema, t reflect.Type) {
	switch t {
	case reflect.TypeFor[HostSkillRoots](), reflect.TypeFor[SetDefaultSkillRootsParams]():
		for _, name := range []string{"roots", "defaults"} {
			if value := schema.Properties[name]; value != nil {
				value.Type, value.Types = "array", nil
				value.MaxItems = new(session.MaxSkillRoots)
				value.UniqueItems = true
			}
		}
	case reflect.TypeFor[HostSkillRoot](), reflect.TypeFor[PublishSkillRootParams]():
		schema.Properties["path"].MaxLength = new(4096)
	}
}
