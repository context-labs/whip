package config

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/context-labs/whip/internal/session"
)

func Load(directory string) (Host, error) {
	dir, err := openDirectory(directory, false)
	if err != nil {
		return Host{}, err
	}
	defer dir.Close()
	value, _, err := readSnapshot(dir)
	if err != nil {
		return Host{}, err
	}
	value.Host.Resources, err = session.ResolveResourceLimits(value.Host.Resources, nil)
	return value.Host, err
}

// Initialize publishes one complete default file, or loads the existing file.
// The directory is explicit; old filenames and installed runtimes are ignored.
func Initialize(directory string) (Host, error) {
	if err := write(directory, Default(), true); err != nil && !errors.Is(err, os.ErrExist) {
		return Host{}, err
	}
	return Load(directory)
}

// Save replaces a complete host declaration for bootstrap and explicit local
// configuration. Interactive updates use Authority.Update with a revision.
func Save(directory string, host Host) error { return write(directory, host, false) }

func write(directory string, host Host, onlyNew bool) error {
	raw, err := encodeHost(host)
	if err != nil {
		return err
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	dir, err := openDirectory(directory, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	return publishHost(context.Background(), dir, raw, onlyNew, (*os.File).Sync)
}

func openDirectory(path string, create bool) (*os.File, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: configuration directory is required", session.ErrInvalid)
	}
	path = filepath.Clean(path)
	if create {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, err
		}
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open configuration directory: %w", err)
	}
	directory := os.NewFile(uintptr(fd), path)
	info, err := directory.Stat()
	if err == nil && (!info.IsDir() || info.Mode().Perm()&0o022 != 0 || !owned(info)) {
		err = fmt.Errorf("%w: configuration directory must be owned without group or public write permission", session.ErrInvalid)
	}
	if err != nil {
		return nil, errors.Join(err, directory.Close())
	}
	return directory, nil
}

func owned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(stat.Uid) == int64(os.Getuid())
}

func openHost(directory *os.File) (*os.File, os.FileInfo, error) {
	fd, err := unix.Openat(int(directory.Fd()), FileName, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open host configuration: %w", err)
	}
	file := os.NewFile(uintptr(fd), FileName)
	info, err := file.Stat()
	// Existing readable 0400/0644 files remain valid inside the private runtime
	// directory. Publications use 0600; other users must never be able to edit.
	if err == nil && (!info.Mode().IsRegular() || !owned(info) || info.Mode().Perm()&0o022 != 0) {
		err = fmt.Errorf("%w: host configuration must be an owned regular file without group or public write permission", session.ErrInvalid)
	}
	if err != nil {
		return nil, nil, errors.Join(err, file.Close())
	}
	return file, info, nil
}

func readSnapshot(directory *os.File) (Snapshot, []byte, error) {
	file, info, err := openHost(directory)
	if err != nil {
		return Snapshot{}, nil, err
	}
	defer file.Close()
	if info.Size() > session.MaxDocumentBytes {
		return Snapshot{}, nil, fmt.Errorf("%w: host configuration exceeds size limit", session.ErrInvalid)
	}
	raw, err := io.ReadAll(io.LimitReader(file, session.MaxDocumentBytes+1))
	if err != nil {
		return Snapshot{}, nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return Snapshot{}, nil, err
	}
	if int64(len(raw)) != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return Snapshot{}, nil, ErrRevisionConflict
	}
	value, err := snapshot(raw)
	return value, raw, err
}

func publishHost(ctx context.Context, directory *os.File, raw []byte, onlyNew bool, syncDirectory func(*os.File) error) (err error) {
	// Never replace a symlink or special file. No contents are needed for Save;
	// Authority.Update separately requires a valid, current declaration.
	existing, _, err := openHost(directory)
	if err == nil {
		if err := existing.Close(); err != nil {
			return err
		}
		if onlyNew {
			return os.ErrExist
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	name := ".host-" + rand.Text()
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary host configuration: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	defer func() {
		if removeErr := unix.Unlinkat(int(directory.Fd()), name, 0); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return errors.Join(err, file.Close())
	}
	if _, err := file.Write(raw); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if onlyNew {
		err = unix.Linkat(int(directory.Fd()), name, int(directory.Fd()), FileName, 0)
	} else {
		err = unix.Renameat(int(directory.Fd()), name, int(directory.Fd()), FileName)
	}
	if err != nil {
		return fmt.Errorf("publish host configuration: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("host configuration published but durability is unconfirmed; reread before retrying: %w", err)
	}
	return nil
}
