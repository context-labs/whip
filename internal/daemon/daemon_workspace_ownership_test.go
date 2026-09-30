package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func TestWorkspaceCoordinatorIsSharedByRootsAndChildren(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		streamText(w, "done")
	}))
	t.Cleanup(server.Close)
	client := llm.New(server.URL, "fixture-key")
	client.MaxRetries = 0
	directory := canonicalPromptDirectory(t, t.TempDir())
	alias := filepath.Join(t.TempDir(), "workspace-alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(t.TempDir(), "sessions.db")
	coordinator := capability.NewWorkspaces()
	store, err := session.Open(database, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 3)
	for i, cwd := range []string{directory, alias, alias} {
		kind := session.SessionKindAgent
		model, provider := "model", "provider"
		if i == 2 {
			kind = session.SessionKindToolHost
			model, provider = "", ""
		}
		id, err := store.Create(kind, cwd, model, provider)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetPermissionMode(t.Context(), id, session.PermissionModeAutomatic); err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	var childID string
	for _, incarnation := range []string{"new child", "restored child"} {
		t.Run(incarnation, func(t *testing.T) {
			if incarnation == "restored child" {
				coordinator = capability.NewWorkspaces()
				store, err = session.Open(database, coordinator)
				if err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { _ = store.Close() })
			if store.Workspaces() != coordinator {
				t.Fatal("store did not borrow the supplied workspace coordinator")
			}
			processes := newTestProcesses(t)
			limits := rlm.DefaultLimits()
			kernels := rlm.NewManager(limits.MaxWorkers)
			t.Cleanup(kernels.Close)
			runtimes := make(map[string]*RecursiveRuntime)
			services := make(map[string]*tools.Services)
			owner, err := New(store, processes, func(ctx context.Context, meta session.Meta, history []llm.Message) (Components, error) {
				host := tools.NewServices()
				services[meta.ID] = host
				if meta.Kind == session.SessionKindToolHost {
					return Components{Runner: NewToolRunner(host)}, nil
				}
				value := agent.NewRuntime(client, meta.Model, 128, "", host)
				value.ModelName, value.Provider, value.WorkingDir = meta.Model, meta.Provider, meta.CWD
				value.ContextLimit = 65536
				definition, _, err := DefinitionFor(ctx, store, meta)
				if err != nil {
					return Components{}, err
				}
				definition.Surface.AutoTitle = false
				runtime, err := NewRecursiveRuntime(RecursiveRuntimeOptions{
					Engine: meta.ExecutionEngine, Definition: definition, Agent: value, History: history,
					Limits: limits, Kernels: kernels, KernelCommand: recursiveKernelCommand,
				})
				if err != nil {
					return Components{}, err
				}
				runtimes[meta.ID] = runtime
				return Components{Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind, Definition: definition}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = owner.Close() })
			for _, id := range ids {
				if _, err := owner.Open(id); err != nil {
					t.Fatal(err)
				}
			}
			runtime := runtimes[ids[0]]
			if incarnation == "new child" {
				runs := &sync.Map{}
				runtime.setRunTurnHook(observeRunTurn(runs))
				spawned, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{
					"name": "keeper", "prompt": "Finish immediately.", "report": "message",
				})
				if err != nil {
					t.Fatal(err)
				}
				childID = spawned.(map[string]any)["id"].(string)
				waitRunTurn(t, runs, childID, 1)
			}
			runtime.mu.RLock()
			child := runtime.agents[childID]
			runtime.mu.RUnlock()
			if child == nil {
				t.Fatal("retained child is missing")
			}
			waitAgentIdle(t, child)
			if store.Workspaces() != coordinator {
				t.Fatal("runtime construction replaced the shared workspace coordinator")
			}
			assertSharedWorkspaceLocks(t, coordinator, directory, alias, []workspaceBorrower{
				{name: "first root", services: services[ids[0]], path: filepath.Join(directory, "target.txt")},
				{name: "second root", services: services[ids[1]], path: filepath.Join(alias, "target.txt")},
				{name: incarnation, services: child.agent.Services, path: filepath.Join(alias, "target.txt")},
				{name: "tool host", services: services[ids[2]], path: filepath.Join(alias, "target.txt")},
			})
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type workspaceBorrower struct {
	name     string
	services *tools.Services
	path     string
}

func assertSharedWorkspaceLocks(t *testing.T, coordinator *capability.Workspaces, directory, alias string, borrowers []workspaceBorrower) {
	t.Helper()
	write := func(ctx context.Context, borrower workspaceBorrower, path, content string) error {
		ctx, err := tools.WithTurnIdentity(ctx, "workspace-ownership")
		if err != nil {
			return err
		}
		arguments, err := json.Marshal(map[string]string{"path": path, "content": content})
		if err != nil {
			return err
		}
		_, err = borrower.services.Invoke(ctx, "write", arguments)
		return err
	}
	for _, borrower := range borrowers {
		if err := write(t.Context(), borrower, borrower.path, "before lock"); err != nil {
			t.Fatalf("%s cannot write before locking: %v", borrower.name, err)
		}
	}
	workspace, err := coordinator.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := workspace.LockCanonicalPath(t.Context(), filepath.Join(directory, "target.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, borrower := range borrowers {
		t.Run(borrower.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
			defer cancel()
			if err := write(ctx, borrower, borrower.path, "must remain blocked"); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("write did not wait on the shared canonical lock: %v", err)
			}
			if body, err := os.ReadFile(filepath.Join(directory, "target.txt")); err != nil || string(body) != "before lock" {
				t.Fatalf("blocked write changed the target: %q, %v", body, err)
			}
			unrelated, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if err := write(unrelated, borrower, filepath.Join(alias, "unrelated.txt"), borrower.name); err != nil {
				t.Fatalf("unrelated path was blocked: %v", err)
			}
		})
	}
	release()
	for _, borrower := range borrowers {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		err := write(ctx, borrower, borrower.path, borrower.name)
		cancel()
		if err != nil {
			t.Fatalf("%s cannot write after release: %v", borrower.name, err)
		}
		if body, err := os.ReadFile(filepath.Join(directory, "target.txt")); err != nil || string(body) != borrower.name {
			t.Fatalf("%s write after release = %q, %v", borrower.name, body, err)
		}
	}
}
