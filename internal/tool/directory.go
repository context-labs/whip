package tool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unicode/utf8"
)

const (
	scanEntries = 10000
	scanBytes   = 8 << 20
	scanDepth   = 64
)

// Directory traversal opens one bounded batch at a time without following
// descendant symlinks. Files and directories may change during inspection.
func openDirectory(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

func (e *fileExecution) list(ctx context.Context) (any, error) {
	directory, err := openDirectory(e.root, e.relative)
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.Close() }()
	names := make([]string, 0, min(e.request.limit, 128))
	var reasons []string
	for len(names) < scanEntries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, readErr := directory.ReadDir(min(128, scanEntries-len(names)))
		for _, entry := range entries {
			name := strings.ToValidUTF8(entry.Name(), "�")
			if entry.IsDir() {
				name += "/"
			}
			names = append(names, name)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
		if len(names) == scanEntries {
			reasons = append(reasons, "directory inspection reached 10000 entries; names are sorted within that inspected portion")
		}
	}
	slices.Sort(names)
	if len(names) > e.request.limit {
		names = names[:e.request.limit]
		reasons = append(reasons, "directory result entry limit reached")
	}
	var output strings.Builder
	count := 0
	for _, name := range names {
		if output.Len()+len(name)+1 > outputBytes {
			reasons = append(reasons, "directory output reached 32768 bytes")
			break
		}
		output.WriteString(name)
		output.WriteByte('\n')
		count++
	}
	return map[string]any{"path": e.request.path, "output": output.String(), "entries": count, "truncated": len(reasons) > 0, "reason": strings.Join(reasons, "; ")}, nil
}

type fileSearch struct {
	root    *os.Root
	query   string
	limit   int
	entries int
	bytes   int
	matches int
	skipped int
	output  strings.Builder
	reasons []string
	stop    bool
}

func (s *fileSearch) truncate(reason string, stop bool) {
	if !slices.Contains(s.reasons, reason) {
		s.reasons = append(s.reasons, reason)
	}
	s.stop = s.stop || stop
}

func (e *fileExecution) search(ctx context.Context) (any, error) {
	info, err := e.root.Stat(e.relative)
	if err != nil {
		return nil, err
	}
	s := fileSearch{root: e.root, query: e.request.query, limit: e.request.limit}
	if info.IsDir() {
		root, err := e.root.OpenRoot(e.relative)
		if err != nil {
			return nil, err
		}
		defer func() { _ = root.Close() }()
		s.root = root
		if err := s.walk(ctx, ".", 0); err != nil {
			return nil, err
		}
	} else if info.Mode().IsRegular() {
		s.entries = 1
		if err := s.file(ctx, e.relative, "."); err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("search requires a directory or regular file")
	}
	return map[string]any{
		"path": e.request.path, "query": e.request.query, "output": s.output.String(), "matches": s.matches,
		"scanned_entries": s.entries, "scanned_bytes": s.bytes, "skipped_files": s.skipped,
		"truncated": len(s.reasons) > 0, "reason": strings.Join(s.reasons, "; "),
	}, nil
}

func (s *fileSearch) walk(ctx context.Context, path string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory, err := openDirectory(s.root, path)
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	for !s.stop {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, readErr := directory.ReadDir(128)
		for _, entry := range entries {
			if s.entries == scanEntries {
				s.truncate("search reached 10000 entries", true)
			}
			if s.stop {
				return nil
			}
			s.entries++
			child := filepath.Join(path, entry.Name())
			if entry.IsDir() {
				if entry.Name() == ".git" {
					continue
				}
				if depth == scanDepth {
					s.truncate("search reached 64 directory levels", false)
					continue
				}
				if err := s.walk(ctx, child, depth+1); err != nil {
					return err
				}
			} else if entry.Type().IsRegular() {
				if err := s.file(ctx, child, child); err != nil {
					return err
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

func (s *fileSearch) file(ctx context.Context, path, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.bytes == scanBytes {
		s.truncate("search reached 8388608 file bytes", true)
		return nil
	}
	file, err := s.root.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		s.skipped++
		s.truncate("unreadable or changed files were skipped", false)
		return nil
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		s.skipped++
		s.truncate("unreadable or changed files were skipped", false)
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(scanBytes-s.bytes)))
	s.bytes += len(data)
	if err != nil {
		s.skipped++
		s.truncate("unreadable or changed files were skipped", false)
		return nil
	}
	partial := int64(len(data)) < info.Size()
	if partial {
		s.truncate("search reached 8388608 file bytes", true)
		for trim := 1; trim < utf8.UTFMax && trim <= len(data) && !utf8.Valid(data); trim++ {
			boundary := len(data) - trim
			if !utf8.FullRune(data[boundary:]) && utf8.Valid(data[:boundary]) {
				data = data[:boundary]
				break
			}
		}
	}
	if !utf8.Valid(data) || slices.Contains(data, byte(0)) {
		s.skipped++
		s.truncate("non-text files were skipped", false)
		return nil
	}
	remaining := string(data)
	for line := 1; remaining != ""; line++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		text, rest, complete := strings.Cut(remaining, "\n")
		remaining = rest
		if strings.Contains(text, s.query) {
			match := fmt.Sprintf("%s:%d:%s\n", strings.ToValidUTF8(label, "�"), line, text)
			if len(match) > outputBytes-s.output.Len() {
				match = match[:outputBytes-s.output.Len()]
				for !utf8.ValidString(match) {
					match = match[:len(match)-1]
				}
				s.output.WriteString(match)
				s.matches++
				s.truncate("search output reached 32768 bytes; the last match is partial", true)
				return nil
			}
			s.output.WriteString(match)
			s.matches++
			if s.matches == s.limit {
				s.truncate("search match limit reached", true)
				return nil
			}
		}
		if !complete && partial {
			s.truncate("the final scanned line is partial", true)
		}
	}
	return nil
}
