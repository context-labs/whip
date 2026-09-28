// Package instruction assembles turn-local instructions from captured policy
// and already-authorized filesystem roots. It owns neither authority nor files.
package instruction

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/skills"
)

const (
	maxSourceBytes = session.MaxInstructionSourceBytes
	maxSkills      = 1024
	maxEntries     = 8192
	skillDirectory = ".agents/skills"
)

// Root is an authorized, caller-owned directory. An empty ID identifies the
// workspace; named roots contain immediate skill directories.
type Root struct {
	ID string
	FS *os.Root
}

type Snapshot struct {
	Text     string
	Sources  []session.InstructionSource
	Selected []Skill
}

// Load reads only through caller-owned roots. No roots means no filesystem
// reads. Project files belong to the workspace; skills follow host root order,
// then workspace precedence. Missing optional sources are omitted.
func Load(ctx context.Context, roots []Root, policy session.Instructions, invoked []string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if err := policy.Validate(); err != nil {
		return Snapshot{}, err
	}
	roots, err := orderedRoots(roots)
	if err != nil {
		return Snapshot{}, err
	}
	var text strings.Builder
	if err := appendText(&text, policy.Text); err != nil {
		return Snapshot{}, err
	}
	var sources []session.InstructionSource
	if len(roots) == 0 {
		return Snapshot{Text: text.String()}, nil
	}
	var workspace *os.Root
	if roots[len(roots)-1].ID == "" {
		workspace = roots[len(roots)-1].FS
	}
	for _, path := range policy.ProjectFiles {
		if workspace == nil {
			break
		}
		data, found, err := readSource(ctx, workspace, path, false)
		if err != nil {
			return Snapshot{}, err
		}
		if !found {
			continue
		}
		if !utf8.Valid(data) || slices.Contains(data, byte(0)) {
			return Snapshot{}, fmt.Errorf("project instructions %s must be UTF-8 without NUL", path)
		}
		framed := "\n\n--- Project instructions: " + strconv.Quote(path) + " ---\n" + string(data)
		if err := appendText(&text, framed); err != nil {
			return Snapshot{}, err
		}
		sources = append(sources, source("project_file", "", path, data))
	}
	var selected []Skill
	if policy.DiscoverSkills || len(invoked) > 0 {
		catalog, err := Catalog(ctx, roots)
		if err != nil {
			return Snapshot{}, err
		}
		if policy.DiscoverSkills {
			if err := appendCatalog(&text, catalog.Skills); err != nil {
				return Snapshot{}, err
			}
		}
		sources = append(sources, catalog.Sources...)
		byName := make(map[string]Skill, len(catalog.Skills))
		for _, skill := range catalog.Skills {
			byName[skill.Name] = skill
		}
		for _, name := range invoked {
			if skill, ok := byName[name]; ok {
				selected = append(selected, skill)
				delete(byName, name)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Text: text.String(), Sources: sources, Selected: selected}, nil
}

func appendText(text *strings.Builder, value string) error {
	if len(value) > session.MaxInstructionBytes-text.Len() {
		return errors.New("composed instructions exceed 1 MiB")
	}
	text.WriteString(value)
	return nil
}

func source(kind, rootID, path string, data []byte) session.InstructionSource {
	hash := sha256.Sum256(data)
	result := session.InstructionSource{Kind: kind, Scope: "workspace", Path: filepath.ToSlash(path), Bytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}
	if rootID != "" {
		result.Scope = "host"
		result.RootID = new(rootID)
	}
	return result
}

// Nonblocking open prevents a substituted FIFO from hanging before descriptor
// validation. Root confines symlink resolution; Stat validates the opened file,
// never a path that could be replaced between validation and use.
func readSource(ctx context.Context, root *os.Root, path string, metadata bool) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	file, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if optionalMissing(root, path, err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open instruction source %s: %w", path, withoutFilesystemPath(err))
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, false, withoutFilesystemPath(err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("instruction source %s must be a regular file", path)
	}
	var data []byte
	if metadata {
		data, err = readMetadata(ctx, file)
	} else {
		data, err = readComplete(file, info, maxSourceBytes)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return nil, false, fmt.Errorf("read instruction source %s: %w", path, err)
	}
	return data, true, nil
}

func readComplete(file *os.File, before fs.FileInfo, limit int64) ([]byte, error) {
	if before.Size() > limit {
		return nil, fmt.Errorf("instruction source exceeds %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, withoutFilesystemPath(err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("instruction source exceeds %d bytes", limit)
	}
	after, err := file.Stat()
	if err != nil {
		return nil, withoutFilesystemPath(err)
	}
	if after.Size() != before.Size() || int64(len(data)) != before.Size() {
		return nil, errors.New("instruction source size changed while reading")
	}
	return data, nil
}

func readMetadata(ctx context.Context, reader io.Reader) ([]byte, error) {
	lines := bufio.NewReader(io.LimitReader(reader, maxSourceBytes+1))
	var data []byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, err := lines.ReadString('\n')
		if len(line) > maxSourceBytes-len(data) {
			return nil, errors.New("skill metadata exceeds 64 KiB")
		}
		first := len(data) == 0
		data = append(data, line...)
		if first && strings.TrimRight(line, "\r\n") != "---" {
			return nil, errors.New("skill metadata has no frontmatter")
		}
		if !first && strings.TrimRight(line, "\r\n") == "---" {
			return data, nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("skill metadata has no closing delimiter")
			}
			return nil, withoutFilesystemPath(err)
		}
	}
}

type catalogBounds struct {
	entries, skills int
}

func loadSkills(ctx context.Context, root Root, bounds *catalogBounds) ([]skills.Skill, []session.InstructionSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	directoryPath := skillDirectory
	if root.ID != "" {
		directoryPath = "."
	}
	directory, err := root.FS.OpenFile(directoryPath, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK, 0)
	if optionalMissing(root.FS, directoryPath, err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("open skill directory: %w", withoutFilesystemPath(err))
	}
	defer func() { _ = directory.Close() }()
	var names []string
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		entries, err := directory.ReadDir(128)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, nil, withoutFilesystemPath(err)
		}
		bounds.entries += len(entries)
		if bounds.entries > maxEntries {
			return nil, nil, errors.New("skill catalog exceeds 8192 directory entries")
		}
		for _, entry := range entries {
			// Preserve immediate-directory discovery; symlink directories are
			// not catalog roots. SKILL.md itself may resolve within root.
			if entry.IsDir() {
				names = append(names, entry.Name())
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	slices.Sort(names)
	var catalog []skills.Skill
	var sources []session.InstructionSource
	for _, name := range names {
		path := filepath.Join(directoryPath, name, "SKILL.md")
		data, found, err := readSource(ctx, root.FS, path, true)
		if err != nil {
			return nil, nil, err
		}
		if !found {
			continue
		}
		if bounds.skills >= maxSkills {
			return nil, nil, errors.New("skill catalog exceeds 1024 skills")
		}
		bounds.skills++
		skill, err := skills.ParsePromptMetadata(filepath.ToSlash(path), data)
		if err != nil {
			return nil, nil, err
		}
		catalog = append(catalog, skill)
		metadata := source("skill_metadata", root.ID, path, data)
		if err := metadata.Validate(); err != nil {
			return nil, nil, err
		}
		sources = append(sources, metadata)
	}
	return catalog, sources, nil
}

// An absent optional path is different from a present dangling final symlink.
// This diagnostic runs only after OpenFile failed; it never authorizes a later
// open or substitutes path metadata for the opened descriptor's type check.
func optionalMissing(root *os.Root, path string, err error) bool {
	if !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	_, statErr := root.Lstat(path)
	return errors.Is(statErr, fs.ErrNotExist)
}

// Files opened through os.Root can retain its absolute name in descriptor
// errors. Expose the operation's relative source context and underlying cause.
func withoutFilesystemPath(err error) error {
	if pathError, ok := errors.AsType[*fs.PathError](err); ok {
		return pathError.Err
	}
	return err
}
