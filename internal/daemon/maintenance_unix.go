//go:build unix

package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

var ErrMaintenance = errors.New("whip is applying a backend update; retry after it finishes")

func maintenanceFile(paths RuntimePaths) (*os.File, error) {
	name := filepath.Join(paths.Runtime, "maintenance.lock")
	fd, err := unix.Open(name, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		_ = file.Close()
		return nil, errors.New("invalid backend maintenance lock")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Getuid() {
		_ = file.Close()
		return nil, errors.New("backend maintenance lock has a different owner")
	}
	return file, nil
}

// AcquireMaintenance excludes daemon starts and other updaters until Close.
// Never unlink this file: every participant must lock the same inode.
func AcquireMaintenance(paths RuntimePaths) (*os.File, error) {
	file, err := maintenanceFile(paths)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errors.Join(ErrMaintenance, err)
	}
	return file, nil
}

// AcquireStartup fences owner acquisition. Ordinary starts fail rather than
// waiting with an old executable mapped. An updater may pass its held lock to
// the replacement child; Close must not explicitly unlock that shared descriptor.
func AcquireStartup(paths RuntimePaths, inheritedFD int) (*os.File, error) {
	file, err := maintenanceFile(paths)
	if err != nil {
		return nil, err
	}
	if inheritedFD == 0 {
		if err := unix.Flock(int(file.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
			_ = file.Close()
			return nil, errors.Join(ErrMaintenance, err)
		}
		return file, nil
	}
	defer func() { _ = file.Close() }()
	if inheritedFD != 3 {
		return nil, errors.New("invalid inherited maintenance descriptor")
	}
	if file.Fd() == uintptr(inheritedFD) {
		return nil, errors.New("inherited maintenance descriptor was not open")
	}
	inherited := os.NewFile(uintptr(inheritedFD), "inherited-maintenance")
	if err := validateInheritedMaintenance(file, inherited); err != nil {
		_ = inherited.Close()
		return nil, err
	}
	unix.CloseOnExec(inheritedFD)
	return inherited, nil
}

// validateInheritedMaintenance verifies both the inode and the shared open-file
// description. A separately opened descriptor to the same file is insufficient.
func validateInheritedMaintenance(file, inherited *os.File) error {
	actual, statErr := inherited.Stat()
	expected, expectedErr := file.Stat()
	if statErr != nil || expectedErr != nil || !os.SameFile(actual, expected) {
		return errors.New("inherited maintenance descriptor does not identify the runtime lock")
	}
	// The separately opened descriptor must conflict, while the inherited open
	// file description must already be able to retain the exclusive lock.
	if err := unix.Flock(int(file.Fd()), unix.LOCK_SH|unix.LOCK_NB); !errors.Is(err, unix.EWOULDBLOCK) {
		return errors.New("inherited maintenance lock is not held exclusively")
	}
	if err := unix.Flock(int(inherited.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("validate inherited maintenance ownership: %w", err)
	}
	return nil
}
