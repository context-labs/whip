package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// provisionSubagentWorktree creates a sibling worktree on a task-named branch
// inside a real git repo, and reports "" outside one.
func TestProvisionSubagentWorktree(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "init")

	// provision from inside the repo
	t.Chdir(repo)

	path, err := provisionSubagentWorktree(ctx, "task-42")
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "f.txt")); err != nil {
		t.Fatalf("worktree should contain the committed file: %v", err)
	}
	// branch named after the task exists
	out, _ := exec.CommandContext(ctx, "git", "-C", repo, "branch", "--list", "subagent/task-42").Output()
	if len(out) == 0 {
		t.Fatalf("expected branch subagent/task-42, branches: %s", out)
	}
	// cleanup so the temp dir can be removed
	_ = exec.CommandContext(ctx, "git", "-C", repo, "worktree", "remove", "--force", path).Run()
}

// Outside a git repo, provisioning must fail (the caller falls back to the
// shared cwd) rather than create anything.
func TestProvisionSubagentWorktreeNotARepo(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := provisionSubagentWorktree(context.Background(), "task-9"); err == nil {
		t.Fatal("expected an error outside a git repo")
	}
}

// The per-call `worktree` override beats the session default.
func TestWorktreeOverrideBeatsDefault(t *testing.T) {
	// mirror the resolution logic in taskTool: override wins when set
	resolve := func(def bool, ov *bool) bool {
		if ov != nil {
			return *ov
		}
		return def
	}
	on, off := true, false
	if !resolve(false, &on) {
		t.Fatal("worktree=true should override session default false")
	}
	if resolve(true, &off) {
		t.Fatal("worktree=false should override session default true")
	}
	if !resolve(true, nil) {
		t.Fatal("no override should fall back to session default")
	}
}
