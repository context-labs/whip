package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testWorktreeRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// provisionSubagentWorktree creates an authorized worktree on a task-named
// branch inside a real git repo, and reports "" outside one.
func TestProvisionSubagentWorktree(t *testing.T) {
	if os.Getenv("WHIP_SKIP_WORKTREE_TEST") == "1" {
		// The pre-commit hook already holds the repository index lock.
		t.Skip("skipped via WHIP_SKIP_WORKTREE_TEST")
	}
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

	path, err := provisionSubagentWorktree(ctx, "task-42", repo, testWorktreeRunner)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "f.txt")); err != nil {
		t.Fatalf("worktree should contain the committed file: %v", err)
	}
	canonicalRepo, _ := filepath.EvalSymlinks(repo)
	if rel, err := filepath.Rel(canonicalRepo, path); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("worktree path %q is outside repository %q", path, repo)
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
	dir := t.TempDir()
	if _, err := provisionSubagentWorktree(context.Background(), "task-9", dir, testWorktreeRunner); err == nil {
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

func TestWorktreeRunnerErrors(t *testing.T) {
	wantErr := errors.New("git failed")
	if root := gitWorktreeRoot(context.Background(), t.TempDir(), func(context.Context, string, ...string) ([]byte, error) {
		return []byte("ignored"), wantErr
	}); root != "" {
		t.Fatalf("gitWorktreeRoot = %q after runner error", root)
	}

	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	rootRunner := func(context.Context, string, ...string) ([]byte, error) { return []byte(canonicalRoot + "\n"), nil }
	if _, err := provisionSubagentWorktree(context.Background(), "nested", nested, rootRunner); err == nil || !strings.Contains(err.Error(), "repository root") {
		t.Fatalf("nested workspace error = %v", err)
	}

	calls := 0
	failingRunner := func(context.Context, string, ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte(canonicalRoot + "\n"), nil
		}
		return []byte("fatal: branch already exists\n"), wantErr
	}
	path, err := provisionSubagentWorktree(context.Background(), "duplicate", root, failingRunner)
	if path != "" || !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "branch already exists") {
		t.Fatalf("failed worktree add = %q, %v", path, err)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	missingRunner := func(context.Context, string, ...string) ([]byte, error) { return []byte(missing), nil }
	if _, err := provisionSubagentWorktree(context.Background(), "missing", missing, missingRunner); err == nil {
		t.Fatal("missing workspace did not fail canonicalization")
	}

	blocked := t.TempDir()
	if err := os.WriteFile(filepath.Join(blocked, ".git"), []byte("gitdir"), 0o600); err != nil {
		t.Fatal(err)
	}
	blockedRunner := func(context.Context, string, ...string) ([]byte, error) { return []byte(blocked), nil }
	if _, err := provisionSubagentWorktree(context.Background(), "blocked", blocked, blockedRunner); err == nil {
		t.Fatal("worktree parent creation unexpectedly succeeded through a file")
	}
}

func TestProvisionSubagentWorktreeRepairsParentPermissions(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".git", "whip-worktrees")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := func(context.Context, string, ...string) ([]byte, error) { return []byte(canonicalRoot + "\n"), nil }
	if _, err := provisionSubagentWorktree(context.Background(), "secure", root, runner); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("worktree parent permissions = %o, want 700", got)
	}
}
