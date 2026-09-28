package instruction

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"github.com/context-labs/whip/internal/session"
)

// LoadStanding reads one explicit file through its authorized, caller-owned
// parent directory. The audit covers the complete file, including comments and
// blank lines omitted from its instruction text. A missing file is an error.
func LoadStanding(ctx context.Context, root *os.Root, name string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if root == nil {
		return Snapshot{}, errors.New("standing instructions require an authorized root")
	}
	if err := session.ValidateText(name, 4096); err != nil {
		return Snapshot{}, err
	}
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, `/\`) {
		return Snapshot{}, fmt.Errorf("%w: standing instructions require a file basename", session.ErrInvalid)
	}
	// Root.OpenFile resolves symlinks internally even with O_NOFOLLOW. Open
	// the validated basename directly against its anchored parent descriptor.
	directory, err := root.Open(".")
	if err != nil {
		return Snapshot{}, fmt.Errorf("open standing instruction directory: %w", withoutFilesystemPath(err))
	}
	defer func() { _ = directory.Close() }()
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open standing instructions %s: %w", name, withoutFilesystemPath(err))
	}
	file := os.NewFile(uintptr(fd), name)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return Snapshot{}, withoutFilesystemPath(err)
	}
	if !info.Mode().IsRegular() {
		return Snapshot{}, errors.New("standing instructions must be a regular file")
	}
	data, err := readComplete(file, info, session.MaxInstructionSourceBytes)
	if err != nil {
		return Snapshot{}, err
	}
	if !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
		return Snapshot{}, errors.New("standing instructions must be UTF-8 without NUL")
	}
	var text strings.Builder
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(line)
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Text: text.String(), Sources: []session.InstructionSource{source("standing_instructions", "host", "standing", name, data)}}, nil
}
