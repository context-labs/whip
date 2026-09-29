package protocol

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

type ComputerConfiguration struct {
	Enabled          bool     `json:"enabled"`
	HelperExecutable string   `json:"helper_executable" maxLength:"4096"`
	Allow            []string `json:"allow"`
	Deny             []string `json:"deny"`
	DefaultDeny      bool     `json:"default_deny"`
}

type ComputerStatus struct {
	Revision          string                `json:"revision" pattern:"^[a-f0-9]{64}$"`
	Configuration     ComputerConfiguration `json:"configuration"`
	Generation        ID                    `json:"generation"`
	State             string                `json:"state" enum:"disabled,available,connected,retired,closed"`
	NativeConfigured  bool                  `json:"native_configured"`
	PlatformSupported bool                  `json:"platform_supported"`
}

type ConfigureComputerParams struct {
	Revision      string                `json:"revision" pattern:"^[a-f0-9]{64}$"`
	Configuration ComputerConfiguration `json:"configuration"`
}

type ComputerConnectionParams struct {
	Generation ID `json:"generation"`
}

func computerSchema(schema *jsonschema.Schema, t reflect.Type) {
	if t == reflect.TypeFor[ComputerConfiguration]() {
		for _, name := range []string{"allow", "deny"} {
			schema.Properties[name] = &jsonschema.Schema{Type: "array", MaxItems: new(64), UniqueItems: true, Items: &jsonschema.Schema{Type: "string", MinLength: new(1), MaxLength: new(256)}}
		}
	}
}
