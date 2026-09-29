package instruction

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"github.com/context-labs/whip/internal/session"
)

var ErrStandingChanged = errors.New("standing instructions changed; read before editing again")

// StandingFile contains the raw published source, including comments. Revision
// binds those bytes to the actual parent and file identity without exposing them.
type StandingFile struct {
	Revision string
	Text     string
}

func ReadStandingFile(ctx context.Context, path string) (StandingFile, error) {
	directory, err := openStandingDirectory(ctx, path)
	if err != nil {
		return StandingFile{}, err
	}
	defer directory.Close()
	value, _, err := readStandingFile(ctx, directory, filepath.Base(path))
	return value, err
}

// WriteStandingFile compares the raw revision and publishes by atomic rename.
// Callers must serialize their writers. External editors are not frozen by this
// cooperative CAS; replacements observed before publication fail closed.
func WriteStandingFile(ctx context.Context, path, expected, text string) (StandingFile, error) {
	if len(expected) != 64 {
		return StandingFile{}, fmt.Errorf("%w: read standing instructions before editing", session.ErrInvalid)
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return StandingFile{}, fmt.Errorf("%w: invalid standing instruction revision", session.ErrInvalid)
	}
	if err := validateStandingText(text); err != nil {
		return StandingFile{}, err
	}
	directory, err := openStandingDirectory(ctx, path)
	if err != nil {
		return StandingFile{}, err
	}
	defer directory.Close()
	return writeStandingFile(ctx, directory, path, expected, text, func(file *os.File) error { return file.Sync() })
}

func writeStandingFile(ctx context.Context, directory *os.File, path, expected, text string, syncDirectory func(*os.File) error) (StandingFile, error) {
	name := filepath.Base(path)
	before, mode, err := readStandingFile(ctx, directory, name)
	if err != nil {
		return StandingFile{}, err
	}
	if before.Revision != expected {
		return StandingFile{}, ErrStandingChanged
	}
	if err := standingDirectoryUnchanged(ctx, directory, path); err != nil {
		return StandingFile{}, err
	}
	if before.Text == text {
		if err := syncDirectory(directory); err != nil {
			return StandingFile{}, fmt.Errorf("standing instruction durability is uncertain; read before another edit: %w", withoutFilesystemPath(err))
		}
		return before, nil
	}
	temporary := ".standing-" + rand.Text()
	fd, err := unix.Openat(int(directory.Fd()), temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return StandingFile{}, fmt.Errorf("create standing instruction edit: %w", err)
	}
	file := os.NewFile(uintptr(fd), temporary)
	defer func() { _ = file.Close(); _ = unix.Unlinkat(int(directory.Fd()), temporary, 0) }()
	if _, err = io.WriteString(file, text); err == nil {
		err = file.Chmod(mode.Perm())
	}
	if err == nil {
		err = file.Sync()
	}
	if err == nil {
		err = file.Close()
	}
	if err != nil {
		return StandingFile{}, fmt.Errorf("prepare standing instruction edit: %w", withoutFilesystemPath(err))
	}
	current, _, err := readStandingFile(ctx, directory, name)
	if err != nil {
		return StandingFile{}, err
	}
	if current.Revision != expected {
		return StandingFile{}, ErrStandingChanged
	}
	if err := standingDirectoryUnchanged(ctx, directory, path); err != nil {
		return StandingFile{}, err
	}
	if err := unix.Renameat(int(directory.Fd()), temporary, int(directory.Fd()), name); err != nil {
		return StandingFile{}, fmt.Errorf("publish standing instruction edit: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return StandingFile{}, fmt.Errorf("standing instruction edit may be visible; read before another edit: %w", withoutFilesystemPath(err))
	}
	// Publication has happened; caller cancellation no longer makes its result
	// ambiguous. This final bounded read reports the source actually visible.
	value, _, err := readStandingFile(context.WithoutCancel(ctx), directory, name)
	return value, err
}

func openStandingDirectory(ctx context.Context, path string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(path) > 4096 || !utf8.ValidString(path) || strings.ContainsRune(path, 0) || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." || filepath.Base(path) == string(filepath.Separator) {
		return nil, fmt.Errorf("%w: explicit standing instruction file required", session.ErrInvalid)
	}
	fd, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open published standing instruction directory: %w", err)
	}
	directory := os.NewFile(uintptr(fd), ".")
	info, err := directory.Stat()
	if err == nil && !safeStandingOwner(info) {
		err = fmt.Errorf("%w: standing instruction directory must be owned and not writable by other users", session.ErrInvalid)
	}
	if err != nil {
		_ = directory.Close()
		return nil, withoutFilesystemPath(err)
	}
	return directory, nil
}

func safeStandingOwner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(stat.Uid) == int64(os.Getuid()) && info.Mode().Perm()&0o022 == 0
}

func standingDirectoryUnchanged(ctx context.Context, directory *os.File, path string) error {
	current, err := openStandingDirectory(ctx, path)
	if err != nil {
		return err
	}
	defer current.Close()
	before, err := directory.Stat()
	if err != nil {
		return withoutFilesystemPath(err)
	}
	after, err := current.Stat()
	if err != nil {
		return withoutFilesystemPath(err)
	}
	if !os.SameFile(before, after) {
		return ErrStandingChanged
	}
	return ctx.Err()
}

func readStandingFile(ctx context.Context, directory *os.File, name string) (StandingFile, os.FileMode, error) {
	if err := ctx.Err(); err != nil {
		return StandingFile{}, 0, err
	}
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return StandingFile{}, 0, fmt.Errorf("read published standing instructions: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return StandingFile{}, 0, withoutFilesystemPath(err)
	}
	if !before.Mode().IsRegular() || !safeStandingOwner(before) {
		return StandingFile{}, 0, fmt.Errorf("%w: standing instructions must be an owned regular file, not writable by other users", session.ErrInvalid)
	}
	raw, err := readComplete(file, before, session.MaxInstructionSourceBytes)
	if err != nil {
		return StandingFile{}, 0, withoutFilesystemPath(err)
	}
	after, err := file.Stat()
	if err != nil {
		return StandingFile{}, 0, withoutFilesystemPath(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		return StandingFile{}, 0, ErrStandingChanged
	}
	text := string(raw)
	if err := validateStandingText(text); err != nil {
		return StandingFile{}, 0, err
	}
	parent, err := directory.Stat()
	if err != nil {
		return StandingFile{}, 0, withoutFilesystemPath(err)
	}
	p, f := parent.Sys().(*syscall.Stat_t), before.Sys().(*syscall.Stat_t)
	revision := sha256.Sum256(fmt.Appendf(nil, "%d:%d:%d:%d:%d:%d:%x", p.Dev, p.Ino, f.Dev, f.Ino, before.ModTime().UnixNano(), before.Mode(), sha256.Sum256(raw)))
	if err := ctx.Err(); err != nil {
		return StandingFile{}, 0, err
	}
	return StandingFile{Revision: hex.EncodeToString(revision[:]), Text: text}, before.Mode(), nil
}

func validateStandingText(text string) error {
	if len(text) > session.MaxInstructionSourceBytes || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return fmt.Errorf("%w: standing instructions require UTF-8 without NUL, at most 65536 bytes", session.ErrInvalid)
	}
	return nil
}
