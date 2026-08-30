package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type worktreeRunner func(context.Context, string, ...string) ([]byte, error)

// gitWorktreeRoot returns the repo root for dir, or "" when dir is not inside
// a git work tree. Used to decide whether subagent worktree isolation applies.
func gitWorktreeRoot(ctx context.Context, dir string, run worktreeRunner) string {
	out, err := run(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
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
func provisionSubagentWorktree(ctx context.Context, taskID, workspaceRoot string, run worktreeRunner) (path string, err error) {
	root := gitWorktreeRoot(ctx, workspaceRoot, run)
	if root == "" {
		return "", errors.New("not inside a git work tree")
	}
	canonicalWD, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return "", err
	}
	if filepath.Clean(canonicalWD) != filepath.Clean(root) {
		return "", errors.New("worktree isolation requires the session workspace to be the repository root")
	}
	// Keep the checkout inside the authorized workspace without making it
	// appear as an untracked directory in the parent checkout.
	branch := "subagent/" + taskID
	path = filepath.Join(root, ".git", "whip-worktrees", taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if out, err := run(ctx, "git", "-C", root, "worktree", "add", "-b", branch, path); err != nil {
		return "", fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return path, nil
}
