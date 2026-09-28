package rlm

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/legacy/session"
	"github.com/context-labs/whip/internal/tools"
)

func TestToolAdapterPreservesOutputIdentityAndHostPresentation(t *testing.T) {
	for _, engine := range []string{EngineStarlark, EngineQuickJS} {
		t.Run(engine, func(t *testing.T) {
			workspace := t.TempDir()
			if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("content"), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := session.Open(filepath.Join(t.TempDir(), "sessions.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			rootID, err := store.Create(session.SessionKindAgent, workspace, "model", "provider")
			if err != nil {
				t.Fatal(err)
			}
			authority, err := store.EnsureAuthority(t.Context(), rootID)
			if err != nil {
				t.Fatal(err)
			}
			services := tools.NewServices()
			if err := services.BindDispatcher(store, store.Workspaces(), store.Processes(), authority); err != nil {
				t.Fatal(err)
			}
			host := ToolHost(HostFunc(func(ctx context.Context, _, operation string, arguments map[string]any) (any, error) {
				input, err := json.Marshal(arguments)
				if err != nil {
					return nil, err
				}
				result, err := services.Invoke(ctx, operation, input)
				arguments["path"] = "changed-after-dispatch"
				return result, err
			}))
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			var observed []PresentedHostCall
			record := func(call PresentedHostCall) { observed = append(observed, call) }
			kernel, err := NewKernel(KernelOptions{
				Command: []string{executable, "-test.run=TestWorkerProcess", "--"},
				Engine:  engine, Host: host, Checkpoints: &memoryCheckpoints{},
				ObserveHost: PresentHostCalls(record, record),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(kernel.Close)
			var outputs []string
			ctx := tools.WithToolCallID(t.Context(), "model-call")
			ctx = tools.WithOnUpdate(ctx, func(output string) { outputs = append(outputs, output) })
			code := "print('before')\nfiles.read(path='note.txt')\nprint('after')"
			if engine == EngineQuickJS {
				code = "print('before'); await files.read({path:'note.txt'}); print('after');"
			}
			input, err := json.Marshal(map[string]string{"code": code})
			if err != nil {
				t.Fatal(err)
			}
			result, err := Tool(kernel).Run(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(outputs, "before\n") || !strings.Contains(result, "after") {
				t.Fatalf("missing streamed output: %q", outputs)
			}
			if len(observed) != 2 || observed[0].Status != "" || observed[1].Status != "completed" || observed[1].OperationID == "" {
				t.Fatalf("host lifecycle or dispatcher identity lost: %+v", observed)
			}
			for _, call := range observed {
				if call.CallID != "model-call" || call.InvocationID == "" || call.Display == nil || call.Display.Target != "note.txt" {
					t.Fatalf("host presentation changed after dispatch: %+v", call)
				}
			}
		})
	}
}

func TestWorkerDescriptionSeparatesExecutionFromGuide(t *testing.T) {
	for _, descriptor := range ExecutionEngines() {
		t.Run(descriptor.ID, func(t *testing.T) {
			for _, enriched := range []bool{false, true} {
				var describe func(EngineDescriptor) EngineDescriptor
				if enriched {
					describe = DescribeEngine
				}
				var output bytes.Buffer
				if err := WorkerMain([]string{"-engine", descriptor.ID, "-describe"}, &bytes.Buffer{}, &output, describe); err != nil {
					t.Fatal(err)
				}
				var got EngineDescriptor
				if err := json.Unmarshal(output.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.ID != descriptor.ID || got.Build != descriptor.Build || got.ABI != descriptor.ABI {
					t.Fatalf("execution identity changed: %+v", got)
				}
				if enriched && len(got.GuideSHA256) != 64 || !enriched && got.GuideSHA256 != "" {
					t.Fatalf("unexpected guide enrichment: %+v", got)
				}
			}
		})
	}
}
