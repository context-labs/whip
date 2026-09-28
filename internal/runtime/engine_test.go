package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestRuntimeWorkerProcess(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--" {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	if err := process.WorkerMain(os.Args[index+1:], os.Stdin, os.Stdout, nil); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func engineOptions(t *testing.T) Options {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Options{Workers: 2, KernelWorkers: 1, PollInterval: time.Millisecond, EngineCommand: []string{executable, "-test.run=^TestRuntimeWorkerProcess$", "--"}}
}

func cellProvider(codes map[string]string) providerFunc {
	return func(_ context.Context, request model.Request) (model.Response, error) {
		last := request.Messages[len(request.Messages)-1]
		if last.Role == session.Tool {
			return model.Response{Parts: []session.Part{{Type: "text", Text: last.Parts[0].Result.Output}}}, nil
		}
		if len(request.Tools) != 1 || request.Tools[0].Name != "execute" {
			return model.Response{}, errors.New("missing execution contract")
		}
		arguments, _ := json.Marshal(map[string]string{"code": codes[last.Parts[0].Text]})
		return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "execute-1", Name: "execute", Arguments: arguments}}}}, nil
	}
}

func openEngineTest(t *testing.T, directory string, provider providerFunc) *Runtime {
	t.Helper()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := Open(t.Context(), directory, provider, engineOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r
}

func createEngineSession(t *testing.T, r *Runtime, engine session.Engine) session.Session {
	t.Helper()
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	_, s, err := r.CreateTree(t.Context(), store.CreateTree{Engine: engine, Policy: session.DefaultTreePolicy(), Definition: refs[0], WorkingDirectory: t.TempDir(), Overrides: session.ConfigPatch{Model: &session.ModelSelection{Provider: "scripted", Name: "scripted"}}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Cold WASM compilation under race-enabled CI can exceed the five-second
// scripted-provider wait. Keep this larger deadline local to real engine work.
func runCellTurn(t *testing.T, r *Runtime, id session.SessionID, key, want string) {
	t.Helper()
	submitTest(t, r, id, key)
	admission := waitTestWithin(t, r, key, terminal, 30*time.Second)
	if admission.Turn.State != session.Succeeded {
		t.Fatalf("turn %s: %+v runtime=%v", key, admission.Turn, r.Err())
	}
	attempts, err := r.ModelAttempts(t.Context(), admission.Turn.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0].LogicalID == attempts[1].LogicalID || attempts[0].MessageID == nil || attempts[1].MessageID == nil {
		t.Fatalf("model loop bypassed attempt ledger: %+v", attempts)
	}
	history, err := r.History(t.Context(), id, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Result process.Result `json:"result"`
	}
	if err := json.Unmarshal([]byte(history[len(history)-1].Parts[0].Text), &output); err != nil {
		t.Fatal(err)
	}
	if output.Result.Output != want {
		t.Fatalf("turn %s output=%q want=%q", key, output.Result.Output, want)
	}
	cell, err := r.store.LatestCell(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if cell == nil || cell.State != session.CellSucceeded || cell.Checkpoint == nil || cell.ResultMessageID == nil {
		t.Fatalf("missing execution boundary: %+v", cell)
	}
}

func TestBothEnginesPreserveSessionStateAcrossModelChangeEvictionAndRestart(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{"first": "x = 41\nprint(x)", "increment": "x += 1\nprint(x)", "other": "print(7)", "evicted": "print(x)", "restart": "print(x)"}
			if engine == session.QuickJS {
				codes = map[string]string{"first": "var x = 41; console.log(x)", "increment": "x += 1; console.log(x)", "other": "console.log(7)", "evicted": "console.log(x)", "restart": "console.log(x)"}
			}
			directory := t.TempDir()
			provider := cellProvider(codes)
			r := openEngineTest(t, directory, provider)
			root := createEngineSession(t, r, engine)
			other := createEngineSession(t, r, engine)
			runCellTurn(t, r, root.ID, "first", "41\n")
			if _, err := r.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "fixture", Name: "different-model"}}); err != nil {
				t.Fatal(err)
			}
			runCellTurn(t, r, root.ID, "increment", "42\n")
			runCellTurn(t, r, other.ID, "other", "7\n")
			runCellTurn(t, r, root.ID, "evicted", "42\n")
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := openEngineTest(t, directory, provider)
			runCellTurn(t, reopened, root.ID, "restart", "42\n")
		})
	}
}

