package protocol

import (
	"reflect"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/context-labs/whip/internal/session"
)

type InputSteeringRef struct {
	ID       ID   `json:"id"`
	TurnID   ID   `json:"turn_id"`
	Consumed bool `json:"consumed"`
}

type SteerInputParams struct {
	EditID    ID `json:"edit_id"`
	SessionID ID `json:"session_id"`
	InputID   ID `json:"input_id"`
	TurnID    ID `json:"turn_id"`
}

type InputSteeringParams struct {
	EditID    ID `json:"edit_id"`
	SessionID ID `json:"session_id"`
}

type InputSteeringResult struct {
	ID        ID     `json:"id"`
	SessionID ID     `json:"session_id"`
	InputID   ID     `json:"input_id"`
	TurnID    ID     `json:"turn_id"`
	CreatedAt string `json:"created_at"`
	Deleted   bool   `json:"deleted"`
	Input     *Input `json:"input"`
}

func steeringRef(value *session.InputSteeringRef) *InputSteeringRef {
	if value == nil {
		return nil
	}
	return &InputSteeringRef{ID: ID(value.ID), TurnID: ID(value.TurnID), Consumed: value.Consumed}
}

func InputSteeringFromDomain(value session.InputSteeringResult) InputSteeringResult {
	s := value.Steering
	result := InputSteeringResult{ID: ID(s.ID), SessionID: ID(s.SessionID), InputID: ID(s.InputID), TurnID: ID(s.TurnID), CreatedAt: s.CreatedAt.Format(time.RFC3339Nano), Deleted: value.Deleted}
	if value.Input != nil {
		result.Input = new(InputFromDomain(*value.Input))
	}
	return result
}

func steeringSchema(schema *jsonschema.Schema, t reflect.Type) {
	if t == reflect.TypeFor[SubmitParams]() {
		schema.AllOf = append(schema.AllOf, &jsonschema.Schema{
			If:   &jsonschema.Schema{Required: []string{"target_turn_id"}, Properties: map[string]*jsonschema.Schema{"target_turn_id": {Type: "string"}}},
			Then: &jsonschema.Schema{Required: []string{"delivery"}, Properties: map[string]*jsonschema.Schema{"delivery": {Const: new(any("steer"))}}},
		})
	}
}
