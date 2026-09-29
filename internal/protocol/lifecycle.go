package protocol

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// HostStatus describes one live native process. It is observation evidence, not
// a durable execution receipt, and its process epoch changes after every restart.
type HostStatus struct {
	RuntimeID    ID     `json:"runtime_id"`
	ProcessEpoch ID     `json:"process_epoch"`
	PID          int    `json:"pid" min:"1" max:"2147483647"`
	Build        string `json:"build" pattern:"^[\\s\\S]{0,256}$"`
	StartedAt    string `json:"started_at" pattern:"^[\\s\\S]{1,64}$"`
	WebEndpoint  string `json:"web_endpoint"`
	WebState     string `json:"web_state,omitempty" enum:"starting,running,failed"`
	WebError     string `json:"web_error,omitempty"`
}

// StopHostParams prevents a delayed stop from shutting down a replacement owner.
type StopHostParams struct {
	RuntimeID    ID `json:"runtime_id"`
	ProcessEpoch ID `json:"process_epoch"`
}

type HostStopAccepted struct {
	RuntimeID    ID `json:"runtime_id"`
	ProcessEpoch ID `json:"process_epoch"`
}

func lifecycleSchema(schema *jsonschema.Schema, t reflect.Type) {
	if t == reflect.TypeFor[HostStatus]() {
		schema.Properties["web_endpoint"].MaxLength = new(4096)
		schema.Properties["web_error"].MaxLength = new(4096)
	}
}
