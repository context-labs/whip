package mcp_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/protocol"
)

func TestMCPConfigContractParity(t *testing.T) {
	savedType := reflect.TypeFor[config.MCPServer]()
	runtimeType := reflect.TypeFor[mcp.ServerConfig]()
	wireType := reflect.TypeFor[protocol.MCPServerConfig]()
	if savedType.NumField()+1 != runtimeType.NumField() || wireType.NumField() != runtimeType.NumField() {
		t.Fatal("MCP config shapes differ beyond runtime-only trust")
	}
	for field := range runtimeType.Fields() {
		wire, ok := wireType.FieldByName(field.Name)
		wantTag := field.Tag
		switch field.Name {
		case "StartupTimeout":
			wantTag = `json:"startup_timeout,omitempty"`
		case "ToolTimeout":
			wantTag = `json:"tool_timeout,omitempty"`
		}
		if !ok || wire.Type != field.Type || wire.Tag != wantTag {
			t.Errorf("wire field %s no longer matches runtime config", field.Name)
		}
		if field.Name == "Trusted" {
			if field.Tag.Get("json") != "-" {
				t.Fatal("runtime trust must not be serialized")
			}
			continue
		}
		saved, ok := savedType.FieldByName(field.Name)
		if !ok || saved.Type != field.Type || saved.Tag != field.Tag {
			t.Errorf("persisted field %s no longer matches runtime config", field.Name)
		}
	}

	// Populate every saved field so adding one cannot silently bypass the
	// hand-written persistence-to-runtime projection.
	saved := config.MCPServer{
		Origin: "claude", Source: "project/.mcp.json", Command: []string{"fixture", "--arg"},
		Env: map[string]string{"KEY": "$REFERENCE"}, Cwd: "/project", URL: "https://example.invalid/mcp",
		Headers: map[string]string{"Authorization": "$TOKEN"}, Enabled: new(false), Note: "fixture",
		StartupTimeout: 17, ToolTimeout: 23,
	}
	for i := range savedType.NumField() {
		if reflect.ValueOf(saved).Field(i).IsZero() {
			t.Fatalf("populate new config field %s to cover its conversion", savedType.Field(i).Name)
		}
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var want mcp.ServerConfig
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	got := mcp.FromConfigMap(map[string]config.MCPServer{"fixture": saved})["fixture"]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("saved config lost fields: got %+v; want %+v", got, want)
	}

	got.Trusted = true
	encoded, err := json.Marshal(protocol.MCPAttachParams{Servers: map[string]mcp.ServerConfig{"fixture": got}})
	if err != nil {
		t.Fatal(err)
	}
	// Reuse a previously trusted object, just as a long-lived decoder could.
	decoded := protocol.MCPAttachParams{Servers: map[string]mcp.ServerConfig{"fixture": got}}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Servers["fixture"], want) {
		t.Fatalf("wire conversion lost config or conveyed trust: %+v", decoded.Servers["fixture"])
	}
}
