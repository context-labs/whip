package protocol

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

type ProviderParams struct {
	Provider ID `json:"provider"`
}

type ProviderPreset struct {
	ID              ID       `json:"id"`
	Name            string   `json:"name"`
	Kind            string   `json:"kind" enum:"openai-chat,openai-responses,openai-codex"`
	BaseURL         string   `json:"base_url"`
	Methods         []string `json:"methods"`
	Environments    []string `json:"environments"`
	KeyURL          string   `json:"key_url"`
	SuggestedModels []string `json:"suggested_models"`
	SuggestedEffort string   `json:"suggested_effort"`
}
type ProviderPresetsResult struct {
	Items []ProviderPreset `json:"items"`
}

// Prices are exact nano-USD per million tokens. Null means unknown; "0" is free.
type ProviderModel struct {
	ID                      string      `json:"id"`
	Name                    string      `json:"name"`
	Prices                  ModelPrices `json:"prices"`
	ContextWindowTokens     *Counter    `json:"context_window_tokens"`
	AdvertisedContextTokens *Counter    `json:"advertised_context_tokens"`
	EffectiveContextPercent *Counter    `json:"effective_context_percent"`
	MaxOutputTokens         *Counter    `json:"max_output_tokens"`
	ReasoningEfforts        []string    `json:"reasoning_efforts"`
	InputModalities         []string    `json:"input_modalities"`
	OutputModalities        []string    `json:"output_modalities"`
	SupportsTools           *bool       `json:"supports_tools"`
	MetadataSource          string      `json:"metadata_source" enum:"advertised,bundled,advertised+bundled"`
}
type ProviderModelsResult struct {
	Items []ProviderModel `json:"items"`
}

type ProviderModelSettings struct {
	Prices              ModelPrices `json:"prices"`
	ContextWindowTokens *Counter    `json:"context_window_tokens"`
	MaxOutputTokens     Counter     `json:"max_output_tokens"`
	TimeoutMillis       Counter     `json:"timeout_millis"`
	MaxAttempts         int         `json:"max_attempts" min:"0" max:"5"`
}

type ProviderCredentialStatus struct {
	Source      string `json:"source" enum:"env,file,command,none,inference-net,openai-codex"`
	State       string `json:"state" enum:"unavailable,unchecked,not_required,missing,available,refresh_required"`
	Environment string `json:"environment"`
	File        string `json:"file"`
}
type ProviderRoute struct {
	ID         ID                               `json:"id"`
	Kind       string                           `json:"kind" enum:"openai-chat,openai-responses,openai-codex"`
	BaseURL    string                           `json:"base_url"`
	Credential ProviderCredentialStatus         `json:"credential"`
	Models     map[string]ProviderModelSettings `json:"models"`
}
type ProviderInventory struct {
	Revision        string          `json:"revision" pattern:"^[a-f0-9]{64}$"`
	Routes          []ProviderRoute `json:"routes"`
	Defaults        *ModelSelection `json:"defaults"`
	CompactionModel *ModelSelection `json:"compaction_model"`
}

