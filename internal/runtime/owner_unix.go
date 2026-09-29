//go:build unix

package runtime

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

type owner struct {
	file            *os.File
	socketDirectory string
}

func acquireOwner(directory, path string) (*owner, error) {
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !ok || int64(stat.Uid) != int64(os.Getuid()) {
		return nil, errors.New("runtime directory must be an owner-only directory")
	}
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err = file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	stat, ok = info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ok || int64(stat.Uid) != int64(os.Getuid()) {
		_ = file.Close()
		return nil, errors.New("runtime owner lock must be an owner-only regular file")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrOwned
		}
		return nil, err
	}
	return &owner{file: file}, nil
}

// Only the holder of the durable execution lock may prepare the fallback.
func (o *owner) prepareSocketDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !ok || int64(stat.Uid) != int64(os.Getuid()) {
		return errors.New("runtime socket directory must be owner-only and not a symlink")
	}
	o.socketDirectory = path
	return nil
}

func (o *owner) Close() error {
	var cleanup error
	if o.socketDirectory != "" {
		// Remove only an empty owned directory; never recursively delete socket
		// occupants or unrelated files. The RPC listener owns its socket's lifetime.
		cleanup = os.Remove(o.socketDirectory)
		if errors.Is(cleanup, os.ErrNotExist) || errors.Is(cleanup, syscall.ENOTEMPTY) {
			cleanup = nil
		}
	}
	return errors.Join(cleanup, unix.Flock(int(o.file.Fd()), unix.LOCK_UN), o.file.Close())
}
