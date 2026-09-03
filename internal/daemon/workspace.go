package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

const workspaceSnapshotRefPrefix = "refs/whip/snapshots/"

// workspaceSnapshotRunner is implemented by the production agent adapter.
// Keeping it behind the runner seam lets the actor remain independent of git
// while every subprocess still belongs to the daemon's root process group.
type workspaceSnapshotRunner interface {
	CaptureWorkspace(context.Context) string
	WorkspaceClean(context.Context) bool
	DropWorkspaceSnapshot(context.Context, string)
	RestoreWorkspace(context.Context, string) (int, error)
}

func (r *AgentSession) CaptureWorkspace(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	commit, err := r.workspaceGit(ctx, "stash", "create")
	if err != nil {
		return ""
	}
	if commit == "" {
		commit, err = r.workspaceGit(ctx, "commit-tree", "HEAD^{tree}", "-m", "whip turn snapshot")
		if err != nil {
			return ""
		}
	}
	if _, err := r.workspaceGit(ctx, "update-ref", workspaceSnapshotRefPrefix+commit, commit); err != nil {
		return ""
	}
	return commit
}

func (r *AgentSession) WorkspaceClean(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := r.workspaceGit(ctx, "status", "--porcelain", "--untracked-files=no")
	return err == nil && output == ""
}

func (r *AgentSession) DropWorkspaceSnapshot(ctx context.Context, ref string) {
	if ref == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, _ = r.workspaceGit(ctx, "update-ref", "-d", workspaceSnapshotRefPrefix+ref)
}

func (r *AgentSession) RestoreWorkspace(ctx context.Context, ref string) (int, error) {
	if ref == "" {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	dirty, err := r.workspaceGit(ctx, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return 0, err
	}
	if _, err := r.workspaceGit(ctx, "checkout", ref, "--", "."); err != nil {
		return 0, err
	}
	r.DropWorkspaceSnapshot(ctx, ref)
	if dirty == "" {
		return 0, nil
	}
	return len(strings.Split(dirty, "\n")), nil
}

func (r *AgentSession) workspaceGit(ctx context.Context, args ...string) (string, error) {
	if r.agent == nil || r.agent.Services == nil {
		return "", errors.New("agent services are required")
	}
	opts := r.agent.Services.ProcessOptions()
	if opts.Processes == nil || opts.RootID == "" {
		return "", errors.New("managed process authority is required")
	}
	cwd := r.agent.WorkingDir
	if cwd == "" {
		cwd = opts.Cwd
	}
	var stdout, stderr bytes.Buffer
	process, err := opts.Processes.Start(ctx, opts.RootID, "git", args, capability.ProcessOptions{
		Cwd: cwd, Env: opts.Env, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
	})
	if err == nil {
		err = process.Wait()
	}
	if err != nil {
		detail, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), detail)
	}
	return strings.TrimSpace(stdout.String()), nil
}
