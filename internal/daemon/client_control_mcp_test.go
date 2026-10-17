package daemon

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/protocol"
)

type statusSurfaceMCP struct {
	controlSurfaceMCP
	live, blocked, sourceErrors []mcp.Server
}

func (m *statusSurfaceMCP) Statuses() []mcp.Server     { return m.live }
func (m *statusSurfaceMCP) Blocked() []mcp.Server      { return m.blocked }
func (m *statusSurfaceMCP) SourceErrors() []mcp.Server { return m.sourceErrors }

func TestClientMCPStatusJSON(t *testing.T) {
	for _, test := range []struct {
		name    string
		manager *statusSurfaceMCP
		want    string
	}{
		{name: "empty", manager: &statusSurfaceMCP{}, want: "[]"},
		{
			name: "sorted live and discovery rows",
			manager: &statusSurfaceMCP{
				live:         []mcp.Server{{Name: "z-live", Status: mcp.StatusReady, Tools: 2}},
				blocked:      []mcp.Server{{Name: "b-blocked", Status: mcp.StatusBlocked, Note: "import disabled", Source: "project"}},
				sourceErrors: []mcp.Server{{Name: "a-source", Status: mcp.StatusUnreadable, Err: "unreadable", Source: "config"}},
			},
			want: `[{"name":"a-source","status":"unreadable","error":"unreadable","source":"config"},{"name":"b-blocked","status":"blocked","note":"import disabled","source":"project"},{"name":"z-live","status":"ready","tools":2}]`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := &Session{mcp: test.manager}
			got, err := root.clientMCP(t.Context(), "mcp.status", clientActionPayload{})
			if err != nil || got != test.want {
				t.Fatalf("status = %s, %v; want %s", got, err, test.want)
			}
		})
	}
}

// Runtime MCP results are serialized directly. Keep their independently owned
// protocol declarations aligned without making either package depend on a copy.
func TestMCPResultContractParity(t *testing.T) {
	for _, test := range []struct {
		name          string
		runtime, wire reflect.Type
	}{
		{"status", reflect.TypeFor[mcp.ServerStatus](), reflect.TypeFor[protocol.MCPStatusResult]()},
		{"refresh", reflect.TypeFor[mcp.RefreshResult](), reflect.TypeFor[protocol.MCPRefreshResult]()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.runtime.NumField() != test.wire.NumField() {
				t.Fatal("runtime and wire result fields differ")
			}
			for i := range test.runtime.NumField() {
				a, b := test.runtime.Field(i), test.wire.Field(i)
				if a.Type == reflect.TypeFor[[]mcp.ServerStatus]() {
					a.Type = reflect.TypeFor[[]protocol.MCPStatusResult]()
				}
				if a.Name != b.Name || a.Type != b.Type || a.Tag != b.Tag {
					t.Errorf("field %d differs: %v / %v", i, a, b)
				}
			}
		})
	}
	for _, test := range []struct {
		name   string
		result mcp.RefreshResult
	}{
		{name: "nil slices"},
		{name: "empty slices", result: mcp.RefreshResult{Added: []string{}, Existing: []string{}, Changed: []string{}, Servers: []mcp.ServerStatus{}, Blocked: []mcp.ServerStatus{}, SourceErrors: []mcp.ServerStatus{}}},
		{name: "populated", result: mcp.RefreshResult{Added: []string{"new"}, Existing: []string{"old"}, Changed: []string{"changed"}, Servers: []mcp.ServerStatus{{Name: "new", Status: "ready", Tools: 2}}, Blocked: []mcp.ServerStatus{{Name: "blocked", Status: "blocked", Note: "disabled", Source: "project"}}, SourceErrors: []mcp.ServerStatus{{Name: "source", Status: "unreadable", Error: "failed", Source: "config"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(test.result)
			if err != nil {
				t.Fatal(err)
			}
			var wire protocol.MCPRefreshResult
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			roundTrip, err := json.Marshal(wire)
			if err != nil || string(raw) != string(roundTrip) {
				t.Fatalf("MCP result changed through protocol: %s => %s, %v", raw, roundTrip, err)
			}
		})
	}
}
