package rlm

import (
	"context"
	"strings"
	"testing"
)

// A kernel started with tool names installs them as the reserved tools module
// and nothing else: an undeclared tool is not a binding, and a kernel without
// tools has no tools module at all.
func TestKernelInstallsOnlyDeclaredTools(t *testing.T) {
	if _, err := NewKernel(KernelOptions{Tools: []string{"Bad-Name"}}); err == nil {
		t.Fatal("invalid tool name accepted")
	}
	if _, err := NewKernel(KernelOptions{Tools: []string{"lookup", "lookup"}}); err == nil {
		t.Fatal("duplicate tool accepted")
	}
	var calls []string
	var arguments map[string]any
	host := HostFunc(func(_ context.Context, module, operation string, args map[string]any) (any, error) {
		calls = append(calls, module+"."+operation)
		arguments = args
		return map[string]any{"ticket": args["id"]}, nil
	})
	for _, tc := range []struct {
		engine, call, undeclared, missing, absent string
	}{
		{EngineStarlark, `tools.lookup(id="7")`, `tools.other(id="7")`, "has no .other attribute", "undefined: tools"},
		{EngineQuickJS, `await tools.lookup({id: "7"})`, `await tools.other({id: "7"})`, "not a function", "tools"},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			calls, arguments = nil, nil
			executable := testModulesKernel(t, tc.engine, []string{"context"}, host)
			options := KernelOptions{Command: executable.command, Engine: tc.engine, Modules: []string{"context"}, Tools: []string{"lookup"}, Host: host}
			if tc.engine == EngineQuickJS {
				options.Checkpoints = &memoryCheckpoints{}
			}
			kernel, err := NewKernel(options)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(kernel.Close)
			result, err := kernel.Exec(t.Context(), tc.call)
			if err != nil {
				t.Fatalf("declared tool failed: %v", err)
			}
			if len(calls) != 1 || calls[0] != "tools.lookup" || arguments["id"] != "7" {
				t.Fatalf("host calls = %v, arguments = %v", calls, arguments)
			}
			if value, ok := result.Value.(map[string]any); !ok || value["ticket"] != "7" {
				t.Fatalf("tool result = %#v", result.Value)
			}
			if _, err := kernel.Exec(t.Context(), tc.undeclared); err == nil || !strings.Contains(err.Error(), tc.missing) {
				t.Fatalf("undeclared tool error = %v", err)
			}
			if len(calls) != 1 {
				t.Fatalf("undeclared tool reached the host: %v", calls)
			}
			// Without tools the module is not installed, matching the prompt catalog.
			if _, err := executable.Exec(t.Context(), tc.call); err == nil || !strings.Contains(err.Error(), tc.absent) {
				t.Fatalf("tools module present without tools: %v", err)
			}
		})
	}
}
