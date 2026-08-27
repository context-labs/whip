package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitWorktreeRoot returns the repo root for dir, or "" when dir is not inside
// a git work tree. Used to decide whether subagent worktree isolation applies.
func gitWorktreeRoot(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// provisionSubagentWorktree creates a throwaway git worktree branched off the
// current HEAD and returns its path ("" when not in a repo / git missing).
// ponytail: this branches off HEAD, not the pushed base — a subagent editing
// on top of the parent's uncommitted state is the common case and rebasing
// onto origin/main here would surprise. The branch name is unique per task.
//
// Cleanup is the caller's/orchestrator's job: whip leaves the worktree on
// disk so the user can inspect or merge the subagent's commit afterward.
func provisionSubagentWorktree(ctx context.Context, taskID string) (path string, err error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root := gitWorktreeRoot(ctx, wd)
	if root == "" {
		return "", errors.New("not inside a git work tree")
	}
	// ponytail: worktrees live as siblings of the repo (../<repo>-wt-<task>)
	// rather than under .git/ so `git status` in the parent stays clean and
	// the path is greppable. Branch name doubles as the dir suffix.
	branch := "subagent/" + taskID
	dirName := filepath.Base(root) + "-wt-" + taskID
	path = filepath.Join(filepath.Dir(root), dirName)
	cmd := exec.CommandContext(ctx, "git", "-C", root, "worktree", "add", "-b", branch, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return path, nil
}
