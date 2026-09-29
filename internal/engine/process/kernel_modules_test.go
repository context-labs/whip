package process

import (
	"context"
	"os"
	"strings"
	"testing"
)

func testModulesKernel(t *testing.T, engine string, modules []string, host Host) *Kernel {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	options := KernelOptions{Command: []string{executable, "-test.run=TestWorkerProcess", "--"}, Engine: engine, Modules: modules, Host: host}
	if engine == EngineQuickJS {
		options.Checkpoints = &memoryCheckpoints{}
	}
	kernel, err := NewKernel(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kernel.Close)
	return kernel
}

// A kernel installs only the selected host modules, so an unselected module is
// an undefined name rather than a binding that always fails.
func TestKernelInstallsOnlySelectedModules(t *testing.T) {
	if _, err := NewKernel(KernelOptions{Modules: []string{"telepathy"}}); err == nil {
		t.Fatal("unknown module accepted")
	}
	var calls []string
	host := HostFunc(func(_ context.Context, module, operation string, _ map[string]any) (any, error) {
		calls = append(calls, module+"."+operation)
		return map[string]any{"ok": true}, nil
	})
	for _, tc := range []struct {
		engine, selected, unselected, missing string
	}{
		{EngineStarlark, `context.inspect()`, `browser.run(command="x")`, "undefined: browser"},
		{EngineQuickJS, `await context.inspect({})`, `await browser.run({command: "x"})`, "browser"},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			calls = nil
			kernel := testModulesKernel(t, tc.engine, []string{"context", "files"}, host)
			if _, err := kernel.Exec(t.Context(), Cell{Code: tc.selected}); err != nil {
				t.Fatalf("selected module failed: %v", err)
			}
			if len(calls) != 1 || calls[0] != "context.inspect" {
				t.Fatalf("host calls = %v", calls)
			}
			if _, err := kernel.Exec(t.Context(), Cell{Code: tc.unselected}); err == nil || !strings.Contains(err.Error(), tc.missing) {
				t.Fatalf("unselected module error = %v", err)
			}
			if len(calls) != 1 {
				t.Fatalf("unselected module reached the host: %v", calls)
			}
			// The local library stays available regardless of host module selection.
			local := `json.encode({"a": 1})`
			if tc.engine == EngineQuickJS {
				local = `json.encode({a: 1})`
			}
			if _, err := kernel.Exec(t.Context(), Cell{Code: local}); err != nil {
				t.Fatalf("local library missing: %v", err)
			}
		})
	}
}

// Every module stays installed when none are selected.
func TestKernelInstallsEveryModuleByDefault(t *testing.T) {
	host := HostFunc(func(_ context.Context, module, operation string, _ map[string]any) (any, error) {
		return map[string]any{"module": module, "operation": operation}, nil
	})
	kernel := testModulesKernel(t, EngineStarlark, nil, host)
	if _, err := kernel.Exec(t.Context(), Cell{Code: `browser.run(command="x")`}); err != nil {
		t.Fatalf("default kernel lost a module: %v", err)
	}
}

func TestKernelExplicitEmptyModulesStayEmptyAfterRestart(t *testing.T) {
	for _, engine := range []string{EngineStarlark, EngineQuickJS} {
		t.Run(engine, func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			options := KernelOptions{Command: []string{executable, "-test.run=TestWorkerProcess", "--"}, Engine: engine, Modules: []string{}, Checkpoints: &memoryCheckpoints{}, Host: HostFunc(func(context.Context, string, string, map[string]any) (any, error) { calls++; return false, nil })}
			for run := range 2 {
				kernel, err := NewKernel(options)
				if err != nil {
					t.Fatal(err)
				}
				code, call := `x=41`, `files.read(path="forbidden")`
				if run == 1 {
					code = `print(x)`
				}
				if engine == EngineQuickJS {
					code = `var x=41`
					call = `await files.read({path:"forbidden"})`
					if run == 1 {
						code = `console.log(x)`
					}
				}
				result, err := kernel.Exec(t.Context(), Cell{Code: code})
				if err != nil || run == 1 && result.Output != "41\n" {
					kernel.Close()
					t.Fatal("checkpoint lost", result, err)
				}
				if _, err := kernel.Exec(t.Context(), Cell{Code: call}); err == nil || !strings.Contains(err.Error(), "files") {
					kernel.Close()
					t.Fatal("empty modules installed files", err)
				}
				kernel.Close()
			}
			if calls != 0 {
				t.Fatal("empty bindings reached host")
			}
		})
	}
}
