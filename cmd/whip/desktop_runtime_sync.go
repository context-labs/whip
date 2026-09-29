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
	"github.com/context-labs/whip/internal/localruntime"
	"golang.org/x/sys/unix"
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
	if flags.NArg() != 0 || buildinfo.UpdateOwner != "desktop" {
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
	if err := ctx.Err(); err != nil {
		return result, err
	}
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
	paths, err := nativeRuntimePaths()
	if err != nil {
		return result, err
	}
	launch, err := nativeRuntimeLaunch()
	if err != nil {
		return result, err
	}
	launch.Executable = options.executable
	maintenance, err := localruntime.AcquireMaintenance(ctx, paths)
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
	status := localruntime.Inspect(ctx, paths)
	if status.State == "unhealthy" {
		return result, fmt.Errorf("cannot update an unverified native runtime: %s", status.Error)
	}
	if actual == options.digest && status.Process != nil && status.Process.Build == version {
		result.State = "ready"
		return result, nil
	}
	if status.Process != nil && !options.interrupt {
		result.State = "approval-required"
		return result, nil
	}
	if status.Process != nil && status.Process.PID == os.Getpid() {
		return result, errors.New("refusing to stop the updater itself")
	}
	if status.Process != nil {
		if err := maintenance.Stop(ctx, *status.Process); err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if current := localruntime.Inspect(ctx, paths); current.State != "stopped" {
		return result, errors.New("native runtime owner changed while stopping; no replacement was made")
	}
	if current, err := desktopBinaryDigest(options.executable); err != nil || current != actual {
		return result, errors.New("canonical executable changed while stopping; no replacement was made")
	}
	if actual != options.digest {
		// Payload integrity and launch exclusion are established before shutdown.
		if err := os.Rename(staged, options.executable); err != nil {
			return result, fmt.Errorf("replace canonical executable: %w", err)
		}
		fd, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return result, err
		}
		directory := os.NewFile(uintptr(fd), parent)
		if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	status, err = maintenance.Start(ctx, launch)
	if err != nil {
		return result, fmt.Errorf("updated backend readiness is unconfirmed; inspect %s and retry explicitly: %w", paths.Log, err)
	}
	if status.Process == nil || status.Process.Build != version {
		return result, errors.New("updated backend reported an unexpected build")
	}
	result.State = "ready"
	return result, nil
}

func openDesktopBinary(name string) (*os.File, os.FileInfo, error) {
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > desktopBinaryLimit {
		_ = file.Close()
		return nil, nil, errors.Join(err, errors.New("backend must be a bounded regular file, not a symlink"))
	}
	return file, info, nil
}

func desktopBinaryDigest(name string) (string, error) {
	file, info, err := openDesktopBinary(name)
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
	input, _, err := openDesktopBinary(source)
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
