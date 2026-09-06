package daemon

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
	"sync"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/skills"
)

type CompletionParams = protocol.CompletionParams
type CompletionResult = protocol.CompletionResult

var completionIndex struct {
	sync.Mutex
	root      string
	files     []string
	truncated bool
	built     time.Time
}

func (s *Server) completeWorkspace(ctx context.Context, p CompletionParams) (CompletionResult, error) {
	if p.RootID == "" || p.Limit < 1 || p.Limit > 64 || len(p.Prefix) > 4096 {
		return CompletionResult{}, errors.New("completion requires root, prefix up to 4096 bytes and limit 1..64")
	}
	id := p.AgentID
	if id == "" {
		id = p.RootID
	}
	agent, err := s.daemon.store.LoadAgent(ctx, p.RootID, id)
	if err != nil {
		return CompletionResult{}, err
	}
	return completeAt(ctx, agent.CWD, p)
}

func completeAt(ctx context.Context, root string, p CompletionParams) (CompletionResult, error) {
	result := CompletionResult{Candidates: []protocol.CompletionCandidate{}}
	add := func(text, description string) {
		if len(result.Candidates) >= p.Limit {
			result.Truncated = true
			return
		}
		result.Candidates = append(result.Candidates, protocol.CompletionCandidate{Text: text, Description: description})
	}
	switch p.Kind {
	case "skill":
		roster, problems := skills.ScanDetailed(skills.DirsFor(root)...)
		warn := func(message string) {
			if len(result.Warnings) >= 16 {
				result.Truncated = true
				return
			}
			result.Warnings = append(result.Warnings, string([]rune(message)[:min(512, len([]rune(message)))]))
		}
		for _, problem := range problems {
			warn(fmt.Sprintf("%s: %s", problem.Path, problem.Err))
		}
		for _, skill := range roster {
			if skill.Warning != "" {
				warn(skill.Name + ": " + skill.Warning)
			}
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if strings.HasPrefix(skill.Name, p.Prefix) {
				add("$"+skill.Name, string([]rune(skill.Description)[:min(80, len([]rune(skill.Description)))]))
			}
		}
	case "mention":
		if p.Prefix == "" || strings.ContainsAny(p.Prefix, "/\\") || strings.HasPrefix(p.Prefix, ".") || strings.HasPrefix(p.Prefix, "~") {
			return completePaths(ctx, root, p, true)
		}
		files, truncated, err := completionFileList(ctx, root)
		if err != nil {
			return result, err
		}
		result.Truncated = truncated
		type match struct {
			path string
			rank int
		}
		matches := []match{}
		for _, path := range files {
			if rank := completionRank(path, strings.ToLower(p.Prefix)); rank >= 0 {
				matches = append(matches, match{path: path, rank: rank})
			}
		}
		slices.SortFunc(matches, func(a, b match) int {
			if a.rank != b.rank {
				return a.rank - b.rank
			}
			return strings.Compare(a.path, b.path)
		})
		for _, match := range matches {
			add("@"+match.path, "")
		}
	case "path":
		return completePaths(ctx, root, p, false)
	default:
		return result, errors.New("unsupported completion kind")
	}
	return result, nil
}

func completePaths(ctx context.Context, root string, p CompletionParams, mention bool) (CompletionResult, error) {
	result := CompletionResult{Candidates: []protocol.CompletionCandidate{}}
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
	// filepath.Join removes a final slash; preserve the user's request to list
	// the directory's children rather than its siblings.
	if p.Prefix == "" || strings.HasSuffix(p.Prefix, "/") {
		prefix += string(filepath.Separator)
	}
	directory, base := filepath.Split(prefix)
	file, err := os.Open(directory)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer func() { _ = file.Close() }()
	entries := []fs.DirEntry{}
	for len(entries) < 20000 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		batch, err := file.ReadDir(128)
		entries = append(entries, batch...)
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, err
		}
	}
	if len(entries) >= 20000 {
		result.Truncated = true
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, entry := range entries {
		if matched, _ := filepath.Match(base+"*", entry.Name()); !matched {
			continue
		}
		if len(result.Candidates) == p.Limit {
			result.Truncated = true
			break
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
		if mention {
			text = "@" + text
		}
		result.Candidates = append(result.Candidates, protocol.CompletionCandidate{Text: text, Description: description})
	}
	return result, nil
}

func completionFileList(ctx context.Context, root string) ([]string, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	completionIndex.Lock()
	if completionIndex.root == root && time.Since(completionIndex.built) < 2*time.Second {
		files, truncated := slices.Clone(completionIndex.files), completionIndex.truncated
		completionIndex.Unlock()
		return files, truncated, nil
	}
	completionIndex.Unlock()
	files := []string{}
	stack := []string{root}
	scanned, bytes := 0, 0
	truncated := false
	deadline := time.Now().Add(2 * time.Second)
	for len(stack) > 0 && !truncated {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		directory := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		file, err := os.Open(directory)
		if err != nil {
			continue
		}
		for !truncated {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return nil, false, err
			}
			entries, readErr := file.ReadDir(128)
			for _, entry := range entries {
				scanned++
				if scanned > 50000 || len(files) >= 20000 || bytes > 4<<20 || time.Now().After(deadline) {
					truncated = true
					break
				}
				path := filepath.Join(directory, entry.Name())
				if entry.IsDir() {
					if !strings.HasPrefix(entry.Name(), ".") && entry.Name() != "vendor" && entry.Name() != "node_modules" {
						stack = append(stack, path)
					}
				} else if relative, err := filepath.Rel(root, path); err == nil {
					files = append(files, filepath.ToSlash(relative))
					bytes += len(relative)
				}
			}
			if readErr != nil {
				break
			}
		}
		_ = file.Close()
	}
	slices.Sort(files)
	completionIndex.Lock()
	completionIndex.root, completionIndex.files, completionIndex.truncated, completionIndex.built = root, files, truncated, time.Now()
	completionIndex.Unlock()
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
