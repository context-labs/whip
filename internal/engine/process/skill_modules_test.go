package process

import (
	"context"
	"strings"
	"testing"
)

func TestSkillReadModuleBothEngines(t *testing.T) {
	for _, tc := range []struct{ engine, code string }{
		{EngineStarlark, `skills.read(root_id=None, name="workspace", offset="0", length=17, sha256=None)
skills.read(root_id="shared", name="host", offset="9007199254740993", length=1, sha256="` + strings.Repeat("a", 64) + `")`},
		{EngineQuickJS, `await skills.read({root_id:null,name:"workspace",offset:"0",length:17,sha256:null});
await skills.read({root_id:"shared",name:"host",offset:"9007199254740993",length:1,sha256:"` + strings.Repeat("a", 64) + `"});`},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			calls := 0
			host := HostFunc(func(_ context.Context, module, operation string, args map[string]any) (any, error) {
				calls++
				if module != "skills" || operation != "read" {
					t.Errorf("unexpected operation %s.%s", module, operation)
				}
				if calls == 1 && (args["root_id"] != nil || args["sha256"] != nil || args["name"] != "workspace") {
					t.Errorf("workspace/null arguments changed: %#v", args)
				}
				if calls == 2 && (args["root_id"] != "shared" || args["offset"] != "9007199254740993" || args["sha256"] != strings.Repeat("a", 64)) {
					t.Errorf("host/exact byte offset arguments changed: %#v", args)
				}
				return map[string]any{"data_base64": "eA==", "next_offset": nil}, nil
			})
			kernel := testModulesKernel(t, tc.engine, []string{"skills"}, host)
			if _, err := kernel.Exec(t.Context(), Cell{Code: tc.code}); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatal("skill calls missing", calls)
			}
		})
	}
	if err := validateModuleOperation("skills", "write"); err == nil {
		t.Fatal("unknown skill operation accepted")
	}
}
