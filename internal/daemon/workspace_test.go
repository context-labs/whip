package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

func TestAgentRunnerWorkspaceSnapshotsUseRootManagedProcesses(t *testing.T) {
	repo := t.TempDir()
	daemonGit(t, repo, "init", "-q")
	daemonGit(t, repo, "config", "user.email", "test@example.com")
	daemonGit(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	daemonGit(t, repo, "add", "tracked.txt")
	daemonGit(t, repo, "commit", "-qm", "base")

	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	t.Cleanup(func() { _ = store.Close() })
	rootID, err := store.Create(repo, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureClassicAuthority(t.Context(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	services := tools.NewServices()
	if err := services.BindDispatcher(store, store.Workspaces(), store.Processes(), authority); err != nil {
		t.Fatal(err)
	}
	agentValue := agent.New(llm.New("http://unused.invalid", ""), "model", 100, "system")
	agentValue.Services = services
	agentValue.WorkingDir = repo
	runner := &agentRunner{agent: agentValue}

	ref := runner.CaptureWorkspace(t.Context())
	if ref == "" {
		t.Fatal("clean repository did not produce a pinned pre-turn snapshot")
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("agent edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runner.WorkspaceClean(t.Context()) {
		t.Fatal("tracked edit reported as clean")
	}
	restored, err := runner.RestoreWorkspace(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if restored != 1 {
		t.Fatalf("restored files = %d, want 1", restored)
	}
	body, _ := os.ReadFile(filepath.Join(repo, "tracked.txt"))
	if string(body) != "base\n" {
		t.Fatalf("tracked file = %q", body)
	}
	if _, err := os.Stat(filepath.Join(repo, "untracked.txt")); err != nil {
		t.Fatal("workspace rewind removed an untracked user file")
	}
	if output := daemonGit(t, repo, "show-ref", workspaceSnapshotRefPrefix+ref); output != "" {
		t.Fatalf("snapshot pin survived restore: %s", output)
	}
}

func TestWorkspaceSnapshotsIgnoreNonGitDirectoriesAndInvalidReferences(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID, err := store.Create(t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureClassicAuthority(t.Context(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	services := tools.NewServices()
	if err := services.BindDispatcher(store, store.Workspaces(), store.Processes(), authority); err != nil {
		t.Fatal(err)
	}
	meta, _, err := store.Load(rootID)
	if err != nil {
		t.Fatal(err)
	}
	runner := &agentRunner{agent: agent.NewWithServices(llm.New("http://unused.invalid", ""), "model", 100, "system", services)}
	runner.agent.WorkingDir = meta.CWD
	if ref := runner.CaptureWorkspace(t.Context()); ref != "" {
		t.Fatalf("non-git snapshot ref = %q", ref)
	}
	if runner.WorkspaceClean(t.Context()) {
		t.Fatal("non-git workspace reported git-clean")
	}
	if restored, err := runner.RestoreWorkspace(t.Context(), "missing"); err == nil || restored != 0 {
		t.Fatalf("invalid restore = %d, %v", restored, err)
	}
	runner.DropWorkspaceSnapshot(t.Context(), "")
}

func daemonGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(context.Background(), "git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		// show-ref exits 1 when the requested ref is absent.
		if len(args) == 2 && args[0] == "show-ref" && len(output) == 0 {
			return ""
		}
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
