package process

import (
	"context"
	"reflect"
	"testing"
)

func TestCompletionInspectionModulesBothEngines(t *testing.T) {
	for _, tc := range []struct{ engine, code string }{
		{EngineStarlark, `agents.pending_reports(after="child", limit=2)
agents.read_report(child_id="child", turn_id="turn", offset="9007199254740993", length=17)
artifacts.read(id="completion_turn", offset="0", length=17)`},
		{EngineQuickJS, `await agents.pending_reports({after:"child",limit:2});
await agents.read_report({child_id:"child",turn_id:"turn",offset:"9007199254740993",length:17});
await artifacts.read({id:"completion_turn",offset:"0",length:17});`},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			var calls []string
			host := HostFunc(func(_ context.Context, module, operation string, args map[string]any) (any, error) {
				calls = append(calls, module+"."+operation)
				if operation == "read_report" && args["offset"] != "9007199254740993" {
					t.Errorf("exact offset lost: %#v", args["offset"])
				}
				return map[string]any{}, nil
			})
			kernel := testModulesKernel(t, tc.engine, []string{"agents", "artifacts"}, host)
			if _, err := kernel.Exec(t.Context(), Cell{Code: tc.code}); err != nil {
				t.Fatal(err)
			}
			if want := []string{"agents.pending_reports", "agents.read_report", "artifacts.read"}; !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls=%v want=%v", calls, want)
			}
		})
	}
}
