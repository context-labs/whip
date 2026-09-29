// Package hostview provides bounded human host metadata without session authority.
package hostview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

type DirectoryParams struct {
	Path       string
	After      string
	Prefix     string
	ShowHidden bool
	Limit      int
}
type DirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type DirectoryResult struct {
	Path      string           `json:"path"`
	Parent    string           `json:"parent"`
	Entries   []DirectoryEntry `json:"entries"`
	NextAfter *string          `json:"next_after"`
	HasMore   bool             `json:"has_more"`
	Truncated bool             `json:"truncated"`
}

func invalid(message string) error { return fmt.Errorf("%w: %s", session.ErrInvalid, message) }

// Directories inspects at most 20,000 entries. Beyond that bound the sorted
// subset is explicitly incomplete; callers can choose a narrower directory.
func Directories(ctx context.Context, p DirectoryParams) (DirectoryResult, error) {
	result := DirectoryResult{Entries: []DirectoryEntry{}}
	if p.Limit < 1 || p.Limit > 128 || len(p.Path) > 4096 || len(p.Prefix) > 256 || len(p.After) > 256 || !utf8.ValidString(p.Path+p.Prefix+p.After) || strings.ContainsRune(p.Path, 0) || strings.ContainsAny(p.Prefix+p.After, `/\`+"\x00") {
		return result, invalid("directories require limit 1..128, path up to 4096 bytes, and filename filters up to 256 bytes")
	}
	path := p.Path
	if path == "" || path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return result, err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		if p.Path == "" || p.Path == "~" {
			path = home
		}
	}
	if !filepath.IsAbs(path) {
		return result, invalid("directory path must be absolute or start with ~/")
	}
	result.Path, result.Parent = filepath.Clean(path), filepath.Dir(filepath.Clean(path))
	//nolint:gosec // The human explicitly selects this host directory; no file bodies or guest authority are exposed.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return result, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return result, err
	}
	if !info.IsDir() {
		return result, invalid("selected path is not a directory")
	}
	entries := []fs.DirEntry{}
	for len(entries) < 20000 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		batch, err := file.ReadDir(min(128, 20000-len(entries)))
		entries = append(entries, batch...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, err
		}
	}
	result.Truncated = len(entries) == 20000
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	bytes := len(result.Path) + len(result.Parent)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !utf8.ValidString(entry.Name()) {
			result.Truncated = true
			continue
		}
		if entry.Name() <= p.After || !strings.HasPrefix(entry.Name(), p.Prefix) || !p.ShowHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		child := filepath.Join(result.Path, entry.Name())
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(child); err == nil {
				isDir = info.IsDir()
			}
		}
		if !isDir {
			continue
		}
		// JSON may escape every input byte; reserve worst-case encoded space.
		bytes += 6*(len(child)+len(entry.Name())) + 128
		if len(result.Entries) == p.Limit || bytes > 384<<10 {
			result.HasMore = true
			break
		}
		result.Entries = append(result.Entries, DirectoryEntry{Name: entry.Name(), Path: child})
	}
	if result.HasMore && len(result.Entries) > 0 {
		result.NextAfter = new(result.Entries[len(result.Entries)-1].Name)
	}
	return result, nil
}
