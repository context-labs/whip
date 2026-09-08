package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/daemon"
)

const desktopBinaryLimit = 512 << 20

type desktopSyncOptions struct {
	executable string
	expected   string
	digest     string
	interrupt  bool
}

type desktopSyncResult struct {
	State      string `json:"state"`
	BuildID    string `json:"buildId"`
	Executable string `json:"executable"`
}

func desktopRuntimeSyncCLI(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("_desktop-runtime-sync", flag.ContinueOnError)
	var options desktopSyncOptions
	flags.StringVar(&options.executable, "executable", "", "canonical executable")
	flags.StringVar(&options.expected, "expected-sha256", "", "previously installed digest")
	flags.StringVar(&options.digest, "sha256", "", "bundled payload digest")
	flags.BoolVar(&options.interrupt, "interrupt", false, "apply the approved backend restart")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || buildinfo.Name != "whipcode" || buildinfo.UpdateOwner != "desktop" {
		return errors.New("backend synchronization requires a desktop-supplied whipcode build")
	}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signals, 40*time.Second)
	defer cancel()
	source, err := os.Executable()
	if err != nil {
		return err
	}
	result, err := syncDesktopRuntime(ctx, source, options)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}

func syncDesktopRuntime(ctx context.Context, source string, options desktopSyncOptions) (desktopSyncResult, error) {
	result := desktopSyncResult{BuildID: version, Executable: options.executable}
	if !filepath.IsAbs(options.executable) || strings.ContainsAny(options.executable, "\x00\r\n") || len(options.executable) > 2048 {
		return result, errors.New("choose an absolute canonical executable path")
	}
	for _, digest := range []string{options.digest, options.expected} {
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size || digest != strings.ToLower(digest) {
			return result, errors.New("invalid backend payload digest")
		}
	}
	parent := filepath.Dir(options.executable)
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() {
		return result, errors.New("canonical executable directory must be an existing writable directory, not a symlink")
	}
	staged, err := stageDesktopRuntime(ctx, source, parent, options.digest)
	if err != nil {
		return result, err
	}
	defer func() { _ = os.Remove(staged) }()
	paths, err := daemonRuntimePaths()
	if err != nil {
		return result, err
	}
	maintenance, err := daemon.AcquireMaintenance(paths)
	if err != nil {
		return result, err
	}
	defer func() { _ = maintenance.Close() }()
	actual, err := desktopBinaryDigest(options.executable)
	if err != nil {
		return result, fmt.Errorf("inspect canonical executable: %w", err)
	}
	if actual != options.expected && actual != options.digest {
		return result, errors.New("the installed executable changed outside this update; choose or reinstall it explicitly")
	}
	status, client := probeDaemon(paths, time.Second)
	if client != nil {
		_ = client.Close()
	}
	if actual == options.digest && status.State == "running" && status.BuildMatch {
		result.State = "ready"
		return result, nil
	}
	_, owned, err := daemon.ActiveOwnerPID(paths.Lock)
	if err != nil {
		return result, err
	}
	if owned && !options.interrupt {
		result.State = "approval-required"
		return result, nil
	}
	if err := stopDesktopOwner(ctx, paths); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if actual != options.digest {
		// The source was copied, verified and fsynced before stopping any work.
		// Every supported starter/updater is excluded until readiness below.
		if err := os.Rename(staged, options.executable); err != nil {
			return result, fmt.Errorf("replace canonical executable: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := daemon.LaunchInstalledDaemon(paths, options.executable, maintenance); err != nil {
		return result, fmt.Errorf("start updated backend: %w", err)
	}
	for {
		status, client := probeDaemon(paths, 250*time.Millisecond)
		if client != nil {
			_ = client.Close()
			if !status.BuildMatch {
				return result, errors.New("updated backend reported an unexpected build")
			}
			result.State = "ready"
			return result, nil
		}
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("updated backend did not become ready; inspect %s and retry: %w", status.Log, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func desktopBinaryDigest(name string) (string, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > desktopBinaryLimit {
		return "", errors.New("backend must be a bounded regular file, not a symlink")
	}
	file, err := os.Open(name) //nolint:gosec // explicit canonical/source path validated above.
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, desktopBinaryLimit+1))
	if err != nil || n != info.Size() {
		return "", errors.New("backend changed while hashing")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func stageDesktopRuntime(ctx context.Context, source, parent, expected string) (string, error) {
	actual, err := desktopBinaryDigest(source)
	if err != nil || actual != expected {
		return "", errors.New("bundled backend failed integrity verification")
	}
	input, err := os.Open(source) //nolint:gosec // verified bundled executable.
	if err != nil {
		return "", err
	}
	defer func() { _ = input.Close() }()
	file, err := os.CreateTemp(parent, ".whipcode-update-*")
	if err != nil {
		return "", fmt.Errorf("stage backend before shutdown: %w", err)
	}
	name := file.Name()
	defer func() { _ = file.Close() }()
	success := false
	defer func() {
		if !success {
			_ = os.Remove(name)
		}
	}()
	if _, err := io.Copy(file, io.LimitReader(input, desktopBinaryLimit+1)); err != nil {
		return "", err
	}
	if err := file.Chmod(0o755); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if actual, err := desktopBinaryDigest(name); err != nil || actual != expected {
		return "", errors.New("staged backend failed integrity verification")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	success = true
	return name, nil
}

func stopDesktopOwner(ctx context.Context, paths daemon.RuntimePaths) error {
	pid, owned, err := daemon.ActiveOwnerPID(paths.Lock)
	if err != nil || !owned {
		return err
	}
	if pid == os.Getpid() {
		return errors.New("refusing to stop the updater itself")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	// SIGTERM follows the daemon's normal graceful shutdown even when the new
	// release changes the application protocol. Never escalate to SIGKILL here.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("stop backend owner: %w", err)
	}
	for {
		_, owned, err := daemon.ActiveOwnerPID(paths.Lock)
		if err != nil || !owned {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("backend is still stopping; no binary was replaced: %w", ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}
