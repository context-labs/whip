package hostview

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	completionScanEntries = 50000
	completionPathCount   = 20000
	completionPathBytes   = 4 << 20
	completionOutputBytes = 256 << 10
	completionTextBytes   = 8192
)

type CompletionParams struct {
	Kind   string
	Prefix string
	Limit  int
}
type CompletionCandidate struct {
	Text        string `json:"text"`
	Description string `json:"description"`
}
type CompletionResult struct {
	WorkingDirectory string                `json:"working_directory"`
	Candidates       []CompletionCandidate `json:"candidates"`
	Truncated        bool                  `json:"truncated"`
}

// CompleteWorkspace reads names for an explicit human request. It grants no
// guest access and never reads file bodies. Relative paths use this exact cwd;
// explicit absolute and ~/ paths intentionally inspect other host directories.
// The scan checks cancellation/deadlines between filesystem calls; a stalled OS
// filesystem call cannot be interrupted by abandoning an unjoined goroutine.
func CompleteWorkspace(ctx context.Context, cwd string, p CompletionParams) (CompletionResult, error) {
	result := CompletionResult{WorkingDirectory: cwd, Candidates: []CompletionCandidate{}}
	if p.Limit < 1 || p.Limit > 64 || len(p.Prefix) > 4096 || !utf8.ValidString(p.Prefix) || strings.ContainsRune(p.Prefix, 0) || !filepath.IsAbs(cwd) || len(cwd) > 4096 || !utf8.ValidString(cwd) || strings.ContainsRune(cwd, 0) || p.Kind != "mention" && p.Kind != "path" {
		return result, invalid("completion requires an absolute workspace, mention or path kind, prefix up to 4096 bytes and limit 1..64")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	deadline := time.Now().Add(2 * time.Second)
	if p.Kind == "path" || p.Prefix == "" || strings.ContainsAny(p.Prefix, "/\\") || strings.HasPrefix(p.Prefix, ".") || strings.HasPrefix(p.Prefix, "~") {
		return completePaths(ctx, cwd, p, deadline)
	}
	files, truncated, err := completionFileList(ctx, cwd, deadline)
	if err != nil {
		return result, err
	}
	result.Truncated = truncated
	type match struct {
		path string
		rank int
	}
	matches := []match{}
	query := strings.ToLower(p.Prefix)
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if time.Now().After(deadline) {
			result.Truncated = true
			break
		}
		if rank := completionRank(path, query); rank >= 0 {
			matches = append(matches, match{path, rank})
		}
	}
	slices.SortFunc(matches, func(a, b match) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		return strings.Compare(a.path, b.path)
	})
	bytes := 6*len(cwd) + 128
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !addCompletion(&result, p.Limit, &bytes, "@"+match.path, "") {
			break
		}
	}
	return result, ctx.Err()
}

func addCompletion(result *CompletionResult, limit int, bytes *int, text, description string) bool {
	// Reserve worst-case JSON escaping, including enclosing field syntax.
	size := 6*(len(text)+len(description)) + 128
	if len(text) > completionTextBytes || !utf8.ValidString(text) {
		result.Truncated = true
		return true
	}
	if len(result.Candidates) == limit || *bytes+size > completionOutputBytes {
		result.Truncated = true
		return false
	}
	*bytes += size
	result.Candidates = append(result.Candidates, CompletionCandidate{text, description})
	return true
}

func openCompletionDirectory(path string) (*os.File, error) {
	//nolint:gosec // Explicit human path metadata; nonblocking open also rejects FIFO/non-directory inputs without reading bodies.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.IsDir() {
		_ = file.Close()
		return nil, invalid("completion path is not a directory")
	}
	return file, nil
}

