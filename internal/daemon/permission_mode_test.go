package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

// The durable snapshot and live client event expose the same session choice.
func TestPermissionModeSnapshotAndUpdateEvent(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: NewToolRunner(tools.NewServices())}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := root.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PermissionMode != "prompt" {
		t.Fatalf("initial snapshot permission mode = %q, want prompt", snapshot.PermissionMode)
	}

	result := clientCommand(t, root, "client", "mode-automatic", "permission.mode", map[string]bool{"external_permissions": false})
	if result.Status != "succeeded" {
		t.Fatalf("permission.mode = %+v", result)
	}

	snapshot, err = root.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PermissionMode != "automatic" {
		t.Fatalf("updated snapshot permission mode = %q, want automatic", snapshot.PermissionMode)
	}

	events, _, err := store.ReplayEvents(t.Context(), rootID, 0, session.MaxEventReplay)
	if err != nil {
		t.Fatal(err)
	}
	var update *protocol.SessionUpdateEvent
	for _, envelope := range events {
		if envelope.Kind != "session.permission_mode.updated" {
			continue
		}
		var event protocol.SessionUpdateEvent
		if err := json.Unmarshal(envelope.Payload.Inline, &event); err != nil {
			t.Fatal(err)
		}
		update = &event
	}
	if update == nil || update.PermissionMode == nil || *update.PermissionMode != "automatic" {
		t.Fatalf("permission mode event = %+v", update)
	}
}

func permissionModeOwner(t *testing.T, store *session.Store, factoryMode bool) *Daemon {
	t.Helper()
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		runner := NewToolRunner(tools.NewServices()).(*toolRunner)
		runner.SetExternalPermissions(factoryMode)
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	return owner
}

func assertPermissionMode(t *testing.T, root *Session, want string) {
	t.Helper()
	for name, snapshot := range map[string]func(context.Context) (session.RootSnapshot, error){
		"full": root.Snapshot, "bounded": root.SnapshotView,
	} {
		got, err := snapshot(t.Context())
		if err != nil || got.PermissionMode != want {
			t.Fatalf("%s snapshot mode=%q, want %q, error=%v", name, got.PermissionMode, want, err)
		}
	}
	if got := root.runner.(clientPermissionRunner).ExternalPermissionsEnabled(); got != (want == session.PermissionModePrompt) {
		t.Fatalf("runner external permissions=%t, want mode %q", got, want)
	}
}

func TestPermissionModeSurvivesRestartAndCommandReplay(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{session.PermissionModeAutomatic, session.PermissionModePrompt} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "sessions.db")
			store := openStore(t, path)
			rootID, otherID := createRoot(t, store), createRoot(t, store)
			owner := permissionModeOwner(t, store, false)
			root, err := owner.Open(rootID)
			if err != nil {
				t.Fatal(err)
			}
			other, err := owner.Open(otherID)
			if err != nil {
				t.Fatal(err)
			}
			assertPermissionMode(t, root, session.PermissionModePrompt)
			external := mode == session.PermissionModePrompt
			oldPayload := map[string]bool{"external_permissions": !external}
			old := clientCommand(t, root, "human", "old-mode", "permission.mode", oldPayload)
			if old.Status != "succeeded" {
				t.Fatalf("first selection=%+v", old)
			}
			selected := clientCommand(t, root, "human", "latest-mode", "permission.mode", map[string]bool{"external_permissions": external})
			if selected.Status != "succeeded" {
				t.Fatalf("latest selection=%+v", selected)
			}
			for restart := range 2 {
				retry := clientCommand(t, root, "human", "old-mode", "permission.mode", oldPayload)
				if !reflect.DeepEqual(retry, old) {
					t.Fatalf("retry=%+v, want original result %+v", retry, old)
				}
				assertPermissionMode(t, root, mode)
				assertPermissionMode(t, other, session.PermissionModePrompt)
				if restart == 1 {
					break
				}
				if err := owner.Close(); err != nil {
					t.Fatal(err)
				}
				store = openStore(t, path)
				// Deliberately contradict the saved mode in the new runtime factory.
				owner = permissionModeOwner(t, store, !external)
				root, err = owner.Open(rootID)
				if err != nil {
					t.Fatal(err)
				}
				other, err = owner.Open(otherID)
				if err != nil {
					t.Fatal(err)
				}
			}
			assertResumedWritePermission(t, store, root, external)
		})
	}
}

