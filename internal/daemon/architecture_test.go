package daemon

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate architecture test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func productionGoFiles(t *testing.T, directory string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestArchitectureKeepsProviderCallsBehindAgentSession(t *testing.T) {
	root := repositoryRoot(t)
	for path, body := range productionGoFiles(t, filepath.Join(root, "internal", "daemon")) {
		if filepath.Base(path) == "agent_session.go" {
			continue
		}
		for _, call := range []string{".Complete(", ".Stream("} {
			if strings.Contains(body, call) {
				t.Errorf("model provider call %q escaped AgentSession: %s", call, path)
			}
		}
	}
}

func TestArchitectureKeepsTUIAsDaemonClient(t *testing.T) {
	root := repositoryRoot(t)
	for path, body := range productionGoFiles(t, filepath.Join(root, "internal", "tui")) {
		for _, forbidden := range []string{
			`github.com/context-labs/whip/internal/agent`,
			`github.com/context-labs/whip/internal/tools`,
			`github.com/context-labs/whip/internal/mcp`,
			"agent.New(", "agent.NewRuntime(", "llm.New(", "session.Open(", "tools.NewServices(", "mcp.NewManager(",
			"config.SaveCatalogs(",
		} {
			if strings.Contains(body, forbidden) {
				t.Errorf("TUI constructs runtime object %q in %s", forbidden, path)
			}
		}
		if strings.Contains(body, "client == nil") || strings.Contains(body, "client != nil") {
			t.Errorf("TUI contains an embedded-runtime client fallback in %s", path)
		}
	}
}

func TestArchitectureContainsNoClassicRuntimeSurface(t *testing.T) {
	root := repositoryRoot(t)
	files := productionGoFiles(t, root)
	for path, body := range files {
		for _, forbidden := range []string{
			"TaskDefault", "WorktreeSubagents", "worktreeSubagents", "taskModel", "taskProvider",
			"ChildAdmission", "AdmitChild", "StartChildTurn", "FinishChildTurn",
			"CREATE TABLE tasks", "CREATE TABLE child_executions", "task_id",
		} {
			if strings.Contains(body, forbidden) {
				t.Errorf("classic runtime symbol %q remains in %s", forbidden, path)
			}
		}
	}
	for _, removed := range []string{
		"internal/agent/background.go", "internal/agent/subagent.go", "internal/agent/wait.go", "internal/agent/worktree.go",
		"internal/daemon/swarm.go", "internal/tui/tasks.go", "internal/tui/taskmodel.go",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(removed))); !os.IsNotExist(err) {
			t.Errorf("classic runtime file remains: %s (stat error %v)", removed, err)
		}
	}
}
