//go:build unix

package runtime

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

type owner struct{ file *os.File }

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

func (o *owner) Close() error {
	return errors.Join(unix.Flock(int(o.file.Fd()), unix.LOCK_UN), o.file.Close())
}
