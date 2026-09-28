// Package workspace owns bounded Git subprocesses and tracked-path overlays.
// It owns no database, transcript, execution checkpoint or permission records.
package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/session"
	"golang.org/x/sys/unix"
)

const (
	ActionTimeout  = 30 * time.Second
	maxOutputBytes = 256 << 10
)

var (
	ErrChanged     = errors.New("workspace identity or snapshot pin changed")
	ErrOutputLimit = errors.New("Git output exceeds the workspace limit")
)

type Git struct {
	processes *capability.ProcessManager
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeErr  error
	root      string
}

func New(root string) *Git {
	ctx, cancel := context.WithCancel(context.Background())
	return &Git{processes: capability.NewProcessManager(), ctx: ctx, cancel: cancel, root: root}
}

// Context bounds one workflow and lets Close cancel lock waits as well as Git.
func (g *Git) Context(ctx context.Context) (context.Context, context.CancelFunc) {
	child, cancel := context.WithTimeout(ctx, ActionTimeout)
	stop := context.AfterFunc(g.ctx, cancel)
	if g.ctx.Err() != nil {
		cancel()
	}
	return child, func() { stop(); cancel() }
}

func (g *Git) Close() error {
	g.closeOnce.Do(func() { g.cancel(); g.closeErr = g.processes.Close() })
	return g.closeErr
}

type boundedOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > maxOutputBytes-w.buffer.Len() {
		w.overflow = true
		return 0, ErrOutputLimit
	}
	return w.buffer.Write(p)
}

func (g *Git) run(ctx context.Context, cwd string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, ActionTimeout)
	defer cancel()
	if len(args) > 16 {
		return "", session.ErrInvalid
	}
	for _, arg := range args {
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return "", session.ErrInvalid
		}
	}
	var stdout, stderr boundedOutput
	// Never invoke shell parsing, hooks, signing, paging or inherited Git env.
	prefix := []string{"--no-pager", "--literal-pathspecs", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgSign=false", "-c", "user.name=Whip", "-c", "user.email=workspace@localhost"}
	process, err := g.processes.Start(ctx, g.root, "git", append(prefix, args...), capability.ProcessOptions{Cwd: cwd, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr})
	if err == nil {
		err = process.Wait()
	}
	if stdout.overflow || stderr.overflow {
		return "", ErrOutputLimit
	}
	if ctx.Err() != nil {
		return "", errors.Join(ctx.Err(), err)
	}
	if err != nil {
		return "", fmt.Errorf("Git workspace command failed: %w", err)
	}
	return strings.TrimSuffix(stdout.buffer.String(), "\n"), nil
}

