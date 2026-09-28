package protocol

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

// AccountTimestamp preserves nanoseconds and accepts canonical UTC only. It is
// local credential/flow metadata, never a claim of verified provider readiness.
type AccountTimestamp string

func (t *AccountTimestamp) UnmarshalJSON(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Year() < 1 || parsed.UTC().Format(time.RFC3339Nano) != value {
		return errors.New("invalid account timestamp")
	}
	*t = AccountTimestamp(value)
	return nil
}

type EmptyParams struct{}

type OpenAIFlowParams struct {
	FlowID string `json:"flow_id" pattern:"^[A-Z2-7]{26}:[A-Z2-7]{26}$"`
}

type OpenAILoginFlow struct {
	ID              string            `json:"id" pattern:"^[A-Z2-7]{26}:[A-Z2-7]{26}$"`
	State           string            `json:"state" enum:"authorizing,succeeded,setup_required,failed,cancelled,expired,interrupted"`
	VerificationURL *string           `json:"verification_url" pattern:"^https://auth[.]openai[.]com/codex/device$"`
	UserCode        *string           `json:"user_code"`
	ExpiresAt       *AccountTimestamp `json:"expires_at"`
	Failure         *string           `json:"failure"`
}

type OpenAIFlowsResult struct {
	Items []OpenAILoginFlow `json:"items"`
}

type OpenAIAccountStatus struct {
	AuthState  string            `json:"auth_state" enum:"signed_out,stored,sign_in_required,unavailable"`
	RouteState string            `json:"route_state" enum:"configured,missing,conflict,unavailable"`
	AccountID  *string           `json:"account_id"`
	Email      *string           `json:"email"`
	Plan       *string           `json:"plan"`
	ExpiresAt  *AccountTimestamp `json:"expires_at"`
	Failure    *string           `json:"failure"`
}

func accountSchema(schema *jsonschema.Schema, t reflect.Type) {
	bounds := map[string]int{}
	switch t {
	case reflect.TypeFor[OpenAILoginFlow]():
		bounds = map[string]int{"user_code": 64, "failure": 512}
		schema.If = &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"state": {Not: &jsonschema.Schema{Enum: []any{"authorizing"}}}}}
		schema.Then = &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"user_code": {Type: "null"}, "verification_url": {Type: "null"}}}
	case reflect.TypeFor[OpenAIAccountStatus]():
		bounds = map[string]int{"account_id": 512, "email": 320, "plan": 128, "failure": 512}
	case reflect.TypeFor[OpenAIFlowsResult]():
		schema.Properties["items"].Type, schema.Properties["items"].Types = "array", nil
		schema.Properties["items"].MaxItems = new(64)
	case reflect.TypeFor[InferenceFlow]():
		bounds = map[string]int{"user_code": 64, "team_id": 256, "project_id": 256, "failure": 512}
		for _, field := range []string{"teams", "projects"} {
			schema.Properties[field].Type, schema.Properties[field].Types = "array", nil
			schema.Properties[field].MaxItems = new(256)
		}
		schema.If = &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"state": {Not: &jsonschema.Schema{Enum: []any{"authorizing"}}}}}
		schema.Then = &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"user_code": {Type: "null"}, "verification_url": {Type: "null"}}}
	case reflect.TypeFor[InferenceTeam](), reflect.TypeFor[InferenceProject]():
		bounds = map[string]int{"id": 256}
		schema.Properties["name"].Pattern = `^[^\x00-\x1f\x7f]{0,512}$`
		if slug := schema.Properties["slug"]; slug != nil {
			slug.Pattern = `^[^\x00-\x1f\x7f]{0,256}$`
		}
	case reflect.TypeFor[InferenceAccountStatus]():
		bounds = map[string]int{"user_id": 256, "email": 1024, "team_id": 256, "team_name": 512, "project_id": 256, "project_name": 512, "failure": 512}
	case reflect.TypeFor[InferenceCleanup]():
		bounds = map[string]int{"team_id": 256, "key_id": 256, "failure": 512}
	case reflect.TypeFor[InferenceFlowsResult](), reflect.TypeFor[InferenceCleanupResult]():
		if t == reflect.TypeFor[InferenceCleanupResult]() {
			bounds = map[string]int{"failure": 512}
		}
		schema.Properties["items"].Type, schema.Properties["items"].Types = "array", nil
		schema.Properties["items"].MaxItems = new(64)
	case reflect.TypeFor[InferenceLogoutResult]():
		bounds = map[string]int{"local_failure": 512, "cleanup_failure": 512}
		schema.Properties["cleanup"].Type, schema.Properties["cleanup"].Types = "array", nil
		schema.Properties["cleanup"].MaxItems = new(64)
	}
	for field, limit := range bounds {
		// Split long bounds for Go's repeat limit. Keeping this in the pattern
		// also leaves standalone browser validators free of runtime imports.
		pattern := `^[^\x00-\x1f\x7f]{1,` + strconv.Itoa(min(limit, 1000)) + `}`
		if limit > 1000 {
			pattern += `[^\x00-\x1f\x7f]{0,` + strconv.Itoa(limit-1000) + `}`
		}
		schema.Properties[field].Pattern = pattern + `$`
	}
}
