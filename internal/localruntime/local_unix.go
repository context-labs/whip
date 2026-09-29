//go:build unix

// Package localruntime selects and controls a native runtime process. It only
// knows private filesystem paths and the public protocol, never session storage.
package localruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"golang.org/x/sys/unix"
)

const Namespace = "runtime-v4"

type Paths struct{ Directory, Socket, Lock, Log string }

// Resolve never creates a directory or reads the retired configuration/store.
func Resolve(home string) (Paths, error) {
	if home == "" {
		return Paths{}, errors.New("runtime home is required")
	}
	directory, err := filepath.Abs(filepath.Join(home, Namespace))
	if err != nil {
		return Paths{}, err
	}
	paths := Paths{Directory: directory, Socket: filepath.Join(directory, "runtime.sock"), Lock: filepath.Join(directory, "runtime.lock"), Log: filepath.Join(directory, "runtime.log")}
	if len(paths.Socket) > 100 {
		return Paths{}, errors.New("native runtime socket path exceeds 100 bytes")
	}
	return paths, nil
}

type Status struct {
	State     string               `json:"state"`
	Process   *protocol.HostStatus `json:"process"`
	Socket    string               `json:"socket"`
	Directory string               `json:"directory"`
	Log       string               `json:"log"`
	Error     string               `json:"error,omitempty"`
}

func Inspect(ctx context.Context, paths Paths) Status {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result := Status{State: "stopped", Socket: paths.Socket, Directory: paths.Directory, Log: paths.Log}
	if err := validatePaths(paths); err != nil {
		result.State, result.Error = "unhealthy", err.Error()
		return result
	}
	if err := validateDirectory(paths.Directory); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			result.State, result.Error = "unhealthy", err.Error()
		}
		return result
	}
	c, err := client.Connect(ctx, paths.Socket, nil)
	if err == nil {
		defer func() { _ = c.Close() }()
		var status protocol.HostStatus
		err = c.Call(ctx, "host.status", protocol.EmptyParams{}, &status)
		if err == nil && status.RuntimeID != c.Identity() {
			err = errors.New("host status identity mismatch")
		}
		if err == nil {
			result.State, result.Process = "running", &status
			return result
		}
	}
	owned, ownerErr := isOwned(paths.Lock)
	if ownerErr != nil || owned || !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
		result.State = "unhealthy"
		result.Error = errors.Join(err, ownerErr).Error()
	}
	return result
}

type Launch struct {
	Executable string
	Arguments  []string
	Build      string
}

// Start explicitly launches detached host work. Cancelling this readiness wait
// does not terminate the independently owned runtime. It never replaces a live
// process or retries a launch after an uncertain result. Call Inspect to recover.
func Start(ctx context.Context, paths Paths, launch Launch) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := validateLaunch(launch); err != nil {
		return Status{}, err
	}
	maintenance, err := AcquireMaintenance(ctx, paths)
	if err != nil {
		return Status{}, err
	}
	defer func() { _ = maintenance.Close() }()
	return maintenance.Start(ctx, launch)
}

func validateLaunch(launch Launch) error {
	if !filepath.IsAbs(launch.Executable) || len(launch.Arguments) > 64 || len(launch.Build) > 256 {
		return errors.New("invalid runtime launch")
	}
	size := 0
	for _, arg := range launch.Arguments {
		size += len(arg)
	}
	if size > 32<<10 {
		return errors.New("runtime arguments exceed 32 KiB")
	}
	return nil
}

// Maintenance excludes other supported starters while a staged replacement is
// inspected and activated. It grants no authority to stop an unverifiable host.
// Close releases the launch lease; concurrent Close waits for Start to settle.
type Maintenance struct {
	mu    sync.Mutex
	paths Paths
	file  *os.File
}

func AcquireMaintenance(ctx context.Context, paths Paths) (*Maintenance, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validatePaths(paths); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(paths.Directory, 0o700); err != nil {
		return nil, err
	}
	if err := validateDirectory(paths.Directory); err != nil {
		return nil, err
	}
	lock, err := privateFile(filepath.Join(paths.Directory, "launch.lock"), unix.O_CREAT|unix.O_RDWR)
	if err != nil {
		return nil, err
	}
	locked := false
	defer func() {
		if !locked {
			_ = lock.Close()
		}
	}()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
	locked = true
	return &Maintenance{paths: paths, file: lock}, nil
}

func (m *Maintenance) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.file == nil {
		return nil
	}
	err := m.file.Close()
	m.file = nil
	return err
}