func completePaths(ctx context.Context, root string, p CompletionParams, deadline time.Time) (CompletionResult, error) {
	result := CompletionResult{WorkingDirectory: root, Candidates: []CompletionCandidate{}}
	prefix := p.Prefix
	absolute := filepath.IsAbs(prefix) || strings.HasPrefix(prefix, "~")
	if prefix == "~" || strings.HasPrefix(prefix, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return result, err
		}
		prefix = home + prefix[1:]
	}
	if !filepath.IsAbs(prefix) {
		prefix = filepath.Join(root, prefix)
	}
	// Preserve the requested trailing slash after filepath.Join cleans it.
	if p.Prefix == "" || strings.HasSuffix(p.Prefix, "/") {
		prefix += string(filepath.Separator)
	}
	directory, base := filepath.Split(prefix)
	file, err := openCompletionDirectory(directory)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer func() { _ = file.Close() }()
	entries := []fs.DirEntry{}
	for len(entries) < completionPathCount {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if time.Now().After(deadline) {
			result.Truncated = true
			break
		}
		batch, err := file.ReadDir(min(128, completionPathCount-len(entries)))
		entries = append(entries, batch...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, err
		}
	}
	if len(entries) == completionPathCount {
		result.Truncated = true
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	bytes := 6*len(root) + 128
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if time.Now().After(deadline) {
			result.Truncated = true
			break
		}
		if !utf8.ValidString(entry.Name()) {
			result.Truncated = true
			continue
		}
		if matched, _ := filepath.Match(base+"*", entry.Name()); !matched {
			continue
		}
		text := filepath.Join(directory, entry.Name())
		if !absolute {
			if relative, err := filepath.Rel(root, text); err == nil {
				text = relative
			}
		}
		text = filepath.ToSlash(text)
		description := ""
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(directory, entry.Name())); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			text += "/"
			description = "dir"
		}
		if p.Kind == "mention" {
			text = "@" + text
		}
		if !addCompletion(&result, p.Limit, &bytes, text, description) {
			break
		}
	}
	return result, ctx.Err()
}

func completionFileList(ctx context.Context, root string, deadline time.Time) ([]string, bool, error) {
	files := []string{}
	stack := []string{root}
	scanned, bytes := 0, len(root)
	truncated := false
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if time.Now().After(deadline) {
			return files, true, nil
		}
		directory := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		bytes -= len(directory)
		file, err := openCompletionDirectory(directory)
		if err != nil {
			if directory == root {
				return nil, false, err
			}
			truncated = true
			continue
		}
		stop := false
		for !stop {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return nil, false, err
			}
			if time.Now().After(deadline) {
				_ = file.Close()
				return files, true, nil
			}
			entries, readErr := file.ReadDir(128)
			for _, entry := range entries {
				scanned++
				if scanned > completionScanEntries || len(files) >= completionPathCount {
					_ = file.Close()
					return files, true, nil
				}
				if !utf8.ValidString(entry.Name()) {
					truncated = true
					continue
				}
				if entry.IsDir() && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "node_modules") {
					continue
				}
				path := filepath.Join(directory, entry.Name())
				if len(path) > 4096 {
					truncated = true
					continue
				}
				if entry.IsDir() {
					if bytes+len(path) > completionPathBytes {
						_ = file.Close()
						return files, true, nil
					}
					stack = append(stack, path)
					bytes += len(path)
				} else if relative, err := filepath.Rel(root, path); err == nil {
					if bytes+len(relative) > completionPathBytes {
						_ = file.Close()
						return files, true, nil
					}
					files = append(files, filepath.ToSlash(relative))
					bytes += len(relative)
				}
			}
			if readErr != nil {
				stop = true
				if !errors.Is(readErr, io.EOF) {
					truncated = true
				}
			}
		}
		_ = file.Close()
	}
	slices.Sort(files)
	return files, truncated, nil
}

func completionRank(path, query string) int {
	path = strings.ToLower(path)
	base := path[strings.LastIndexByte(path, '/')+1:]
	if strings.Contains(base, query) {
		return 0
	}
	if strings.Contains(path, query) {
		return 1
	}
	subsequence := func(value string) bool {
		for _, r := range query {
			index := strings.IndexRune(value, r)
			if index < 0 {
				return false
			}
			value = value[index+len(string(r)):]
		}
		return true
	}
	if subsequence(base) {
		return 2
	}
	if subsequence(path) {
		return 3
	}
	return -1
}
