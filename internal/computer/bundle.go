package computer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const maxBundleBytes = 128 << 20

var ErrBundledUnavailable = errors.New("bundled computer helper unavailable")

// BundledAvailable is an embedded-payload fact, without filesystem or process
// effects. Empty development builds do not search for another executable.
func BundledAvailable() bool { return len(helperBinary) > 0 && len(helperBinary) <= maxBundleBytes }

// PublishBundled installs the embedded signed bytes only for explicit human
// setup. It neither enables computer control nor starts or probes the helper.
// A publication error can follow rename; callers must inspect configuration
// before retrying. An unreferenced executable conveys no control authority.
func PublishBundled(ctx context.Context, directory string) (string, error) {
	return publishBundle(ctx, directory, helperBinary)
}

func publishBundle(ctx context.Context, directory string, payload []byte) (string, error) {
	if len(payload) == 0 || len(payload) > maxBundleBytes {
		return "", ErrBundledUnavailable
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return "", errors.New("computer helper publication requires an absolute runtime directory")
	}
	root, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = unix.Close(root) }()
	if err := privateBundleDirectory(root); err != nil {
		return "", err
	}
	if err := unix.Mkdirat(root, "bin", 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
		return "", err
	}
	bin, err := unix.Openat(root, "bin", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = unix.Close(bin) }()
	if err := privateBundleDirectory(bin); err != nil {
		return "", err
	}
	name := ".computer-" + rand.Text()
	fd, err := unix.Openat(bin, name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o700)
	if err != nil {
		return "", err
	}
	staged := os.NewFile(uintptr(fd), name)
	defer staged.Close()
	defer func() { _ = unix.Unlinkat(bin, name, 0) }()
	if _, err := staged.Write(payload); err != nil {
		return "", err
	}
	if err := staged.Sync(); err != nil {
		return "", err
	}
	// Verify the staged file before the atomic publication. ReadAt avoids a
	// second path lookup; the fixed bound also catches unexpected file growth.
	digest := sha256.New()
	if n, err := io.Copy(digest, io.NewSectionReader(staged, 0, int64(len(payload))+1)); err != nil || n != int64(len(payload)) {
		return "", errors.Join(errors.New("bundled computer helper size changed"), err)
	}
	expected := sha256.Sum256(payload)
	if string(digest.Sum(nil)) != string(expected[:]) {
		return "", errors.New("bundled computer helper verification failed")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := unix.Renameat(bin, name, bin, "whip-computer"); err != nil {
		return "", err
	}
	// Persist both the executable and a newly created bin directory.
	if err := unix.Fsync(bin); err != nil {
		return "", fmt.Errorf("sync bundled helper publication: %w", err)
	}
	if err := unix.Fsync(root); err != nil {
		return "", fmt.Errorf("sync bundled helper directory: %w", err)
	}
	return filepath.Join(directory, "bin", "whip-computer"), nil
}

func privateBundleDirectory(fd int) error {
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return err
	}
	if int64(info.Uid) != int64(os.Geteuid()) || info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Mode&0o077 != 0 {
		return errors.New("computer helper publication requires an owned private directory")
	}
	return nil
}
