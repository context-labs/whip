package rlm

import (
	"context"
	"reflect"
	"testing"
)

func TestMCPRecoverySurfaceBothEngines(t *testing.T) {
	for _, tc := range []struct{ engine, code string }{
		{EngineStarlark, `mcp.refresh()
mcp.reconnect(server="local")`},
		{EngineQuickJS, `await mcp.refresh({});
await mcp.reconnect({server: "local"});`},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			var calls []string
			host := HostFunc(func(_ context.Context, module, operation string, args map[string]any) (any, error) {
				calls = append(calls, module+"."+operation)
				if operation == "refresh" && len(args) != 0 {
					t.Errorf("refresh args=%#v", args)
				}
				if operation == "reconnect" && !reflect.DeepEqual(args, map[string]any{"server": "local"}) {
					t.Errorf("reconnect args=%#v", args)
				}
				return map[string]any{"status": "ready"}, nil
			})
			kernel := testModulesKernel(t, tc.engine, []string{"mcp"}, host)
			if _, err := kernel.Exec(t.Context(), tc.code); err != nil {
				t.Fatal(err)
			}
			if want := []string{"mcp.refresh", "mcp.reconnect"}; !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls=%v want=%v", calls, want)
			}
		})
	}
}
