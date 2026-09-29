package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/sys/unix"
)

// Local file links are a presentation observation, not file authority. A
// composition root must explicitly assert that the host shares this filesystem.
// Every frame resolves the current owner's cwd afresh; no mapping survives an
// owner/cwd change, symlink replacement, or detach. Clicking later is owned by
// the terminal and can observe later filesystem changes.
func nativeFileLinks(frame, directory string) string {
	if len(frame) > 1<<20 || !filepath.IsAbs(directory) || len(directory) > 4096 || !fileRefRE.MatchString(ansi.Strip(frame)) {
		return frame
	}
	root := nativeLinkRoot(directory)
	if root == nil {
		return frame
	}
	defer func() { _ = root.Close() }()
	remaining := 128
	var result strings.Builder
	linked := false
	for len(frame) > 0 {
		end := strings.IndexByte(frame, '\x1b')
		if end < 0 {
			end = len(frame)
		}
		if end > 0 {
			text := frame[:end]
			if !linked && remaining > 0 {
				text = replaceMatches(text, fileRefRE, func(match string, before byte) string {
					if remaining == 0 || strings.ContainsRune("([]/:;\"`", rune(before)) {
						return match
					}
					remaining--
					path, line := splitLineRef(match)
					if !isFileRef(path) || !nativeLinkFile(root, directory, path) {
						return match
					}
					return hyperlink(absFileURIAt(directory, path, line), match)
				})
			}
			result.WriteString(text)
			frame = frame[end:]
		}
		if len(frame) == 0 {
			break
		}
		sequence, _, count, _ := ansi.DecodeSequence(frame, 0, nil)
		if count == 0 {
			return result.String() + frame
		}
		if strings.HasPrefix(sequence, "\x1b]8;") {
			body := strings.TrimSuffix(strings.TrimSuffix(sequence[len("\x1b]8;"):], "\a"), "\x1b\\")
			_, destination, _ := strings.Cut(body, ";")
			linked = destination != ""
		}
		result.WriteString(sequence)
		frame = frame[count:]
	}
	return result.String()
}

func nativeLinkRoot(directory string) *os.File {
	if filepath.Clean(directory) != directory {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(directory, "/"), "/")
	if len(parts) > 64 {
		return nil
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil
	}
	for _, part := range parts {
		if part == "" {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if err != nil {
			return nil
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), directory)
}

func nativeLinkFile(root *os.File, directory, path string) bool {
	if len(path) > 4096 {
		return false
	}
	if filepath.IsAbs(path) {
		var err error
		path, err = filepath.Rel(directory, path)
		if err != nil {
			return false
		}
	}
	if !filepath.IsLocal(path) {
		return false
	}
	parts := strings.Split(path, "/")
	if len(parts) > 64 {
		return false
	}
	fd := int(root.Fd())
	owned := false
	defer func() {
		if owned {
			_ = unix.Close(fd)
		}
	}()
	for index, part := range parts {
		if part == "." {
			continue
		}
		if part == ".." || part == "" {
			return false
		}
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
		if index < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, err := unix.Openat(fd, part, flags, 0)
		if err != nil {
			return false
		}
		if owned {
			_ = unix.Close(fd)
		}
		fd, owned = next, true
	}
	var info unix.Stat_t
	return owned && unix.Fstat(fd, &info) == nil && info.Mode&unix.S_IFMT == unix.S_IFREG
}