func assertResumedWritePermission(t *testing.T, store *session.Store, root *Session, external bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	path := filepath.Join(root.meta.CWD, "resumed.txt")
	arguments, err := json.Marshal(map[string]string{"path": path, "content": "resumed write"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := root.runner.(*toolRunner).CallTool(ctx, "write", arguments)
		done <- err
	}()
	if external {
		pending := waitMCPPermission(t, store, root)
		if pending.Operation != "write" || pending.AgentID != root.AgentID() {
			t.Fatalf("resumed approval=%+v", pending)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("write occurred before approval: %v", err)
		}
		payload := json.RawMessage(`{"decision":"allow"}`)
		digest, err := requestDigest("root", root.ID(), "permission.decide", payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := root.DecidePermissionCommand(ctx, session.CommandAdmission{
			ClientID: "human", CommandID: "approve-resumed-write", RequestDigest: digest,
			Payload: session.RuntimePayload{Data: payload, MediaType: "application/json"},
		}, pending.ID, capability.Decision{Allow: true, PrincipalID: "human"}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("resumed write: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("resumed write did not finish")
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "resumed write" {
		t.Fatalf("resumed file=%q, error=%v", content, err)
	}
	if pending, err := store.ListPendingPermissions(ctx, root.ID()); err != nil || len(pending) != 0 {
		t.Fatalf("resumed write left pending approvals=%+v, error=%v", pending, err)
	}
}

func TestPermissionModeFailedSavePreservesLivePolicy(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createRoot(t, store)
	owner := permissionModeOwner(t, store, false)
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_mode_event BEFORE INSERT ON events
		WHEN NEW.kind='session.permission_mode.updated' BEGIN SELECT RAISE(ABORT,'mode event rejected'); END`); err != nil {
		t.Fatal(err)
	}
	result := clientCommand(t, root, "human", "failed-mode", "permission.mode", map[string]bool{"external_permissions": false})
	if result.Status != "failed" || !strings.Contains(result.Error, "mode event rejected") {
		t.Fatalf("rejected mode result=%+v", result)
	}
	assertPermissionMode(t, root, session.PermissionModePrompt)
	if _, err := db.ExecContext(t.Context(), `DROP TRIGGER reject_mode_event`); err != nil {
		t.Fatal(err)
	}
	result = clientCommand(t, root, "human", "saved-mode", "permission.mode", map[string]bool{"external_permissions": false})
	if result.Status != "succeeded" {
		t.Fatalf("recovered mode update=%+v", result)
	}
	assertPermissionMode(t, root, session.PermissionModeAutomatic)
}

func TestPermissionModeRestoresChildrenBeforeResumedWork(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{session.PermissionModeAutomatic, session.PermissionModePrompt} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				streamText(w, "done")
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "sessions.db")
			store := openStore(t, path)
			rootID := createRoot(t, store)
			external := mode == session.PermissionModePrompt
			var runtime *RecursiveRuntime
			var runs, firstModes *sync.Map
			open := func() (*Daemon, *Session) {
				t.Helper()
				runs, firstModes = &sync.Map{}, &sync.Map{}
				owner, err := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
					value := agent.NewRuntime(llm.New(server.URL, "key"), "model", 1024, "", tools.NewServices())
					value.ModelName, value.Provider, value.WorkingDir = meta.Model, meta.Provider, meta.CWD
					value.Services.SetExternalPermissions(!external)
					limits := rlm.DefaultLimits()
					var err error
					runtime, err = NewRecursiveRuntime(RecursiveRuntimeOptions{
						Agent: value, History: history, Limits: limits, KernelCommand: recursiveKernelCommand,
					})
					if err != nil {
						return Components{}, err
					}
					runtime.setRunTurnHook(func(node *AgentSession) {
						firstModes.LoadOrStore(node.id, node.ExternalPermissionsEnabled())
						observeRunTurn(runs)(node)
					})
					return Components{Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind}, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = owner.Close() })
				root, err := owner.Open(rootID)
				if err != nil {
					t.Fatal(err)
				}
				return owner, root
			}
			spawn := func(name string) string {
				t.Helper()
				result, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": name, "prompt": "work", "report": "message"})
				if err != nil {
					t.Fatal(err)
				}
				id := result.(map[string]any)["id"].(string)
				waitRunTurn(t, runs, id, 1)
				runtime.mu.RLock()
				node := runtime.agents[id]
				runtime.mu.RUnlock()
				waitAgentIdle(t, node)
				return id
			}
			owner, root := open()
			old := clientCommand(t, root, "human", "old", "permission.mode", map[string]bool{"external_permissions": !external})
			if old.Status != "succeeded" {
				t.Fatalf("initial selection=%+v", old)
			}
			childID := spawn("retained")
			selected := clientCommand(t, root, "human", "latest", "permission.mode", map[string]bool{"external_permissions": external})
			if selected.Status != "succeeded" {
				t.Fatalf("latest selection=%+v", selected)
			}
			if got := runtime.PermissionResolver(childID).ExternalPermissionsEnabled(); got != external {
				t.Fatalf("live retained child mode=%t, want %t", got, external)
			}
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
			store = openStore(t, path)
			// Queued durable input causes the retained child to wake inside Bind.
			if _, err := store.SendMailboxMessage(t.Context(), rootID, rootID, childID, session.MailboxSend{Subject: "resume", Body: "work again"}); err != nil {
				t.Fatal(err)
			}
			_, root = open()
			assertPermissionMode(t, root, mode)
			waitRunTurn(t, runs, childID, 1)
			newID := spawn("new-child")
			for _, id := range []string{childID, newID} {
				if got, ok := firstModes.Load(id); !ok || got != external {
					t.Fatalf("child %q first resumed/spawned turn mode=%v, want %t", id, got, external)
				}
			}
		})
	}
}
