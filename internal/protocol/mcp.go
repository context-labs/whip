package protocol

import (
	"encoding/json"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

type (
	MCPSelection struct {
		All     bool     `json:"all"`
		Servers []string `json:"servers"`
	}
	MCPImportSource struct {
		Enabled *bool    `json:"enabled"`
		Only    []string `json:"only"`
		Exclude []string `json:"exclude"`
	}
	MCPImportPolicy struct {
		Claude   *MCPImportSource `json:"claude"`
		Codex    *MCPImportSource `json:"codex"`
		Project  *MCPImportSource `json:"project"`
		Opencode *MCPImportSource `json:"opencode"`
		Offered  bool             `json:"offered"`
	}
	// MCPServerInput is an explicit host declaration. Trust and source provenance
	// are assigned by the host and cannot be supplied over this boundary.
	MCPServerInput struct {
		Command               []string          `json:"command"`
		Env                   map[string]string `json:"env"`
		Cwd                   string            `json:"cwd"`
		URL                   string            `json:"url"`
		Headers               map[string]string `json:"headers"`
		Enabled               *bool             `json:"enabled"`
		Note                  string            `json:"note"`
		StartupTimeoutSeconds int               `json:"startup_timeout_seconds" min:"0" max:"300"`
		ToolTimeoutSeconds    int               `json:"tool_timeout_seconds" min:"0" max:"300"`
	}
)

type (
	MCPDeclaration struct {
		Name                  string `json:"name"`
		Transport             string `json:"transport" enum:"stdio,http"`
		Enabled               bool   `json:"enabled"`
		StartupTimeoutSeconds int    `json:"startup_timeout_seconds" min:"1" max:"300"`
		ToolTimeoutSeconds    int    `json:"tool_timeout_seconds" min:"1" max:"300"`
		BrandHint             string `json:"brand_hint"`
		BrandKey              string `json:"brand_key"`
	}
	MCPConfiguration struct {
		Revision   string           `json:"revision" pattern:"^[a-f0-9]{64}$"`
		Servers    []MCPDeclaration `json:"servers"`
		Imports    MCPImportPolicy  `json:"imports"`
		BrandIcons bool             `json:"brand_icons"`
	}
	ConfigureMCPParams struct {
		Revision   string           `json:"revision" pattern:"^[a-f0-9]{64}$"`
		Name       string           `json:"name"`
		Server     *MCPServerInput  `json:"server"`
		Remove     bool             `json:"remove"`
		Imports    *MCPImportPolicy `json:"imports"`
		BrandIcons *bool            `json:"brand_icons"`
	}
	MCPImportCandidatesParams struct {
		SessionID *ID `json:"session_id"`
	}
	MCPImportCandidate struct {
		Fingerprint string `json:"fingerprint" pattern:"^[a-f0-9]{64}$"`
		Name        string `json:"name"`
		Source      string `json:"source" enum:"codex,claude,project,opencode"`
		State       string `json:"state" enum:"importable,native,disabled,excluded,unsupported"`
		Gated       bool   `json:"gated"`
		Note        string `json:"note"`
		BrandHint   string `json:"brand_hint"`
		BrandKey    string `json:"brand_key"`
	}
	MCPImportCandidatesResult struct {
		Revision     string               `json:"revision" pattern:"^[a-f0-9]{64}$"`
		Candidates   []MCPImportCandidate `json:"candidates"`
		SourceErrors map[string]string    `json:"source_errors"`
	}
	MCPImportParams struct {
		SessionID    *ID               `json:"session_id"`
		Revision     string            `json:"revision" pattern:"^[a-f0-9]{64}$"`
		Fingerprints map[string]string `json:"fingerprints"`
	}
	MCPImportResult struct {
		Configuration MCPConfiguration  `json:"configuration"`
		Added         []string          `json:"added"`
		Skipped       map[string]string `json:"skipped"`
	}
	MCPServerParams struct {
		SessionID ID     `json:"session_id"`
		Server    string `json:"server"`
	}
	MCPAttachParams struct {
		SessionID ID                        `json:"session_id"`
		Servers   map[string]MCPServerInput `json:"servers"`
	}
	MCPServerStatus struct {
		Name    string  `json:"name"`
		State   string  `json:"state" enum:"not_started,disabled,connecting,ready,failed,blocked,unreadable"`
		Note    string  `json:"note"`
		Failure *string `json:"failure"`
		Tools   int     `json:"tools" min:"0" max:"2048"`
		Source  string  `json:"source"`
	}
	MCPStatusResult struct {
		Items []MCPServerStatus `json:"items"`
	}
	// Refresh is additive. Changed declarations retain their live configuration
	// until the user explicitly reloads; added does not imply ready.
	MCPRefreshResult struct {
		Added        []string          `json:"added"`
		Existing     []string          `json:"existing"`
		Changed      []string          `json:"changed"`
		Servers      []MCPServerStatus `json:"servers"`
		Blocked      []MCPServerStatus `json:"blocked"`
		SourceErrors []MCPServerStatus `json:"source_errors"`
	}
	MCPTool struct {
		Name        string          `json:"name"`
		Title       string          `json:"title"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
		Server      string          `json:"server"`
		Generation  string          `json:"generation"`
		Capability  string          `json:"capability" enum:"mcp.call,mcp.call.trusted"`
		Resource    string          `json:"resource"`
	}
	MCPToolsResult struct {
		Items []MCPTool `json:"items"`
	}
	MCPInstructionsResult struct {
		Server       string             `json:"server"`
		Generation   string             `json:"generation"`
		Resource     string             `json:"resource"`
		Text         string             `json:"text"`
		ContentParts []ContentReference `json:"content_parts"`
		Bytes        Counter            `json:"bytes"`
	}
	MCPBrandIconsParams struct {
		Keys []string `json:"keys"`
	}
	MCPBrandIconsResult struct {
		Icons map[string]string `json:"icons"`
	}
)

func mcpSchema(schema *jsonschema.Schema, t reflect.Type) {
	arrays, texts := map[string]int{}, map[string]int{}
	maps := map[string]int{}
	switch t {
	case reflect.TypeFor[MCPSelection]():
		arrays["servers"] = 64
		schema.Properties["servers"].UniqueItems = true
		schema.Properties["servers"].Items.MinLength = new(1)
		schema.Properties["servers"].Items.MaxLength = new(256)
		schema.AllOf = append(schema.AllOf, &jsonschema.Schema{If: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"all": {Const: new(any(true))}}}, Then: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"servers": {MaxItems: new(0)}}}})
	case reflect.TypeFor[MCPImportSource]():
		arrays = map[string]int{"only": 256, "exclude": 256}
	case reflect.TypeFor[MCPServerInput]():
		arrays["command"] = 128
		maps = map[string]int{"env": 128, "headers": 64}
		texts = map[string]int{"cwd": 4096, "url": 8192, "note": 4096}
		schema.Properties["command"].Items.MaxLength = new(16 << 10)
	case reflect.TypeFor[MCPDeclaration]():
		texts = map[string]int{"name": 256, "brand_hint": 16384, "brand_key": 253}
	case reflect.TypeFor[MCPConfiguration]():
		arrays["servers"] = 64
	case reflect.TypeFor[ConfigureMCPParams]():
		texts["name"] = 256
	case reflect.TypeFor[MCPImportCandidate]():
		texts = map[string]int{"name": 256, "note": 4096, "brand_hint": 16384, "brand_key": 253}
	case reflect.TypeFor[MCPImportCandidatesResult]():
		arrays["candidates"] = 256
		maps["source_errors"] = 16
	case reflect.TypeFor[MCPImportParams]():
		maps["fingerprints"] = 64
		schema.Properties["fingerprints"].AdditionalProperties.Pattern = `^[a-f0-9]{64}$`
	case reflect.TypeFor[MCPImportResult]():
		arrays["added"] = 64
		maps["skipped"] = 64
	case reflect.TypeFor[MCPServerParams]():
		texts["server"] = 256
		schema.Properties["server"].MinLength = new(1)
	case reflect.TypeFor[MCPAttachParams]():
		maps["servers"] = 64
	case reflect.TypeFor[MCPServerStatus]():
		texts = map[string]int{"name": 256, "note": 4096, "failure": 256, "source": 256}
	case reflect.TypeFor[MCPStatusResult]():
		arrays["items"] = 336
	case reflect.TypeFor[MCPRefreshResult]():
		arrays = map[string]int{"added": 64, "existing": 64, "changed": 64, "servers": 64, "blocked": 256, "source_errors": 16}
	case reflect.TypeFor[MCPTool]():
		texts = map[string]int{"name": 256, "title": 2 << 20, "description": 2 << 20, "server": 256, "generation": 256, "resource": 256}
	case reflect.TypeFor[MCPToolsResult]():
		arrays["items"] = 2048
	case reflect.TypeFor[MCPInstructionsResult]():
		texts = map[string]int{"server": 256, "generation": 256, "resource": 256, "text": 64 << 10}
		arrays["content_parts"] = 4
	case reflect.TypeFor[MCPBrandIconsParams]():
		arrays["keys"] = 64
		schema.Properties["keys"].Items.MaxLength = new(253)
	case reflect.TypeFor[MCPBrandIconsResult]():
		maps["icons"] = 64
		schema.Properties["icons"].AdditionalProperties.MaxLength = new(66 << 10)
	}
	for name, limit := range arrays {
		field := schema.Properties[name]
		field.Type, field.Types = "array", nil
		field.MaxItems = new(limit)
		if field.Items.Type == "string" && field.Items.MaxLength == nil {
			field.Items.MaxLength = new(256)
		}
	}
	for name, limit := range texts {
		schema.Properties[name].MaxLength = new(limit)
	}
	for name, limit := range maps {
		field := schema.Properties[name]
		field.Type, field.Types = "object", nil
		field.MaxProperties = new(limit)
		field.PropertyNames = &jsonschema.Schema{Type: "string", MinLength: new(1), MaxLength: new(256)}
		if field.AdditionalProperties.Type == "string" && field.AdditionalProperties.MaxLength == nil {
			field.AdditionalProperties.MaxLength = new(16 << 10)
		}
	}
}