func canonicalDirectory(path string) (string, string, error) {
	if !filepath.IsAbs(path) {
		return "", "", session.ErrInvalid
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok {
		return "", "", session.ErrInvalid
	}
	return filepath.Clean(canonical), fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}

// Inspect derives authority only from a retained session directory. Bare repos
// and changes of worktree, linked gitdir, common dir or scope inode are rejected.
func (g *Git) Inspect(ctx context.Context, cwd string) (binding session.WorkspaceBinding, err error) {
	cwd, binding.ScopeIdentity, err = canonicalDirectory(cwd)
	if err != nil {
		return binding, err
	}
	bare, err := g.run(ctx, cwd, "rev-parse", "--is-bare-repository")
	if err != nil {
		return binding, err
	}
	if bare != "false" {
		return binding, fmt.Errorf("%w: workspace requires a non-bare Git worktree", session.ErrInvalid)
	}
	root, err := g.run(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return binding, err
	}
	binding.Worktree, binding.WorktreeIdentity, err = canonicalDirectory(root)
	if err != nil {
		return binding, err
	}
	gitDir, err := g.run(ctx, cwd, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return binding, err
	}
	binding.GitDirectory, binding.GitIdentity, err = canonicalDirectory(gitDir)
	if err != nil {
		return binding, err
	}
	common, err := g.run(ctx, cwd, "rev-parse", "--git-common-dir")
	if err != nil {
		return binding, err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(cwd, common)
	}
	binding.CommonDirectory, binding.CommonIdentity, err = canonicalDirectory(common)
	if err != nil {
		return binding, err
	}
	binding.Scope, err = filepath.Rel(binding.Worktree, cwd)
	if err != nil {
		return binding, err
	}
	return binding, binding.Validate()
}

// Lock serializes this adapter across runtime processes sharing a worktree.
// It does not lock ordinary file/shell tools, editors, or other Git clients.
func (g *Git) Lock(ctx context.Context, binding session.WorkspaceBinding) (func(), error) {
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	if err := g.check(ctx, binding); err != nil {
		return nil, err
	}
	path := filepath.Join(binding.GitDirectory, "whip-workspace.lock")
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	locked := false
	defer func() {
		if !locked {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || int64(stat.Uid) != int64(os.Getuid()) {
		return nil, ErrChanged
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	currentLock, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, currentLock) {
		return nil, errors.Join(ErrChanged, err)
	}
	if err := g.check(ctx, binding); err != nil {
		return nil, err
	}
	locked = true
	return sync.OnceFunc(func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = file.Close() }), nil
}

func (g *Git) check(ctx context.Context, binding session.WorkspaceBinding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	current, err := g.Inspect(ctx, filepath.Join(binding.Worktree, binding.Scope))
	if err != nil {
		return errors.Join(ErrChanged, err)
	}
	if current != binding {
		return ErrChanged
	}
	return nil
}

func (g *Git) pin(id session.WorkspaceSnapshotID) string {
	digest := sha256.Sum256([]byte(g.root + "\x00" + string(id)))
	return "refs/whip/workspace/" + hex.EncodeToString(digest[:])
}

// Capture writes only unreachable Git objects. The caller persists the returned
// identity before calling Pin, so a lost pin acknowledgement stays releasable.
func (g *Git) Capture(ctx context.Context, binding session.WorkspaceBinding) (string, error) {
	if err := g.check(ctx, binding); err != nil {
		return "", err
	}
	object, err := g.run(ctx, binding.Worktree, "stash", "create")
	if err != nil {
		return "", err
	}
	if object == "" {
		object, err = g.run(ctx, binding.Worktree, "commit-tree", "HEAD^{tree}", "-m", "Whip workspace snapshot")
	}
	if err != nil {
		return "", err
	}
	if err := session.ValidateWorkspaceObject(object); err != nil {
		return "", err
	}
	return object, nil
}

func (g *Git) Pin(ctx context.Context, value session.WorkspaceSnapshot) error {
	if value.ObjectID == nil || session.ValidateWorkspaceObject(*value.ObjectID) != nil {
		return session.ErrInvalid
	}
	if err := g.check(ctx, value.Binding); err != nil {
		return err
	}
	_, err := g.run(ctx, value.Binding.Worktree, "update-ref", g.pin(value.ID), *value.ObjectID, strings.Repeat("0", len(*value.ObjectID)))
	return err
}

func (g *Git) readPin(ctx context.Context, value session.WorkspaceSnapshot) (string, error) {
	// show-ref --verify exits nonzero for a missing ref. rev-parse with --verify
	// has the same ambiguity; for-each-ref gives an empty successful projection.
	ref := g.pin(value.ID)
	raw, err := g.run(ctx, value.Binding.Worktree, "for-each-ref", "--format=%(refname)%09%(objectname)", ref)
	if err != nil {
		return "", err
	}
	if raw == "" {
		return "", nil
	}
	name, object, found := strings.Cut(raw, "\t")
	if !found || name != ref || session.ValidateWorkspaceObject(object) != nil {
		return "", ErrChanged
	}
	return object, nil
}

func (g *Git) ValidatePin(ctx context.Context, value session.WorkspaceSnapshot, allowMissing bool) error {
	if err := g.check(ctx, value.Binding); err != nil {
		return err
	}
	object, err := g.readPin(ctx, value)
	if err != nil {
		return err
	}
	if object == "" && allowMissing {
		return nil
	}
	if value.ObjectID == nil || object != *value.ObjectID {
		return ErrChanged
	}
	return nil
}

func (g *Git) Restore(ctx context.Context, value session.WorkspaceSnapshot) error {
	if err := g.ValidatePin(ctx, value, false); err != nil {
		return err
	}
	_, err := g.run(ctx, value.Binding.Worktree, "checkout", *value.ObjectID, "--", value.Binding.Scope)
	return err
}

func (g *Git) Release(ctx context.Context, value session.WorkspaceSnapshot) error {
	if err := g.ValidatePin(ctx, value, true); err != nil {
		return err
	}
	object, err := g.readPin(ctx, value)
	if err != nil {
		return err
	}
	if object == "" {
		return nil
	}
	if value.ObjectID == nil || object != *value.ObjectID {
		return ErrChanged
	}
	_, err = g.run(ctx, value.Binding.Worktree, "update-ref", "-d", g.pin(value.ID), *value.ObjectID)
	return err
}

var _ io.Writer = (*boundedOutput)(nil)