func TestCorruptCheckpointBlocksExecutionWithoutSilentlyResettingREPL(t *testing.T) {
	directory := t.TempDir()
	provider := cellProvider(map[string]string{"first": "x=41\nprint(x)", "corrupt": "print(x)"})
	r := openEngineTest(t, directory, provider)
	root := createEngineSession(t, r, session.Starlark)
	runCellTurn(t, r, root.ID, "first", "41\n")
	before, err := r.store.LatestCell(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "artifacts", "sha256", before.Checkpoint.Digest)
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened := openEngineTest(t, directory, provider)
	submitTest(t, reopened, root.ID, "corrupt")
	admission := waitTest(t, reopened, "corrupt", terminal)
	if admission.Turn.State != session.Failed {
		t.Fatalf("corrupt restore: %+v", admission.Turn)
	}
	after, err := reopened.store.LatestCell(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != before.ID {
		t.Fatal("executed despite failed restore")
	}
	history, err := reopened.History(t.Context(), root.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	result := history[len(history)-1].Parts[0].Result
	if result == nil || !strings.Contains(result.Output, "not dispatched") {
		t.Fatalf("untruthful call result: %+v", result)
	}
}

func TestLanguageFailureKeepsItsSettledState(t *testing.T) {
	provider := cellProvider(map[string]string{"failure": "x = 42\nfail(\"intentional\")", "read": "print(x)"})
	r := openEngineTest(t, t.TempDir(), provider)
	root := createEngineSession(t, r, session.Starlark)
	submitTest(t, r, root.ID, "failure")
	admission := waitTest(t, r, "failure", terminal)
	if admission.Turn.State != session.Succeeded {
		t.Fatalf("model could not inspect language failure: %+v", admission.Turn)
	}
	cell, err := r.store.LatestCell(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cell.State != session.CellFailed || cell.Checkpoint == nil {
		t.Fatalf("language failure lost checkpoint: %+v", cell)
	}
	runCellTurn(t, r, root.ID, "read", "42\n")
}

func TestCheckpointMetadataCompatibilityIsVerified(t *testing.T) {
	r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"first": "print(1)"}))
	root := createEngineSession(t, r, session.Starlark)
	runCellTurn(t, r, root.ID, "first", "1\n")
	latest, err := r.store.LatestCell(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := process.ResolveExecutionEngine(string(session.Starlark))
	if err != nil {
		t.Fatal(err)
	}
	descriptor.Build = "incompatible"
	adapter := cellCheckpoints{runtime: r, sessionID: root.ID, descriptor: descriptor}
	if _, err := adapter.Load(t.Context()); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("accepted incompatible image %s: %v", fmt.Sprint(latest.ID), err)
	}
}

func TestPartialRestoreReportSurvivesTurnLease(t *testing.T) {
	directory := t.TempDir()
	provider := cellProvider(map[string]string{"first": "x=41\nunsupported=files.read\nprint(x)", "restart": "print(x)"})
	r := openEngineTest(t, directory, provider)
	root := createEngineSession(t, r, session.Starlark)
	runCellTurn(t, r, root.ID, "first", "41\n")
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openEngineTest(t, directory, provider)
	runCellTurn(t, reopened, root.ID, "restart", "41\n")
	history, err := reopened.History(t.Context(), root.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Result process.Result `json:"result"`
	}
	if err := json.Unmarshal([]byte(history[len(history)-1].Parts[0].Text), &output); err != nil {
		t.Fatal(err)
	}
	if output.Result.Restored == nil || len(output.Result.Restored.Failed) != 1 || output.Result.Restored.Failed[0].Name != "unsupported" {
		t.Fatalf("restore omissions hidden: %+v", output.Result.Restored)
	}
}

func TestDeleteSubtreeClosesOnlyItsKernels(t *testing.T) {
	r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"root": "print(1)", "child": "print(2)", "other": "print(3)"}))
	root := createEngineSession(t, r, session.Starlark)
	other := createEngineSession(t, r, session.Starlark)
	child, err := r.SpawnSession(t.Context(), store.SpawnSession{ParentID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	runCellTurn(t, r, root.ID, "root", "1\n")
	runCellTurn(t, r, child.ID, "child", "2\n")
	runCellTurn(t, r, other.ID, "other", "3\n")
	r.mu.Lock()
	rootKernel, childKernel, otherKernel := r.kernels[root.ID], r.kernels[child.ID], r.kernels[other.ID]
	r.mu.Unlock()
	if err := r.DeleteSubtree(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := childKernel.kernel.Exec(t.Context(), process.Cell{Code: "print(999)"}); !errors.Is(err, process.ErrKernelClosed) {
		t.Fatalf("deleted child kept kernel: %v", err)
	}
	if err := r.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := rootKernel.kernel.Exec(t.Context(), process.Cell{Code: "print(999)"}); !errors.Is(err, process.ErrKernelClosed) {
		t.Fatalf("deleted root kept kernel: %v", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.kernels) != 1 || r.kernels[other.ID] != otherKernel {
		t.Fatal("deletion changed unrelated kernel ownership")
	}
}

func TestCheckpointPublicationFailureDoesNotPublishAnOldBoundary(t *testing.T) {
	directory := t.TempDir()
	provider := cellProvider(map[string]string{"first": "x=41\nprint(x)", "failed-save": "x=42\nprint(x)", "blocked": "print(x)"})
	r := openEngineTest(t, directory, provider)
	root := createEngineSession(t, r, session.Starlark)
	runCellTurn(t, r, root.ID, "first", "41\n")
	path := filepath.Join(directory, "artifacts", "sha256")
	if err := os.Rename(path, path+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("injected publication failure"), 0o600); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, root.ID, "failed-save")
	result := waitTest(t, r, "failed-save", terminal)
	if result.Turn.State != session.Failed {
		t.Fatalf("failed checkpoint turn=%+v", result.Turn)
	}
	latest, err := r.store.LatestCell(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.State != session.CellSucceeded || latest.Checkpoint != nil {
		t.Fatalf("completed execution lost or stale image published: %+v", latest)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+"-saved", path); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openEngineTest(t, directory, provider)
	submitTest(t, reopened, root.ID, "blocked")
	blocked := waitTest(t, reopened, "blocked", terminal)
	if blocked.Turn.State != session.Failed || !strings.Contains(*blocked.Turn.Failure, "checkpoint unavailable") {
		t.Fatalf("silently restored stale state: %+v", blocked.Turn)
	}
	after, err := reopened.store.LatestCell(t.Context(), root.ID)
	if err != nil || after.ID != latest.ID {
		t.Fatalf("blocked cell dispatched: %+v %v", after, err)
	}
}