// Credential commands and pasted keys occur only in explicit transient inputs.
type ProviderCredentialCommand struct {
	Executable  string   `json:"executable"`
	Arguments   []string `json:"arguments"`
	Environment []string `json:"environment"`
}
type ProviderCredentialInput struct {
	Source      string                     `json:"source" enum:"env,file,command,none,inference-net"`
	Environment string                     `json:"environment"`
	File        string                     `json:"file"`
	Command     *ProviderCredentialCommand `json:"command"`
}
type ProviderDeclaration struct {
	Kind       string                           `json:"kind" enum:"openai-chat,openai-responses,openai-codex"`
	BaseURL    string                           `json:"base_url"`
	Credential *ProviderCredentialInput         `json:"credential"`
	Models     map[string]ProviderModelSettings `json:"models"`
}
type ProviderKeyPublication struct {
	ID  ID     `json:"id"`
	Key string `json:"key"`
}
type ChangeProviderParams struct {
	Revision       string                  `json:"revision" pattern:"^[a-f0-9]{64}$"`
	Provider       ID                      `json:"provider"`
	Declaration    ProviderDeclaration     `json:"declaration"`
	KeepCredential bool                    `json:"keep_credential"`
	Key            *ProviderKeyPublication `json:"key"`
}
type ProviderDefaults struct {
	Selection *ModelSelection        `json:"selection"`
	Settings  *ProviderModelSettings `json:"settings"`
}
type ProviderDefaultsParams struct {
	Revision string           `json:"revision" pattern:"^[a-f0-9]{64}$"`
	Defaults ProviderDefaults `json:"defaults"`
}
type RemoveProviderParams struct {
	Revision    string            `json:"revision" pattern:"^[a-f0-9]{64}$"`
	Provider    ID                `json:"provider"`
	Replacement *ProviderDefaults `json:"replacement"`
}
type ProviderCatalog struct {
	Provider   ID                `json:"provider"`
	State      string            `json:"state" enum:"missing,scope_changed,cached"`
	ScopeState string            `json:"scope_state" enum:"unverified,current"`
	Discovery  string            `json:"discovery" enum:"not_checked,failed,catalog_response,account_catalog,authenticated_catalog,public_catalog"`
	FetchedAt  *AccountTimestamp `json:"fetched_at"`
	Stale      bool              `json:"stale"`
	Failure    *string           `json:"failure"`
	Models     []ProviderModel   `json:"models"`
}
type ProviderReadinessParams struct {
	Selection ModelSelection `json:"selection"`
}
type ProviderReadiness struct {
	Configured      bool   `json:"configured"`
	CredentialState string `json:"credential_state" enum:"unavailable,unchecked,not_required,missing,available,refresh_required"`
	CatalogState    string `json:"catalog_state" enum:"missing,scope_changed,cached"`
	ModelState      string `json:"model_state" enum:"unknown,configured,catalogued"`
	InferenceState  string `json:"inference_state" enum:"not_tested"`
}

func providerSchema(schema *jsonschema.Schema, t reflect.Type) {
	arrays, texts := map[string]int{}, map[string]int{}
	switch t {
	case reflect.TypeFor[ProviderPresetsResult]():
		arrays["items"] = 11
	case reflect.TypeFor[ProviderPreset]():
		arrays = map[string]int{"methods": 2, "environments": 2, "suggested_models": 16}
		texts = map[string]int{"name": 128, "base_url": 4000, "key_url": 4000, "suggested_effort": 64}
	case reflect.TypeFor[ProviderModelsResult]():
		arrays["items"] = 1024
	case reflect.TypeFor[ProviderModel]():
		texts = map[string]int{"id": 256, "name": 512}
		for _, name := range []string{"reasoning_efforts", "input_modalities", "output_modalities"} {
			schema.Properties[name].MaxItems = new(32)
			schema.Properties[name].Items.MaxLength = new(64)
		}
	case reflect.TypeFor[ProviderRoute](), reflect.TypeFor[ProviderDeclaration]():
		texts["base_url"] = 4000
		schema.Properties["models"].MaxProperties = new(1024)
		schema.Properties["models"].PropertyNames = &jsonschema.Schema{Type: "string", MinLength: new(1), MaxLength: new(256)}
	case reflect.TypeFor[ProviderInventory]():
		arrays["routes"] = 128
	case reflect.TypeFor[ProviderCredentialStatus](), reflect.TypeFor[ProviderCredentialInput]():
		texts = map[string]int{"environment": 256, "file": 4096}
	case reflect.TypeFor[ProviderCredentialCommand]():
		texts["executable"] = 4096
		arrays = map[string]int{"arguments": 64, "environment": 64}
		schema.Properties["arguments"].Items.MaxLength = new(4096)
		schema.Properties["environment"].Items.MaxLength = new(256)
	case reflect.TypeFor[ProviderKeyPublication]():
		schema.Properties["key"].Pattern = `^[!-~]+$`
		texts["key"] = 64 << 10
	case reflect.TypeFor[ProviderCatalog]():
		arrays["models"] = 1024
		texts["failure"] = 512
	}
	for name, limit := range arrays {
		schema.Properties[name].Type, schema.Properties[name].Types = "array", nil
		schema.Properties[name].MaxItems = new(limit)
	}
	for name, limit := range texts {
		schema.Properties[name].MaxLength = new(limit)
	}
}
