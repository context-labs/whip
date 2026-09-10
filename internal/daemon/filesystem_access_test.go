package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestFilesystemAccessStarlarkRootAndChildSurviveRestart(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		streamText(w, "done")
	}))
	t.Cleanup(server.Close)
	client := llm.New(server.URL, "fixture-key")
	client.MaxRetries = 0
	database := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, database)
	base := canonicalPromptDirectory(t, t.TempDir())
	project, sibling := filepath.Join(base, "project"), filepath.Join(base, "sibling")
	writeDaemonPromptFile(t, filepath.Join(project, "inside.txt"), "inside-project")
	writeDaemonPromptFile(t, filepath.Join(sibling, "seed.txt"), "sibling-seed")
	rootID, err := store.Create(session.SessionKindAgent, project, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	owner, root, runtime := openPromptRuntime(t, store, rootID, client)
	deniedFilesystemCell(t, runtime.rootNode, fmt.Sprintf(`files.list(path=%q)`, sibling))
	if pending, err := store.ListPendingPermissions(t.Context(), rootID); err != nil || len(pending) != 0 {
		t.Fatalf("out-of-scope read created permission requests: %+v error=%v", pending, err)
	}
	setFilesystemMode(t, root, "full-access", false)
	assertFilesystemCell(t, runtime.rootNode, fmt.Sprintf(`listing = files.list(path=%q)
seed = files.read(path=%q)
files.write(path=%q, content="root-before")
files.patch(path=%q, old="before", new="after")
"seed.txt" in listing["output"] and "sibling-seed" in seed["output"] and "root-after" in files.read(path=%q)["output"]`,
		sibling, filepath.Join(sibling, "seed.txt"), filepath.Join(sibling, "root.txt"), filepath.Join(sibling, "root.txt"), filepath.Join(sibling, "root.txt")))
	if content, err := os.ReadFile(filepath.Join(sibling, "root.txt")); err != nil || string(content) != "root-after" {
		t.Fatalf("root sibling mutation=%q error=%v", content, err)
	}

	runs := &sync.Map{}
	runtime.setRunTurnHook(observeRunTurn(runs))
	result, err := runtime.rootNode.kernel.Exec(t.Context(), `agents.spawn(name="keeper", prompt="Finish immediately.", report="message")["id"]`)
	if err != nil {
		t.Fatal(err)
	}
	childID, ok := result.Value.(string)
	if !ok {
		t.Fatalf("spawn returned %+v", result)
	}
	waitRunTurn(t, runs, childID, 1)
	runtime.mu.Lock()
	child := runtime.agents[childID]
	runtime.mu.Unlock()
	waitAgentIdle(t, child)
	assertFilesystemCell(t, child, fmt.Sprintf(`files.write(path=%q, content="child-before-restart")
"root-after" in files.read(path=%q)["output"]`, filepath.Join(sibling, "child.txt"), filepath.Join(sibling, "root.txt")))
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}

	store = openStore(t, database)
	_, root, runtime = openPromptRuntime(t, store, rootID, client)
	if mode, err := store.PermissionMode(t.Context(), rootID); err != nil || mode != session.PermissionModeAutomatic {
		t.Fatalf("reopened permission mode=%s error=%v", mode, err)
	}
	child = runtime.agents[childID]
	if child == nil {
		t.Fatal("restart lost the default child")
	}
	for _, node := range []*AgentSession{runtime.rootNode, child} {
		assertFilesystemCell(t, node, fmt.Sprintf(`"child-before-restart" in files.read(path=%q)["output"] and "root.txt" in files.list(path=%q)["output"]`, filepath.Join(sibling, "child.txt"), sibling))
	}
	assertFilesystemCell(t, child, fmt.Sprintf(`files.write(path=%q, content="child-after-restart")
True`, filepath.Join(sibling, "child.txt")))
	if content, err := os.ReadFile(filepath.Join(sibling, "child.txt")); err != nil || string(content) != "child-after-restart" {
		t.Fatalf("retained child sibling mutation=%q error=%v", content, err)
	}
	setFilesystemMode(t, root, "ask-after-restart", true)
	for _, node := range []*AgentSession{runtime.rootNode, child} {
		deniedFilesystemCell(t, node, fmt.Sprintf(`files.read(path=%q)`, filepath.Join(sibling, "seed.txt")))
		assertFilesystemCell(t, node, fmt.Sprintf(`"inside-project" in files.read(path=%q)["output"]`, filepath.Join(project, "inside.txt")))
	}
}

func TestFilesystemAccessOutsideCWDCanDowngradeAndNavigateBack(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	base := canonicalPromptDirectory(t, t.TempDir())
	project, sibling := filepath.Join(base, "project"), filepath.Join(base, "sibling")
	writeDaemonPromptFile(t, filepath.Join(project, "inside.txt"), "inside-project")
	writeDaemonPromptFile(t, filepath.Join(sibling, "outside.txt"), "outside-project")
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID, err := store.Create(session.SessionKindAgent, project, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	_, root, runtime := openPromptRuntime(t, store, rootID, llm.New("http://unused.invalid", ""))
	setFilesystemMode(t, root, "full-access", false)
	change := clientCommand(t, root, "filesystem-test", "outside-cwd", "workspace.set", map[string]any{"path": sibling})
	if change.Status != "succeeded" {
		t.Fatalf("Full Access outside cwd=%+v", change)
	}
	assertFilesystemCell(t, runtime.rootNode, fmt.Sprintf(`files.write(path="relative.txt", content="relative-sibling")
%q in shell.run(command="pwd")["output"]`, sibling))
	if content, err := os.ReadFile(filepath.Join(sibling, "relative.txt")); err != nil || string(content) != "relative-sibling" {
		t.Fatalf("relative path did not follow cwd: %q error=%v", content, err)
	}
	setFilesystemMode(t, root, "ask-outside-cwd", true)
	meta, _, err := store.Load(rootID)
	if err != nil || meta.CWD != sibling {
		t.Fatalf("downgrade moved cwd: %s error=%v", meta.CWD, err)
	}
	deniedFilesystemCell(t, runtime.rootNode, `files.read(path="outside.txt")`)
	assertFilesystemCell(t, runtime.rootNode, fmt.Sprintf(`"inside-project" in files.read(path=%q)["output"]`, filepath.Join(project, "inside.txt")))
	change = clientCommand(t, root, "filesystem-test", "return-cwd", "workspace.set", map[string]any{"path": project})
	if change.Status != "succeeded" {
		t.Fatalf("Ask could not navigate back from outside cwd: %+v", change)
	}
	assertFilesystemCell(t, runtime.rootNode, `"inside-project" in files.read(path="inside.txt")["output"]`)
}

func setFilesystemMode(t *testing.T, root *Session, commandID string, external bool) {
	t.Helper()
	result := clientCommand(t, root, "filesystem-test", commandID, "permission.mode", map[string]bool{"external_permissions": external})
	if result.Status != "succeeded" {
		t.Fatalf("permission mode command=%+v", result)
	}
}

func assertFilesystemCell(t *testing.T, node *AgentSession, code string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := node.kernel.Exec(ctx, code)
	if err != nil || result.Value != true {
		t.Fatalf("filesystem cell for %s: result=%+v error=%v", node.id, result, err)
	}
}

func deniedFilesystemCell(t *testing.T, node *AgentSession, code string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err := node.kernel.Exec(ctx, code)
	if err == nil || !strings.Contains(err.Error(), "capability denied") {
		t.Fatalf("out-of-scope filesystem cell for %s: %v", node.id, err)
	}
}