// Stop stops exactly the previously inspected runtime epoch while retaining
// launch exclusion. A replacement owner must be observed and selected anew.
func (m *Maintenance) Stop(ctx context.Context, selected protocol.HostStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.file == nil {
		return errors.New("native runtime maintenance lease is closed")
	}
	_, err := stopSelected(ctx, m.paths, selected)
	return err
}

// Start uses the held launch lease. A failed readiness observation never
// terminates the detached process or launches a second process automatically.
func (m *Maintenance) Start(ctx context.Context, launch Launch) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.file == nil {
		return Status{}, errors.New("native runtime maintenance lease is closed")
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	if err := validateLaunch(launch); err != nil {
		return Status{}, err
	}
	paths := m.paths
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	status := Inspect(ctx, paths)
	if status.State == "running" {
		return status, nil
	}
	owned, err := isOwned(paths.Lock)
	if err != nil {
		return status, err
	}
	if status.State == "unhealthy" && !owned {
		return status, errors.New(status.Error)
	}
	if !owned {
		if err := ctx.Err(); err != nil {
			return status, err
		}
		log, err := privateFile(paths.Log, unix.O_CREAT|unix.O_WRONLY|unix.O_APPEND)
		if err != nil {
			return status, err
		}
		args := append([]string{}, launch.Arguments...)
		args = append(args, "-directory", paths.Directory, "-build", launch.Build)
		command := exec.CommandContext(context.Background(), launch.Executable, args...)
		command.Stdout, command.Stderr = log, log
		command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		err = command.Start()
		if err == nil {
			err = command.Process.Release()
		}
		err = errors.Join(err, log.Close())
		if err != nil {
			return status, fmt.Errorf("runtime launch: %w", err)
		}
	}
	for {
		status = Inspect(ctx, paths)
		if status.State == "running" {
			return status, nil
		}
		select {
		case <-ctx.Done():
			return status, fmt.Errorf("runtime readiness is unconfirmed; inspect %s: %w", paths.Log, ctx.Err())
		case <-ticker.C:
		}
	}
}

// Stop targets only the live epoch just observed. An unhealthy process has no
// verifiable protocol identity and is never signalled using a possibly reused PID.
func Stop(ctx context.Context, paths Paths) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	status := Inspect(ctx, paths)
	if status.State == "stopped" {
		return false, nil
	}
	if status.Process == nil {
		return false, fmt.Errorf("cannot safely stop an unverified native runtime: %s", status.Error)
	}
	return stopSelected(ctx, paths, *status.Process)
}

func stopSelected(ctx context.Context, paths Paths, selected protocol.HostStatus) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c, err := client.Connect(ctx, paths.Socket, &selected.RuntimeID)
	if err != nil {
		return true, err
	}
	defer func() { _ = c.Close() }()
	var result protocol.HostStopAccepted
	sendErr := c.Call(ctx, "host.stop", protocol.StopHostParams{RuntimeID: selected.RuntimeID, ProcessEpoch: selected.ProcessEpoch}, &result)
	if sendErr == nil && (result.RuntimeID != selected.RuntimeID || result.ProcessEpoch != selected.ProcessEpoch) {
		return true, errors.New("stop acknowledgement identity mismatch")
	}
	if _, ok := errors.AsType[*client.Error](sendErr); ok {
		return true, sendErr
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		owned, err := isOwned(paths.Lock)
		if err != nil {
			return true, err
		}
		if !owned {
			return true, nil
		}
		current := Inspect(ctx, paths)
		if current.Process != nil && current.Process.ProcessEpoch != selected.ProcessEpoch {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return true, errors.Join(sendErr, ctx.Err())
		case <-ticker.C:
		}
	}
}

func validateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !ok || int64(stat.Uid) != int64(os.Getuid()) {
		return errors.New("native runtime requires an owner-only directory without a symlink")
	}
	return nil
}

func privateFile(path string, flags int) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ok || int64(stat.Uid) != int64(os.Getuid()) {
		_ = file.Close()
		return nil, errors.New("native runtime file must be owner-only and regular")
	}
	return file, nil
}

func isOwned(path string) (bool, error) {
	file, err := privateFile(path, unix.O_RDWR)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func validatePaths(paths Paths) error {
	if !filepath.IsAbs(paths.Directory) || filepath.Clean(paths.Directory) != paths.Directory || filepath.Base(paths.Directory) != Namespace {
		return errors.New("invalid native runtime directory")
	}
	expected, err := Resolve(filepath.Dir(paths.Directory))
	if err != nil {
		return err
	}
	if paths != expected {
		return errors.New("native runtime path identity mismatch")
	}
	return nil
}
